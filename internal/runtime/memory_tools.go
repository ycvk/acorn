package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/workspace"
)

type memoryNamespacedTool struct {
	inner        einotool.BaseTool
	invokable    einotool.InvokableTool
	memory       memorymodule.Service
	name         string
	description  string
	originalName string
}

type memorySearchTool struct {
	infoSource einotool.BaseTool
	memory     memorymodule.Service
}

type memorySearchInput struct {
	Query           string   `json:"query" jsonschema:"description=Natural language query to search Acorn memory records."`
	Scope           string   `json:"scope,omitempty" jsonschema:"description=Optional memory scope such as workspace:acorn. Empty searches all scopes."`
	Kinds           []string `json:"kinds,omitempty" jsonschema:"description=Optional record kinds to include: fact skill history."`
	Limit           int      `json:"limit,omitempty" jsonschema:"description=Maximum number of memory records to return."`
	IncludeInactive bool     `json:"include_inactive,omitempty" jsonschema:"description=Include inactive records."`
	IncludeRetired  bool     `json:"include_retired,omitempty" jsonschema:"description=Include retired records. Also includes inactive records."`
	Explain         bool     `json:"explain,omitempty" jsonschema:"description=Include retrieval scoring explanation."`
}

type memorySearchOutput struct {
	Items   []memorySearchOutputItem    `json:"items"`
	Explain *memorymodule.SearchExplain `json:"explain,omitempty"`
}

type memorySearchOutputItem struct {
	Ref          string                        `json:"ref"`
	Kind         string                        `json:"kind"`
	Title        string                        `json:"title"`
	Status       string                        `json:"status"`
	Scope        string                        `json:"scope,omitempty"`
	Tags         []string                      `json:"tags,omitempty"`
	Origin       string                        `json:"origin,omitempty"`
	TaskPattern  string                        `json:"task_pattern,omitempty"`
	Path         string                        `json:"path,omitempty"`
	Snippet      string                        `json:"snippet,omitempty"`
	Score        float64                       `json:"score"`
	SourceRun    string                        `json:"source_run,omitempty"`
	SourceRefs   []string                      `json:"source_refs,omitempty"`
	EvidenceRefs []string                      `json:"evidence_refs,omitempty"`
	Relations    []memorymodule.RecordRelation `json:"relations,omitempty"`
	Created      string                        `json:"created,omitempty"`
	Updated      string                        `json:"updated,omitempty"`
	ValidFrom    string                        `json:"valid_from,omitempty"`
	ValidUntil   string                        `json:"valid_until,omitempty"`
}

func buildMemoryFileTools(ctx context.Context, memory memorymodule.Service) ([]einotool.BaseTool, error) {
	if memory == nil {
		return nil, fmt.Errorf("memory service is required")
	}
	root := memory.Root()
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		return nil, fmt.Errorf("memory root is required")
	}
	ws, err := workspace.New(workspace.Config{
		RootDir:    trimmedRoot,
		StorageDir: filepath.Join(trimmedRoot, ".index"),
	})
	if err != nil {
		return nil, fmt.Errorf("build memory workspace: %w", err)
	}
	catalog, err := tools.BuildCatalog(tools.CatalogConfig{
		Workspace:       ws,
		MutationEnabled: true,
	}, nil, nil, delegateTaskBridge{})
	if err != nil {
		return nil, fmt.Errorf("build memory tools: %w", err)
	}
	searchTool, err := newMemorySearchTool(memory)
	if err != nil {
		return nil, err
	}
	tools := catalog.Tools
	result := make([]einotool.BaseTool, 0, len(tools)+1)
	result = append(result, searchTool)
	for _, item := range tools {
		wrapped, ok, err := wrapMemoryFileTool(ctx, memory, item)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		result = append(result, wrapped)
	}
	return result, nil
}

func newMemorySearchTool(memory memorymodule.Service) (einotool.BaseTool, error) {
	infoSource, err := toolutils.InferTool("memory_search", "Search Acorn memory records through the canonical semantic retrieval path.", func(ctx context.Context, input memorySearchInput) (memorySearchOutput, error) {
		return memorySearchOutput{}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build memory_search tool: %w", err)
	}
	return &memorySearchTool{infoSource: infoSource, memory: memory}, nil
}

func wrapMemoryFileTool(ctx context.Context, memory memorymodule.Service, inner einotool.BaseTool) (*memoryNamespacedTool, bool, error) {
	info, err := inner.Info(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("read memory tool info: %w", err)
	}
	if info == nil {
		return nil, false, fmt.Errorf("read memory tool info: nil ToolInfo")
	}
	name := strings.TrimSpace(info.Name)
	switch name {
	case "read_file", "list_files", "create_file", "replace_span":
	default:
		return nil, false, nil
	}
	invokable, ok := inner.(einotool.InvokableTool)
	if !ok {
		return nil, false, fmt.Errorf("memory tool %q is not invokable", name)
	}
	return &memoryNamespacedTool{
		inner:        inner,
		invokable:    invokable,
		memory:       memory,
		name:         "memory_" + name,
		description:  strings.TrimSpace(info.Desc + "\n\nThis tool operates on Acorn's memory root, not the project workspace."),
		originalName: name,
	}, true, nil
}

func (t *memoryNamespacedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := t.inner.Info(ctx)
	if err != nil {
		return nil, err
	}
	return &schema.ToolInfo{
		Name:        t.name,
		Desc:        t.description,
		ParamsOneOf: info.ParamsOneOf,
	}, nil
}

func (t *memoryNamespacedTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	switch t.originalName {
	case "create_file":
		return t.runMemoryCreateFile(ctx, argumentsInJSON, opts...)
	case "replace_span":
		return t.runMemoryReplaceSpan(ctx, argumentsInJSON, opts...)
	default:
		return t.invokable.InvokableRun(ctx, argumentsInJSON, opts...)
	}
}

func (t *memorySearchTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.infoSource.Info(ctx)
}

func (t *memorySearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	if t == nil || t.memory == nil {
		return "", fmt.Errorf("memory service is required")
	}
	var input memorySearchInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse memory_search arguments: %w", err)
	}
	kinds, err := parseMemorySearchKinds(input.Kinds)
	if err != nil {
		return "", err
	}
	result, err := t.memory.Search(ctx, memorymodule.SearchRequest{
		Query:           input.Query,
		Scope:           strings.TrimSpace(input.Scope),
		Kinds:           kinds,
		Limit:           input.Limit,
		IncludeInactive: input.IncludeInactive,
		IncludeRetired:  input.IncludeRetired,
		Explain:         input.Explain,
	})
	if err != nil {
		return "", err
	}
	output := memorySearchOutput{}
	if result != nil {
		output.Items = make([]memorySearchOutputItem, 0, len(result.Items))
		for _, item := range result.Items {
			output.Items = append(output.Items, memorySearchOutputItemFromSearchItem(item))
		}
		output.Explain = result.Explain
	}
	body, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("marshal memory_search output: %w", err)
	}
	return string(body), nil
}

func (t *memoryNamespacedTool) runMemoryCreateFile(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	var input tools.CreateFileInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse memory_create_file arguments: %w", err)
	}
	result, err := t.memory.ApplyMemoryMutation(ctx, memorymodule.PlanMemoryMutationRequest{
		Path:    input.Path,
		Content: input.Content,
	})
	if err != nil {
		return "", err
	}
	if result == nil || result.MutationPlan == nil {
		return "", fmt.Errorf("memory_create_file returned no mutation plan")
	}
	if result.MutationPlan.Action == memorymodule.MemoryMutationRejectInvalid {
		return "", fmt.Errorf("memory mutation rejected: %s", result.MutationPlan.Reason)
	}
	if result.MutationPlan.Action != memorymodule.MemoryMutationCreate && result.MutationPlan.Action != memorymodule.MemoryMutationNoopDuplicate {
		return "", fmt.Errorf("memory_create_file cannot execute mutation plan action %q: %s", result.MutationPlan.Action, result.MutationPlan.Reason)
	}
	if result.MutationPlan.Action == memorymodule.MemoryMutationCreate && result.SemanticRebuild == nil {
		return "", fmt.Errorf("memory_create_file requires semantic rebuild after mutation")
	}
	return memoryMutationResultOutput(result)
}

func (t *memoryNamespacedTool) runMemoryReplaceSpan(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	var input tools.ReplaceSpanInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse memory_replace_span arguments: %w", err)
	}
	content, err := finalMemoryReplaceSpanContent(t.memory.Root(), input)
	if err != nil {
		return "", err
	}
	result, err := t.memory.ApplyMemoryMutation(ctx, memorymodule.PlanMemoryMutationRequest{
		Path:    input.Path,
		Content: content,
	})
	if err != nil {
		return "", err
	}
	if result == nil || result.MutationPlan == nil {
		return "", fmt.Errorf("memory_replace_span returned no mutation plan")
	}
	if result.MutationPlan.Action == memorymodule.MemoryMutationRejectInvalid {
		return "", fmt.Errorf("memory mutation rejected: %s", result.MutationPlan.Reason)
	}
	switch result.MutationPlan.Action {
	case memorymodule.MemoryMutationReplaceExisting, memorymodule.MemoryMutationRetireExisting:
		if result.SemanticRebuild == nil {
			return "", fmt.Errorf("memory_replace_span requires semantic rebuild after mutation")
		}
	case memorymodule.MemoryMutationNoopDuplicate:
	default:
		return "", fmt.Errorf("memory_replace_span cannot execute mutation plan action %q: %s", result.MutationPlan.Action, result.MutationPlan.Reason)
	}
	return memoryMutationResultOutput(result)
}

func parseMemorySearchKinds(values []string) ([]memorymodule.Kind, error) {
	kinds := make([]memorymodule.Kind, 0, len(values))
	seen := make(map[memorymodule.Kind]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		kind := memorymodule.Kind(trimmed)
		switch kind {
		case memorymodule.KindFact, memorymodule.KindSkill, memorymodule.KindHistory:
		default:
			return nil, fmt.Errorf("unsupported memory_search kind %q", value)
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		kinds = append(kinds, kind)
	}
	return kinds, nil
}

func memorySearchOutputItemFromSearchItem(item memorymodule.SearchItem) memorySearchOutputItem {
	return memorySearchOutputItem{
		Ref:          item.Ref,
		Kind:         item.Kind,
		Title:        item.Title,
		Status:       item.Status,
		Scope:        item.Scope,
		Tags:         append([]string(nil), item.Tags...),
		Origin:       item.Origin,
		TaskPattern:  item.TaskPattern,
		Path:         item.Path,
		Snippet:      item.Snippet,
		Score:        item.Score,
		SourceRun:    item.SourceRun,
		SourceRefs:   append([]string(nil), item.SourceRefs...),
		EvidenceRefs: append([]string(nil), item.EvidenceRefs...),
		Relations:    append([]memorymodule.RecordRelation(nil), item.Relations...),
		Created:      item.Created,
		Updated:      item.Updated,
		ValidFrom:    item.ValidFrom,
		ValidUntil:   item.ValidUntil,
	}
}

func memoryMutationResultOutput(result *memorymodule.MemoryMutationResult) (string, error) {
	body, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal memory tool output: %w", err)
	}
	return string(body), nil
}

func finalMemoryReplaceSpanContent(root string, input tools.ReplaceSpanInput) (string, error) {
	if strings.TrimSpace(input.Path) == "" {
		return "", fmt.Errorf("path is required")
	}
	if input.StartLine <= 0 || input.EndLine <= 0 {
		return "", fmt.Errorf("start_line and end_line must be > 0")
	}
	resolved, err := resolveMemoryToolPath(root, input.Path)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read memory file %s: %w", resolved, err)
	}
	offsets := memoryLineStartOffsets(body)
	totalLines := len(offsets)
	startLine, endLine, err := normalizeMemoryLineRange(totalLines, input.StartLine, input.EndLine)
	if err != nil {
		return "", err
	}
	startByte := offsets[startLine-1]
	endByte := len(body)
	if endLine < totalLines {
		endByte = offsets[endLine]
	}
	replaced := append([]byte(nil), body[:startByte]...)
	replaced = append(replaced, []byte(input.Replacement)...)
	replaced = append(replaced, body[endByte:]...)
	return string(replaced), nil
}

func resolveMemoryToolPath(root string, path string) (string, error) {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		return "", fmt.Errorf("memory root is required")
	}
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("memory path must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(trimmed))
	if clean == "." || strings.HasPrefix(filepath.ToSlash(clean), "../") || clean == ".." {
		return "", fmt.Errorf("memory path must stay inside the memory root")
	}
	return filepath.Join(trimmedRoot, clean), nil
}

func memoryLineStartOffsets(body []byte) []int {
	offsets := []int{0}
	for i, ch := range body {
		if ch == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

func normalizeMemoryLineRange(totalLines int, startLine int, endLine int) (int, int, error) {
	if totalLines <= 0 {
		totalLines = 1
	}
	if startLine > endLine {
		return 0, 0, fmt.Errorf("start_line %d must be <= end_line %d", startLine, endLine)
	}
	if startLine > totalLines {
		return 0, 0, fmt.Errorf("start_line %d exceeds file line count %d", startLine, totalLines)
	}
	if endLine > totalLines {
		return 0, 0, fmt.Errorf("end_line %d exceeds file line count %d", endLine, totalLines)
	}
	return startLine, endLine, nil
}
