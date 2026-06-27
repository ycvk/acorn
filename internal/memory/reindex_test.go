package memory

import (
	"context"
	"strings"
	"testing"
)

// TestReindexEmbeddingsNotEnabled verifies that reindex returns a clear error
// when embedding is not configured, rather than silently doing nothing.
func TestReindexEmbeddingsNotEnabled(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.ReindexEmbeddings(context.Background())
	if err == nil {
		t.Fatal("expected error when embedding is not enabled")
	}
	if !strings.Contains(err.Error(), "embedding is not enabled") {
		t.Fatalf("expected 'embedding is not enabled' error, got: %v", err)
	}
}
