package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// seedThread writes a thread with a finished run and the records such a run
// leaves: messages, a tool result, a pending action, a checkpoint, an
// artifact, a notification and context snapshots. shared is a snapshot hash
// another run also uses.
func seedThread(t *testing.T, s *Store, sessionID, runID, shared string) core.ArtifactRecord {
	t.Helper()
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, sessionID, "orders"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: runID, SessionID: sessionID, TurnIndex: 1, Input: "查一下订单 " + runID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendSessionMessage(ctx, sessionID, 1, "user", "查一下订单 "+runID, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendSessionMessage(ctx, sessionID, 1, "assistant", "订单已发货", runID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvent(ctx, runID, "tool.call.succeeded", map[string]any{"tool_name": "shell", "output": "shipped"}); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{"own-" + runID, shared} {
		if err := s.SaveContextSnapshot(ctx, hash, "context of "+runID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AppendEvent(ctx, runID, "presence.snapshot", map[string]any{"hash": hash}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreatePendingAction(ctx, core.PendingActionInput{ActionID: "action_" + runID, RunID: runID, Kind: core.PendingActionKindToolApproval, Subject: "Approve shell", PayloadJSON: "{}", Status: core.PendingActionStatusPending}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCheckpoint(ctx, runID, []byte("checkpoint")); err != nil {
		t.Fatal(err)
	}
	artifact, err := s.WriteArtifact(ctx, core.ArtifactWriteRequest{ArtifactID: "artifact_" + runID, RunID: runID, SessionID: sessionID, Kind: string(ArtifactKindLog), Title: "stdout", MIMEType: "text/plain", Content: []byte("shipped"), CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueNotification(ctx, core.Notification{Title: "订单", Body: "已发货", ThreadID: sessionID, RunID: runID, Status: core.NotificationQueued, SendAfter: time.Now(), CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun(ctx, runID, core.RunStatusSucceeded, "订单已发货", ""); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func countRows(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestDeleteThreadRemovesWhatItsRunsLeft(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	artifact := seedThread(t, s, "session_gone", "run_gone", "shared")
	seedThread(t, s, "session_kept", "run_kept", "shared")
	sources, err := s.ThreadMemorySourceIDs(ctx, "session_gone")
	if err != nil || len(sources) != 3 {
		t.Fatalf("thread sources = %v, %v; want two messages and a tool result", sources, err)
	}

	if err := s.DeleteThread(ctx, "session_gone"); err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]int{
		`SELECT COUNT(*) FROM sessions WHERE session_id = 'session_gone'`:                                        0,
		`SELECT COUNT(*) FROM session_messages WHERE session_id = 'session_gone'`:                                0,
		`SELECT COUNT(*) FROM runs WHERE run_id = 'run_gone'`:                                                    0,
		`SELECT COUNT(*) FROM runs_fts WHERE run_id = 'run_gone'`:                                                0,
		`SELECT COUNT(*) FROM events WHERE run_id = 'run_gone'`:                                                  0,
		`SELECT COUNT(*) FROM pending_actions WHERE run_id = 'run_gone'`:                                         0,
		`SELECT COUNT(*) FROM agent_checkpoints WHERE checkpoint_id = 'run_gone'`:                                0,
		`SELECT COUNT(*) FROM artifacts WHERE run_id = 'run_gone'`:                                               0,
		`SELECT COUNT(*) FROM notifications WHERE thread_id = 'session_gone'`:                                    0,
		`SELECT COUNT(*) FROM context_snapshots WHERE hash = 'own-run_gone'`:                                     0,
		`SELECT COUNT(*) FROM context_snapshots WHERE hash IN ('shared', 'own-run_kept')`:                        2,
		`SELECT COUNT(*) FROM memory_sources WHERE session_id = 'session_gone'`:                                  3,
		`SELECT COUNT(*) FROM runs r JOIN session_messages m ON m.run_id = r.run_id WHERE r.run_id = 'run_kept'`: 2,
		`SELECT COUNT(*) FROM runs_fts WHERE run_id = 'run_kept'`:                                                1,
	} {
		if got := countRows(t, s, query); got != want {
			t.Errorf("%s = %d, want %d", query, got, want)
		}
	}
	if _, err := os.Stat(s.artifactDir + "/" + artifact.RelativePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact file still there: %v", err)
	}
	if err := s.DeleteThread(ctx, "session_gone"); !errors.Is(err, core.ErrSessionNotFound) {
		t.Fatalf("delete a missing thread: err = %v", err)
	}
}

func TestDeleteThreadRefusesARunningRun(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, "session_busy", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_busy", SessionID: "session_busy", TurnIndex: 1, Input: "working"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteThread(ctx, "session_busy"); !errors.Is(err, core.ErrThreadBusy) {
		t.Fatalf("err = %v", err)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM runs WHERE run_id = 'run_busy'`); got != 1 {
		t.Fatalf("busy thread lost its run")
	}
}

// A forgotten source whose thread is gone reads as excluded, without the
// content it no longer has.
func TestForgottenSourcesOfADeletedThreadReadAsExcluded(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	seedThread(t, s, "session_gone", "run_gone", "shared")
	sources, err := s.ThreadMemorySourceIDs(ctx, "session_gone")
	if err != nil {
		t.Fatal(err)
	}
	request, err := s.RegisterMemorySource(ctx, core.MemorySource{ID: "request:delete", Kind: core.MemoryRequestSource, ObjectID: "request:delete", Version: "1", Speaker: "owner", Body: "Delete conversation: orders", RecordedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: sources, RequestSourceID: request.ID, Reason: "owner deleted the conversation", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteThread(ctx, "session_gone"); err != nil {
		t.Fatal(err)
	}
	for _, id := range sources {
		if _, err := s.LoadMemorySource(ctx, id); !errors.Is(err, core.ErrMemoryExcluded) {
			t.Fatalf("load %s: err = %v", id, err)
		}
	}
}

// Threads deleted before deletion forgot them are finished by the upgrade to
// version 5.
func TestUpgradeForgetsThreadsDeletedEarlier(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	seedThread(t, s, "session_gone", "run_gone", "shared")
	seedThread(t, s, "session_kept", "run_kept", "shared")
	sources, err := s.ThreadMemorySourceIDs(ctx, "session_gone")
	if err != nil {
		t.Fatal(err)
	}
	// What deleting a thread used to do.
	if _, err := s.db.Exec(`DELETE FROM artifacts WHERE run_id = 'run_gone'`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`DELETE FROM session_messages WHERE session_id = 'session_gone'`, `DELETE FROM sessions WHERE session_id = 'session_gone'`} {
		if _, err := s.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := forgetDeletedThreads(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]int{
		`SELECT COUNT(*) FROM runs WHERE run_id = 'run_gone'`:                                                 0,
		`SELECT COUNT(*) FROM events WHERE run_id = 'run_gone'`:                                               0,
		`SELECT COUNT(*) FROM pending_actions WHERE run_id = 'run_gone'`:                                      0,
		`SELECT COUNT(*) FROM notifications WHERE thread_id = 'session_gone'`:                                 0,
		`SELECT COUNT(*) FROM runs WHERE run_id = 'run_kept'`:                                                 1,
		`SELECT COUNT(*) FROM memory_sources WHERE id = 'request:deleted-session_gone' AND speaker = 'owner'`: 1,
	} {
		if got := countRows(t, s, query); got != want {
			t.Errorf("%s = %d, want %d", query, got, want)
		}
	}
	for _, id := range sources {
		if got := countRows(t, s, `SELECT COUNT(*) FROM memory_exclusions WHERE source_id = ? AND quote = ''`, id); got != 1 {
			t.Errorf("source %s is not excluded", id)
		}
	}
	kept, err := s.ThreadMemorySourceIDs(ctx, "session_kept")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range kept {
		if _, err := s.LoadMemorySource(ctx, id); err != nil {
			t.Errorf("kept source %s: %v", id, err)
		}
	}
}
