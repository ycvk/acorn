package wire

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

// TestCloseFailsActiveRuns covers serve stopping while a run waits on the
// model: Close cancels the run and records it failed before the store
// closes, for owner runs and for runs the wake scheduler starts.
func TestCloseFailsActiveRuns(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start func(context.Context, *Container) (string, error)
	}{
		{name: "owner", start: func(ctx context.Context, c *Container) (string, error) {
			thread, err := c.threads.CreateThread(ctx, "owner")
			if err != nil {
				return "", err
			}
			run, err := c.runs.CreateRun(ctx, thread.ID, "", "say something", nil)
			if err != nil {
				return "", err
			}
			return run.ID, nil
		}},
		{name: "routine", start: func(ctx context.Context, c *Container) (string, error) {
			starter := &wakeRunStarter{runs: c.runs, store: c.store}
			_, runID, err := starter.StartRoutineRun(ctx, "", "Night", "reflect", core.ScheduledWake{Reason: "night reflection", Autonomous: true})
			return runID, err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(gate) }) }
			provider := &fakeOpenAI{
				replies: []string{sseChunks(doneContent, stopFinish)},
				gates:   map[int]chan struct{}{0: gate},
			}
			server := httptest.NewServer(provider)
			defer server.Close()
			defer release()
			cfg := writeTestConfig(t, server.URL, briefingOff)

			ctx := context.Background()
			c, err := NewContainer(ctx, cfg)
			if err != nil {
				t.Fatalf("container: %v", err)
			}
			runID, err := tc.start(ctx, c)
			if err != nil {
				_ = c.Close()
				t.Fatalf("start run: %v", err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for provider.request(0) == nil {
				if time.Now().After(deadline) {
					_ = c.Close()
					t.Fatal("the run never called the model")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := c.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			db, err := store.Open(cfg.Runtime.StorageDir)
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer func() { _ = db.Close() }()
			record, err := db.LoadRun(ctx, runID)
			if err != nil {
				t.Fatalf("load run: %v", err)
			}
			if record.Status != core.RunStatusFailed || record.Error != core.ErrShuttingDown.Error() {
				t.Fatalf("run after close = %s %q, want failed %q", record.Status, record.Error, core.ErrShuttingDown)
			}
			events, err := db.LoadEvents(ctx, runID)
			if err != nil {
				t.Fatalf("load events: %v", err)
			}
			last := events[len(events)-1]
			payload, _ := last.Payload.(map[string]any)
			if last.Kind != "run.failed" || payload["error"] != core.ErrShuttingDown.Error() {
				t.Fatalf("last event = %s %v, want run.failed with the shutdown error; events: %s", last.Kind, last.Payload, eventKinds(events))
			}
		})
	}
}

// TestStartFailsRunsLeftRunning covers a process that stopped mid-run without
// recording the run's end: the next start fails the run, so its thread can be
// deleted again.
func TestStartFailsRunsLeftRunning(t *testing.T) {
	cfg := writeTestConfig(t, "http://127.0.0.1:1", briefingOff)
	ctx := context.Background()
	db, err := store.Open(cfg.Runtime.StorageDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := db.CreateSession(ctx, "thread_left", "left running"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.CreateRun(ctx, core.RunCreateParams{RunID: "run_left", SessionID: "thread_left", TurnIndex: 1, Input: "hello"}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	c, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer func() { _ = c.Close() }()
	run, err := c.runs.GetRun(ctx, "run_left")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != "failed" {
		t.Fatalf("run status after start = %s, want failed", run.Status)
	}
	record, err := c.store.LoadRun(ctx, "run_left")
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	if record.Error != abandonedRunError {
		t.Fatalf("run error = %q, want %q", record.Error, abandonedRunError)
	}
	if err := c.threads.DeleteThread(ctx, "thread_left"); err != nil {
		t.Fatalf("delete thread: %v", err)
	}
}

// TestResumeRefusedWhileStoppingStaysInterrupted covers a decided run whose
// resume would start while serve stops: the run is not claimed, stays
// interrupted, and the next start resumes it.
func TestResumeRefusedWhileStoppingStaysInterrupted(t *testing.T) {
	provider := &fakeOpenAI{replies: []string{
		sseChunks(recallCallStream, toolCallsFinish),
		sseChunks(doneContent, stopFinish),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeApprovalTestConfig(t, server.URL)
	runID, actionID := startApprovalRun(t, cfg)

	ctx := context.Background()
	c, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	if _, err := c.store.DecidePendingAction(ctx, actionID, core.PendingActionStatusApproved, `{"action":"accept"}`); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if err := c.runController.Stop(ctx); err != nil {
		t.Fatalf("stop runs: %v", err)
	}
	if _, err := c.runResume.Resume(ctx, runID); !errors.Is(err, core.ErrShuttingDown) {
		t.Fatalf("resume while stopping = %v, want ErrShuttingDown", err)
	}
	record, err := c.store.LoadRun(ctx, runID)
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	if record.Status != core.RunStatusInterrupted {
		t.Fatalf("run status = %s, want interrupted", record.Status)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatalf("second container: %v", err)
	}
	defer func() { _ = second.Close() }()
	if err := second.ResumeReadyRuns(ctx); err != nil {
		t.Fatalf("resume ready runs: %v", err)
	}
	waitRunStatus(t, second, runID, "completed")
	if events, err := second.store.LoadEvents(ctx, runID); err != nil || strings.Contains(eventKinds(events), "run.failed") {
		t.Fatalf("events = %s, %v; want no run.failed", eventKinds(events), err)
	}
}
