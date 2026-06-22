package toolkit

import (
	"context"
	"fmt"
	"sort"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
)

type ExecutionPolicyResolver interface {
	ExecutionPolicy(toolName string, args map[string]any) (ToolExecutionPolicy, error)
}

type Catalog struct {
	specs  []ToolSpec
	byName map[string]ToolSpec
}

func NewCatalog(ctx context.Context, specs []ToolSpec) (*Catalog, error) {
	normalized := make([]ToolSpec, 0, len(specs))
	byName := make(map[string]ToolSpec, len(specs))
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

func (c *Catalog) Specs() []ToolSpec {
	if c == nil || len(c.specs) == 0 {
		return nil
	}
	out := make([]ToolSpec, len(c.specs))
	copy(out, c.specs)
	return out
}

func (c *Catalog) EnabledSpecs() []ToolSpec {
	if c == nil || len(c.specs) == 0 {
		return nil
	}
	out := make([]ToolSpec, 0, len(c.specs))
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

func (c *Catalog) Find(name string) (ToolSpec, bool) {
	if c == nil {
		return ToolSpec{}, false
	}
	spec, ok := c.byName[strings.TrimSpace(name)]
	return spec, ok
}

func (c *Catalog) ExecutionPolicy(toolName string, args map[string]any) (ToolExecutionPolicy, error) {
	spec, ok := c.Find(toolName)
	if !ok {
		return ToolExecutionPolicy{}, fmt.Errorf("tool execution policy for %q is not registered", strings.TrimSpace(toolName))
	}
	if err := spec.ToolContract.Validate(); err != nil {
		return ToolExecutionPolicy{}, err
	}
	return spec.Execution, nil
}

func normalizeSpec(ctx context.Context, spec ToolSpec) (ToolSpec, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Source = strings.TrimSpace(spec.Source)
	if spec.Health.State == "" {
		if spec.Tool != nil {
			spec.Health = healthyTool("")
		} else {
			spec.Health = disabledTool("tool implementation missing")
		}
	}
	if spec.Tool != nil {
		info, err := spec.Tool.Info(ctx)
		if err != nil {
			return ToolSpec{}, fmt.Errorf("read tool info for %q: %w", spec.Source, err)
		}
		if info == nil {
			return ToolSpec{}, fmt.Errorf("tool info for %q is nil", spec.Source)
		}
		actualName := strings.TrimSpace(info.Name)
		if spec.Name == "" {
			spec.Name = actualName
		} else if actualName != "" && actualName != spec.Name {
			return ToolSpec{}, fmt.Errorf("tool spec name %q does not match tool info %q", spec.Name, actualName)
		}
	}
	if spec.Name == "" {
		return ToolSpec{}, fmt.Errorf("tool spec from %s has empty name", spec.Source)
	}
	spec.ToolContract = spec.ToolContract.normalized()
	if err := spec.ToolContract.Validate(); err != nil {
		return ToolSpec{}, err
	}
	if spec.Enabled() && spec.Tool == nil {
		return ToolSpec{}, fmt.Errorf("enabled tool spec %q is missing tool implementation", spec.Name)
	}
	return spec, nil
}

func errUnknownParallelPolicy(raw string) error {
	return fmt.Errorf("unknown tool parallel policy %q: valid values are readonly|read_only, serial", raw)
}
