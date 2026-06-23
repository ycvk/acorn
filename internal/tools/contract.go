package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/config"
)

type ToolLoadingMode string

const (
	ToolLoadingModeEager    ToolLoadingMode = "eager"
	ToolLoadingModeDeferred ToolLoadingMode = "deferred"
	ToolLoadingModeHidden   ToolLoadingMode = "hidden"
)

type ToolLoadingPolicy struct {
	Mode   ToolLoadingMode
	Reason string
}

type ToolExecutionPolicy struct {
	ParallelPolicy ParallelPolicy
	PathArg        string
}

type ToolContract struct {
	Name      string
	Source    string
	Kind      ToolKind
	Category  ToolCategory
	Loading   ToolLoadingPolicy
	Execution ToolExecutionPolicy
}

func (c ToolContract) normalized() ToolContract {
	c.Name = strings.TrimSpace(c.Name)
	c.Source = strings.TrimSpace(c.Source)
	c.Loading.Reason = strings.TrimSpace(c.Loading.Reason)
	c.Execution.PathArg = strings.TrimSpace(c.Execution.PathArg)
	return c
}

func (c ToolContract) Validate() error {
	c = c.normalized()
	if c.Name == "" {
		return fmt.Errorf("tool contract has empty name")
	}
	if c.Source == "" {
		return fmt.Errorf("tool contract %q has empty source", c.Name)
	}
	if c.Kind == "" {
		return fmt.Errorf("tool contract %q has empty kind", c.Name)
	}
	if c.Category == "" {
		return fmt.Errorf("tool contract %q has empty category", c.Name)
	}
	switch c.Loading.Mode {
	case ToolLoadingModeEager, ToolLoadingModeHidden:
	case ToolLoadingModeDeferred:
		if c.Loading.Reason == "" {
			return fmt.Errorf("tool contract %q deferred loading requires reason", c.Name)
		}
	default:
		return fmt.Errorf("tool contract %q has unknown loading mode %q", c.Name, c.Loading.Mode)
	}
	switch c.Execution.ParallelPolicy {
	case ParallelPolicyReadOnly, ParallelPolicySerial:
	default:
		return fmt.Errorf("tool contract %q has unknown parallel policy %q", c.Name, c.Execution.ParallelPolicy)
	}
	return nil
}

func EagerLoadingPolicy() ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeEager}
}

func DeferredLoadingPolicy(reason string) ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeDeferred, Reason: strings.TrimSpace(reason)}
}

type ToolProgressEvent struct {
	Delta string
}

type ToolProgressEmitter func(ctx context.Context, event ToolProgressEvent) error

type ProgressTool interface {
	einotool.BaseTool
	InvokableRunWithProgress(ctx context.Context, argumentsInJSON string, emit ToolProgressEmitter, opts ...einotool.Option) (string, error)
}

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

type ToolKind string

const (
	ToolKindNative ToolKind = "native"
	ToolKindMemory ToolKind = "memory"
	ToolKindSkill  ToolKind = "skill"
	ToolKindMCP    ToolKind = "mcp"
)

type ToolCategory string

const (
	ToolCategoryRead        ToolCategory = "read"
	ToolCategoryWrite       ToolCategory = "write"
	ToolCategoryExecute     ToolCategory = "execute"
	ToolCategoryInspect     ToolCategory = "inspect"
	ToolCategoryMemory      ToolCategory = "memory"
	ToolCategorySkill       ToolCategory = "skill"
	ToolCategoryIntegration ToolCategory = "integration"
)

type ParallelPolicy string

const (
	ParallelPolicyReadOnly ParallelPolicy = "read_only"
	ParallelPolicySerial   ParallelPolicy = "serial"
)

func ParseParallelPolicy(raw string) (ParallelPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "readonly", "read_only":
		return ParallelPolicyReadOnly, nil
	case "serial":
		return ParallelPolicySerial, nil
	default:
		return "", errUnknownParallelPolicy(raw)
	}
}

type HealthState string

const (
	HealthStateHealthy  HealthState = "healthy"
	HealthStateDisabled HealthState = "disabled"
	HealthStateDegraded HealthState = "degraded"
)

type ToolHealth struct {
	State  HealthState
	Reason string
}

type ToolSpec struct {
	ToolContract
	Tool   einotool.BaseTool
	Health ToolHealth
}

func (s ToolSpec) Enabled() bool {
	return s.Health.State != HealthStateDisabled
}

func healthyTool(reason string) ToolHealth {
	return ToolHealth{State: HealthStateHealthy, Reason: strings.TrimSpace(reason)}
}

func disabledTool(reason string) ToolHealth {
	return ToolHealth{State: HealthStateDisabled, Reason: strings.TrimSpace(reason)}
}

// localToolDef declares a static local tool plus whether the given config
// enables it. localToolDefs is the single source of truth for the static local
// toolset: both ConfiguredLocalSpecs and ConfiguredLocalSpec derive from it, so
// the tool list and its enable rules live in exactly one place (no parallel
// switch to drift out of sync).
type localToolDef struct {
	name    string
	enabled bool
}

func localToolDefs(cfg *config.Config) []localToolDef {
	mutation := !cfg.Tools.Mutation.Disabled
	runCommand := !cfg.Tools.RunCommand.Disabled
	return []localToolDef{
		{"read_file", true},
		{"list_files", true},
		{"search_text", true},
		{"inspect_git_status", true},
		{"inspect_git_diff", true},
		{"git_summary", true},
		{"artifact_write", true},
		{"artifact_read", true},
		{"artifact_list", true},
		{"ask_operator", true},
		{"web_fetch", true},
		{"web_search", true},
		{"browser", true},
		{"create_file", mutation},
		{"replace_span", mutation},
		{"apply_unified_patch", mutation},
		{"multi_edit", mutation},
		{"rollback_workspace_checkpoint", mutation},
		{"run_command", runCommand},
		{"run_verification", runCommand},
	}
}

func ConfiguredLocalSpecs(cfg *config.Config) []ToolSpec {
	if cfg == nil {
		return nil
	}
	defs := localToolDefs(cfg)
	specs := make([]ToolSpec, 0, len(defs))
	for _, def := range defs {
		specs = append(specs, configuredLocalSpec(def.name, def.enabled))
	}
	return specs
}

func ConfiguredLocalSpec(cfg *config.Config, name string) (ToolSpec, bool) {
	if cfg == nil {
		return ToolSpec{}, false
	}
	name = strings.TrimSpace(name)
	for _, def := range localToolDefs(cfg) {
		if def.name == name {
			return configuredLocalSpec(name, def.enabled), true
		}
	}
	return ToolSpec{}, false
}

func configuredLocalSpec(name string, enabled bool) ToolSpec {
	spec := ToolSpec{
		ToolContract: ToolContract{
			Name:     name,
			Source:   "local",
			Kind:     ToolKindNative,
			Category: ToolCategoryInspect,
			Loading:  EagerLoadingPolicy(),
			Execution: ToolExecutionPolicy{
				ParallelPolicy: ParallelPolicyReadOnly,
			},
		},
	}
	switch name {
	case "read_file", "list_files", "search_text", "inspect_git_status", "inspect_git_diff":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
		spec.Execution.PathArg = "path"
	case "git_summary":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryInspect
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "artifact_read", "artifact_list":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
	case "artifact_write":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "ask_operator":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryIntegration
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "web_fetch", "web_search":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryRead
		spec.Loading = DeferredLoadingPolicy("web_access")
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
	case "browser":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryIntegration
		spec.Loading = DeferredLoadingPolicy("web_access")
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "create_file", "replace_span", "apply_unified_patch":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.Execution.ParallelPolicy = ParallelPolicySerial
		if name == "apply_unified_patch" {
			spec.Execution.PathArg = "paths"
		} else {
			spec.Execution.PathArg = "path"
		}
	case "multi_edit":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "rollback_workspace_checkpoint":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryWrite
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "run_command":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryExecute
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	case "run_verification":
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryExecute
		spec.Execution.ParallelPolicy = ParallelPolicySerial
	default:
		spec.Kind = ToolKindNative
		spec.Category = ToolCategoryInspect
		spec.Execution.ParallelPolicy = ParallelPolicyReadOnly
	}
	if enabled {
		spec.Health = healthyTool("")
	} else {
		spec.Health = disabledTool(name + " disabled in config")
	}
	return spec
}
