package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceWriteReadRangeVerifyAndList(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	root := t.TempDir()
	service, err := NewService(root, store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	createdAt := time.Unix(1_710_000_000, 0).UTC()
	record, err := service.Write(ctx, WriteRequest{
		ArtifactID:          "artifact_1",
		RunID:               "run_1",
		SessionID:           "session_1",
		SourceToolResultRef: "tool_result:run_1:call_1",
		Kind:                KindLog,
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

	firstRange, err := service.ReadRange(ctx, ReadRangeRequest{ArtifactID: record.ArtifactID, Offset: 2, Limit: 3})
	if err != nil {
		t.Fatalf("read range: %v", err)
	}
	if got, want := string(firstRange.Content), "cde"; got != want {
		t.Fatalf("range content = %q, want %q", got, want)
	}
	if firstRange.EOF {
		t.Fatal("range should not be EOF")
	}

	finalRange, err := service.ReadRange(ctx, ReadRangeRequest{ArtifactID: record.ArtifactID, Offset: 5, Limit: 10})
	if err != nil {
		t.Fatalf("read final range: %v", err)
	}
	if got, want := string(finalRange.Content), "f"; got != want {
		t.Fatalf("final range content = %q, want %q", got, want)
	}
	if !finalRange.EOF {
		t.Fatal("final range should be EOF")
	}

	if _, err := service.Verify(ctx, record.ArtifactID); err != nil {
		t.Fatalf("verify artifact: %v", err)
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

func TestServiceVerifyDetectsTamperedContent(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	root := t.TempDir()
	service, err := NewService(root, store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	record, err := service.Write(ctx, WriteRequest{
		ArtifactID: "artifact_1",
		RunID:      "run_1",
		Kind:       KindText,
		Content:    []byte("original"),
	})
	if err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(record.RelativePath)), []byte("tampered"), 0o600); err != nil {
		t.Fatalf("tamper content: %v", err)
	}
	if _, err := service.Verify(ctx, record.ArtifactID); err == nil {
		t.Fatal("expected verify error for tampered content")
	}
}

func TestNormalizeRecordRejectsUnsafeRelativePath(t *testing.T) {
	_, err := NormalizeRecord(Record{
		ArtifactID:   "artifact_1",
		RunID:        "run_1",
		Kind:         KindText,
		RelativePath: "../escape",
		SizeBytes:    1,
		SHA256:       sha256Hex([]byte("x")),
	})
	if err == nil {
		t.Fatal("expected unsafe relative path error")
	}
}

type memoryStore struct {
	records map[string]Record
}

func newMemoryStore() *memoryStore {
	return &memoryStore{records: make(map[string]Record)}
}

func (s *memoryStore) SaveArtifact(_ context.Context, record Record) (Record, error) {
	normalized, err := NormalizeRecord(record)
	if err != nil {
		return Record{}, err
	}
	s.records[normalized.ArtifactID] = normalized
	return normalized, nil
}

func (s *memoryStore) LoadArtifact(_ context.Context, artifactID string) (Record, error) {
	record, ok := s.records[artifactID]
	if !ok {
		return Record{}, ErrArtifactNotFound
	}
	return record, nil
}

func (s *memoryStore) ListArtifactsByRun(_ context.Context, runID string) ([]Record, error) {
	var items []Record
	for _, record := range s.records {
		if record.RunID == runID {
			items = append(items, record)
		}
	}
	return items, nil
}

func (s *memoryStore) ListArtifactsBySession(_ context.Context, sessionID string) ([]Record, error) {
	var items []Record
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
