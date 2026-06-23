package runtime

import (
	"context"
	"fmt"
	"strings"

	"encoding/json"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/memorymodule"
)

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

func memorySearchOutputItemFromSearchItem(item memorymodule.SearchItem) memorySearchOutputItem {
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
func buildMemorySearchOutput(result *memorymodule.SearchResult) memorySearchOutput {
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
	body, err := json.Marshal(buildMemorySearchOutput(result))
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
	memory     memorymodule.Service
}

// newMemoryRememberTool builds the structured fact writer exposed to the model.
// The model supplies only title/text/tags/scope; Acorn generates Record V2
// frontmatter and auto-stamps created/updated/status/scope, so the model never
// hand-authors YAML, dates, or status (which previously caused reject loops).
func newMemoryRememberTool(memory memorymodule.Service) (einotool.BaseTool, error) {
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
	record, err := t.memory.CreateFact(ctx, memorymodule.CreateFactRequest{
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
