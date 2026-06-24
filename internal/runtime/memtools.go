package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"

	"encoding/json"
	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/workspace"
	"path/filepath"
)

func BuildMemoryFileTools(ctx context.Context, memory memory.Service) ([]einotool.BaseTool, error) {
	if memory == nil {
		return nil, fmt.Errorf("memory service is required")
	}
	catalog, err := buildMemoryToolCatalog(ctx, memory)
	if err != nil {
		return nil, err
	}
	return collectMemoryFileTools(ctx, memory, catalog)
}

func buildMemoryToolCatalog(ctx context.Context, memory memory.Service) (*tools.LocalCatalog, error) {
	trimmedRoot := strings.TrimSpace(memory.Root())
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
	catalog, err := tools.BuildCatalog(tools.CatalogConfig{Workspace: ws, MutationEnabled: true}, nil)
	if err != nil {
		return nil, fmt.Errorf("build memory tools: %w", err)
	}
	return catalog, nil
}

func collectMemoryFileTools(ctx context.Context, memory memory.Service, catalog *tools.LocalCatalog) ([]einotool.BaseTool, error) {
	searchTool, err := newMemorySearchTool(memory)
	if err != nil {
		return nil, err
	}
	rememberTool, err := newMemoryRememberTool(memory)
	if err != nil {
		return nil, err
	}
	result := []einotool.BaseTool{searchTool, rememberTool}
	for _, item := range catalog.Tools {
		wrapped, ok, err := wrapMemoryFileTool(ctx, memory, item)
		if err != nil {
			return nil, err
		}
		if ok {
			result = append(result, wrapped)
		}
	}
	return result, nil
}

type memoryNamespacedTool struct {
	inner        einotool.BaseTool
	invokable    einotool.InvokableTool
	memory       memory.Service
	name         string
	description  string
	originalName string
}

func wrapMemoryFileTool(ctx context.Context, memory memory.Service, inner einotool.BaseTool) (*memoryNamespacedTool, bool, error) {
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
	return &schema.ToolInfo{Name: t.name, Desc: t.description, ParamsOneOf: info.ParamsOneOf}, nil
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

func (t *memoryNamespacedTool) runMemoryCreateFile(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	var input tools.CreateFileInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse memory_create_file arguments: %w", err)
	}
	result, err := t.memory.ApplyMemoryMutation(ctx, memory.PlanMemoryMutationRequest{Path: input.Path, Content: input.Content})
	if err != nil {
		return "", err
	}
	if rejected := memoryMutationRejection(result); rejected != "" {
		return "", fmt.Errorf("memory mutation rejected: %s", rejected)
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal memory_create_file result: %w", err)
	}
	return string(body), nil
}

func (t *memoryNamespacedTool) runMemoryReplaceSpan(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	var input tools.ReplaceSpanInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse memory_replace_span arguments: %w", err)
	}
	existingBytes, err := os.ReadFile(filepath.Join(t.memory.Root(), input.Path))
	if err != nil {
		return "", fmt.Errorf("read existing file for replace_span: %w", err)
	}
	replaced, err := applyLineRangeReplacement(string(existingBytes), input.StartLine, input.EndLine, input.Replacement)
	if err != nil {
		return "", fmt.Errorf("apply line range replacement: %w", err)
	}
	result, err := t.memory.ApplyMemoryMutation(ctx, memory.PlanMemoryMutationRequest{Path: input.Path, Content: replaced})
	if err != nil {
		return "", err
	}
	if rejected := memoryMutationRejection(result); rejected != "" {
		return "", fmt.Errorf("memory mutation rejected: %s", rejected)
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal memory_replace_span result: %w", err)
	}
	return string(body), nil
}

func memoryMutationRejection(result *memory.MemoryMutationResult) string {
	if result == nil || result.MutationPlan == nil {
		return ""
	}
	if result.MutationPlan.Action == memory.MemoryMutationRejectInvalid {
		return result.MutationPlan.Reason
	}
	return ""
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
	Items   []memorySearchOutputItem `json:"items"`
	Explain *memory.SearchExplain    `json:"explain,omitempty"`
}

type memorySearchOutputItem struct {
	Ref         string   `json:"ref"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Status      string   `json:"status,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Origin      string   `json:"origin,omitempty"`
	TaskPattern string   `json:"task_pattern,omitempty"`
	Path        string   `json:"path,omitempty"`
	Snippet     string   `json:"snippet,omitempty"`
	Score       float64  `json:"score"`
	SourceRun   string   `json:"source_run,omitempty"`
	SourceRefs  []string `json:"source_refs,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
}

func memorySearchOutputItemFromSearchItem(item memory.SearchItem) memorySearchOutputItem {
	return memorySearchOutputItem{
		Ref:         item.Ref,
		Kind:        item.Kind,
		Title:       item.Title,
		Status:      item.Status,
		Scope:       item.Scope,
		Tags:        append([]string(nil), item.Tags...),
		Origin:      item.Origin,
		TaskPattern: item.TaskPattern,
		Path:        item.Path,
		Snippet:     item.Snippet,
		Score:       item.Score,
		SourceRun:   item.SourceRun,
		SourceRefs:  append([]string(nil), item.SourceRefs...),
		CreatedAt:   item.Created,
		UpdatedAt:   item.Updated,
	}
}

func buildMemorySearchOutput(result *memory.SearchResult) memorySearchOutput {
	var output memorySearchOutput
	if result == nil {
		return output
	}
	output.Items = make([]memorySearchOutputItem, 0, len(result.Items))
	for _, item := range result.Items {
		output.Items = append(output.Items, memorySearchOutputItemFromSearchItem(item))
	}
	if result.Explain != nil {
		output.Explain = result.Explain
	}
	return output
}

type memorySearchTool struct {
	infoSource einotool.BaseTool
	memory     memory.Service
}

func newMemorySearchTool(memory memory.Service) (einotool.BaseTool, error) {
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
	result, err := t.memory.Search(ctx, memory.SearchRequest{
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
	body, err := json.Marshal(buildMemorySearchOutput(result))
	if err != nil {
		return "", fmt.Errorf("marshal memory_search result: %w", err)
	}
	return string(body), nil
}

func parseMemorySearchKinds(values []string) ([]memory.Kind, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]memory.Kind, 0, len(values))
	for _, v := range values {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "fact":
			result = append(result, memory.KindFact)
		case "skill":
			result = append(result, memory.KindSkill)
		case "history":
			result = append(result, memory.KindHistory)
		default:
			return nil, fmt.Errorf("unknown memory search kind %q", v)
		}
	}
	return result, nil
}

type memoryRememberInput struct {
	Title string   `json:"title" jsonschema:"description=Short title/heading for the fact to remember."`
	Text  string   `json:"text" jsonschema:"description=The fact body to store in long-term memory."`
	Tags  []string `json:"tags,omitempty" jsonschema:"description=Optional tags to aid later retrieval."`
	Scope string   `json:"scope,omitempty" jsonschema:"description=Optional scope: user (default) or workspace:{slug}."`
}

type memoryRememberOutput struct {
	Ref   string `json:"ref"`
	Path  string `json:"path"`
	Title string `json:"title"`
	Scope string `json:"scope"`
}

type memoryRememberTool struct {
	infoSource einotool.BaseTool
	memory     memory.Service
}

func newMemoryRememberTool(memory memory.Service) (einotool.BaseTool, error) {
	infoSource, err := toolutils.InferTool("remember", "Store a new long-term fact. Provide title and text (and optional tags/scope); Acorn generates the record metadata and timestamps — do not hand-write frontmatter or dates. Use memory_search to recall.", func(ctx context.Context, input memoryRememberInput) (memoryRememberOutput, error) {
		return memoryRememberOutput{}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build remember tool: %w", err)
	}
	return &memoryRememberTool{infoSource: infoSource, memory: memory}, nil
}

func (t *memoryRememberTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.infoSource.Info(ctx)
}

func (t *memoryRememberTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	if t == nil || t.memory == nil {
		return "", fmt.Errorf("memory service is required")
	}
	var input memoryRememberInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse remember arguments: %w", err)
	}
	record, err := t.memory.CreateFact(ctx, memory.CreateFactRequest{
		Title: input.Title,
		Body:  input.Text,
		Tags:  input.Tags,
		Scope: input.Scope,
	})
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(memoryRememberOutput{
		Ref:   record.Ref,
		Path:  record.RelPath,
		Title: record.Title,
		Scope: record.Scope,
	})
	if err != nil {
		return "", fmt.Errorf("marshal remember result: %w", err)
	}
	return string(body), nil
}
