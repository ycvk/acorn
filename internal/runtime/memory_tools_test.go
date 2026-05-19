package runtime

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/memorymodule"
)

func TestMemoryCreateFileRejectsInvalidRecordBeforeWrite(t *testing.T) {
	service := newMemoryToolTestService(t)
	tool := memoryToolByName(t, service, "memory_create_file")

	_, err := tool.InvokableRun(context.Background(), memoryCreateFileArgs(t, "facts/workspaces/bad.md", `---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2026-05-18
updated: 2026-05-18
source: old field
---

# Bad Fact

Bad.
`))
	if err == nil || !strings.Contains(err.Error(), "memory mutation rejected") || !strings.Contains(err.Error(), "field source not found") {
		t.Fatalf("error = %v, want planner rejection", err)
	}
	if _, err := os.Stat(filepath.Join(service.Root(), "facts", "workspaces", "bad.md")); !os.IsNotExist(err) {
		t.Fatalf("invalid memory file was written or stat failed: %v", err)
	}
}

func TestMemoryCreateFileReturnsMutationPlan(t *testing.T) {
	service := newMemoryToolTestService(t)
	tool := memoryToolByName(t, service, "memory_create_file")

	output, err := tool.InvokableRun(context.Background(), memoryCreateFileArgs(t, "facts/workspaces/new.md", `---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2026-05-18
updated: 2026-05-18
---

# New Fact

New fact.
`))
	if err != nil {
		t.Fatalf("memory_create_file: %v", err)
	}
	var decoded struct {
		MutationPlan memorymodule.MemoryMutationPlan `json:"mutation_plan"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, output)
	}
	if decoded.MutationPlan.Action != memorymodule.MemoryMutationCreate || decoded.MutationPlan.Ref != "facts/workspaces/new.md#new-fact" {
		t.Fatalf("mutation plan = %#v, want create new fact", decoded.MutationPlan)
	}
	if _, err := os.Stat(filepath.Join(service.Root(), "facts", "workspaces", "new.md")); err != nil {
		t.Fatalf("created file missing: %v", err)
	}
}

func TestMemoryReplaceSpanNoopDoesNotWrite(t *testing.T) {
	service := newMemoryToolTestService(t)
	rel := "facts/workspaces/existing.md"
	content := `---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2026-05-18
updated: 2026-05-18
---

# Existing Fact

Existing fact.
`
	writeMemoryToolFile(t, service, rel, content)
	tool := memoryToolByName(t, service, "memory_replace_span")

	args, err := json.Marshal(map[string]any{
		"path":        rel,
		"start_line":  1,
		"end_line":    len(strings.Split(content, "\n")),
		"replacement": content,
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	output, err := tool.InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatalf("memory_replace_span: %v", err)
	}
	var decoded struct {
		Message      string                          `json:"message"`
		MutationPlan memorymodule.MemoryMutationPlan `json:"mutation_plan"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, output)
	}
	if decoded.Message != string(memorymodule.MemoryMutationNoopDuplicate) || decoded.MutationPlan.Action != memorymodule.MemoryMutationNoopDuplicate {
		t.Fatalf("decoded = %#v, want noop_duplicate", decoded)
	}
	body, err := os.ReadFile(filepath.Join(service.Root(), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != content {
		t.Fatalf("content changed:\n%s", string(body))
	}
}

func newMemoryToolTestService(t *testing.T) *memorymodule.LocalService {
	t.Helper()
	root := t.TempDir()
	runMemoryToolGit(t, root, "init")
	runMemoryToolGit(t, root, "config", "user.email", "acorn@example.invalid")
	runMemoryToolGit(t, root, "config", "user.name", "Acorn Test")
	if err := os.WriteFile(filepath.Join(root, ".gitkeep"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("WriteFile .gitkeep: %v", err)
	}
	runMemoryToolGit(t, root, "add", ".gitkeep")
	runMemoryToolGit(t, root, "commit", "-m", "initial")

	service, err := memorymodule.NewLocalService(memorymodule.Config{Root: root})
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}
	if err := service.EnsureLayout(t.Context()); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	if err := service.BuildIndex(t.Context()); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	return service
}

func memoryToolByName(t *testing.T, service memorymodule.Service, name string) einotool.InvokableTool {
	t.Helper()
	items, err := buildMemoryFileTools(t.Context(), service)
	if err != nil {
		t.Fatalf("buildMemoryFileTools: %v", err)
	}
	for _, item := range items {
		info, err := item.Info(t.Context())
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		if info.Name != name {
			continue
		}
		invokable, ok := item.(einotool.InvokableTool)
		if !ok {
			t.Fatalf("%s is not invokable", name)
		}
		return invokable
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func writeMemoryToolFile(t *testing.T, service *memorymodule.LocalService, rel string, content string) {
	t.Helper()
	path := filepath.Join(service.Root(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := service.BuildIndex(t.Context()); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
}

func runMemoryToolGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func memoryCreateFileArgs(t *testing.T, path string, content string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"path":    path,
		"content": content,
	})
	if err != nil {
		t.Fatalf("marshal create args: %v", err)
	}
	return string(body)
}
