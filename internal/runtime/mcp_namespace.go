package runtime

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const (
	mcpNamespacePrefix = "mcp__"
	mcpNamespaceSep    = "__"
)

func mcpToolName(provider, toolName string) string {
	return mcpNamespacePrefix + sanitizeMCPProviderName(provider) + mcpNamespaceSep + toolName
}

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

func augmentDescription(desc, provider string) string {
	if strings.TrimSpace(desc) == "" {
		return fmt.Sprintf("Provided by MCP server: %s", provider)
	}
	return desc + fmt.Sprintf("\n\nProvided by MCP server: %s", provider)
}

type mcpNamespacedTool struct {
	inner         einotool.BaseTool
	invokable     einotool.InvokableTool
	prefixedName  string
	augmentedDesc string
}

func NewMCPNamespacedTool(ctx context.Context, inner einotool.BaseTool, provider, originalToolName string) (*mcpNamespacedTool, error) {
	invokable, ok := inner.(einotool.InvokableTool)
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

func (t *mcpNamespacedTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	return t.invokable.InvokableRun(ctx, argumentsInJSON, opts...)
}
