package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/memorymodule"
)

// buildMemoryService constructs the file-backed memory service.
// Semantic retrieval (embedding + vector store) will be wired in Phase 4.
func buildMemoryService(ctx context.Context, cfg *config.Config) (memorymodule.Service, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	memoryRoot := strings.TrimSpace(cfg.Runtime.StorageDir)
	svc, err := memorymodule.NewLocalService(memorymodule.Config{
		Root: memoryRoot,
	})
	if err != nil {
		return nil, fmt.Errorf("build memory service: %w", err)
	}
	return svc, nil
}
