package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const worktreeDirName = "worktrees"

type WorkspaceWorktree struct {
	WorktreeID string    `json:"worktree_id"`
	Path       string    `json:"path"`
	SourceRoot string    `json:"source_root"`
	Commit     string    `json:"commit"`
	CreatedAt  time.Time `json:"created_at"`
}

func (w *Workspace) CreateChildWorktree(ctx context.Context, worktreeID string) (*WorkspaceWorktree, error) {
	if w == nil {
		return nil, errors.New("workspace is required")
	}
	id := sanitizeWorktreeID(worktreeID)
	if id == "" {
		return nil, errors.New("worktree id is required")
	}
	if strings.TrimSpace(w.storageDir) == "" {
		return nil, errors.New("workspace storage dir is required for child worktrees")
	}
	sourceRoot, err := w.gitTopLevel(ctx)
	if err != nil {
		return nil, err
	}
	rootEval, err := filepath.EvalSymlinks(w.rootDir)
	if err != nil {
		rootEval = w.rootDir
	}
	if filepath.Clean(sourceRoot) != filepath.Clean(rootEval) {
		return nil, fmt.Errorf("workspace root %q is not git top-level %q", rootEval, sourceRoot)
	}
	commit, err := w.gitOutput(ctx, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(w.storageDir, worktreeDirName, id)
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("worktree path already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat worktree path %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("prepare worktree parent: %w", err)
	}
	if _, err := w.gitOutput(ctx, "worktree", "add", "--detach", path, "HEAD"); err != nil {
		_ = os.RemoveAll(path)
		return nil, err
	}
	return &WorkspaceWorktree{
		WorktreeID: id,
		Path:       path,
		SourceRoot: sourceRoot,
		Commit:     strings.TrimSpace(commit),
		CreatedAt:  time.Now().UTC(),
	}, nil
}

func (w *Workspace) OpenWorktree(worktree *WorkspaceWorktree) (*Workspace, error) {
	if w == nil {
		return nil, errors.New("workspace is required")
	}
	if worktree == nil {
		return nil, errors.New("worktree is required")
	}
	if strings.TrimSpace(worktree.Path) == "" {
		return nil, errors.New("worktree path is required")
	}
	return New(Config{
		RootDir:                  worktree.Path,
		StorageDir:               w.storageDir,
		MutationDenylist:         append([]string(nil), w.mutationDenylist...),
		RunCommandDefaultTimeout: w.runCommandDefaultTimeout,
		RunCommandEnvWhitelist:   append([]string(nil), w.runCommandEnvWhitelist...),
	})
}

func (w *Workspace) gitTopLevel(ctx context.Context) (string, error) {
	out, err := w.gitOutput(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Clean(strings.TrimSpace(out)), nil
}

func (w *Workspace) gitOutput(ctx context.Context, args ...string) (string, error) {
	if w == nil {
		return "", errors.New("workspace is required")
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", w.rootDir}, args...)...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func sanitizeWorktreeID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
