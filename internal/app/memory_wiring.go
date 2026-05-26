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
	semanticIndex, semanticEmbedder, err := buildMemorySemanticDependencies(ctx, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	if semanticIndex != nil {
		if err := memoryModule.SetSemanticRuntime(memorymodule.SemanticRuntimeOptions{
			Index:      semanticIndex,
			Embedder:   semanticEmbedder,
			Model:      cfg.Memory.Semantic.Embedding.Model,
			Dimensions: cfg.Memory.Semantic.Embedding.Dimensions,
			BatchSize:  cfg.Memory.Semantic.Embedding.BatchSize,
			Schema:     memorymodule.SemanticSchemaMemoryRecordsV1,
			IndexName:  cfg.Memory.Semantic.Bleve.IndexName,
			Mode:       "hybrid",
		}); err != nil {
			return nil, nil, nil, fmt.Errorf("set semantic runtime: %w", err)
		}
	}
	return memoryModule, semanticIndex, semanticEmbedder, nil
}

func buildMemorySemanticDependencies(ctx context.Context, cfg *config.Config) (memorymodule.SemanticIndex, memorymodule.Embedder, error) {
	if cfg == nil {
		return nil, nil, errors.New("config is required")
	}
	if err := cfg.ValidateMemorySemanticReady(); err != nil {
		return nil, nil, err
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
		return nil, nil, fmt.Errorf("build semantic embedder: %w", err)
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
		return nil, nil, fmt.Errorf("build bleve semantic index: %w", err)
	}
	return index, embedder, nil
}
