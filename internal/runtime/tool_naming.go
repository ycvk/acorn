package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const (
	// mcpNamespacePrefix is the prefix added to all MCP-sourced tool names
	// exposed to the LLM. Follows the Claude Code / OpenAI Codex convention.
	mcpNamespacePrefix = "mcp__"

	// mcpNamespaceSep separates the provider name and the original tool name
	// in the namespaced tool name.
	mcpNamespaceSep = "__"
)

// mcpToolName constructs the namespaced tool name for an MCP-sourced tool.
// Format: mcp__{provider}__{tool}
// The provider name is sanitized to be alphanumeric+dashes only.
func mcpToolName(provider, toolName string) string {
	return mcpNamespacePrefix + sanitizeMCPProviderName(provider) + mcpNamespaceSep + toolName
}

// sanitizeMCPProviderName replaces non-alphanumeric characters with dashes
// and trims leading/trailing dashes, underscores, and dots.
// Falls back to "provider" if the result is empty.
func sanitizeMCPProviderName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	result := strings.Trim(b.String(), "-_ .")
	if result == "" {
		return "provider"
	}
	return result
}

// augmentDescription appends MCP provider attribution to a tool description.
func augmentDescription(desc, provider string) string {
	if strings.TrimSpace(desc) == "" {
		return fmt.Sprintf("Provided by MCP server: %s", provider)
	}
	return desc + fmt.Sprintf("\n\nProvided by MCP server: %s", provider)
}

// mcpNamespacedTool wraps a BaseTool so that Info() returns the namespaced
// (prefixed) tool name and augmented description, while InvokableRun()
// delegates to the original tool which uses the original name for MCP RPC.
type mcpNamespacedTool struct {
	inner         tool.BaseTool
	invokable     tool.InvokableTool
	prefixedName  string
	augmentedDesc string
}

// newMCPNamespacedTool creates a namespace-prefixed wrapper around an MCP tool.
// The LLM sees prefixedName in the tool schema, but tool calls are routed
// through the original inner tool which preserves the original MCP name for
// the tools/call RPC.
func newMCPNamespacedTool(ctx context.Context, inner tool.BaseTool, provider, originalToolName string) (*mcpNamespacedTool, error) {
	invokable, ok := inner.(tool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("MCP tool %q is not invokable", originalToolName)
	}
	info, err := inner.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("read tool info for MCP namespacing: %w", err)
	}
	prefixed := mcpToolName(provider, originalToolName)
	augDesc := augmentDescription(info.Desc, provider)
	return &mcpNamespacedTool{
		inner:         inner,
		invokable:     invokable,
		prefixedName:  prefixed,
		augmentedDesc: augDesc,
	}, nil
}

func (t *mcpNamespacedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := t.inner.Info(ctx)
	if err != nil {
		return nil, err
	}
	return &schema.ToolInfo{
		Name:        t.prefixedName,
		Desc:        t.augmentedDesc,
		ParamsOneOf: info.ParamsOneOf,
	}, nil
}

func (t *mcpNamespacedTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	return t.invokable.InvokableRun(ctx, argumentsInJSON, opts...)
}
