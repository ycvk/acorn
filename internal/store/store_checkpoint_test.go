package store

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func TestCheckpointRoundTripSurvivesReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	ctx := context.Background()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, ok, err := s.LoadCheckpoint(ctx, "run_1"); err != nil || ok {
		t.Fatalf("missing checkpoint: ok=%v err=%v", ok, err)
	}
	if err := s.SaveCheckpoint(ctx, "run_1", []byte("v1")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.SaveCheckpoint(ctx, "run_1", []byte("v2")); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s, err = Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	data, ok, err := s.LoadCheckpoint(ctx, "run_1")
	if err != nil || !ok || !bytes.Equal(data, []byte("v2")) {
		t.Fatalf("after reopen: data=%q ok=%v err=%v", data, ok, err)
	}
	if err := s.DeleteCheckpoint(ctx, "run_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, err := s.LoadCheckpoint(ctx, "run_1"); err != nil || ok {
		t.Fatalf("after delete: ok=%v err=%v", ok, err)
	}
}
