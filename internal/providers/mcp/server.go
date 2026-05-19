package mcpprovider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
)

// serverBlockedTools is a compile-time set of tools that MUST NEVER be exposed
// via the MCP server, even if explicitly listed in the allowlist. Per D-11,
// these tools (run_command, create_file, replace_span, apply_unified_patch,
// memory_query) are hardcoded as non-exposable because they grant arbitrary
// code execution, file mutation,
// or access to private operator memory.
var serverBlockedTools = map[string]bool{
	"run_command":         true,
	"create_file":         true,
	"replace_span":        true,
	"apply_unified_patch": true,
	"memory_query":        true,
}

// NewMCPServer creates an MCP server that exposes a curated subset of tools
// to external clients. Per D-10, no tools are exposed if the allowlist is
// empty. Per D-11, blocked tools are never exposed regardless of allowlist.
func NewMCPServer(ctx context.Context, cfg config.ServeConfig, tools []einotool.BaseTool) (*mcp.Server, error) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "acorn",
		Version: "v0.1.0",
	}, nil)

	if len(cfg.Tools.Allowlist) == 0 {
		return server, nil
	}

	allowSet := make(map[string]bool, len(cfg.Tools.Allowlist))
	for _, name := range cfg.Tools.Allowlist {
		if serverBlockedTools[name] {
			return nil, fmt.Errorf("serve.tools.allowlist contains blocked tool %q", name)
		}
		allowSet[name] = true
	}

	registered := make(map[string]bool, len(allowSet))
	for _, tool := range tools {
		info, err := tool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read MCP server tool info: %w", err)
		}

		if !allowSet[info.Name] {
			continue
		}

		inputSchema, err := buildInputSchema(info)
		if err != nil {
			return nil, fmt.Errorf("build input schema for MCP server tool %q: %w", info.Name, err)
		}
		mcpTool := &mcp.Tool{
			Name:        info.Name,
			Description: info.Desc,
			InputSchema: inputSchema,
		}
		server.AddTool(mcpTool, wrapToolForMCPServer(tool))
		registered[info.Name] = true
	}

	for name := range allowSet {
		if !registered[name] {
			return nil, fmt.Errorf("serve.tools.allowlist references unavailable tool %q", name)
		}
	}

	return server, nil
}

// buildInputSchema constructs a JSON Schema input definition for the MCP tool
// from the Eino ToolInfo. The MCP SDK requires a non-nil InputSchema with type
// "object". We use ParamsOneOf.ToJSONSchema() when available, which produces a
// proper jsonschema.Schema that marshals to valid JSON Schema. Tools without
// ParamsOneOf are represented as an empty object schema.
func buildInputSchema(info *schema.ToolInfo) (any, error) {
	if info.ParamsOneOf != nil {
		js, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			return nil, err
		}
		if js == nil {
			return nil, fmt.Errorf("empty JSON schema")
		}
		// The Eino jsonschema.Schema implements json.Marshaler, so it will
		// produce valid JSON Schema when the MCP SDK marshals it. However,
		// the MCP SDK's Server.AddTool validates that InputSchema marshals
		// to a JSON object with type "object". The Eino schema's Type field
		// uses a custom string type; marshal it to raw JSON first so the MCP
		// SDK's remarshal check can read it correctly.
		raw, err := json.Marshal(js)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(raw), nil
	}

	return json.RawMessage(`{"type":"object"}`), nil
}

// wrapToolForMCPServer wraps a BaseTool into an mcp.ToolHandler.
// It marshals the request arguments to JSON, calls InvokableRun, and
// returns the result as a CallToolResult.
func wrapToolForMCPServer(tool einotool.BaseTool) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invokable, ok := tool.(einotool.InvokableTool)
		if !ok {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: tool does not implement InvokableTool"}},
				IsError: true,
			}, nil
		}

		argsJSON, err := json.Marshal(req.Params.Arguments)
		if err != nil {
			return nil, fmt.Errorf("marshal tool arguments: %w", err)
		}

		result, err := invokable.InvokableRun(ctx, string(argsJSON))
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("error: %s", err)}},
				IsError: true,
			}, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: result}},
		}, nil
	}
}
