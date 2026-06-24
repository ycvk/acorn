package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
)

// ResolvingToolRegistry extends the read-write core.ToolRegistry with lazy
// tool resolution: Resolve invokes each registered spec's Factory under a run
// context to produce concrete einotool.BaseTool instances on demand. The
// runtime uses this to build per-run tool sets from names.
type ResolvingToolRegistry interface {
	core.ToolRegistry
	Resolve(ctx context.Context, runCtx core.RunContext, names []string) ([]einotool.BaseTool, error)
}

// toolRegistry is the concrete implementation of ResolvingToolRegistry. It
// holds core.ToolSpec entries indexed by name under a mutex. Specs are the
// source of truth for metadata; concrete einotool.BaseTool instances are
// produced lazily via each spec's Factory (see Resolve).
//
// It deliberately mirrors the existing port-backed Catalog shape (Catalog in
// catalog.go) so the runtime can adopt the registry as a drop-in replacement
// without re-learning the access patterns.
type toolRegistry struct {
	mu     sync.RWMutex
	byName map[string]core.ToolSpec
}

// NewToolRegistry returns an empty, ready-to-use registry. The return type is
// ResolvingToolRegistry (which embeds core.ToolRegistry) so callers that only
// need the read-write catalog interface still get it, while the runtime can
// use Resolve for lazy tool construction.
func NewToolRegistry() ResolvingToolRegistry {
	return &toolRegistry{byName: make(map[string]core.ToolSpec)}
}

// compile-time assertion that *toolRegistry satisfies ResolvingToolRegistry.
var _ ResolvingToolRegistry = (*toolRegistry)(nil)

// Register stores spec under its (normalized) name. A spec whose name is empty
// or that already exists is rejected. The spec's Factory is kept as-is so later
// Resolve calls can construct the concrete tool on demand; eager tools with a
// pre-built Tool field are also supported.
func (r *toolRegistry) Register(spec core.ToolSpec) error {
	if r == nil {
		return fmt.Errorf("tool registry: receiver is nil")
	}
	spec = normalizeCoreSpec(spec)
	if spec.Name == "" {
		return fmt.Errorf("tool registry: spec name is required")
	}
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("tool registry: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[spec.Name]; exists {
		return fmt.Errorf("tool registry: tool %q is already registered", spec.Name)
	}
	r.byName[spec.Name] = spec
	return nil
}

// Unregister removes the spec identified by name. Trimming and an empty map are
// handled gracefully; unknown names return an error so callers can distinguish
// "removed" from "was never there".
func (r *toolRegistry) Unregister(name string) error {
	if r == nil {
		return fmt.Errorf("tool registry: receiver is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("tool registry: name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[name]; !exists {
		return fmt.Errorf("tool registry: tool %q is not registered", name)
	}
	delete(r.byName, name)
	return nil
}

// Specs returns all registered specs sorted by name (deterministic output).
// It returns nil for a nil or empty registry to match Catalog conventions.
func (r *toolRegistry) Specs() []core.ToolSpec {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.byName) == 0 {
		return nil
	}
	out := make([]core.ToolSpec, 0, len(r.byName))
	for _, spec := range r.byName {
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// EnabledSpecs returns the subset of specs whose Health.State is not disabled,
// sorted by name.
func (r *toolRegistry) EnabledSpecs() []core.ToolSpec {
	specs := r.Specs()
	if len(specs) == 0 {
		return nil
	}
	out := make([]core.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.Enabled() {
			out = append(out, spec)
		}
	}
	return out
}

// Tools returns the pre-built einotool.BaseTool instances held by enabled specs.
// Tools registered with only a Factory (no Tool) are skipped here; use Resolve
// to construct those lazily.
func (r *toolRegistry) Tools() []einotool.BaseTool {
	specs := r.EnabledSpecs()
	if len(specs) == 0 {
		return nil
	}
	out := make([]einotool.BaseTool, 0, len(specs))
	for _, spec := range specs {
		if spec.Tool == nil {
			continue
		}
		out = append(out, spec.Tool)
	}
	return out
}

// Find returns the spec registered under name. The name is trimmed; unknown
// names return ok=false.
func (r *toolRegistry) Find(name string) (core.ToolSpec, bool) {
	if r == nil {
		return core.ToolSpec{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.byName[strings.TrimSpace(name)]
	return spec, ok
}

// ExecutionPolicy returns the execution policy for the named tool after
// validating its contract. Unknown tools and invalid contracts surface as
// errors, matching Catalog.ExecutionPolicy semantics.
func (r *toolRegistry) ExecutionPolicy(toolName string, args map[string]any) (core.ToolExecutionPolicy, error) {
	spec, ok := r.Find(toolName)
	if !ok {
		return core.ToolExecutionPolicy{}, fmt.Errorf("tool execution policy for %q is not registered", strings.TrimSpace(toolName))
	}
	if err := spec.Validate(); err != nil {
		return core.ToolExecutionPolicy{}, err
	}
	return spec.Execution, nil
}

// Resolve produces concrete einotool.BaseTool instances for the requested names
// by invoking each spec's Factory under the given run context. Names that are
// not registered are skipped (not an error): the runtime requests tool sets by
// name and tolerates providers that were never registered. A spec whose Factory
// is nil but whose Tool is pre-built returns that Tool instance instead. A
// factory that returns (nil, nil) — e.g. when its backing service is absent —
// is also skipped, so callers receive only usable tool instances.
func (r *toolRegistry) Resolve(ctx context.Context, runCtx core.RunContext, names []string) ([]einotool.BaseTool, error) {
	if r == nil {
		return nil, nil
	}
	out := make([]einotool.BaseTool, 0, len(names))
	for _, name := range names {
		spec, ok := r.Find(name)
		if !ok {
			continue
		}
		if spec.Factory == nil {
			if spec.Tool != nil {
				out = append(out, spec.Tool)
			}
			continue
		}
		tool, err := spec.Factory(ctx, runCtx)
		if err != nil {
			return nil, fmt.Errorf("tool registry: resolve %q: %w", name, err)
		}
		// A factory may return (nil, nil) when its backing service is absent
		// (e.g. no workspace configured). Skip nil instances so callers receive
		// only usable tools, matching the existing Catalog's silent-omit behavior.
		if tool == nil {
			continue
		}
		out = append(out, tool)
	}
	return out, nil
}

// normalizeCoreSpec trims the human-editable string fields of a core.ToolSpec
// the same way the existing port-backed normalizeSpec trims a core.ToolSpec,
// without invoking the tool (which requires a concrete instance).
func normalizeCoreSpec(spec core.ToolSpec) core.ToolSpec {
	spec.ToolContract = spec.ToolContract.Normalized()
	if spec.Health.State == "" {
		if spec.Tool != nil || spec.Factory != nil {
			spec.Health = core.ToolHealth{State: core.HealthStateHealthy}
		} else {
			spec.Health = core.ToolHealth{State: core.HealthStateDisabled, Reason: "tool implementation missing"}
		}
	}
	return spec
}
