package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/stream"
)

func TestFinishCollectedRunInterruptMarksRunInterrupted(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)
	runID := createFinalizationRun(t, ctx, store, "", "hello")

	result, err := exec.finishCollectedRun(ctx, runID, "hello", RunState{
		lastOutput: "partial",
		interrupt: map[string]any{
			"kind": "elicitation",
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("finishCollectedRun: %v", err)
	}
	if result.Status != events.RunStatusInterrupted {
		t.Fatalf("result status = %q, want %q", result.Status, events.RunStatusInterrupted)
	}
	if got := result.Interrupted["kind"]; got != "elicitation" {
		t.Fatalf("interrupt kind = %#v, want elicitation", got)
	}

	run, err := store.LoadRun(ctx, runID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.Status != events.RunStatusInterrupted {
		t.Fatalf("run status = %q, want %q", run.Status, events.RunStatusInterrupted)
	}
	if run.Output != "partial" {
		t.Fatalf("run output = %q, want partial", run.Output)
	}
}

func TestFailRunSetupMarksRunFailedAndEmitsLifecycleFailure(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)
	runID := createFinalizationRun(t, ctx, store, "", "hello")

	if err := exec.failRunSetup(ctx, runID, errors.New("setup boom"), nil); err != nil {
		t.Fatalf("failRunSetup: %v", err)
	}

	run, err := store.LoadRun(ctx, runID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.Status != events.RunStatusFailed {
		t.Fatalf("run status = %q, want %q", run.Status, events.RunStatusFailed)
	}
	if run.Error != "setup boom" {
		t.Fatalf("run error = %q, want setup boom", run.Error)
	}

	records, err := store.LoadEvents(ctx, runID)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	var found bool
	for _, record := range records {
		item := ProjectEventToStreamItem(record)
		if item.Kind != stream.StreamKindRunFailed {
			continue
		}
		if item.GetError() == "setup boom" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected run_failed event with setup boom")
	}
}

func TestFailRunSetupPersistsFailureAfterContextCancellation(t *testing.T) {
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)
	runID := createFinalizationRun(t, context.Background(), store, "", "hello")

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := exec.failRunSetup(cancelledCtx, runID, errors.New("setup boom"), nil); err != nil {
		t.Fatalf("failRunSetup with cancelled context: %v", err)
	}

	run, err := store.LoadRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.Status != events.RunStatusFailed {
		t.Fatalf("run status = %q, want %q", run.Status, events.RunStatusFailed)
	}
	if run.Error != "setup boom" {
		t.Fatalf("run error = %q, want setup boom", run.Error)
	}
}
