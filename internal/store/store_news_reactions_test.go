package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// briefedStory sorts an item of bbc and one of ft into one story and marks
// them briefed by runID, as a briefing that offered the story does.
func briefedStory(t *testing.T, s *Store, bbc, ft core.Watch, runID string) int64 {
	t.Helper()
	ctx := context.Background()
	b := recordItems(t, s, bbc, "b1")[0]
	f := recordItems(t, s, ft, "f1")[0]
	batch, err := s.ClaimNewsBatch(ctx, watchTestNow, time.Minute, 10, false)
	if err != nil || len(batch.Items) != 2 {
		t.Fatalf("batch = %+v, %v", batch, err)
	}
	if err := s.CommitNewsBatch(ctx, batch.Token, core.NewsCommit{
		At:      watchTestNow,
		Stories: []core.NewsStoryChange{{Ref: "n1", Section: "国际", Title: "菲律宾央行加息"}},
		Scores:  []core.NewsItemScore{{ItemID: b.ID, Score: 4, Story: "n1"}, {ItemID: f.ID, Score: 3, Story: "n1"}},
	}); err != nil {
		t.Fatal(err)
	}
	news, err := s.BriefingNewsStories(ctx, 10)
	if err != nil || len(news.Stories) != 1 {
		t.Fatalf("briefing news = %+v, %v", news, err)
	}
	if err := s.MarkWatchItems(ctx, news.IDs, core.WatchItemBriefed, runID); err != nil {
		t.Fatal(err)
	}
	return news.Stories[0].ID
}

// briefingRun records a briefing run in a thread, started by a wake message,
// that finished with status and output.
func briefingRun(t *testing.T, s *Store, slot, threadID, runID string, status core.RunStatus, output string) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.LoadSession(ctx, threadID); errors.Is(err, core.ErrSessionNotFound) {
		if _, err := s.CreateSession(ctx, threadID, "Briefings"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ClaimRoutine(ctx, "briefing", slot, watchTestNow); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: runID, SessionID: threadID, Input: "[briefing " + slot + "]"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendSessionMessage(ctx, threadID, 0, core.MessageRoleWake, "[briefing "+slot+"]", runID); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun(ctx, runID, status, output, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoutineRun(ctx, "briefing", slot, threadID, runID); err != nil {
		t.Fatal(err)
	}
}

func TestBriefingPicksAreRecordedOnceAndReactionsOutliveThem(t *testing.T) {
	s, bbc, ft, _ := newsStoryFixture(t)
	ctx := context.Background()
	storyID := briefedStory(t, s, bbc, ft, "run_b")
	body := "## 国际\n1. **菲律宾央行加息**：加息 25 个基点。[BBC](https://bbc.test/b1)"
	briefingRun(t, s, "2026-10-03", "thread_b", "run_b", core.RunStatusSucceeded, body)
	briefingRun(t, s, "2026-10-02", "thread_b", "run_failed", core.RunStatusFailed, "")
	since := time.Now().Add(-time.Hour)

	unrecorded, err := s.UnrecordedBriefings(ctx, since)
	if err != nil || len(unrecorded) != 1 || unrecorded[0].RunID != "run_b" || unrecorded[0].Text != body || unrecorded[0].FinishedAt.IsZero() {
		t.Fatalf("unrecorded briefings = %+v, %v", unrecorded, err)
	}
	if later, err := s.UnrecordedBriefings(ctx, time.Now().Add(time.Hour)); err != nil || len(later) != 0 {
		t.Fatalf("briefings finished before since = %+v, %v", later, err)
	}
	stories, err := s.BriefedNewsStories(ctx, "run_b")
	if err != nil || len(stories) != 1 || stories[0].ID != storyID || len(stories[0].Items) != 2 {
		t.Fatalf("briefed stories = %+v, %v", stories, err)
	}
	if woken, err := s.RunWoken(ctx, "run_b"); err != nil || !woken {
		t.Fatalf("a briefing run is woken: %t, %v", woken, err)
	}

	picked := core.BriefingStory{StoryID: storyID, Section: "国际", Title: "菲律宾央行加息", Summary: "加息 25 个基点。", URL: "https://bbc.test/b1", Focus: true, Sources: []int64{bbc.ID, ft.ID}}
	if err := s.RecordBriefingPicks(ctx, core.BriefingPicks{RunID: "run_b", BriefedAt: unrecorded[0].FinishedAt, Stories: []core.BriefingStory{picked}, Unmatched: 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordBriefingPicks(ctx, core.BriefingPicks{RunID: "run_b", BriefedAt: unrecorded[0].FinishedAt}); err != nil {
		t.Fatalf("recording a briefing again: %v", err)
	}
	if got, err := s.BriefingStories(ctx, "run_b"); err != nil || !reflect.DeepEqual(got, []core.BriefingStory{picked}) {
		t.Fatalf("briefing stories = %+v, %v", got, err)
	}
	if unrecorded, err := s.UnrecordedBriefings(ctx, since); err != nil || len(unrecorded) != 0 {
		t.Fatalf("a recorded briefing is listed again: %+v, %v", unrecorded, err)
	}
	var unmatched int
	if err := s.db.QueryRowContext(ctx, `SELECT unmatched FROM briefing_picks WHERE run_id = 'run_b'`).Scan(&unmatched); err != nil || unmatched != 2 {
		t.Fatalf("recorded unmatched = %d, %v", unmatched, err)
	}

	if err := s.ReactToNewsStory(ctx, storyID, core.NewsReactionMore, "device_1", watchTestNow); err != nil {
		t.Fatal(err)
	}
	if err := s.ReactToNewsStory(ctx, storyID, core.NewsReactionLess, "device_2", watchTestNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.ReactToNewsStory(ctx, storyID+100, core.NewsReactionLess, "device_2", watchTestNow); !errors.Is(err, core.ErrNewsStoryNotFound) {
		t.Fatalf("reacting to a missing story err = %v", err)
	}
	if err := s.ReactToNewsStory(ctx, storyID, "like", "device_2", watchTestNow); err == nil {
		t.Fatal("a reaction other than more or less was stored")
	}
	want := core.StoryReaction{StoryID: storyID, Reaction: core.NewsReactionLess, Section: "国际", Title: "菲律宾央行加息", Sources: []int64{bbc.ID, ft.ID}, DeviceID: "device_2", At: watchTestNow.Add(time.Minute)}
	if got, err := s.NewsReactions(ctx, watchTestNow); err != nil || !reflect.DeepEqual(got, []core.StoryReaction{want}) {
		t.Fatalf("reactions = %+v, %v", got, err)
	}
	if got, err := s.BriefingStories(ctx, "run_b"); err != nil || len(got) != 1 || got[0].Reaction != core.NewsReactionLess {
		t.Fatalf("briefing stories with the reaction = %+v, %v", got, err)
	}
	if err := s.ClearNewsReaction(ctx, storyID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.BriefingStories(ctx, "run_b"); err != nil || len(got) != 1 || got[0].Reaction != "" {
		t.Fatalf("briefing stories after the reaction was cleared = %+v, %v", got, err)
	}

	// Deleting the Briefings thread takes its briefings' picks with it; a
	// reaction belongs to the story and stays until it is pruned.
	if err := s.ReactToNewsStory(ctx, storyID, core.NewsReactionMore, "device_1", watchTestNow); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteThread(ctx, "thread_b"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.BriefingStories(ctx, "run_b"); err != nil || len(got) != 0 {
		t.Fatalf("picks of a deleted thread = %+v, %v", got, err)
	}
	var recorded int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM briefing_picks`).Scan(&recorded); err != nil || recorded != 0 {
		t.Fatalf("briefings recorded after the thread was deleted = %d, %v", recorded, err)
	}
	if got, err := s.NewsReactions(ctx, watchTestNow); err != nil || len(got) != 1 {
		t.Fatalf("reactions after the thread was deleted = %+v, %v", got, err)
	}
	if err := s.PruneNewsReactions(ctx, watchTestNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.NewsReactions(ctx, time.Time{}); err != nil || len(got) != 0 {
		t.Fatalf("pruned reactions = %+v, %v", got, err)
	}
}

func TestOldBriefingPicksArePruned(t *testing.T) {
	s, bbc, ft, _ := newsStoryFixture(t)
	ctx := context.Background()
	storyID := briefedStory(t, s, bbc, ft, "run_old")
	if err := s.RecordBriefingPicks(ctx, core.BriefingPicks{RunID: "run_old", BriefedAt: watchTestNow, Stories: []core.BriefingStory{{StoryID: storyID, Section: "国际", Title: "t", URL: "https://bbc.test/b1"}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneNewsReactions(ctx, watchTestNow); err != nil {
		t.Fatal(err)
	}
	if got, err := s.BriefingStories(ctx, "run_old"); err != nil || len(got) != 1 {
		t.Fatalf("picks at the cutoff are kept: %+v, %v", got, err)
	}
	if err := s.PruneNewsReactions(ctx, watchTestNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.BriefingStories(ctx, "run_old"); err != nil || len(got) != 0 {
		t.Fatalf("pruned picks = %+v, %v", got, err)
	}
}

func TestRunWokenTellsWakeInputsFromTheOwners(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, "thread_1", "chat"); err != nil {
		t.Fatal(err)
	}
	for _, run := range []struct{ id, role string }{{"run_owner", "user"}, {"run_capture", core.MessageRoleCapture}, {"run_night", core.MessageRoleWake}} {
		if err := s.CreateRun(ctx, core.RunCreateParams{RunID: run.id, SessionID: "thread_1", Input: "x"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AppendSessionMessage(ctx, "thread_1", 0, run.role, "x", run.id); err != nil {
			t.Fatal(err)
		}
	}
	for runID, want := range map[string]bool{"run_owner": false, "run_capture": false, "run_night": true, "run_missing": false} {
		if got, err := s.RunWoken(ctx, runID); err != nil || got != want {
			t.Errorf("%s woken = %t, %v", runID, got, err)
		}
	}
}
