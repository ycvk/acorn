package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMutationCheckpointRollbackRestoresBeforeState(t *testing.T) {
	ws := newGitWorkspace(t)
	writeWorkspaceFile(t, ws, "target.txt", "original\n")
	gitCommitAll(t, ws.Root(), "initial")

	checkpoint, err := ws.CreateMutationCheckpoint(context.Background(), "replace_span", []string{"target.txt"})
	if err != nil {
		t.Fatalf("CreateMutationCheckpoint: %v", err)
	}
	writeWorkspaceFile(t, ws, "target.txt", "changed\n")
	if _, err := ws.CompleteMutationCheckpoint(context.Background(), checkpoint.CheckpointID); err != nil {
		t.Fatalf("CompleteMutationCheckpoint: %v", err)
	}

	result, err := ws.RollbackMutationCheckpoint(context.Background(), checkpoint.CheckpointID)
	if err != nil {
		t.Fatalf("RollbackMutationCheckpoint: %v result=%+v", err, result)
	}
	if result.Status != "succeeded" || strings.Join(result.RestoredPaths, ",") != "target.txt" {
		t.Fatalf("unexpected rollback result: %+v", result)
	}
	if got := readWorkspaceFile(t, ws, "target.txt"); got != "original\n" {
		t.Fatalf("target content = %q, want original", got)
	}
}

func TestMutationCheckpointRollbackDeletesCreatedFile(t *testing.T) {
	ws := newGitWorkspace(t)
	writeWorkspaceFile(t, ws, "README.md", "root\n")
	gitCommitAll(t, ws.Root(), "initial")

	checkpoint, err := ws.CreateMutationCheckpoint(context.Background(), "create_file", []string{"created.txt"})
	if err != nil {
		t.Fatalf("CreateMutationCheckpoint: %v", err)
	}
	writeWorkspaceFile(t, ws, "created.txt", "new\n")
	if _, err := ws.CompleteMutationCheckpoint(context.Background(), checkpoint.CheckpointID); err != nil {
		t.Fatalf("CompleteMutationCheckpoint: %v", err)
	}
	if _, err := ws.RollbackMutationCheckpoint(context.Background(), checkpoint.CheckpointID); err != nil {
		t.Fatalf("RollbackMutationCheckpoint: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws.Root(), "created.txt")); !os.IsNotExist(err) {
		t.Fatalf("created file still exists or stat failed: %v", err)
	}
}

func TestMutationCheckpointRollbackHandlesIncompleteCheckpoint(t *testing.T) {
	ws := newGitWorkspace(t)
	writeWorkspaceFile(t, ws, "target.txt", "original\n")
	gitCommitAll(t, ws.Root(), "initial")

	checkpoint, err := ws.CreateMutationCheckpoint(context.Background(), "replace_span", []string{"target.txt"})
	if err != nil {
		t.Fatalf("CreateMutationCheckpoint: %v", err)
	}
	writeWorkspaceFile(t, ws, "target.txt", "changed\n")

	result, err := ws.RollbackMutationCheckpoint(context.Background(), checkpoint.CheckpointID)
	if err != nil {
		t.Fatalf("RollbackMutationCheckpoint: %v result=%+v", err, result)
	}
	if result.Status != "succeeded" || strings.Join(result.RestoredPaths, ",") != "target.txt" {
		t.Fatalf("unexpected rollback result: %+v", result)
	}
	if got := readWorkspaceFile(t, ws, "target.txt"); got != "original\n" {
		t.Fatalf("target content = %q, want original", got)
	}
}

func TestMutationCheckpointRollbackDetectsTouchedPathConflict(t *testing.T) {
	ws := newGitWorkspace(t)
	writeWorkspaceFile(t, ws, "target.txt", "original\n")
	gitCommitAll(t, ws.Root(), "initial")

	checkpoint, err := ws.CreateMutationCheckpoint(context.Background(), "replace_span", []string{"target.txt"})
	if err != nil {
		t.Fatalf("CreateMutationCheckpoint: %v", err)
	}
	writeWorkspaceFile(t, ws, "target.txt", "changed\n")
	if _, err := ws.CompleteMutationCheckpoint(context.Background(), checkpoint.CheckpointID); err != nil {
		t.Fatalf("CompleteMutationCheckpoint: %v", err)
	}
	writeWorkspaceFile(t, ws, "target.txt", "changed again\n")

	result, err := ws.RollbackMutationCheckpoint(context.Background(), checkpoint.CheckpointID)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if result == nil || strings.Join(result.ConflictPaths, ",") != "target.txt" {
		t.Fatalf("unexpected conflict result: %+v err=%v", result, err)
	}
	if got := readWorkspaceFile(t, ws, "target.txt"); got != "changed again\n" {
		t.Fatalf("rollback should not mutate conflicted file, got %q", got)
	}
}

func TestMutationCheckpointRollbackDetectsNonBaselineDirtyConflict(t *testing.T) {
	ws := newGitWorkspace(t)
	writeWorkspaceFile(t, ws, "target.txt", "original\n")
	writeWorkspaceFile(t, ws, "baseline.txt", "base\n")
	gitCommitAll(t, ws.Root(), "initial")
	writeWorkspaceFile(t, ws, "baseline.txt", "baseline dirty\n")

	checkpoint, err := ws.CreateMutationCheckpoint(context.Background(), "replace_span", []string{"target.txt"})
	if err != nil {
		t.Fatalf("CreateMutationCheckpoint: %v", err)
	}
	writeWorkspaceFile(t, ws, "target.txt", "changed\n")
	if _, err := ws.CompleteMutationCheckpoint(context.Background(), checkpoint.CheckpointID); err != nil {
		t.Fatalf("CompleteMutationCheckpoint: %v", err)
	}
	writeWorkspaceFile(t, ws, "unrelated.txt", "new dirty\n")

	result, err := ws.RollbackMutationCheckpoint(context.Background(), checkpoint.CheckpointID)
	if err == nil {
		t.Fatal("expected unrelated dirty conflict")
	}
	if result == nil || strings.Join(result.ConflictPaths, ",") != "unrelated.txt" {
		t.Fatalf("unexpected conflict result: %+v err=%v", result, err)
	}
}

func TestLoadMutationCheckpointFailsOnCorruptPayload(t *testing.T) {
	ws := newGitWorkspace(t)
	path, err := ws.mutationCheckpointPath("workspace_checkpoint_corrupt")
	if err != nil {
		t.Fatalf("mutationCheckpointPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"checkpoint_id":`), 0o600); err != nil {
		t.Fatalf("write corrupt checkpoint: %v", err)
	}
	_, err = ws.LoadMutationCheckpoint("workspace_checkpoint_corrupt")
	if err == nil || !strings.Contains(err.Error(), "decode workspace mutation checkpoint") {
		t.Fatalf("error = %v, want decode error", err)
	}
}

func newGitWorkspace(t *testing.T) *Workspace {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	root := t.TempDir()
	runGitForCheckpointTest(t, root, "init")
	runGitForCheckpointTest(t, root, "config", "user.email", "acorn@example.com")
	runGitForCheckpointTest(t, root, "config", "user.name", "Acorn Test")
	ws, err := New(Config{
		RootDir:    root,
		StorageDir: filepath.Join(t.TempDir(), "state"),
	})
	if err != nil {
		t.Fatalf("New workspace: %v", err)
	}
	return ws
}

func writeWorkspaceFile(t *testing.T, ws *Workspace, path string, content string) {
	t.Helper()
	resolved, err := ws.ResolveWritePath(path)
	if err != nil {
		t.Fatalf("ResolveWritePath(%s): %v", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readWorkspaceFile(t *testing.T, ws *Workspace, path string) string {
	t.Helper()
	resolved, err := ws.ResolveReadPath(path)
	if err != nil {
		t.Fatalf("ResolveReadPath(%s): %v", path, err)
	}
	body, err := os.ReadFile(resolved)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func gitCommitAll(t *testing.T, root string, message string) {
	t.Helper()
	runGitForCheckpointTest(t, root, "add", ".")
	runGitForCheckpointTest(t, root, "commit", "-m", message)
}

func runGitForCheckpointTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(output))
	}
}
