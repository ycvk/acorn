package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateChildWorktreeCreatesIndependentWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	runGitWorktreeTest(t, root, "init")
	runGitWorktreeTest(t, root, "config", "user.email", "test@example.com")
	runGitWorktreeTest(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("root\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitWorktreeTest(t, root, "add", "README.md")
	runGitWorktreeTest(t, root, "commit", "-m", "seed")

	ws, err := New(Config{RootDir: root, StorageDir: filepath.Join(t.TempDir(), ".acorn")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	worktree, err := ws.CreateChildWorktree(context.Background(), "run:child/1")
	if err != nil {
		t.Fatalf("CreateChildWorktree: %v", err)
	}
	if !strings.Contains(worktree.WorktreeID, "run_child_1") {
		t.Fatalf("WorktreeID = %q", worktree.WorktreeID)
	}
	rootEval, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(root): %v", err)
	}
	if worktree.Path == "" || filepath.Clean(worktree.SourceRoot) != filepath.Clean(rootEval) || worktree.Commit == "" {
		t.Fatalf("worktree = %+v", worktree)
	}
	if _, err := os.Stat(filepath.Join(worktree.Path, "README.md")); err != nil {
		t.Fatalf("stat worktree README: %v", err)
	}

	childWorkspace, err := ws.OpenWorktree(worktree)
	if err != nil {
		t.Fatalf("OpenWorktree: %v", err)
	}
	if childWorkspace.Root() != worktree.Path {
		t.Fatalf("child root = %q, want %q", childWorkspace.Root(), worktree.Path)
	}
	if err := os.WriteFile(filepath.Join(childWorkspace.Root(), "child.txt"), []byte("child\n"), 0o644); err != nil {
		t.Fatalf("write child file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "child.txt")); !os.IsNotExist(err) {
		t.Fatalf("child file should stay out of source root, err=%v", err)
	}
}

func runGitWorktreeTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}
