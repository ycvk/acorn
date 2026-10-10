package wire

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
	"github.com/ycvk/acorn/internal/store"
)

func ReindexMemory(ctx context.Context, cfg *config.Config) (core.MemoryIndex, error) {
	if err := cfg.ValidateExecutionReady(); err != nil {
		return core.MemoryIndex{}, err
	}
	git, err := knowledge.LookupGit()
	if err != nil {
		return core.MemoryIndex{}, err
	}
	db, err := store.Open(cfg.Runtime.StorageDir, store.OpenOptions{Exclusive: true, SourceReader: &knowledge.SourceReader{Git: git, Dir: cfg.KnowledgeDir()}})
	if err != nil {
		return core.MemoryIndex{}, err
	}
	defer db.Close()
	engine, err := buildMemory(ctx, cfg, db, time.Now, true, nil)
	if err != nil {
		return core.MemoryIndex{}, err
	}
	if err := engine.Reindex(ctx); err != nil {
		return core.MemoryIndex{}, err
	}
	return db.MemoryIndex(ctx)
}
