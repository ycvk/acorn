package core

import (
	"context"

	einotool "github.com/cloudwego/eino/components/tool"
)

// RunContext carries the identifying triple for a single run in the execution
// tree. It is passed to ToolFactory invocations so factories can attribute
// artifacts, evidence, and events to the correct run/session/turn.
type RunContext struct {
	RunID     string
	SessionID string
	TurnIndex int
}

// ToolFactory is the unified constructor signature for both native and MCP tools.
// When a ToolSpec.Factory is non-nil, the runtime calls it (lazily) to produce
// the concrete einotool.BaseTool instance for that run context.
type ToolFactory func(ctx context.Context, runCtx RunContext) (einotool.BaseTool, error)

// MCPToolSpecBuilder translates a discovered MCP tool (provider + tool) into a
// core.ToolSpec for registration in the unified ToolRegistry. It is supplied by
// the runtime — which owns MCP namespacing, parallel-policy resolution, and
// description augmentation — so the mcp.Manager can register tools without
// taking a config/runtime dependency. The namespaced tool name returned in the
// spec MUST match what the capability catalog expects (see runtime.mcpToolName).
// Returning an error aborts registration of that single tool without failing the
// whole provider; the caller decides whether to surface it.
type MCPToolSpecBuilder func(ctx context.Context, providerName string, tool einotool.BaseTool) (ToolSpec, error)

// ToolRegistry is the writable tool registry: it extends the read-only Catalog
// with registration, removal, and lazy resolution of tool specs.
type ToolRegistry interface {
	Catalog
	Register(spec ToolSpec) error
	Unregister(name string) error
	// Resolve returns concrete tool instances for the given names.
	// Tools not found are silently skipped; the caller checks len(result) vs len(names).
	Resolve(ctx context.Context, runCtx RunContext, names []string) ([]einotool.BaseTool, error)
}

// ProviderRegistry manages the lifecycle of MCP providers.
type ProviderRegistry interface {
	RegisterProvider(config ProviderConfig) error
	UnregisterProvider(name string) error
	GetProvider(name string) (ProviderInfo, bool)
	ListProviders() []ProviderInfo
	// Reconcile applies a new provider config set to the live registry.
	Reconcile(ctx context.Context, configs []ProviderConfig) error
}
