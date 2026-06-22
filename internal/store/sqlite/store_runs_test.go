package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/domain"
	storecore "github.com/ycvk/acorn/internal/store"
)

func TestStoreLifecycle(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.CreateRun(context.Background(), "run_1", "hello"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), "run_1", "run.started", map[string]any{"input": "hello"}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	if err := store.FinishRunContext(context.Background(), "run_1", domain.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	run, err := store.LoadRun(context.Background(), "run_1")
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	items, err := store.LoadEvents(context.Background(), "run_1")
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if run == nil || len(items) != 1 {
		t.Fatalf("expected run and one event, got run=%#v items=%#v", run, items)
	}
	if run.Status != domain.RunStatusSucceeded {
		t.Fatalf("Status = %q, want %q", run.Status, domain.RunStatusSucceeded)
	}
	if run.Output != "done" {
		t.Fatalf("Output = %q, want %q", run.Output, "done")
	}
}

func TestCreateRunWithParamsPersistsFields(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	err = store.CreateRunWithParams(context.Background(), storecore.RunCreateParams{
		RunID:     "run_child",
		SessionID: "session_child",
		TurnIndex: 2,
		Input:     "child task",
	})
	if err != nil {
		t.Fatalf("CreateRunWithParams: %v", err)
	}

	run, err := store.LoadRun(context.Background(), "run_child")
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.SessionID != "session_child" {
		t.Fatalf("SessionID = %q, want session_child", run.SessionID)
	}
	if run.TurnIndex != 2 {
		t.Fatalf("TurnIndex = %d, want 2", run.TurnIndex)
	}
	if run.Input != "child task" {
		t.Fatalf("Input = %q, want child task", run.Input)
	}
	if run.Status != domain.RunStatusRunning {
		t.Fatalf("Status = %q, want %q", run.Status, domain.RunStatusRunning)
	}
}

func TestLoadEventsFailsLoudlyOnCorruptPayloadJSON(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.CreateRun(context.Background(), "run_bad_event", "hello"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = store.db.Exec(
		`INSERT INTO events(run_id, kind, payload_json, created_at) VALUES(?, ?, ?, ?)`,
		"run_bad_event",
		"assistant.delta",
		`{"assistant_delta":`,
		formatTimestamp(time.Now().UTC()),
	)
	if err != nil {
		t.Fatalf("insert corrupt event: %v", err)
	}

	_, err = store.LoadEvents(context.Background(), "run_bad_event")
	if err == nil {
		t.Fatal("expected corrupt payload error, got nil")
	}
	for _, want := range []string{"run_id=run_bad_event", "sequence=", "kind=assistant.delta"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want substring %q", err.Error(), want)
		}
	}
}

func TestLoadEventsAfterFiltersExclusiveCursor(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.CreateRun(context.Background(), "run_after", "hello"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	first, err := store.AppendEventContext(context.Background(), "run_after", "run.started", map[string]any{"input": "hello"})
	if err != nil {
		t.Fatalf("append first event: %v", err)
	}
	second, err := store.AppendEventContext(context.Background(), "run_after", "assistant.delta", map[string]any{"delta": "he"})
	if err != nil {
		t.Fatalf("append second event: %v", err)
	}
	third, err := store.AppendEventContext(context.Background(), "run_after", "run.completed", map[string]any{"message": map[string]any{"content": "done"}})
	if err != nil {
		t.Fatalf("append third event: %v", err)
	}

	items, err := store.LoadEventsAfter(context.Background(), "run_after", first.Sequence)
	if err != nil {
		t.Fatalf("LoadEventsAfter: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Sequence != second.Sequence || items[1].Sequence != third.Sequence {
		t.Fatalf("sequences = [%d, %d], want [%d, %d]", items[0].Sequence, items[1].Sequence, second.Sequence, third.Sequence)
	}
}

func TestListInboxRunsFiltersSessionRunsByStatus(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	for _, sessionID := range []string{"session_active", "session_done", "session_failed"} {
		if _, err := store.CreateSession(ctx, sessionID, ""); err != nil {
			t.Fatalf("create session %s: %v", sessionID, err)
		}
	}
	if err := store.CreateRunWithSession(ctx, "run_active", "session_active", 1, "working"); err != nil {
		t.Fatalf("create active run: %v", err)
	}
	if err := store.CreateRunWithSession(ctx, "run_done", "session_done", 1, "done"); err != nil {
		t.Fatalf("create done run: %v", err)
	}
	if err := store.FinishRunContext(ctx, "run_done", domain.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatalf("finish done run: %v", err)
	}
	if err := store.CreateRunWithSession(ctx, "run_failed", "session_failed", 1, "failed"); err != nil {
		t.Fatalf("create failed run: %v", err)
	}
	if err := store.FinishRunContext(ctx, "run_failed", domain.RunStatusFailed, "", "boom"); err != nil {
		t.Fatalf("finish failed run: %v", err)
	}
	if err := store.CreateRun(ctx, "run_cli", "cli"); err != nil {
		t.Fatalf("create cli run: %v", err)
	}

	active, err := store.ListActiveRuns(ctx, 10)
	if err != nil {
		t.Fatalf("ListActiveRuns: %v", err)
	}
	if len(active) != 1 || active[0].RunID != "run_active" {
		t.Fatalf("active runs = %#v, want only run_active", active)
	}

	terminal, err := store.ListRecentTerminalRuns(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentTerminalRuns: %v", err)
	}
	got := runIDs(terminal)
	want := []string{"run_failed", "run_done"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("terminal runs = %#v, want %#v", got, want)
	}
}

func runIDs(items []domain.RunRecord) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.RunID)
	}
	return out
}

func TestLoadEventsAfterFailsLoudlyOnCorruptPayloadJSON(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.CreateRun(context.Background(), "run_bad_event_after", "hello"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = store.db.Exec(
		`INSERT INTO events(run_id, kind, payload_json, created_at) VALUES(?, ?, ?, ?)`,
		"run_bad_event_after",
		"assistant.delta",
		`{"assistant_delta":`,
		formatTimestamp(time.Now().UTC()),
	)
	if err != nil {
		t.Fatalf("insert corrupt event: %v", err)
	}

	_, err = store.LoadEventsAfter(context.Background(), "run_bad_event_after", 0)
	if err == nil {
		t.Fatal("expected corrupt payload error, got nil")
	}
	for _, want := range []string{"run_id=run_bad_event_after", "sequence=", "kind=assistant.delta"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want substring %q", err.Error(), want)
		}
	}
}

func TestLoadEventsAfterFailsLoudlyOnBadTimestamp(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.CreateRun(context.Background(), "run_bad_timestamp", "hello"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = store.db.Exec(
		`INSERT INTO events(run_id, kind, payload_json, created_at) VALUES(?, ?, ?, ?)`,
		"run_bad_timestamp",
		"assistant.delta",
		`{"assistant_delta":{"delta":"hello"}}`,
		"not-a-timestamp",
	)
	if err != nil {
		t.Fatalf("insert bad timestamp event: %v", err)
	}

	_, err = store.LoadEventsAfter(context.Background(), "run_bad_timestamp", 0)
	if err == nil {
		t.Fatal("expected timestamp parse error, got nil")
	}
	if !strings.Contains(err.Error(), "event.created_at") {
		t.Fatalf("error = %q, want event.created_at context", err.Error())
	}
}
