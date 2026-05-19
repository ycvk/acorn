package app

import (
	"context"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
)

func TestSessionStateServiceLoadSessionDoesNotCheckpointFallback(t *testing.T) {
	ctx := context.Background()
	store := openSessionStateSQLiteStore(t)
	createInterruptedSessionRun(t, store, "session_1", "run_unknown_interrupt", "manual_gate")

	service := NewSessionStateService(&config.Config{}, store, NewTraceService(store))
	detail, err := service.LoadSession(ctx, "session_1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if detail.Resumable {
		t.Fatalf("checkpoint-only unsupported interrupt must not be resumable: %+v", detail)
	}
	if !strings.Contains(detail.ResumeReason, `unsupported kind "manual_gate"`) {
		t.Fatalf("resume reason should come from trace resume status, got %q", detail.ResumeReason)
	}
}

func TestSessionStateServiceLoadSessionRequiresTraceForInterruptedRuns(t *testing.T) {
	ctx := context.Background()
	store := openSessionStateSQLiteStore(t)
	createInterruptedSessionRun(t, store, "session_1", "run_1", "")

	service := NewSessionStateService(&config.Config{}, store, nil)
	_, err := service.LoadSession(ctx, "session_1")
	if err == nil || !strings.Contains(err.Error(), "trace service is nil") {
		t.Fatalf("expected missing trace service error, got %v", err)
	}
}

func openSessionStateSQLiteStore(t *testing.T) *storesqlite.Store {
	t.Helper()
	store, err := storesqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close sqlite store: %v", err)
		}
	})
	return store
}

func createInterruptedSessionRun(t *testing.T, store *storesqlite.Store, sessionID, runID, interruptKind string) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.CreateSession(ctx, sessionID, "Session "+sessionID); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := store.CreateRunWithSession(ctx, runID, sessionID, 1, "need approval", "checkpoint_1"); err != nil {
		t.Fatalf("CreateRunWithSession: %v", err)
	}
	contextPayload := map[string]any{
		"id":            "ctx_root",
		"is_root_cause": true,
	}
	if interruptKind != "" {
		contextPayload["info"] = map[string]any{"kind": interruptKind}
	}
	if _, err := store.AppendEventContext(context.Background(), runID, "run.interrupted", map[string]any{
		"interrupt": map[string]any{
			"contexts": []any{contextPayload},
		},
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := store.MarkInterruptedContext(context.Background(), runID, "waiting"); err != nil {
		t.Fatalf("MarkInterrupted: %v", err)
	}
}
