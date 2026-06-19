package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/memorymodule"
)

func buildMemoryModule(ctx context.Context, cfg *config.Config) (memorymodule.Service, memorymodule.SemanticIndex, memorymodule.Embedder, error) {
	memoryModule, err := memorymodule.NewLocalService(memorymodule.Config{Root: filepath.Join(cfg.Runtime.StorageDir, "memory")})
	if err != nil {
		return nil, nil, nil, err
	}
	if err := memoryModule.EnsureLayout(ctx); err != nil {
		return nil, nil, nil, err
	}
	if err := memoryModule.BuildIndex(ctx); err != nil {
		return nil, nil, nil, err
	}

	// Semantic deps (embedder + FAISS/Bleve index) are constructed lazily on the
	// first real Search/Prepare/Rebuild rather than at container construction, so
	// `acorn pair` / `acorn doctor` / serve startup is never blocked by embedding
	// config or FAISS availability. A real semantic query still constructs them
	// and fails loud (ValidateMemorySemanticReady) if misconfigured. When
	// embedding is not configured at all, no semantic runtime is wired and
	// Search/Prepare fail loud ("semantic search runtime is required") — no silent
	// degradation, but the pair/inbox/approvals control surface stays usable.
	if !semanticConfigured(cfg) {
		return memoryModule, nil, nil, nil
	}

	lazyIndex := newLazySemanticIndex(func(ctx context.Context) (memorymodule.SemanticIndex, error) {
		return buildBleveSemanticIndex(ctx, cfg)
	})
	lazyEmbedder := newLazyEmbedder(func() (memorymodule.Embedder, error) {
		return buildSemanticEmbedder(cfg)
	})
	if err := memoryModule.SetSemanticRuntime(semanticRuntimeOptions(cfg, lazyIndex, lazyEmbedder)); err != nil {
		return nil, nil, nil, fmt.Errorf("set semantic runtime: %w", err)
	}
	return memoryModule, lazyIndex, lazyEmbedder, nil
}

// semanticConfigured reports whether the operator intends to use semantic
// retrieval (embedding base_url/model present). Full validation happens lazily
// at first use via ValidateMemorySemanticReady.
func semanticConfigured(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	embedding := cfg.Memory.Semantic.Embedding
	return strings.TrimSpace(embedding.Model) != "" || strings.TrimSpace(embedding.BaseURL) != ""
}

// semanticRuntimeOptions builds the single SemanticRuntimeOptions from config so
// the config field reads live in one place, shared by the memory module runtime
// (SetSemanticRuntime) and the rebuild-capable MemoryService.
func semanticRuntimeOptions(cfg *config.Config, index memorymodule.SemanticIndex, embedder memorymodule.Embedder) memorymodule.SemanticRuntimeOptions {
	return memorymodule.SemanticRuntimeOptions{
		Index:      index,
		Embedder:   embedder,
		Model:      cfg.Memory.Semantic.Embedding.Model,
		Dimensions: cfg.Memory.Semantic.Embedding.Dimensions,
		BatchSize:  cfg.Memory.Semantic.Embedding.BatchSize,
		Schema:     memorymodule.SemanticSchemaMemoryRecordsV1,
		IndexName:  cfg.Memory.Semantic.Bleve.IndexName,
		Mode:       "hybrid",
	}
}

func buildSemanticEmbedder(cfg *config.Config) (memorymodule.Embedder, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if err := cfg.ValidateMemorySemanticReady(); err != nil {
		return nil, err
	}
	embedding := cfg.Memory.Semantic.Embedding
	embedder, err := memorymodule.NewOpenAICompatibleEmbedder(memorymodule.OpenAICompatibleEmbedderConfig{
		BaseURL:        embedding.BaseURL,
		APIKey:         embedding.APIKey,
		Model:          embedding.Model,
		Dimensions:     embedding.Dimensions,
		TimeoutSeconds: embedding.TimeoutSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("build semantic embedder: %w", err)
	}
	return embedder, nil
}

func buildBleveSemanticIndex(ctx context.Context, cfg *config.Config) (memorymodule.SemanticIndex, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if err := cfg.ValidateMemorySemanticReady(); err != nil {
		return nil, err
	}
	blevePath := strings.TrimSpace(cfg.Memory.Semantic.Bleve.Path)
	if blevePath == "" {
		blevePath = filepath.Join(cfg.Runtime.StorageDir, "bleve-semantic")
	}
	index, err := memorymodule.NewBleveSemanticIndex(ctx, memorymodule.BleveSemanticIndexConfig{
		Path:       blevePath,
		IndexName:  cfg.Memory.Semantic.Bleve.IndexName,
		Dimensions: cfg.Memory.Semantic.Embedding.Dimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("build bleve semantic index: %w", err)
	}
	return index, nil
}
