//go:build !bleve_faiss || !vectors || !cgo

package app

import (
	"context"
	"errors"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/memorymodule"
)

func TestNewContainerRequiresBleveFAISSSupport(t *testing.T) {
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
	_, err := NewContainer(context.Background(), cfg)
	if !errors.Is(err, memorymodule.ErrBleveFAISSSupportNotBuilt) {
		t.Fatalf("NewContainer error = %v, want ErrBleveFAISSSupportNotBuilt", err)
	}
}
