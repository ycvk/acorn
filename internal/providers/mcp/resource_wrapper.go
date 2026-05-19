package mcpprovider

import (
	"context"
	"encoding/json"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listResourcesTool implements einotool.InvokableTool to list resources
// available from an MCP provider's session.
type listResourcesTool struct {
	session      *mcp.ClientSession
	providerName string
}

func (t *listResourcesTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "mcp__" + sanitizeProviderName(t.providerName) + "__list_resources",
		Desc: "List resources available from MCP provider " + t.providerName,
	}, nil
}

func (t *listResourcesTool) InvokableRun(ctx context.Context, _ string, _ ...einotool.Option) (string, error) {
	if t.session == nil {
		return "", fmt.Errorf("MCP provider %s: no session for list_resources", t.providerName)
	}
	result, err := t.session.ListResources(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("list resources from MCP provider %s: %w", t.providerName, err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal list_resources result: %w", err)
	}
	return string(data), nil
}

// readResourceTool implements einotool.InvokableTool to read a specific
// resource by URI from an MCP provider's session.
type readResourceTool struct {
	session      *mcp.ClientSession
	providerName string
}

func (t *readResourceTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "mcp__" + sanitizeProviderName(t.providerName) + "__read_resource",
		Desc: "Read a resource from MCP provider " + t.providerName + ". Provide 'uri' in arguments.",
	}, nil
}

func (t *readResourceTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	if t.session == nil {
		return "", fmt.Errorf("MCP provider %s: no session for read_resource", t.providerName)
	}
	var args struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("parse read_resource arguments: %w", err)
	}
	if args.URI == "" {
		return "", fmt.Errorf("MCP provider %s: uri is required for read_resource", t.providerName)
	}
	result, err := t.session.ReadResource(ctx, &mcp.ReadResourceParams{URI: args.URI})
	if err != nil {
		return "", fmt.Errorf("read resource from MCP provider %s: %w", t.providerName, err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal read_resource result: %w", err)
	}
	return string(data), nil
}

// buildResourceTools creates the list_resources and read_resource tool instances
// for a given provider session.
func buildResourceTools(session *mcp.ClientSession, providerName string) []einotool.BaseTool {
	return []einotool.BaseTool{
		&listResourcesTool{session: session, providerName: providerName},
		&readResourceTool{session: session, providerName: providerName},
	}
}
