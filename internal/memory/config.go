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
	return &LocalService{root: abs}, nil
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
