package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInspectGitStatusCleanWorkspace(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	initGitRepo(t, root)

	ws, err := New(Config{RootDir: root})
	if err != nil {
		t.Fatalf("new workspace: %v", err)
	}

	status, err := ws.InspectGitStatus(context.Background(), "")
	if err != nil {
		t.Fatalf("InspectGitStatus: %v", err)
	}
	if !status.Clean {
		t.Fatalf("Clean = false, want true: %+v", status)
	}
	if status.WorkspaceRoot != root {
		t.Fatalf("WorkspaceRoot = %q, want %q", status.WorkspaceRoot, root)
	}
}

func TestInspectGitStatusDirtyWorkspaceAndScopedPath(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	initGitRepo(t, root)

	trackedPath := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(trackedPath, []byte("tracked"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "seed")

	if err := os.WriteFile(trackedPath, []byte("tracked changed"), 0o644); err != nil {
		t.Fatalf("mutate tracked file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	ws, err := New(Config{RootDir: root})
	if err != nil {
		t.Fatalf("new workspace: %v", err)
	}

	status, err := ws.InspectGitStatus(context.Background(), "")
	if err != nil {
		t.Fatalf("InspectGitStatus(all): %v", err)
	}
	if status.Clean {
		t.Fatalf("Clean = true, want false: %+v", status)
	}
	if len(status.Entries) < 2 {
		t.Fatalf("entry count = %d, want at least 2", len(status.Entries))
	}

	scoped, err := ws.InspectGitStatus(context.Background(), "nested/new.txt")
	if err != nil {
		t.Fatalf("InspectGitStatus(scoped): %v", err)
	}
	if scoped.Clean {
		t.Fatalf("scoped Clean = true, want false: %+v", scoped)
	}
	if len(scoped.Entries) != 1 {
		t.Fatalf("scoped entry count = %d, want 1", len(scoped.Entries))
	}
	if scoped.Entries[0].Path != "nested/new.txt" {
		t.Fatalf("scoped path = %q, want nested/new.txt", scoped.Entries[0].Path)
	}
}

func initGitRepo(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "Acorn Test")
	runGit(t, root, "config", "user.email", "acorn@example.com")
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(output))
	}
}
