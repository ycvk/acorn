package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listPromptsTool implements einotool.InvokableTool to list prompts
// available from an MCP provider's session.
type listPromptsTool struct {
	session      *mcp.ClientSession
	providerName string
}

func (t *listPromptsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "mcp__" + sanitizeProviderName(t.providerName) + "__list_prompts",
		Desc: "List prompts available from MCP provider " + t.providerName,
	}, nil
}

func (t *listPromptsTool) InvokableRun(ctx context.Context, _ string, _ ...einotool.Option) (string, error) {
	if t.session == nil {
		return "", fmt.Errorf("MCP provider %s: no session for list_prompts", t.providerName)
	}
	result, err := t.session.ListPrompts(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("list prompts from MCP provider %s: %w", t.providerName, err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal list_prompts result: %w", err)
	}
	return string(data), nil
}

// getPromptTool implements einotool.InvokableTool to get a specific prompt
// by name (with optional arguments) from an MCP provider's session.
type getPromptTool struct {
	session      *mcp.ClientSession
	providerName string
}

func (t *getPromptTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "mcp__" + sanitizeProviderName(t.providerName) + "__get_prompt",
		Desc: "Get a prompt from MCP provider " + t.providerName + ". Provide 'name' and optional 'arguments' in JSON input.",
	}, nil
}

func (t *getPromptTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	if t.session == nil {
		return "", fmt.Errorf("MCP provider %s: no session for get_prompt", t.providerName)
	}
	var args struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("parse get_prompt arguments: %w", err)
	}
	if args.Name == "" {
		return "", fmt.Errorf("MCP provider %s: name is required for get_prompt", t.providerName)
	}
	result, err := t.session.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      args.Name,
		Arguments: args.Arguments,
	})
	if err != nil {
		return "", fmt.Errorf("get prompt from MCP provider %s: %w", t.providerName, err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal get_prompt result: %w", err)
	}
	return string(data), nil
}

// buildPromptTools creates the list_prompts and get_prompt tool instances
// for a given provider session.
func buildPromptTools(session *mcp.ClientSession, providerName string) []einotool.BaseTool {
	return []einotool.BaseTool{
		&listPromptsTool{session: session, providerName: providerName},
		&getPromptTool{session: session, providerName: providerName},
	}
}
