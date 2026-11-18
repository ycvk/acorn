package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// pickedStory records a briefing in thread_b that picked one story under a
// title of its own, 菲律宾央行意外加息, and returns the story with its items.
func pickedStory(t *testing.T, s *Store, bbc, ft core.Watch) core.NewsStory {
	t.Helper()
	ctx := context.Background()
	id := briefedStory(t, s, bbc, ft, "run_b")
	briefingRun(t, s, "2026-10-03", "thread_b", "run_b", core.RunStatusSucceeded, "## 国际\n1. **菲律宾央行意外加息**")
	if err := s.RecordBriefingPicks(ctx, core.BriefingPicks{RunID: "run_b", BriefedAt: watchTestNow, Stories: []core.BriefingStory{{StoryID: id, Section: "国际", Title: "菲律宾央行意外加息", URL: "https://bbc.test/b1", Sources: []int64{bbc.ID, ft.ID}}}}); err != nil {
		t.Fatal(err)
	}
	story, err := s.NewsStory(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return story
}

func storyOnCard(t *testing.T, s *Store) core.BriefingStory {
	t.Helper()
	stories, err := s.BriefingStories(context.Background(), "run_b")
	if err != nil || len(stories) != 1 {
		t.Fatalf("briefing stories = %+v, %v", stories, err)
	}
	return stories[0]
}

// The owner's messages from a story go to one conversation named after it,
// keep the story as it stood after it is pruned, and leave with the
// conversation; the card follows the conversation and its concern.
func TestStoryMessagesKeepAStoryInItsConversation(t *testing.T) {
	s, bbc, ft, _ := newsStoryFixture(t)
	ctx := context.Background()
	story := pickedStory(t, s, bbc, ft)
	if story.Title != "菲律宾央行加息" || len(story.Items) != 2 {
		t.Fatalf("story = %+v", story)
	}
	if _, err := s.NewsStory(ctx, story.ID+100); !errors.Is(err, core.ErrNewsStoryNotFound) {
		t.Fatalf("loading a missing story err = %v", err)
	}
	if title, err := s.BriefingStoryTitle(ctx, story.ID); err != nil || title != "菲律宾央行意外加息" {
		t.Fatalf("briefing title = %q, %v", title, err)
	}
	if title, err := s.BriefingStoryTitle(ctx, story.ID+100); err != nil || title != "" {
		t.Fatalf("briefing title of a story no briefing picked = %q, %v", title, err)
	}
	if card := storyOnCard(t, s); card.ThreadID != "" || card.Following {
		t.Fatalf("a story nobody asked about = %+v", card)
	}

	snapshot := core.SnapshotOf(story)
	discuss, err := s.SendStoryMessage(ctx, core.StoryMessageWrite{Intent: core.StoryDiscuss, Text: "展开讲讲：菲律宾央行意外加息", Title: "菲律宾央行意外加息", Story: snapshot, NewThreadID: "thread_s", ClientID: core.NewActionID()})
	if err != nil {
		t.Fatal(err)
	}
	if discuss.SessionID != "thread_s" || discuss.Role != "user" || discuss.Content != "展开讲讲：菲律宾央行意外加息" || discuss.TurnIndex != 1 {
		t.Fatalf("discuss message = %+v", discuss)
	}
	if thread, err := s.LoadSession(ctx, "thread_s"); err != nil || thread.Title != "菲律宾央行意外加息" {
		t.Fatalf("the story's conversation = %+v, %v", thread, err)
	}
	if card := storyOnCard(t, s); card.ThreadID != "thread_s" || card.Following {
		t.Fatalf("a story opened once = %+v", card)
	}

	// A conversation with a run going on, or waiting for the owner, takes no
	// message from the card.
	for _, status := range []core.RunStatus{core.RunStatusRunning, core.RunStatusInterrupted} {
		runID := "run_s_" + string(status)
		if err := s.CreateRun(ctx, core.RunCreateParams{RunID: runID, SessionID: "thread_s", TurnIndex: discuss.TurnIndex, Input: discuss.Content}); err != nil {
			t.Fatal(err)
		}
		if status != core.RunStatusRunning {
			if _, _, err := s.FinishRun(ctx, core.RunEnd{RunID: runID, Status: status, Output: "", Error: "", Event: "run.completed"}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.SendStoryMessage(ctx, core.StoryMessageWrite{Intent: core.StoryFollow, Text: "持续跟进：菲律宾央行意外加息", Title: "菲律宾央行意外加息", Story: snapshot, NewThreadID: "thread_x", ClientID: core.NewActionID()}); !errors.Is(err, core.ErrThreadBusy) {
			t.Fatalf("a message while the latest run is %s: %v", status, err)
		}
		if _, _, err := s.FinishRun(ctx, core.RunEnd{RunID: runID, Status: core.RunStatusSucceeded, Output: "说明", Error: "", Event: "run.completed"}); err != nil {
			t.Fatal(err)
		}
	}
	follow, err := s.SendStoryMessage(ctx, core.StoryMessageWrite{Intent: core.StoryFollow, Text: "持续跟进：菲律宾央行意外加息", Title: "菲律宾央行意外加息", Story: snapshot, NewThreadID: "thread_x", ClientID: core.NewActionID()})
	if err != nil {
		t.Fatal(err)
	}
	if follow.SessionID != "thread_s" || follow.TurnIndex != 2 {
		t.Fatalf("a second message from the story = %+v", follow)
	}
	want := []core.StoryMessage{
		{MessageID: discuss.ID, SessionID: "thread_s", Intent: core.StoryDiscuss, Story: snapshot},
		{MessageID: follow.ID, SessionID: "thread_s", Intent: core.StoryFollow, Story: snapshot},
	}
	if got := storyMessages(t, s, "thread_s"); !reflect.DeepEqual(got, want) {
		t.Fatalf("story messages = %+v, want %+v", got, want)
	}

	source, err := s.RegisterMemorySource(ctx, core.MemorySource{ID: "follow-1", Kind: "fixture", ObjectID: "follow-1", Version: "1", Speaker: "owner", Body: follow.Content, RecordedAt: watchTestNow})
	if err != nil {
		t.Fatal(err)
	}
	concern, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "跟进菲律宾央行加息", State: "active", Reason: "owner 从早安卡要求跟进", SourceID: source.ID, ThreadID: "thread_s", UpdatedAt: watchTestNow}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if card := storyOnCard(t, s); !card.Following {
		t.Fatalf("a story whose conversation has an active concern = %+v", card)
	}
	concern.State, concern.Reason = "released", "owner 说不用跟了"
	if _, err := s.SaveConcern(ctx, concern, concern.Revision); err != nil {
		t.Fatal(err)
	}
	if card := storyOnCard(t, s); card.Following {
		t.Fatalf("a story whose concern was released = %+v", card)
	}

	if err := s.DeleteThread(ctx, "thread_s"); err != nil {
		t.Fatal(err)
	}
	if got := storyMessages(t, s, "thread_s"); len(got) != 0 {
		t.Fatalf("story messages of a deleted conversation = %+v", got)
	}
	if card := storyOnCard(t, s); card.ThreadID != "" {
		t.Fatalf("a story whose conversation was deleted = %+v", card)
	}
	again, err := s.SendStoryMessage(ctx, core.StoryMessageWrite{Intent: core.StoryDiscuss, Text: "展开讲讲：菲律宾央行意外加息", Title: "菲律宾央行意外加息", Story: snapshot, NewThreadID: "thread_new", ClientID: core.NewActionID()})
	if err != nil || again.SessionID != "thread_new" {
		t.Fatalf("a story opened after its conversation was deleted = %+v, %v", again, err)
	}

	if _, err := s.PruneNews(ctx, watchTestNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NewsStory(ctx, story.ID); !errors.Is(err, core.ErrNewsStoryNotFound) {
		t.Fatalf("a pruned story err = %v", err)
	}
	if got := storyMessages(t, s, "thread_new"); len(got) != 1 || !reflect.DeepEqual(got[0].Story, snapshot) {
		t.Fatalf("story messages after the story was pruned = %+v", got)
	}
}

// storyMessages lists a conversation's story messages without their times.
func storyMessages(t *testing.T, s *Store, sessionID string) []core.StoryMessage {
	t.Helper()
	messages, err := s.StoryMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range messages {
		if messages[i].CreatedAt.IsZero() {
			t.Fatalf("story message %d has no time", messages[i].MessageID)
		}
		messages[i].CreatedAt = time.Time{}
	}
	return messages
}

func TestStoryMessagesNeedAnIntentTextTitleAndStory(t *testing.T) {
	s := openTestStore(t)
	story := core.StorySnapshot{ID: 1, Title: "菲律宾央行加息"}
	for name, in := range map[string]core.StoryMessageWrite{
		"intent":    {Intent: "share", Text: "看看", Title: "加息", Story: story, NewThreadID: "thread_s", ClientID: "c1"},
		"text":      {Intent: core.StoryDiscuss, Text: " ", Title: "加息", Story: story, NewThreadID: "thread_s", ClientID: "c1"},
		"title":     {Intent: core.StoryDiscuss, Text: "看看", Story: story, NewThreadID: "thread_s", ClientID: "c1"},
		"story":     {Intent: core.StoryDiscuss, Text: "看看", Title: "加息", Story: core.StorySnapshot{Title: "菲律宾央行加息"}, NewThreadID: "thread_s", ClientID: "c1"},
		"thread id": {Intent: core.StoryDiscuss, Text: "看看", Title: "加息", Story: story, ClientID: "c1"},
		"client id": {Intent: core.StoryDiscuss, Text: "看看", Title: "加息", Story: story, NewThreadID: "thread_s"},
	} {
		if _, err := s.SendStoryMessage(context.Background(), in); err == nil {
			t.Errorf("a story message without its %s was recorded", name)
		}
	}
}

// An ask made again under its client id, as when its answer was lost,
// writes nothing: it comes back as the message the first ask wrote, with the
// run that took it, or with none while its run never started.
func TestAStoryAskMadeAgainIsWrittenOnce(t *testing.T) {
	s, bbc, ft, _ := newsStoryFixture(t)
	ctx := context.Background()
	snapshot := core.SnapshotOf(pickedStory(t, s, bbc, ft))
	ask := core.StoryMessageWrite{Intent: core.StoryDiscuss, Text: "展开讲讲：菲律宾央行意外加息", Title: "菲律宾央行意外加息", Story: snapshot, NewThreadID: "thread_s", ClientID: "ask-1"}
	first, err := s.SendStoryMessage(ctx, ask)
	if err != nil {
		t.Fatal(err)
	}
	ask.NewThreadID = "thread_unused"
	again, err := s.SendStoryMessage(ctx, ask)
	if err != nil || again.ID != first.ID || again.SessionID != "thread_s" || again.RunID != "" || again.Content != first.Content {
		t.Fatalf("asked again before its run started = %+v, %v", again, err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_ask", SessionID: "thread_s", TurnIndex: first.TurnIndex, Input: first.Content}); err != nil {
		t.Fatal(err)
	}
	if err := s.BindUserMessageRunIDByID(ctx, first.ID, "run_ask"); err != nil {
		t.Fatal(err)
	}
	// The conversation is busy now, and the ask is still answered with its run.
	again, err = s.SendStoryMessage(ctx, ask)
	if err != nil || again.ID != first.ID || again.RunID != "run_ask" {
		t.Fatalf("asked again once its run started = %+v, %v", again, err)
	}
	if got := storyMessages(t, s, "thread_s"); len(got) != 1 {
		t.Fatalf("story messages = %+v", got)
	}
}

// A story's conversation is busy while any run of it runs or waits for the
// owner, as for an owner message, not only its latest.
func TestAStoryConversationWithAWaitingRunIsBusy(t *testing.T) {
	s, bbc, ft, _ := newsStoryFixture(t)
	ctx := context.Background()
	snapshot := core.SnapshotOf(pickedStory(t, s, bbc, ft))
	first, err := s.SendStoryMessage(ctx, core.StoryMessageWrite{Intent: core.StoryDiscuss, Text: "展开讲讲：菲律宾央行意外加息", Title: "菲律宾央行意外加息", Story: snapshot, NewThreadID: "thread_s", ClientID: "ask-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_waiting", SessionID: "thread_s", TurnIndex: first.TurnIndex, Input: first.Content}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkInterrupted(ctx, "run_waiting", ""); err != nil {
		t.Fatal(err)
	}
	// A later run, a view's press say, finished while the first still waits.
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_later", SessionID: "thread_s", TurnIndex: first.TurnIndex + 1, Input: "继续"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.FinishRun(ctx, core.RunEnd{RunID: "run_later", Status: core.RunStatusSucceeded, Output: "好", Event: "run.completed", Payload: map[string]any{"message": "好"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendStoryMessage(ctx, core.StoryMessageWrite{Intent: core.StoryFollow, Text: "持续跟进：菲律宾央行意外加息", Title: "菲律宾央行意外加息", Story: snapshot, NewThreadID: "thread_x", ClientID: "ask-2"}); !errors.Is(err, core.ErrThreadBusy) {
		t.Fatalf("ask while an earlier run waits err = %v", err)
	}
}
