package retrievaleval

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileSinkWritesJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "captures", "retrieval.jsonl")
	sink, err := NewFileSink(path)
	if err != nil {
		t.Fatalf("NewFileSink: %v", err)
	}
	sink.now = func() time.Time { return time.Date(2026, 5, 15, 1, 2, 3, 0, time.UTC) }
	if err := sink.Capture(context.Background(), Sample{
		ID:           "sample-1",
		Kind:         KindMemorySearch,
		RunID:        "run_1",
		Query:        "go lint",
		ReturnedRefs: []string{"facts/a.md#a", "facts/a.md#a"},
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("missing jsonl line")
	}
	var sample Sample
	if err := json.Unmarshal(scanner.Bytes(), &sample); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if sample.ID != "sample-1" || sample.Kind != KindMemorySearch || sample.CapturedAt.IsZero() {
		t.Fatalf("sample = %#v", sample)
	}
	if got, want := strings.Join(sample.ReturnedRefs, ","), "facts/a.md#a"; got != want {
		t.Fatalf("returned refs = %q, want %q", got, want)
	}
	if scanner.Scan() {
		t.Fatalf("expected one line, got extra %q", scanner.Text())
	}
}

func TestFileSinkReturnsOpenError(t *testing.T) {
	dir := t.TempDir()
	sink, err := NewFileSink(dir)
	if err != nil {
		t.Fatalf("NewFileSink: %v", err)
	}
	err = sink.Capture(context.Background(), Sample{Kind: KindMemorySearch, Query: "go lint"})
	if err == nil || !strings.Contains(err.Error(), "open capture sink") {
		t.Fatalf("Capture error = %v, want open capture sink error", err)
	}
}

func TestNormalizeSampleRequiresKindAndQuery(t *testing.T) {
	_, err := NormalizeSample(Sample{Kind: KindMemorySearch}, time.Time{})
	if err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("NormalizeSample error = %v, want query required", err)
	}
	_, err = NormalizeSample(Sample{Query: "go lint"}, time.Time{})
	if err == nil || !strings.Contains(err.Error(), "kind is required") {
		t.Fatalf("NormalizeSample error = %v, want kind required", err)
	}
}
