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

// ToolRegistry is the writable tool registry: it extends the read-only Catalog
// with registration and removal of tool specs.
type ToolRegistry interface {
	Catalog
	Register(spec ToolSpec) error
	Unregister(name string) error
}

// ProviderRegistry manages the lifecycle of MCP providers.
type ProviderRegistry interface {
	RegisterProvider(config ProviderConfig) error
	UnregisterProvider(name string) error
	GetProvider(name string) (ProviderInfo, bool)
	ListProviders() []ProviderInfo
}
