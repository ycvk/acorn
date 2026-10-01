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
