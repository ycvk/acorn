package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/domain"
)

func TestUpsertAndGetSessionSummary(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC().Truncate(time.Second)
	if err := store.UpsertSessionSummary(context.Background(), domain.SessionSummary{
		SessionID:   "session_1",
		SourceRunID: "run_1",
		RunStatus:   "succeeded",
		Summary:     "latest session state summary",
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("upsert session summary: %v", err)
	}

	got, err := store.GetSessionSummary(context.Background(), "session_1")
	if err != nil {
		t.Fatalf("get session summary: %v", err)
	}
	if got == nil {
		t.Fatal("expected summary record")
	}
	if got.SourceRunID != "run_1" || got.RunStatus != "succeeded" {
		t.Fatalf("unexpected summary metadata: %+v", got)
	}
	if got.Summary != "latest session state summary" {
		t.Fatalf("summary = %q", got.Summary)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero updated time")
	}
}
