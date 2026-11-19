package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// A run is stored the same way whether CreateRun, an owner message, the end
// of the run before it or a delegation starts it: running, with its input,
// without output, error or end, and held by this process. A sub-run keeps
// the time its start gives; the others take the time they are stored.
func TestStartedRunsAreStoredAlike(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	for _, id := range []string{"created", "sent"} {
		if _, err := s.CreateSession(ctx, id, id); err != nil {
			t.Fatal(err)
		}
	}
	before := time.Now().UTC().Add(-time.Second)
	// stored checks the run's row; a zero created means the time it was stored.
	stored := func(runID, session string, turn int, input string, created time.Time) {
		t.Helper()
		var gotSession, status, gotInput, output, errText, createdAt, finished, process string
		var gotTurn int
		err := s.db.QueryRowContext(ctx, `SELECT r.session_id, r.turn_index, r.status, r.input_text, r.output_text, r.error_text, r.created_at, r.finished_at, p.process
			FROM runs r JOIN run_processes p ON p.run_id = r.run_id WHERE r.run_id = ?`, runID).Scan(&gotSession, &gotTurn, &status, &gotInput, &output, &errText, &createdAt, &finished, &process)
		if err != nil {
			t.Fatalf("%s: %v", runID, err)
		}
		if gotSession != session || gotTurn != turn || status != string(core.RunStatusRunning) || gotInput != input || output != "" || errText != "" || finished != "" || process != s.process {
			t.Errorf("%s stored as session=%q turn=%d status=%q input=%q output=%q error=%q finished=%q process=%q", runID, gotSession, gotTurn, status, gotInput, output, errText, finished, process)
		}
		at, err := parseTimestamp(createdAt, "created_at")
		if err != nil {
			t.Fatalf("%s created_at: %v", runID, err)
		}
		if created.IsZero() {
			if at.Before(before) || at.After(time.Now().UTC().Add(time.Second)) {
				t.Errorf("%s created_at = %v, want when it was stored", runID, at)
			}
		} else if createdAt != formatTimestamp(created) {
			t.Errorf("%s created_at = %q, want %q", runID, createdAt, formatTimestamp(created))
		}
	}

	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_created", SessionID: "created", TurnIndex: 1, Input: "建一个"}); err != nil {
		t.Fatal(err)
	}
	stored("run_created", "created", 1, "建一个", time.Time{})

	sent, err := s.SendOwnerMessage(ctx, core.OwnerMessage{SessionID: "sent", Content: "查一下天气", RunID: "run_sent", ClientID: core.NewActionID()})
	if err != nil || sent.Started == nil {
		t.Fatalf("owner message in an idle conversation = %+v, %v, want a started run", sent, err)
	}
	stored("run_sent", "sent", 1, "查一下天气", time.Time{})

	if waiting, err := s.SendOwnerMessage(ctx, core.OwnerMessage{SessionID: "sent", Content: "再查一下明天", RunID: "run_unused", ClientID: core.NewActionID()}); err != nil || waiting.Queued == nil {
		t.Fatalf("owner message while a run is going = %+v, %v, want it queued", waiting, err)
	}
	if _, next, err := s.FinishRun(ctx, core.RunEnd{RunID: "run_sent", Status: core.RunStatusSucceeded, Output: "晴", Event: "run.completed", Next: "run_next"}); err != nil || next == nil {
		t.Fatalf("end of a run with a message waiting = %+v, %v, want the next run", next, err)
	}
	stored("run_next", "sent", 2, "再查一下明天", time.Time{})

	delegatedAt := time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
	startSubRun(t, s, core.SubRunStart{RunID: "run_delegated", ParentRunID: "run_created", Brief: core.SubRunBrief{Objective: "读一篇"}, Input: "[brief] 读一篇", CreatedAt: delegatedAt})
	stored("run_delegated", "created", 1, "[brief] 读一篇", delegatedAt)
}

// An owner message and a goal need their conversation.
func TestOwnerMessagesAndGoalsNeedTheirConversation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	_, err := s.SendOwnerMessage(ctx, core.OwnerMessage{SessionID: "gone", Content: "在吗", RunID: "run_1", ClientID: core.NewActionID()})
	if !errors.Is(err, core.ErrSessionNotFound) || err.Error() != "send owner message to gone: session not found: gone" {
		t.Errorf("owner message to a missing conversation = %v", err)
	}
	_, err = s.StartGoal(ctx, core.Goal{SessionID: "gone", Objective: "读完四大名著", MaxRuns: 3, CreatedAt: time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)})
	if !errors.Is(err, core.ErrSessionNotFound) || err.Error() != "start goal: session not found: gone" {
		t.Errorf("goal in a missing conversation = %v", err)
	}
}
