package core

import (
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
)

// --- Tool loading ---

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

// --- Tool classification ---

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
	ToolCategoryRead        ToolCategory = "read"
	ToolCategoryWrite       ToolCategory = "write"
	ToolCategoryExecute     ToolCategory = "execute"
	ToolCategoryInspect     ToolCategory = "inspect"
	ToolCategoryMutation    ToolCategory = "mutation"
	ToolCategoryMemory      ToolCategory = "memory"
	ToolCategorySkill       ToolCategory = "skill"
	ToolCategoryIntegration ToolCategory = "integration"
	ToolCategoryWorkflow    ToolCategory = "workflow"
)

// --- Tool health ---

type HealthState string

const (
	HealthStateHealthy  HealthState = "healthy"
	HealthStateDegraded HealthState = "degraded"
	HealthStateDisabled HealthState = "disabled"
)

type ToolHealth struct {
	State  HealthState
	Reason string
}

// --- Tool contract ---

type ToolContract struct {
	Name     string
	Source   string
	Kind     ToolKind
	Category ToolCategory
	Loading  ToolLoadingPolicy
}

func (c ToolContract) Normalized() ToolContract {
	c.Name = strings.TrimSpace(c.Name)
	c.Source = strings.TrimSpace(c.Source)
	c.Loading.Reason = strings.TrimSpace(c.Loading.Reason)
	return c
}

func (c ToolContract) Validate() error {
	c = c.Normalized()
	if c.Name == "" {
		return fmt.Errorf("tool contract: name is required")
	}
	if c.Kind == "" {
		return fmt.Errorf("tool contract %q: kind is required", c.Name)
	}
	if c.Category == "" {
		return fmt.Errorf("tool contract %q: category is required", c.Name)
	}
	if c.Loading.Mode == "" {
		return fmt.Errorf("tool contract %q: loading mode is required", c.Name)
	}
	switch c.Loading.Mode {
	case ToolLoadingModeEager, ToolLoadingModeDeferred, ToolLoadingModeHidden:
	default:
		return fmt.Errorf("tool contract %q: unknown loading mode %q", c.Name, c.Loading.Mode)
	}
	return nil
}

// --- Tool spec ---

// ToolSpec embeds ToolContract so callers can access spec.Name, spec.Kind, etc.
// directly. It adds the tool implementation, factory, health state, and origin flags.
//
// Factory unifies tool construction for both native and MCP tools: when non-nil,
// the runtime invokes it to lazily produce the einotool.BaseTool instance.
// The pre-built Tool field serves eager-loaded tools that need no factory.
type ToolSpec struct {
	ToolContract
	Tool    einotool.BaseTool
	Factory ToolFactory
	Health  ToolHealth
}

func (s ToolSpec) Enabled() bool {
	return s.Health.State != HealthStateDisabled
}

// --- Catalog ---

// Catalog is the read-only tool catalog interface.
type Catalog interface {
	Specs() []ToolSpec
	EnabledSpecs() []ToolSpec
	Tools() []einotool.BaseTool
	Find(name string) (ToolSpec, bool)
}

// --- Policy constructors ---

func EagerLoadingPolicy() ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeEager}
}

func DeferredLoadingPolicy(reason string) ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeDeferred, Reason: reason}
}
