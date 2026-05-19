package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"github.com/ycvk/acorn/internal/tooling"
	workspacepkg "github.com/ycvk/acorn/internal/workspace"
)

func TestBuildCatalogIncludesReadOnlySuiteAndOptionalTools(t *testing.T) {
	ws := testWorkspace(t, t.TempDir())
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		MutationEnabled:   false,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	if got, want := len(catalog.Tools), 6; got != want {
		t.Fatalf("tool count = %d, want %d", got, want)
	}
}

func TestBuildCatalogAllowsEmptyCatalog(t *testing.T) {
	catalog, err := BuildCatalog(CatalogConfig{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("build empty catalog: %v", err)
	}
	if len(catalog.Tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(catalog.Tools))
	}
}

func TestBuildCatalogAppendsExtraTools(t *testing.T) {
	extra, err := toolutils.InferTool("extra_tool", "extra tool", func(ctx context.Context, input map[string]any) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("build extra tool: %v", err)
	}

	catalog, err := BuildCatalog(CatalogConfig{
		Workspace: testWorkspace(t, t.TempDir()),
	}, []einotool.BaseTool{extra}, nil, nil)
	if err != nil {
		t.Fatalf("build catalog with extra tools: %v", err)
	}
	if got, want := len(catalog.Tools), 6; got != want {
		t.Fatalf("expected %d tools, got %d", want, got)
	}
}

func TestReadFileReturnsStructuredLineRange(t *testing.T) {
	root := t.TempDir()
	body := "line 1\nline 2\nline 3\n"
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{Workspace: ws}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "read_file")

	output, err := tool.InvokableRun(context.Background(), `{"path":"notes.txt","start_line":2,"end_line":3}`)
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}

	var decoded ReadFileOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(read_file output): %v\noutput=%s", err, output)
	}
	if decoded.StartLine != 2 || decoded.EndLine != 3 {
		t.Fatalf("range = %d-%d, want 2-3", decoded.StartLine, decoded.EndLine)
	}
	if decoded.Content != "line 2\nline 3\n" {
		t.Fatalf("content = %q", decoded.Content)
	}
}

func TestCreateFileReturnsVerificationPreview(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:       ws,
		MutationEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "create_file")

	output, err := tool.InvokableRun(context.Background(), `{"path":"notes.txt","content":"hello from acorn"}`)
	if err != nil {
		t.Fatalf("create_file: %v", err)
	}

	var decoded CreateFileOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(create_file output): %v\noutput=%s", err, output)
	}
	if decoded.Path != filepath.Join(root, "notes.txt") {
		t.Fatalf("Path = %q, want %q", decoded.Path, filepath.Join(root, "notes.txt"))
	}
	if decoded.VerifiedBytes != len("hello from acorn") {
		t.Fatalf("VerifiedBytes = %d, want %d", decoded.VerifiedBytes, len("hello from acorn"))
	}
	if decoded.VerifiedContent != "hello from acorn" {
		t.Fatalf("VerifiedContent = %q", decoded.VerifiedContent)
	}
	if decoded.VerificationTruncated {
		t.Fatal("VerificationTruncated should be false for short content")
	}
	if decoded.CheckpointID == "" {
		t.Fatal("CheckpointID is required")
	}
	if strings.Join(decoded.CheckpointPaths, ",") != "notes.txt" {
		t.Fatalf("CheckpointPaths = %+v", decoded.CheckpointPaths)
	}
}

func TestRollbackWorkspaceCheckpointRestoresMutationToolCheckpoint(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:       ws,
		MutationEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	createTool := mustToolByName(t, catalog.Tools, "create_file")
	rollbackTool := mustToolByName(t, catalog.Tools, "rollback_workspace_checkpoint")

	output, err := createTool.InvokableRun(context.Background(), `{"path":"notes.txt","content":"hello from acorn"}`)
	if err != nil {
		t.Fatalf("create_file: %v", err)
	}
	var created CreateFileOutput
	if err := json.Unmarshal([]byte(output), &created); err != nil {
		t.Fatalf("json.Unmarshal(create_file output): %v\noutput=%s", err, output)
	}

	rollbackOutput, err := rollbackTool.InvokableRun(context.Background(), `{"checkpoint_id":"`+created.CheckpointID+`"}`)
	if err != nil {
		t.Fatalf("rollback_workspace_checkpoint: %v", err)
	}
	var rolledBack RollbackWorkspaceCheckpointOutput
	if err := json.Unmarshal([]byte(rollbackOutput), &rolledBack); err != nil {
		t.Fatalf("json.Unmarshal(rollback output): %v\noutput=%s", err, rollbackOutput)
	}
	if rolledBack.Status != "succeeded" || strings.Join(rolledBack.RestoredPaths, ",") != "notes.txt" {
		t.Fatalf("unexpected rollback output: %+v", rolledBack)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("notes.txt still exists or stat failed: %v", err)
	}
}

func TestSearchTextReturnsStructuredMatches(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("beta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{Workspace: ws}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "search_text")

	output, err := tool.InvokableRun(context.Background(), `{"query":"beta","limit":10}`)
	if err != nil {
		t.Fatalf("search_text: %v", err)
	}
	var decoded SearchTextOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(search_text output): %v\noutput=%s", err, output)
	}
	if len(decoded.Matches) != 2 {
		t.Fatalf("match count = %d, want 2", len(decoded.Matches))
	}
}

func TestSearchTextEmitsMatchProgress(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{Workspace: ws}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustProgressToolByName(t, catalog.Tools, "search_text")

	var chunks []string
	_, err = tool.InvokableRunWithProgress(context.Background(), `{"query":"beta","limit":10}`, func(_ context.Context, event tooling.ToolProgressEvent) error {
		chunks = append(chunks, event.Delta)
		return nil
	})
	if err != nil {
		t.Fatalf("search_text: %v", err)
	}
	if got := strings.Join(chunks, "\n"); !strings.Contains(got, "a.txt:2:1 beta") {
		t.Fatalf("progress chunks = %#v, want match location", chunks)
	}
}

func TestNativeWorkspaceToolsExposeProgressInterface(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		MutationEnabled:   true,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	for _, name := range []string{
		"read_file",
		"list_files",
		"search_text",
		"create_file",
		"replace_span",
		"apply_unified_patch",
		"rollback_workspace_checkpoint",
		"run_command",
	} {
		mustProgressToolByName(t, catalog.Tools, name)
	}
}

func TestInspectGitStatusReturnsStructuredOutput(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	runGitCommandForTest(t, root, "init")
	runGitCommandForTest(t, root, "config", "user.name", "Acorn Test")
	runGitCommandForTest(t, root, "config", "user.email", "acorn@example.com")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGitCommandForTest(t, root, "add", "tracked.txt")
	runGitCommandForTest(t, root, "commit", "-m", "seed")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{Workspace: ws}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "inspect_git_status")

	output, err := tool.InvokableRun(context.Background(), `{"path":"nested/new.txt"}`)
	if err != nil {
		t.Fatalf("inspect_git_status: %v", err)
	}

	var decoded InspectGitStatusOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(inspect_git_status output): %v\noutput=%s", err, output)
	}
	if decoded.RootPath != root {
		t.Fatalf("RootPath = %q, want %q", decoded.RootPath, root)
	}
	if decoded.Clean {
		t.Fatalf("Clean = true, want false")
	}
	if len(decoded.Entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(decoded.Entries))
	}
	if decoded.Entries[0].Path != "nested/new.txt" {
		t.Fatalf("entry path = %q, want nested/new.txt", decoded.Entries[0].Path)
	}
}

func TestRunCommandReturnsExactFailureTruth(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "run_command")

	output, err := tool.InvokableRun(context.Background(), `{"command":["sh","-lc","printf hi && printf err 1>&2; exit 7"]}`)
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}

	var decoded RunCommandOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(run_command output): %v\noutput=%s", err, output)
	}
	if decoded.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", decoded.ExitCode)
	}
	if decoded.Stdout != "hi" {
		t.Fatalf("Stdout = %q, want hi", decoded.Stdout)
	}
	if strings.TrimSpace(decoded.Stderr) != "err" {
		t.Fatalf("Stderr = %q, want err", decoded.Stderr)
	}
	if decoded.Cwd != root {
		t.Fatalf("Cwd = %q, want %q", decoded.Cwd, root)
	}
}

func TestRunCommandDoesNotRequireCommandNameList(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	ws := testWorkspaceWithConfig(t, workspacepkg.Config{
		RootDir:                  root,
		StorageDir:               t.TempDir(),
		RunCommandDefaultTimeout: 5,
	})

	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "run_command")

	output, err := tool.InvokableRun(context.Background(), `{"command":["git","status","--short"]}`)
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}

	var decoded RunCommandOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(run_command output): %v\noutput=%s", err, output)
	}
	if decoded.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", decoded.ExitCode)
	}
	if decoded.Cwd != root {
		t.Fatalf("Cwd = %q, want %q", decoded.Cwd, root)
	}
}

func TestRunCommandEmitsProgressChunks(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustProgressToolByName(t, catalog.Tools, "run_command")

	var mu sync.Mutex
	var chunks []string
	output, err := tool.InvokableRunWithProgress(context.Background(), `{"command":["sh","-lc","printf out; printf err 1>&2"]}`, func(_ context.Context, event tooling.ToolProgressEvent) error {
		mu.Lock()
		defer mu.Unlock()
		chunks = append(chunks, event.Delta)
		return nil
	})
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	mu.Lock()
	progressText := strings.Join(chunks, "")
	mu.Unlock()
	if !strings.Contains(progressText, "out") || !strings.Contains(progressText, "err") {
		t.Fatalf("progress chunks = %#v, want stdout and stderr", chunks)
	}
	var decoded RunCommandOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(run_command output): %v\noutput=%s", err, output)
	}
	if decoded.Stdout != "out" || decoded.Stderr != "err" {
		t.Fatalf("output = stdout:%q stderr:%q, want out/err", decoded.Stdout, decoded.Stderr)
	}
}

func TestRunCommandCancellationKillsProcessGroup(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("process-group cancellation test only runs on darwin/linux")
	}

	root := t.TempDir()
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "run_command")
	pidFile := filepath.Join(root, "child.pid")

	_, err = tool.InvokableRun(context.Background(), `{"command":["sh","-lc","sleep 30 & child=$!; echo $child > child.pid; wait $child"],"timeout_seconds":1}`)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("timeout error = %v, want deadline exceeded", err)
	}

	body, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		t.Fatalf("parse child pid: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		probeErr := syscall.Kill(childPID, 0)
		if errors.Is(probeErr, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d is still running after run_command cancellation", childPID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRunCommandUsesWhitelistedEnvOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ACORN_ALLOWED", "visible")
	t.Setenv("ACORN_BLOCKED", "hidden")
	ws := testWorkspaceWithConfig(t, workspacepkg.Config{
		RootDir:                  root,
		StorageDir:               t.TempDir(),
		RunCommandDefaultTimeout: 5,
		RunCommandEnvWhitelist:   []string{"ACORN_ALLOWED"},
	})

	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "run_command")

	output, err := tool.InvokableRun(context.Background(), `{"command":["sh","-lc","printf \"%s|%s\" \"$ACORN_ALLOWED\" \"$ACORN_BLOCKED\""]}`)
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}

	var decoded RunCommandOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(run_command output): %v\noutput=%s", err, output)
	}
	if decoded.Stdout != "visible|" {
		t.Fatalf("Stdout = %q, want visible|", decoded.Stdout)
	}
}

func TestRunCommandKeepsInheritedEnvWhenWhitelistEmpty(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ACORN_VISIBLE_WITHOUT_FILTER", "present")
	ws := testWorkspace(t, root)

	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "run_command")

	output, err := tool.InvokableRun(context.Background(), `{"command":["sh","-lc","printf \"%s\" \"$ACORN_VISIBLE_WITHOUT_FILTER\""]}`)
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}

	var decoded RunCommandOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(run_command output): %v\noutput=%s", err, output)
	}
	if decoded.Stdout != "present" {
		t.Fatalf("Stdout = %q, want present", decoded.Stdout)
	}
}

func mustToolByName(t *testing.T, tools []einotool.BaseTool, name string) einotool.InvokableTool {
	t.Helper()
	for _, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool.Info(%q): %v", name, err)
		}
		if info != nil && info.Name == name {
			invokable, ok := tool.(einotool.InvokableTool)
			if !ok {
				t.Fatalf("%s tool is not invokable", name)
			}
			return invokable
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func mustProgressToolByName(t *testing.T, tools []einotool.BaseTool, name string) tooling.ProgressTool {
	t.Helper()
	for _, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool.Info(%q): %v", name, err)
		}
		if info != nil && info.Name == name {
			progress, ok := tool.(tooling.ProgressTool)
			if !ok {
				t.Fatalf("%s tool is not progress-capable", name)
			}
			return progress
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func testWorkspace(t *testing.T, root string) *workspacepkg.Workspace {
	t.Helper()
	return testWorkspaceWithConfig(t, workspacepkg.Config{
		RootDir:                  root,
		StorageDir:               t.TempDir(),
		RunCommandDefaultTimeout: 5,
	})
}

func testWorkspaceWithConfig(t *testing.T, cfg workspacepkg.Config) *workspacepkg.Workspace {
	t.Helper()
	ws, err := workspacepkg.New(cfg)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	return ws
}

func initGitRepoForToolsTest(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	runGitCommandForTest(t, root, "init")
	runGitCommandForTest(t, root, "config", "user.name", "Acorn Test")
	runGitCommandForTest(t, root, "config", "user.email", "acorn@example.com")
	if err := os.WriteFile(filepath.Join(root, ".gitkeep"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("write .gitkeep: %v", err)
	}
	runGitCommandForTest(t, root, "add", ".gitkeep")
	runGitCommandForTest(t, root, "commit", "-m", "seed")
}

func runGitCommandForTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(output))
	}
}
