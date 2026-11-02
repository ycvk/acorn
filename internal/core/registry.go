package core

import (
	"context"

	einotool "github.com/cloudwego/eino/components/tool"
)

// ToolFactory is the unified constructor signature for both native and MCP tools.
// When a ToolSpec.Factory is non-nil, the runtime calls it (lazily) to produce
// the concrete einotool.BaseTool instance for a run.
type ToolFactory func(ctx context.Context) (einotool.BaseTool, error)

// ToolRegistry is the writable tool registry: it extends the read-only Catalog
// with registration, removal, and lazy resolution of tool specs.
type ToolRegistry interface {
	Catalog
	Register(spec ToolSpec) error
	Unregister(name string) error
	// Resolve returns concrete tool instances for the given names.
	// Tools not found are silently skipped; the caller checks len(result) vs len(names).
	Resolve(ctx context.Context, names []string) ([]einotool.BaseTool, error)
	// ResolveEnabledSpecs returns every enabled spec with its concrete tool
	// instance populated via Factory. This is the spec-preserving
	// counterpart to Resolve for callers (e.g. the runtime catalog assembler)
	// that need the full ToolSpec, not just the tool instance.
	ResolveEnabledSpecs(ctx context.Context) ([]ToolSpec, error)
}
