package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// A briefing's pieces are started and its push is sent once each. A piece
// is the wake input of a run in the story's conversation: the card shows how
// far it is and its text once ready, the conversation stays out of the list
// until the owner writes in it, and the owner's asks go to the same one.
func TestABriefingPieceIsWrittenInTheStorysConversation(t *testing.T) {
	s, bbc, ft, _ := newsStoryFixture(t)
	ctx := context.Background()
	story := pickedStory(t, s, bbc, ft)

	open, err := s.OpenBriefings(ctx, watchTestNow.Add(-time.Hour))
	if err != nil || len(open) != 1 || open[0].RunID != "run_b" || open[0].Slot != "2026-10-03" || open[0].PiecesStarted || open[0].Pushed {
		t.Fatalf("open briefings = %+v, %v", open, err)
	}
	if later, err := s.OpenBriefings(ctx, watchTestNow.Add(time.Hour)); err != nil || len(later) != 0 {
		t.Fatalf("briefings recorded before since = %+v, %v", later, err)
	}
	for _, claim := range []func(context.Context, string, time.Time) (bool, error){s.ClaimBriefingPieces, s.ClaimBriefingPush} {
		if claimed, err := claim(ctx, "run_b", watchTestNow); err != nil || !claimed {
			t.Fatalf("first claim = %t, %v", claimed, err)
		}
		if claimed, err := claim(ctx, "run_b", watchTestNow); err != nil || claimed {
			t.Fatalf("second claim = %t, %v", claimed, err)
		}
	}
	if open, err := s.OpenBriefings(ctx, watchTestNow.Add(-time.Hour)); err != nil || len(open) != 0 {
		t.Fatalf("a briefing with both steps done is listed: %+v, %v", open, err)
	}
	if claimed, err := s.ClaimBriefingPush(ctx, "run_missing", watchTestNow); err != nil || claimed {
		t.Fatalf("claiming an unrecorded briefing = %t, %v", claimed, err)
	}

	if card := storyOnCard(t, s); card.Piece != nil || !reflect.DeepEqual(card.SourceNames, []string{bbc.Name, ft.Name}) {
		t.Fatalf("a story without a piece = %+v", card)
	}
	piece, err := s.WriteStoryPiece(ctx, core.StoryPieceWrite{Text: "[piece 2026-10-03] 菲律宾央行意外加息", Title: "菲律宾央行意外加息", Story: core.SnapshotOf(story), NewThreadID: "thread_p"})
	if err != nil {
		t.Fatal(err)
	}
	if piece.SessionID != "thread_p" || piece.Role != core.MessageRoleWake || piece.TurnIndex != 1 {
		t.Fatalf("piece message = %+v", piece)
	}
	if messages, err := s.StoryMessages(ctx, "thread_p"); err != nil || len(messages) != 1 || messages[0].Intent != core.StoryPiece || messages[0].Story.ID != story.ID {
		t.Fatalf("story messages = %+v, %v", messages, err)
	}
	// Before its run starts the piece has nothing to show.
	if card := storyOnCard(t, s); card.ThreadID != "thread_p" || card.Piece == nil || card.Piece.Status != core.BriefingPieceFailed || card.Piece.RunID != "" {
		t.Fatalf("a piece without a run = %+v, piece %+v", card, card.Piece)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_p", SessionID: "thread_p", Input: piece.Content}); err != nil {
		t.Fatal(err)
	}
	if err := s.BindUserMessageRunIDByID(ctx, piece.ID, "run_p"); err != nil {
		t.Fatal(err)
	}
	if got := storyOnCard(t, s).Piece; got == nil || got.Status != core.BriefingPieceWriting || got.RunID != "run_p" || got.Body != "" {
		t.Fatalf("a piece being written = %+v", got)
	}
	// Another piece cannot start while this one is being written.
	if _, err := s.WriteStoryPiece(ctx, core.StoryPieceWrite{Text: "[piece 2026-10-03] 再写一篇", Title: "菲律宾央行意外加息", Story: core.SnapshotOf(story), NewThreadID: "thread_other"}); !errors.Is(err, core.ErrThreadBusy) {
		t.Fatalf("a piece in a busy conversation err = %v", err)
	}
	if _, _, err := s.FinishRun(ctx, core.RunEnd{RunID: "run_p", Status: core.RunStatusSucceeded, Output: "## 发生了什么\n加息 25 个基点。\n", Event: "run.completed"}); err != nil {
		t.Fatal(err)
	}
	if got := storyOnCard(t, s).Piece; got == nil || got.Status != core.BriefingPieceReady || got.Body != "## 发生了什么\n加息 25 个基点。" || got.FinishedAt.IsZero() {
		t.Fatalf("a finished piece = %+v", got)
	}

	listed := func() []string {
		t.Helper()
		sessions, err := s.ListSessions(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, session := range sessions {
			ids = append(ids, session.SessionID)
		}
		return ids
	}
	if got := listed(); !reflect.DeepEqual(got, []string{"thread_b"}) {
		t.Fatalf("conversations with only a piece are listed: %v", got)
	}
	discuss, err := s.SendStoryMessage(ctx, core.StoryMessageWrite{Intent: core.StoryDiscuss, Text: "展开讲讲：菲律宾央行意外加息", Title: "菲律宾央行意外加息", Story: core.SnapshotOf(story), NewThreadID: "thread_unused"})
	if err != nil || discuss.SessionID != "thread_p" || discuss.Role != "user" {
		t.Fatalf("the owner's ask after a piece = %+v, %v", discuss, err)
	}
	if got := listed(); !reflect.DeepEqual(got, []string{"thread_p", "thread_b"}) {
		t.Fatalf("conversations after the owner wrote in the story's = %v", got)
	}
	// The owner's ask leaves the piece as the briefing's.
	if got := storyOnCard(t, s).Piece; got == nil || got.Status != core.BriefingPieceReady || got.RunID != "run_p" {
		t.Fatalf("the piece after the owner's ask = %+v", got)
	}

	// A piece whose run failed, waits for the owner or succeeded without a
	// reply has nothing to show.
	for _, end := range []struct {
		status core.RunStatus
		output string
	}{{core.RunStatusFailed, ""}, {core.RunStatusInterrupted, ""}, {core.RunStatusSucceeded, "  "}} {
		if _, err := s.db.ExecContext(ctx, `UPDATE runs SET status = ?, output_text = ? WHERE run_id = 'run_p'`, string(end.status), end.output); err != nil {
			t.Fatal(err)
		}
		if got := storyOnCard(t, s).Piece; got == nil || got.Status != core.BriefingPieceFailed || got.Body != "" {
			t.Fatalf("a piece whose run ended %s with %q = %+v", end.status, end.output, got)
		}
	}
}

// Upgrading from version 37 keeps the story messages and marks the recorded
// briefings as pushed, with no pieces to start.
func TestUpgradeTo38MarksRecordedBriefingsDone(t *testing.T) {
	s, bbc, ft, _ := newsStoryFixture(t)
	ctx := context.Background()
	story := pickedStory(t, s, bbc, ft)
	if _, err := s.SendStoryMessage(ctx, core.StoryMessageWrite{Intent: core.StoryFollow, Text: "持续跟进：菲律宾央行意外加息", Title: "菲律宾央行意外加息", Story: core.SnapshotOf(story), NewThreadID: "thread_s"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if err := migrateStep(ctx, s.db, 37, addBriefingPieces); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if open, err := s.OpenBriefings(ctx, watchTestNow.Add(-time.Hour)); err != nil || len(open) != 0 {
		t.Fatalf("briefings recorded before the upgrade are open: %+v, %v", open, err)
	}
	if card := storyOnCard(t, s); card.ThreadID != "thread_s" || card.Title != "菲律宾央行意外加息" {
		t.Fatalf("the story after the upgrade = %+v", card)
	}
	if messages, err := s.StoryMessages(ctx, "thread_s"); err != nil || len(messages) != 1 || messages[0].Intent != core.StoryFollow {
		t.Fatalf("story messages after the upgrade = %+v, %v", messages, err)
	}
}
