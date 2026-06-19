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

func TestMemoryRememberCreatesFactWithoutFrontmatter(t *testing.T) {
	service := newMemoryToolTestService(t)
	tool := memoryToolByName(t, service, "remember")

	output, err := tool.InvokableRun(context.Background(), `{"title":"VPS IP","text":"The VPS IP is 1.2.3.4","tags":["infra"]}`)
	if err != nil {
		t.Fatalf("remember error = %v", err)
	}
	var out struct {
		Ref   string `json:"ref"`
		Path  string `json:"path"`
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("decode remember output: %v", err)
	}
	if out.Scope != "user" {
		t.Fatalf("default scope = %q, want user", out.Scope)
	}
	if !strings.HasPrefix(out.Path, "facts/user/") {
		t.Fatalf("remember path = %q, want under facts/user/", out.Path)
	}
	// The file must exist on disk and carry backend-generated, valid frontmatter.
	data, err := os.ReadFile(filepath.Join(service.Root(), filepath.FromSlash(out.Path)))
	if err != nil {
		t.Fatalf("remembered fact not written: %v", err)
	}
	for _, want := range []string{"status: unverified", "created:", "updated:", "scope: user"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("remembered fact missing %q:\n%s", want, string(data))
		}
	}
}

func TestMemoryRememberEquivalentDuplicateSucceeds(t *testing.T) {
	service := newMemoryToolTestService(t)
	tool := memoryToolByName(t, service, "remember")
	args := `{"title":"VPS IP","text":"The VPS IP is 1.2.3.4","tags":["infra"]}`

	first, err := tool.InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatalf("first remember: %v", err)
	}
	second, err := tool.InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatalf("duplicate remember must noop, got error: %v", err)
	}
	var firstOut, secondOut struct {
		Ref  string `json:"ref"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(first), &firstOut); err != nil {
		t.Fatalf("decode first remember output: %v", err)
	}
	if err := json.Unmarshal([]byte(second), &secondOut); err != nil {
		t.Fatalf("decode second remember output: %v", err)
	}
	if firstOut != secondOut {
		t.Fatalf("duplicate remember returned different record: first=%#v second=%#v", firstOut, secondOut)
	}
	facts, err := service.ListFacts(t.Context(), memorymodule.RecordSelection{IncludeInactive: true})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("len(facts) = %d, want 1 after duplicate remember noop", len(facts))
	}
}

func TestMemoryCreateFileReturnsMutationPlan(t *testing.T) {
	service := newMemoryToolTestService(t)
	writeMemoryToolFile(t, service, "facts/workspaces/existing.md", `---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2026-05-18
updated: 2026-05-18
---

# Existing Fact

Existing fact.
`)
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
		MutationPlan    memorymodule.MemoryMutationPlan     `json:"mutation_plan"`
		SemanticRebuild *memorymodule.SemanticRebuildResult `json:"semantic_rebuild"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, output)
	}
	if decoded.MutationPlan.Action != memorymodule.MemoryMutationCreate || decoded.MutationPlan.Ref != "facts/workspaces/new.md#new-fact" {
		t.Fatalf("mutation plan = %#v, want create new fact", decoded.MutationPlan)
	}
	if decoded.SemanticRebuild == nil || decoded.SemanticRebuild.IndexedCount != 2 {
		t.Fatalf("semantic rebuild = %#v, want two indexed records", decoded.SemanticRebuild)
	}
	if _, err := os.Stat(filepath.Join(service.Root(), "facts", "workspaces", "new.md")); err != nil {
		t.Fatalf("created file missing: %v", err)
	}
	facts, err := service.ListFacts(t.Context(), memorymodule.RecordSelection{})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("len(facts) = %d, want 2 after memory_create_file refresh", len(facts))
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

func TestMemoryReplaceSpanRefreshesIndexAndSemanticIndex(t *testing.T) {
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
		"start_line":  11,
		"end_line":    11,
		"replacement": "Updated fact.\n",
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	output, err := tool.InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatalf("memory_replace_span: %v", err)
	}
	var decoded struct {
		MutationPlan    memorymodule.MemoryMutationPlan     `json:"mutation_plan"`
		SemanticRebuild *memorymodule.SemanticRebuildResult `json:"semantic_rebuild"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, output)
	}
	if decoded.MutationPlan.Action != memorymodule.MemoryMutationReplaceExisting {
		t.Fatalf("mutation plan = %#v, want replace_existing", decoded.MutationPlan)
	}
	if decoded.SemanticRebuild == nil || decoded.SemanticRebuild.IndexedCount != 1 {
		t.Fatalf("semantic rebuild = %#v, want one indexed record", decoded.SemanticRebuild)
	}
	record, err := service.GetRecordByRef(t.Context(), "facts/workspaces/existing.md#existing-fact")
	if err != nil {
		t.Fatalf("GetRecordByRef: %v", err)
	}
	if !strings.Contains(record.Body, "Updated fact.") {
		t.Fatalf("record body = %q, want updated body", record.Body)
	}
}

func TestMemorySearchUsesCanonicalSemanticSearch(t *testing.T) {
	service := newMemoryToolTestService(t)
	createTool := memoryToolByName(t, service, "memory_create_file")
	if _, err := createTool.InvokableRun(context.Background(), memoryCreateFileArgs(t, "facts/workspaces/new.md", `---
scope: workspace:acorn
tags: [memory]
status: verified
created: 2026-05-18
updated: 2026-05-18
---

# New Fact

New semantic fact.
`)); err != nil {
		t.Fatalf("memory_create_file: %v", err)
	}
	searchTool := memoryToolByName(t, service, "memory_search")
	output, err := searchTool.InvokableRun(context.Background(), `{"query":"semantic","kinds":["fact"],"limit":5}`)
	if err != nil {
		t.Fatalf("memory_search: %v", err)
	}
	var decoded struct {
		Items []struct {
			Ref  string `json:"ref"`
			Kind string `json:"kind"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, output)
	}
	if len(decoded.Items) != 1 || decoded.Items[0].Ref != "facts/workspaces/new.md#new-fact" || decoded.Items[0].Kind != "fact" {
		t.Fatalf("items = %#v, want new fact", decoded.Items)
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
	if err := service.SetSemanticRuntime(memorymodule.SemanticRuntimeOptions{
		Index:      &memoryToolSemanticIndex{},
		Embedder:   memoryToolEmbedder{model: "test-embedding", dimensions: 3},
		Model:      "test-embedding",
		Dimensions: 3,
		BatchSize:  64,
		Schema:     memorymodule.SemanticSchemaMemoryRecordsV1,
		IndexName:  "memory_records",
		Mode:       "hybrid",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	return service
}

func memoryToolByName(t *testing.T, service memorymodule.Service, name string) einotool.InvokableTool {
	t.Helper()
	items, err := BuildMemoryFileTools(t.Context(), service, nil)
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

type memoryToolEmbedder struct {
	model      string
	dimensions int
}

func (e memoryToolEmbedder) Embed(ctx context.Context, req memorymodule.EmbedRequest) (*memorymodule.EmbedResult, error) {
	vectors := make([]memorymodule.EmbeddingVector, 0, len(req.Inputs))
	for i, input := range req.Inputs {
		values := make([]float32, e.dimensions)
		for j := range values {
			values[j] = float32(i + j + 1)
		}
		vectors = append(vectors, memorymodule.EmbeddingVector{Ref: input.Ref, Values: values})
	}
	return &memorymodule.EmbedResult{Model: e.model, Dimensions: e.dimensions, Vectors: vectors}, nil
}

type memoryToolSemanticIndex struct {
	records []memorymodule.IndexedSemanticRecord
}

func (i *memoryToolSemanticIndex) Rebuild(ctx context.Context, req memorymodule.SemanticRebuildRequest) (*memorymodule.SemanticRebuildResult, error) {
	i.records = append([]memorymodule.IndexedSemanticRecord(nil), req.Records...)
	return &memorymodule.SemanticRebuildResult{
		Model:        req.Model,
		Dimensions:   req.Dimensions,
		Schema:       req.Schema,
		IndexName:    req.IndexName,
		IndexedCount: len(req.Records),
	}, nil
}

func (i *memoryToolSemanticIndex) Search(ctx context.Context, req memorymodule.SemanticSearchRequest) (*memorymodule.SemanticSearchResult, error) {
	hits := make([]memorymodule.SemanticHit, 0, len(i.records))
	for _, record := range i.records {
		hits = append(hits, memorymodule.SemanticHit{
			Ref:   record.Record.Ref,
			Kind:  record.Record.Kind,
			Score: 5,
			Stage: "semantic_hybrid",
		})
	}
	return &memorymodule.SemanticSearchResult{Hits: hits}, nil
}

func (i *memoryToolSemanticIndex) Close() error {
	return nil
}
