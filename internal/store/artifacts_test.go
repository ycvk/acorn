package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestArtifactServiceWriteReadRangeAndList(t *testing.T) {
	ctx := context.Background()
	store := newMemoryArtifactStore()
	root := t.TempDir()
	service, err := NewArtifactService(root, store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	createdAt := time.Unix(1_710_000_000, 0).UTC()
	record, err := service.WriteArtifact(ctx, core.ArtifactWriteRequest{
		ArtifactID:          "artifact_1",
		RunID:               "run_1",
		SessionID:           "session_1",
		SourceToolResultRef: "tool_result:run_1:call_1",
		Kind:                string(ArtifactKindLog),
		Title:               "stdout",
		MIMEType:            "text/plain",
		Content:             []byte("abcdef"),
		CreatedAt:           createdAt,
	})
	if err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if got, want := record.SizeBytes, int64(6); got != want {
		t.Fatalf("size = %d, want %d", got, want)
	}
	if got, want := record.SHA256, sha256Hex([]byte("abcdef")); got != want {
		t.Fatalf("sha256 = %q, want %q", got, want)
	}

	firstRange, err := service.ReadArtifactRange(ctx, core.ArtifactReadRangeRequest{ArtifactID: record.ArtifactID, Offset: 2, Limit: 3})
	if err != nil {
		t.Fatalf("read range: %v", err)
	}
	if got, want := string(firstRange.Content), "cde"; got != want {
		t.Fatalf("range content = %q, want %q", got, want)
	}
	if firstRange.EOF {
		t.Fatal("range should not be EOF")
	}

	finalRange, err := service.ReadArtifactRange(ctx, core.ArtifactReadRangeRequest{ArtifactID: record.ArtifactID, Offset: 5, Limit: 10})
	if err != nil {
		t.Fatalf("read final range: %v", err)
	}
	if got, want := string(finalRange.Content), "f"; got != want {
		t.Fatalf("final range content = %q, want %q", got, want)
	}
	if !finalRange.EOF {
		t.Fatal("final range should be EOF")
	}

	byRun, err := service.ListByRun(ctx, "run_1")
	if err != nil {
		t.Fatalf("list by run: %v", err)
	}
	if len(byRun) != 1 || byRun[0].ArtifactID != record.ArtifactID {
		t.Fatalf("by run = %#v", byRun)
	}
	bySession, err := service.ListBySession(ctx, "session_1")
	if err != nil {
		t.Fatalf("list by session: %v", err)
	}
	if len(bySession) != 1 || bySession[0].ArtifactID != record.ArtifactID {
		t.Fatalf("by session = %#v", bySession)
	}
}

func TestNormalizeArtifactRecordRejectsUnsafeRelativePath(t *testing.T) {
	_, err := NormalizeArtifactRecord(core.ArtifactRecord{
		ArtifactID:   "artifact_1",
		RunID:        "run_1",
		Kind:         string(ArtifactKindText),
		RelativePath: "../escape",
		SizeBytes:    1,
		SHA256:       sha256Hex([]byte("x")),
	})
	if err == nil {
		t.Fatal("expected unsafe relative path error")
	}
}

type memoryArtifactStore struct {
	records map[string]core.ArtifactRecord
}

func newMemoryArtifactStore() *memoryArtifactStore {
	return &memoryArtifactStore{records: make(map[string]core.ArtifactRecord)}
}

func (s *memoryArtifactStore) SaveArtifact(_ context.Context, record core.ArtifactRecord) (core.ArtifactRecord, error) {
	normalized, err := NormalizeArtifactRecord(record)
	if err != nil {
		return core.ArtifactRecord{}, err
	}
	s.records[normalized.ArtifactID] = normalized
	return normalized, nil
}

func (s *memoryArtifactStore) LoadArtifact(_ context.Context, artifactID string) (core.ArtifactRecord, error) {
	record, ok := s.records[artifactID]
	if !ok {
		return core.ArtifactRecord{}, ErrArtifactNotFound
	}
	return record, nil
}

func (s *memoryArtifactStore) ListArtifactsByRun(_ context.Context, runID string) ([]core.ArtifactRecord, error) {
	var items []core.ArtifactRecord
	for _, record := range s.records {
		if record.RunID == runID {
			items = append(items, record)
		}
	}
	return items, nil
}

func (s *memoryArtifactStore) ListArtifactsBySession(_ context.Context, sessionID string) ([]core.ArtifactRecord, error) {
	var items []core.ArtifactRecord
	for _, record := range s.records {
		if record.SessionID == sessionID {
			items = append(items, record)
		}
	}
	return items, nil
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
