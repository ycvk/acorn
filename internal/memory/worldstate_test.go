package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorldStateLoadEmptyWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewWorldState(dir)
	if err != nil {
		t.Fatalf("NewWorldState: %v", err)
	}

	state, err := ws.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(state) != 0 {
		t.Fatalf("Load on empty = %v, want empty map", state)
	}
}

func TestWorldStateApplyDeltaUpsertsAndDeletes(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewWorldState(dir)
	if err != nil {
		t.Fatalf("NewWorldState: %v", err)
	}

	// Upsert two keys.
	if err := ws.ApplyDelta(context.Background(), WorldStateDelta{
		Upserts: map[string]string{"unread_emails": "5", "last_deploy": "success"},
	}); err != nil {
		t.Fatalf("ApplyDelta upsert: %v", err)
	}

	state, err := ws.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state["unread_emails"] != "5" {
		t.Fatalf("unread_emails = %q, want '5'", state["unread_emails"])
	}
	if state["last_deploy"] != "success" {
		t.Fatalf("last_deploy = %q, want 'success'", state["last_deploy"])
	}

	// Delete one key, upsert another.
	if err := ws.ApplyDelta(context.Background(), WorldStateDelta{
		Upserts: map[string]string{"pending_approval": "yes"},
		Deletes: []string{"unread_emails"},
	}); err != nil {
		t.Fatalf("ApplyDelta delete: %v", err)
	}

	state, err = ws.Load(context.Background())
	if err != nil {
		t.Fatalf("Load after delete: %v", err)
	}
	if _, exists := state["unread_emails"]; exists {
		t.Fatalf("unread_emails should be deleted, still present: %q", state["unread_emails"])
	}
	if state["pending_approval"] != "yes" {
		t.Fatalf("pending_approval = %q, want 'yes'", state["pending_approval"])
	}
	if state["last_deploy"] != "success" {
		t.Fatalf("last_deploy should survive = %q, want 'success'", state["last_deploy"])
	}
}

func TestWorldStatePersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	ws1, err := NewWorldState(dir)
	if err != nil {
		t.Fatalf("NewWorldState 1: %v", err)
	}
	if err := ws1.ApplyDelta(context.Background(), WorldStateDelta{
		Upserts: map[string]string{"key": "value"},
	}); err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}

	// New instance pointing at the same dir should load persisted state.
	ws2, err := NewWorldState(dir)
	if err != nil {
		t.Fatalf("NewWorldState 2: %v", err)
	}
	state, err := ws2.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state["key"] != "value" {
		t.Fatalf("key = %q, want 'value' (persisted)", state["key"])
	}
}

func TestWorldStateApplyDeltaEmptyIsNoop(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewWorldState(dir)
	if err != nil {
		t.Fatalf("NewWorldState: %v", err)
	}

	// Empty delta should not error or create a file.
	if err := ws.ApplyDelta(context.Background(), WorldStateDelta{}); err != nil {
		t.Fatalf("ApplyDelta empty: %v", err)
	}

	// No file should have been created.
	if _, err := os.Stat(filepath.Join(dir, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("empty delta should not create state.json, got err=%v", err)
	}
}
