package toolfactory

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

func BuildMemoryFileTools(ctx context.Context, memory memorymodule.Service) ([]einotool.BaseTool, error) {
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
	}, nil, nil, nil)
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

type memorySearchInput struct {
	Query           string   `json:"query,omitempty"`
	Scope           string   `json:"scope,omitempty"`
	Kinds           []string `json:"kinds,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	IncludeInactive bool     `json:"include_inactive,omitempty"`
	IncludeRetired  bool     `json:"include_retired,omitempty"`
	Explain         bool     `json:"explain,omitempty"`
}

type memorySearchOutput struct {
	Items   []memorySearchOutputItem `json:"items,omitempty"`
	Explain string                   `json:"explain,omitempty"`
}

type memorySearchOutputItem struct {
	Ref         string   `json:"ref,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Title       string   `json:"title,omitempty"`
	Content     string   `json:"content,omitempty"`
	Score       float64  `json:"score,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	ValidFrom   string   `json:"valid_from,omitempty"`
	ValidUntil  string   `json:"valid_until,omitempty"`
	SourceRunID string   `json:"source_run_id,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func memorySearchOutputItemFromSearchItem(item memorymodule.SearchItem) memorySearchOutputItem {
	return memorySearchOutputItem{
		Ref:         item.Ref,
		Kind:        item.Kind,
		Title:       item.Title,
		Content:     item.Snippet,
		Score:       item.Score,
		CreatedAt:   item.Created,
		UpdatedAt:   item.Updated,
		ValidFrom:   item.ValidFrom,
		ValidUntil:  item.ValidUntil,
		SourceRunID: item.SourceRun,
		Tags:        append([]string(nil), item.Tags...),
	}
}

type memorySearchTool struct {
	infoSource einotool.BaseTool
	memory     memorymodule.Service
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
		if result.Explain != nil {
			explainBody, err := json.Marshal(result.Explain)
			if err == nil {
				output.Explain = string(explainBody)
			}
		}
	}
	body, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("marshal memory_search result: %w", err)
	}
	return string(body), nil
}

func parseMemorySearchKinds(values []string) ([]memorymodule.Kind, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]memorymodule.Kind, 0, len(values))
	for _, v := range values {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "fact":
			result = append(result, memorymodule.KindFact)
		case "skill":
			result = append(result, memorymodule.KindSkill)
		case "history":
			result = append(result, memorymodule.KindHistory)
		default:
			return nil, fmt.Errorf("unknown memory search kind %q", v)
		}
	}
	return result, nil
}

type memoryNamespacedTool struct {
	inner        einotool.BaseTool
	invokable    einotool.InvokableTool
	memory       memorymodule.Service
	name         string
	description  string
	originalName string
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
		description:  strings.TrimSpace(info.Desc + "\n\nThis tool operates on Acorn's memory root, not the workspace."),
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
	if result != nil && result.MutationPlan != nil && result.MutationPlan.Action == memorymodule.MemoryMutationRejectInvalid {
		return "", fmt.Errorf("memory mutation rejected: %s", result.MutationPlan.Reason)
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal memory_create_file result: %w", err)
	}
	return string(body), nil
}

func (t *memoryNamespacedTool) runMemoryReplaceSpan(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	var input tools.ReplaceSpanInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse memory_replace_span arguments: %w", err)
	}
	existingBytes, err := os.ReadFile(filepath.Join(t.memory.Root(), input.Path))
	if err != nil {
		return "", fmt.Errorf("read existing file for replace_span: %w", err)
	}
	existing := string(existingBytes)
	replaced, err := applyLineRangeReplacement(existing, input.StartLine, input.EndLine, input.Replacement)
	if err != nil {
		return "", fmt.Errorf("apply line range replacement: %w", err)
	}
	result, err := t.memory.ApplyMemoryMutation(ctx, memorymodule.PlanMemoryMutationRequest{
		Path:    input.Path,
		Content: replaced,
	})
	if err != nil {
		return "", err
	}
	if result != nil && result.MutationPlan != nil && result.MutationPlan.Action == memorymodule.MemoryMutationRejectInvalid {
		return "", fmt.Errorf("memory mutation rejected: %s", result.MutationPlan.Reason)
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal memory_replace_span result: %w", err)
	}
	return string(body), nil
}

func applyLineRangeReplacement(content string, startLine, endLine int, replacement string) (string, error) {
	lines := strings.Split(content, "\n")
	if startLine < 1 || startLine > len(lines) {
		return "", fmt.Errorf("start_line %d out of range (1-%d)", startLine, len(lines))
	}
	if endLine < 1 || endLine > len(lines) {
		return "", fmt.Errorf("end_line %d out of range (1-%d)", endLine, len(lines))
	}
	if endLine < startLine {
		return "", fmt.Errorf("end_line %d < start_line %d", endLine, startLine)
	}
	var result []string
	result = append(result, lines[:startLine-1]...)
	result = append(result, strings.Split(replacement, "\n")...)
	result = append(result, lines[endLine:]...)
	return strings.Join(result, "\n"), nil
}
