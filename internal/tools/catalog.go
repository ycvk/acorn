package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/port"
)

// Catalog is the concrete implementation of port.Catalog. It holds normalized
// tool specs indexed by name for fast lookup.
type Catalog struct {
	specs  []port.ToolSpec
	byName map[string]port.ToolSpec
}

// compile-time assertion that *Catalog satisfies port.Catalog.
var _ port.Catalog = (*Catalog)(nil)

func NewCatalog(ctx context.Context, specs []port.ToolSpec) (*Catalog, error) {
	normalized := make([]port.ToolSpec, 0, len(specs))
	byName := make(map[string]port.ToolSpec, len(specs))
	for _, spec := range specs {
		current, err := normalizeSpec(ctx, spec)
		if err != nil {
			return nil, err
		}
		if _, exists := byName[current.Name]; exists {
			return nil, fmt.Errorf("duplicate capability name %q", current.Name)
		}
		normalized = append(normalized, current)
		byName[current.Name] = current
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return normalized[i].Name < normalized[j].Name
	})
	return &Catalog{specs: normalized, byName: byName}, nil
}

func (c *Catalog) Specs() []port.ToolSpec {
	if c == nil || len(c.specs) == 0 {
		return nil
	}
	out := make([]port.ToolSpec, len(c.specs))
	copy(out, c.specs)
	return out
}

func (c *Catalog) EnabledSpecs() []port.ToolSpec {
	if c == nil || len(c.specs) == 0 {
		return nil
	}
	out := make([]port.ToolSpec, 0, len(c.specs))
	for _, spec := range c.specs {
		if !spec.Enabled() {
			continue
		}
		out = append(out, spec)
	}
	return out
}

func (c *Catalog) Tools() []einotool.BaseTool {
	specs := c.EnabledSpecs()
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

func (c *Catalog) Find(name string) (port.ToolSpec, bool) {
	if c == nil {
		return port.ToolSpec{}, false
	}
	spec, ok := c.byName[strings.TrimSpace(name)]
	return spec, ok
}

func (c *Catalog) ExecutionPolicy(toolName string, args map[string]any) (port.ToolExecutionPolicy, error) {
	spec, ok := c.Find(toolName)
	if !ok {
		return port.ToolExecutionPolicy{}, fmt.Errorf("tool execution policy for %q is not registered", strings.TrimSpace(toolName))
	}
	if err := spec.Validate(); err != nil {
		return port.ToolExecutionPolicy{}, err
	}
	return spec.Execution, nil
}

func normalizeSpec(ctx context.Context, spec port.ToolSpec) (port.ToolSpec, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Source = strings.TrimSpace(spec.Source)
	if spec.Health.State == "" {
		if spec.Tool != nil {
			spec.Health = port.ToolHealth{State: port.HealthStateHealthy}
		} else {
			spec.Health = port.ToolHealth{State: port.HealthStateDisabled, Reason: "tool implementation missing"}
		}
	}
	if spec.Tool != nil {
		info, err := spec.Tool.Info(ctx)
		if err != nil {
			return port.ToolSpec{}, fmt.Errorf("read tool info for %q: %w", spec.Source, err)
		}
		if info == nil {
			return port.ToolSpec{}, fmt.Errorf("tool info for %q is nil", spec.Source)
		}
		actualName := strings.TrimSpace(info.Name)
		if spec.Name == "" {
			spec.Name = actualName
		} else if actualName != "" && actualName != spec.Name {
			return port.ToolSpec{}, fmt.Errorf("tool spec name %q does not match tool info %q", spec.Name, actualName)
		}
	}
	if spec.Name == "" {
		return port.ToolSpec{}, fmt.Errorf("tool spec from %s has empty name", spec.Source)
	}
	spec.ToolContract = spec.ToolContract.Normalized()
	if err := spec.Validate(); err != nil {
		return port.ToolSpec{}, err
	}
	if spec.Enabled() && spec.Tool == nil {
		return port.ToolSpec{}, fmt.Errorf("enabled tool spec %q is missing tool implementation", spec.Name)
	}
	return spec, nil
}
