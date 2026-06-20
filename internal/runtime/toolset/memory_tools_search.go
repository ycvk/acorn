package toolset

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	Ref          string                        `json:"ref"`
	Kind         string                        `json:"kind"`
	Title        string                        `json:"title"`
	Status       string                        `json:"status,omitempty"`
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
	CreatedAt    string                        `json:"created_at,omitempty"`
	UpdatedAt    string                        `json:"updated_at,omitempty"`
	ValidFrom    string                        `json:"valid_from,omitempty"`
	ValidUntil   string                        `json:"valid_until,omitempty"`
	Relations    []memorymodule.RecordRelation `json:"relations,omitempty"`
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
		CreatedAt:    item.Created,
		UpdatedAt:    item.Updated,
		ValidFrom:    item.ValidFrom,
		ValidUntil:   item.ValidUntil,
		Relations:    append([]memorymodule.RecordRelation(nil), item.Relations...),
	}
}

func buildMemorySearchOutput(result *memorymodule.SearchResult) memorySearchOutput {
	output := memorySearchOutput{}
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
