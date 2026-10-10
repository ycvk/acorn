package wire

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

func ReindexMemory(ctx context.Context, cfg *config.Config) (core.MemoryIndex, error) {
	if err := cfg.ValidateExecutionReady(); err != nil {
		return core.MemoryIndex{}, err
	}
	db, err := store.Open(cfg.Runtime.StorageDir, store.OpenOptions{Exclusive: true})
	if err != nil {
		return core.MemoryIndex{}, err
	}
	defer db.Close()
	engine, err := buildMemory(ctx, cfg, db, time.Now, true)
	if err != nil {
		return core.MemoryIndex{}, err
	}
	if err := engine.Reindex(ctx); err != nil {
		return core.MemoryIndex{}, err
	}
	return db.MemoryIndex(ctx)
}
