package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func NewLocalService(cfg Config) (*LocalService, error) {
	root := strings.TrimSpace(cfg.Root)
	if root == "" {
		return nil, fmt.Errorf("memory root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve memory root: %w", err)
	}
	svc := &LocalService{root: abs}

	// Embedding + vector index are optional: when EmbeddingClient is nil,
	// search falls back to keyword-only (the pre-existing path). When
	// configured, the vector index lives in {root}/vectors.db.
	if cfg.Embedding != nil && cfg.Embedding.Enabled() {
		vi, err := NewVectorIndex(abs, cfg.Embedding.Dims())
		if err != nil {
			return nil, fmt.Errorf("create vector index: %w", err)
		}
		svc.embedding = cfg.Embedding
		svc.vectors = vi
	}
	return svc, nil
}

func (s *LocalService) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *LocalService) EnsureLayout(ctx context.Context) error {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return fmt.Errorf("memory service root is required")
	}
	for _, dir := range []string{
		filepath.Join(s.root, "facts", "user"),
		filepath.Join(s.root, "facts", "workspaces"),
		filepath.Join(s.root, "skills", "built-in"),
		filepath.Join(s.root, "skills", "learned"),
		filepath.Join(s.root, "history"),
	} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create memory directory %q: %w", dir, err)
		}
	}
	return nil
}

func (s *LocalService) path(parts ...string) string {
	items := append([]string{s.root}, parts...)
	return filepath.Join(items...)
}

// Close releases the vector index database connection if present.
func (s *LocalService) Close() error {
	if s == nil {
		return nil
	}
	if s.vectors != nil {
		return s.vectors.Close()
	}
	return nil
}
