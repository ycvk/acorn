package tooling

import (
	"fmt"
	"strings"
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
	SideEffects    []ToolSideEffect
}

type ToolSideEffect string

const (
	ToolSideEffectReadWorkspace  ToolSideEffect = "read_workspace"
	ToolSideEffectWriteWorkspace ToolSideEffect = "write_workspace"
	ToolSideEffectRunCommand     ToolSideEffect = "run_command"
	ToolSideEffectMemoryRead     ToolSideEffect = "memory_read"
	ToolSideEffectMemoryWrite    ToolSideEffect = "memory_write"
	ToolSideEffectSkillRead      ToolSideEffect = "skill_read"
	ToolSideEffectIntegration    ToolSideEffect = "integration"
)

type ToolResultMode string

const (
	ToolResultModeInline     ToolResultMode = "inline"
	ToolResultModePreviewRef ToolResultMode = "preview_ref"
	ToolResultModeRefOnly    ToolResultMode = "ref_only"
)

type ToolResultPolicy struct {
	Mode           ToolResultMode
	MaxInlineBytes int
}

type ToolBoundaryMode string

const (
	ToolBoundaryModeToolResult ToolBoundaryMode = "tool_result"
	ToolBoundaryModeRunFailure ToolBoundaryMode = "run_failure"
)

type ToolBoundaryPolicy struct {
	Mode ToolBoundaryMode
}

type ToolProjectionMode string

const (
	ToolProjectionModeActivity ToolProjectionMode = "activity"
	ToolProjectionModeInternal ToolProjectionMode = "internal"
)

type ToolProjectionPolicy struct {
	Mode ToolProjectionMode
}

type ToolContract struct {
	Name          string
	Source        string
	Kind          ToolKind
	Category      ToolCategory
	ResourceScope ResourceScope
	Profiles      []ToolProfile
	PlanPolicy    PlanPolicy
	FactPolicy    FactPolicy
	Loading       ToolLoadingPolicy
	Execution     ToolExecutionPolicy
	Result        ToolResultPolicy
	Boundary      ToolBoundaryPolicy
	Projection    ToolProjectionPolicy
	Compressible  *bool
}

func (c ToolContract) normalized() ToolContract {
	c.Name = strings.TrimSpace(c.Name)
	c.Source = strings.TrimSpace(c.Source)
	c.Loading.Reason = strings.TrimSpace(c.Loading.Reason)
	c.Execution.PathArg = strings.TrimSpace(c.Execution.PathArg)
	c.Profiles = append([]ToolProfile(nil), c.Profiles...)
	c.Execution.SideEffects = append([]ToolSideEffect(nil), c.Execution.SideEffects...)
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
	if c.ResourceScope == "" {
		return fmt.Errorf("tool contract %q has empty resource scope", c.Name)
	}
	if len(c.Profiles) == 0 {
		return fmt.Errorf("tool contract %q has no profiles", c.Name)
	}
	for _, profile := range c.Profiles {
		switch profile {
		case ToolProfileRun, ToolProfileServe:
		default:
			return fmt.Errorf("tool contract %q has unknown profile %q", c.Name, profile)
		}
	}
	switch c.PlanPolicy {
	case PlanPolicyNone, PlanPolicyRequireActivePlan:
	default:
		return fmt.Errorf("tool contract %q has unknown plan policy %q", c.Name, c.PlanPolicy)
	}
	switch c.FactPolicy {
	case FactPolicyAuto, FactPolicySuppress:
	default:
		return fmt.Errorf("tool contract %q has unknown fact policy %q", c.Name, c.FactPolicy)
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
	case ParallelPolicyReadOnly, ParallelPolicyWriteScoped, ParallelPolicyNeverParallel:
	default:
		return fmt.Errorf("tool contract %q has unknown parallel policy %q", c.Name, c.Execution.ParallelPolicy)
	}
	if c.Execution.ParallelPolicy == ParallelPolicyWriteScoped && c.Execution.PathArg == "" {
		return fmt.Errorf("tool contract %q write-scoped execution requires path arg", c.Name)
	}
	for _, effect := range c.Execution.SideEffects {
		switch effect {
		case ToolSideEffectReadWorkspace,
			ToolSideEffectWriteWorkspace,
			ToolSideEffectRunCommand,
			ToolSideEffectMemoryRead,
			ToolSideEffectMemoryWrite,
			ToolSideEffectSkillRead,
			ToolSideEffectIntegration:
		default:
			return fmt.Errorf("tool contract %q has unknown side effect %q", c.Name, effect)
		}
	}
	switch c.Result.Mode {
	case ToolResultModeInline, ToolResultModePreviewRef, ToolResultModeRefOnly:
	default:
		return fmt.Errorf("tool contract %q has unknown result mode %q", c.Name, c.Result.Mode)
	}
	if c.Result.MaxInlineBytes < 0 {
		return fmt.Errorf("tool contract %q has negative max inline bytes", c.Name)
	}
	switch c.Boundary.Mode {
	case ToolBoundaryModeToolResult, ToolBoundaryModeRunFailure:
	default:
		return fmt.Errorf("tool contract %q has unknown boundary mode %q", c.Name, c.Boundary.Mode)
	}
	switch c.Projection.Mode {
	case ToolProjectionModeActivity, ToolProjectionModeInternal:
	default:
		return fmt.Errorf("tool contract %q has unknown projection mode %q", c.Name, c.Projection.Mode)
	}
	return nil
}

func EagerLoadingPolicy() ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeEager}
}

func DeferredLoadingPolicy(reason string) ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeDeferred, Reason: strings.TrimSpace(reason)}
}

func InlineResultPolicy(maxBytes int) ToolResultPolicy {
	return ToolResultPolicy{Mode: ToolResultModeInline, MaxInlineBytes: maxBytes}
}

func ToolResultBoundaryPolicy() ToolBoundaryPolicy {
	return ToolBoundaryPolicy{Mode: ToolBoundaryModeToolResult}
}

func ActivityProjectionPolicy() ToolProjectionPolicy {
	return ToolProjectionPolicy{Mode: ToolProjectionModeActivity}
}
