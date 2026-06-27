package wire

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ycvk/acorn/internal/memory"
)

// TestTriggerSkipDuplicateWorldState verifies that when the WorldState
// projection hasn't changed since the last fire, the run is skipped (no
// duplicate LLM call for the same world view).
func TestTriggerSkipDuplicateWorldState(t *testing.T) {
	ws, err := memory.NewWorldState(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("NewWorldState: %v", err)
	}
	if err := ws.ApplyDelta(context.Background(), memory.WorldStateDelta{
		Upserts: map[string]string{"status": "ok"},
	}); err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}

	creator := &triggerRunCreator{worldState: ws}

	// First call: should not skip (no previous fingerprint).
	if creator.shouldSkipRun(ws, "event") {
		t.Fatal("first fire should not be skipped")
	}

	// Second call with same state + input: should skip.
	if !creator.shouldSkipRun(ws, "event") {
		t.Fatal("expected run to be skipped when WorldState unchanged")
	}

	// Change WorldState: should NOT skip.
	if err := ws.ApplyDelta(context.Background(), memory.WorldStateDelta{
		Upserts: map[string]string{"status": "changed"},
	}); err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if creator.shouldSkipRun(ws, "event") {
		t.Fatal("expected run NOT to be skipped when WorldState changed")
	}
}

// TestTriggerSkipDifferentInputDoesNotSkip verifies that different input
// (even with same WorldState) triggers a run — the input is part of the
// fingerprint.
func TestTriggerSkipDifferentInputDoesNotSkip(t *testing.T) {
	ws, err := memory.NewWorldState(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("NewWorldState: %v", err)
	}
	if err := ws.ApplyDelta(context.Background(), memory.WorldStateDelta{
		Upserts: map[string]string{"status": "ok"},
	}); err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}

	creator := &triggerRunCreator{worldState: ws}
	creator.shouldSkipRun(ws, "first event") // set fingerprint

	// Same state, different input → should NOT skip.
	if creator.shouldSkipRun(ws, "second event") {
		t.Fatal("expected run NOT to be skipped with different input")
	}
}

// TestTriggerSkipNilWorldStateNeverSkips verifies that a nil WorldState
// (e.g. when WorldState failed to initialize) never skips — always run.
func TestTriggerSkipNilWorldStateNeverSkips(t *testing.T) {
	creator := &triggerRunCreator{}
	if creator.shouldSkipRun(nil, "event") {
		t.Fatal("nil WorldState should never skip")
	}
}

// TestTriggerSkipFingerprintDeterministic verifies that the same WorldState
// produces the same fingerprint regardless of map iteration order. Without
// key sorting, Go's random map iteration would make this test flaky.
func TestTriggerSkipFingerprintDeterministic(t *testing.T) {
	ws, err := memory.NewWorldState(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("NewWorldState: %v", err)
	}
	// Multiple keys to increase chance of random order differing.
	if err := ws.ApplyDelta(context.Background(), memory.WorldStateDelta{
		Upserts: map[string]string{
			"a": "1", "b": "2", "c": "3", "d": "4", "e": "5",
			"f": "6", "g": "7", "h": "8", "i": "9", "j": "10",
		},
	}); err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}

	creator := &triggerRunCreator{worldState: ws}
	// Call shouldSkipRun 100 times with same input. First call sets
	// fingerprint; subsequent calls must all be "skip" (same fp).
	creator.shouldSkipRun(ws, "event")
	for i := 0; i < 100; i++ {
		if !creator.shouldSkipRun(ws, "event") {
			t.Fatalf("iteration %d: fingerprint changed with same state (non-deterministic)", i)
		}
	}
}
