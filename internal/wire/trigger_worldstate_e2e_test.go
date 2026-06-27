package wire

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/memory"
)

// TestE2EWorldStateInjection verifies the ambient loop's read path: when a
// trigger fires, the WorldState projection is loaded and prepended to the run
// input. This is the seam that makes WorldState visible to the agent — if it
// breaks, the agent runs blind every trigger cycle.
//
// The full chain tested here:
//
//	WorldState.ApplyDelta (write) → injectWorldState (read + prefix) → input
//
// The write-back half (agent calls worldstate_update) is unit-tested in
// internal/tools/worldstate_tool_test.go. Together they prove the loop closes.
func TestE2EWorldStateInjection(t *testing.T) {
	// Use a real file-backed WorldState in a temp dir.
	ws, err := memory.NewWorldState(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorldState: %v", err)
	}

	// Empty state → no injection.
	got := injectWorldState(context.Background(), ws, "do something")
	if got != "do something" {
		t.Fatalf("empty state should not inject, got %q", got)
	}

	// Write some state — simulates what worldstate_update tool does.
	if err := ws.ApplyDelta(context.Background(), memory.WorldStateDelta{
		Upserts: map[string]string{
			"unread_emails": "5",
			"last_deploy":   "success",
		},
	}); err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}

	// Now injection should prepend the projection.
	got = injectWorldState(context.Background(), ws, "check inbox")
	if !strings.HasPrefix(got, "[World state") {
		t.Fatalf("missing projection header, got:\n%s", got)
	}
	if !strings.Contains(got, "unread_emails: 5") {
		t.Fatalf("missing unread_emails key, got:\n%s", got)
	}
	if !strings.Contains(got, "last_deploy: success") {
		t.Fatalf("missing last_deploy key, got:\n%s", got)
	}
	if !strings.HasSuffix(got, "check inbox") {
		t.Fatalf("original input should be at the end, got:\n%s", got)
	}
}

// TestE2EWorldStateInjectionAfterDelete verifies that deleted keys do not
// appear in the projection — the agent won't see stale state.
func TestE2EWorldStateInjectionAfterDelete(t *testing.T) {
	ws, err := memory.NewWorldState(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("NewWorldState: %v", err)
	}

	if err := ws.ApplyDelta(context.Background(), memory.WorldStateDelta{
		Upserts: map[string]string{"stale_key": "stale_value", "live_key": "live_value"},
	}); err != nil {
		t.Fatalf("ApplyDelta upsert: %v", err)
	}
	if err := ws.ApplyDelta(context.Background(), memory.WorldStateDelta{
		Deletes: []string{"stale_key"},
	}); err != nil {
		t.Fatalf("ApplyDelta delete: %v", err)
	}

	got := injectWorldState(context.Background(), ws, "task")
	if strings.Contains(got, "stale_key") {
		t.Fatalf("deleted key should not appear, got:\n%s", got)
	}
	if !strings.Contains(got, "live_key: live_value") {
		t.Fatalf("live key should appear, got:\n%s", got)
	}
}

// TestE2EWorldStateInjectionNilSafe verifies that a nil WorldState (e.g. when
// triggers are configured but WorldState failed to initialize) does not panic.
func TestE2EWorldStateInjectionNilSafe(t *testing.T) {
	got := injectWorldState(context.Background(), nil, "task")
	if got != "task" {
		t.Fatalf("nil WorldState should return input unchanged, got %q", got)
	}
}
