package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestStoreArtifactsSaveLoadAndList(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	createdAt := time.Unix(1_710_000_000, 0).UTC()
	record, err := store.SaveArtifact(context.Background(), core.ArtifactRecord{
		ArtifactID:          "artifact_1",
		RunID:               "run_1",
		SessionID:           "session_1",
		SourceToolResultRef: "tool_result:run_1:call_1",
		Kind:                string(ArtifactKindJSON),
		Title:               "verification",
		MIMEType:            "application/json",
		RelativePath:        "runs/run_1/artifact_1",
		SizeBytes:           2,
		SHA256:              testSHA256([]byte("{}")),
		CreatedAt:           createdAt,
	})
	if err != nil {
		t.Fatalf("save artifact: %v", err)
	}
	if got, want := record.CreatedAt, createdAt; !got.Equal(want) {
		t.Fatalf("created_at = %s, want %s", got, want)
	}

	loaded, err := store.LoadArtifact(context.Background(), "artifact_1")
	if err != nil {
		t.Fatalf("load artifact: %v", err)
	}
	if got, want := loaded.SourceToolResultRef, "tool_result:run_1:call_1"; got != want {
		t.Fatalf("source tool result ref = %q, want %q", got, want)
	}

	byRun, err := store.ListByRun(context.Background(), "run_1")
	if err != nil {
		t.Fatalf("list artifacts by run: %v", err)
	}
	if len(byRun) != 1 || byRun[0].ArtifactID != "artifact_1" {
		t.Fatalf("by run = %#v", byRun)
	}
	bySession, err := store.ListBySession(context.Background(), "session_1")
	if err != nil {
		t.Fatalf("list artifacts by session: %v", err)
	}
	if len(bySession) != 1 || bySession[0].ArtifactID != "artifact_1" {
		t.Fatalf("by session = %#v", bySession)
	}
}

func TestStoreArtifactsRejectsInvalidRecord(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.SaveArtifact(context.Background(), core.ArtifactRecord{
		ArtifactID:   "artifact_1",
		RunID:        "run_1",
		Kind:         "bad",
		RelativePath: "runs/run_1/artifact_1",
		SizeBytes:    1,
		SHA256:       testSHA256([]byte("x")),
	})
	if err == nil {
		t.Fatal("expected invalid artifact kind error")
	}
}

func TestStoreArtifactsLoadMissing(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.LoadArtifact(context.Background(), "missing")
	if !errors.Is(err, core.ErrArtifactNotFound) {
		t.Fatalf("load missing err = %v, want core.ErrArtifactNotFound", err)
	}
}

func testSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
