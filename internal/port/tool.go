package port

import (
	einotool "github.com/cloudwego/eino/components/tool"
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

type ParallelPolicy string

const (
	ParallelPolicyReadOnly ParallelPolicy = "read_only"
	ParallelPolicySerial   ParallelPolicy = "serial"
)

type ToolExecutionPolicy struct {
	ParallelPolicy ParallelPolicy
	PathArg        string
}

type ToolKind string

const (
	ToolKindNative   ToolKind = "native"
	ToolKindMCP      ToolKind = "mcp"
	ToolKindMemory   ToolKind = "memory"
	ToolKindSkill    ToolKind = "skill"
	ToolKindWorkflow ToolKind = "workflow"
)

type ToolCategory string

const (
	ToolCategoryInspect     ToolCategory = "inspect"
	ToolCategoryMutation    ToolCategory = "mutation"
	ToolCategoryMemory      ToolCategory = "memory"
	ToolCategorySkill       ToolCategory = "skill"
	ToolCategoryIntegration ToolCategory = "integration"
	ToolCategoryWorkflow    ToolCategory = "workflow"
)

type HealthState string

const (
	HealthStateHealthy  HealthState = "healthy"
	HealthStateDisabled HealthState = "disabled"
)

type ToolHealth struct {
	State  HealthState
	Reason string
}

type ToolContract struct {
	Name      string
	Source    string
	Kind      ToolKind
	Category  ToolCategory
	Loading   ToolLoadingPolicy
	Execution ToolExecutionPolicy
}

type ToolSpec struct {
	Contract  ToolContract
	Tool      einotool.BaseTool
	Health    ToolHealth
	IsMCP     bool
	IsBuiltin bool
}

func (s ToolSpec) Enabled() bool {
	return s.Health.State != HealthStateDisabled
}

type ExecutionPolicyResolver interface {
	ExecutionPolicy(toolName string, args map[string]any) (ToolExecutionPolicy, error)
}

// Catalog is the read-only tool catalog interface.
type Catalog interface {
	Specs() []ToolSpec
	EnabledSpecs() []ToolSpec
	Tools() []einotool.BaseTool
	Find(name string) (ToolSpec, bool)
	ExecutionPolicy(toolName string, args map[string]any) (ToolExecutionPolicy, error)
}

func EagerLoadingPolicy() ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeEager}
}

func DeferredLoadingPolicy(reason string) ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeDeferred, Reason: reason}
}
