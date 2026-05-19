package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWritePathRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	ws, err := New(Config{RootDir: root})
	if err != nil {
		t.Fatalf("new workspace: %v", err)
	}

	if _, err := ws.ResolveWritePath("../secret.txt"); err == nil {
		t.Fatal("expected traversal path to fail")
	}
	if _, err := ws.ResolveWritePath("/tmp/secret.txt"); err == nil {
		t.Fatal("expected absolute path to fail")
	}
}

func TestResolveWritePathHonorsNestedDenylist(t *testing.T) {
	root := t.TempDir()
	ws, err := New(Config{
		RootDir:          root,
		MutationDenylist: []string{".config/gh"},
	})
	if err != nil {
		t.Fatalf("new workspace: %v", err)
	}

	if _, err := ws.ResolveWritePath(".config/gh/hosts.yml"); err == nil {
		t.Fatal("expected nested denylist path to fail")
	}
}

func TestResolveReadPathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "links"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(root, "links", "outside")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	ws, err := New(Config{RootDir: root})
	if err != nil {
		t.Fatalf("new workspace: %v", err)
	}
	if _, err := ws.ResolveReadPath("links/outside/file.txt"); err == nil {
		t.Fatal("expected symlink escape to fail")
	}
}

func TestResolveCwdDefaultsToRoot(t *testing.T) {
	root := t.TempDir()
	ws, err := New(Config{RootDir: root})
	if err != nil {
		t.Fatalf("new workspace: %v", err)
	}

	got, err := ws.ResolveCwd("")
	if err != nil {
		t.Fatalf("resolve cwd: %v", err)
	}
	if got != root {
		t.Fatalf("cwd = %q, want %q", got, root)
	}
}

func TestOpenWorktreePropagatesRunCommandSettings(t *testing.T) {
	root := t.TempDir()
	childPath := t.TempDir()
	ws, err := New(Config{
		RootDir:                  root,
		RunCommandDefaultTimeout: 42,
		RunCommandEnvWhitelist:   []string{"PATH", "HOME"},
	})
	if err != nil {
		t.Fatalf("new workspace: %v", err)
	}

	child, err := ws.OpenWorktree(&WorkspaceWorktree{Path: childPath})
	if err != nil {
		t.Fatalf("open worktree: %v", err)
	}
	if got, want := child.RunCommandDefaultTimeout(), 42; got != want {
		t.Fatalf("child run_command timeout = %d, want %d", got, want)
	}
	if got := child.RunCommandEnvWhitelist(); len(got) != 2 || got[0] != "PATH" || got[1] != "HOME" {
		t.Fatalf("child env whitelist = %v, want [PATH HOME]", got)
	}
}
