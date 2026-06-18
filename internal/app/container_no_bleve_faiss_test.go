//go:build !bleve_faiss || !vectors || !cgo

package app

import (
	"context"
	"errors"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/memorymodule"
)

// TestNewContainerStartsWithoutBleveFAISS verifies semantic deps are lazy: even
// in a non-FAISS build, container construction (and thus pair/doctor/serve
// startup) succeeds. FAISS is only required at the first real semantic query.
func TestNewContainerStartsWithoutBleveFAISS(t *testing.T) {
	cfg := testContainerConfig(t)
	cfg.Memory.Semantic = config.MemorySemanticConfig{
		Bleve: config.BleveSemanticConfig{IndexName: "test"},
		Embedding: config.EmbeddingProviderConfig{
			Provider:       "openai_compatible",
			Model:          "text-embedding-3-small",
			BaseURL:        "https://api.openai.com/v1",
			APIKey:         "test-key",
			Dimensions:     3,
			TimeoutSeconds: 30,
			BatchSize:      2,
		},
	}
	c, err := NewContainer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewContainer should succeed (semantic deps are lazy): %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
}

// TestBuildBleveSemanticIndexFailsWithoutFAISS verifies the FAISS requirement is
// still fail-loud — just deferred to the point of actual index construction.
func TestBuildBleveSemanticIndexFailsWithoutFAISS(t *testing.T) {
	cfg := testContainerConfig(t)
	_, err := buildBleveSemanticIndex(context.Background(), cfg)
	if !errors.Is(err, memorymodule.ErrBleveFAISSSupportNotBuilt) {
		t.Fatalf("buildBleveSemanticIndex error = %v, want ErrBleveFAISSSupportNotBuilt", err)
	}
}
