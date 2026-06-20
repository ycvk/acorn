package toolset

import (
	"context"
	"encoding/json"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/memorymodule"
)

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
