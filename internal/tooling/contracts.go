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
}

type ToolContract struct {
	Name          string
	Source        string
	Kind          ToolKind
	Category      ToolCategory
	ResourceScope ResourceScope
	Profiles      []ToolProfile
	PlanPolicy    PlanPolicy
	Loading       ToolLoadingPolicy
	Execution     ToolExecutionPolicy
	Compressible  *bool
}

func (c ToolContract) normalized() ToolContract {
	c.Name = strings.TrimSpace(c.Name)
	c.Source = strings.TrimSpace(c.Source)
	c.Loading.Reason = strings.TrimSpace(c.Loading.Reason)
	c.Execution.PathArg = strings.TrimSpace(c.Execution.PathArg)
	c.Profiles = append([]ToolProfile(nil), c.Profiles...)
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
	switch c.ResourceScope {
	case ResourceScopeWorkspaceFile,
		ResourceScopeWorkspaceCommand,
		ResourceScopeMemory,
		ResourceScopeSkill,
		ResourceScopeMCP,
		ResourceScopeArtifact,
		ResourceScopeOperator,
		ResourceScopeWeb,
		ResourceScopeBrowser:
	default:
		return fmt.Errorf("tool contract %q has unknown resource scope %q", c.Name, c.ResourceScope)
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
	return nil
}

func EagerLoadingPolicy() ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeEager}
}

func DeferredLoadingPolicy(reason string) ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeDeferred, Reason: strings.TrimSpace(reason)}
}
