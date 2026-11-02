package api

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

func (s *CapabilitiesService) snapshotTools(ctx context.Context, providers []SystemMCPProviderCapability) ([]SystemToolCapability, error) {
	specs, err := s.loadToolSpecs(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]SystemToolCapability, 0, len(specs))
	for _, spec := range specs {
		items = append(items, toolCapabilityFromSpec(spec))
	}
	for _, provider := range providers {
		items = append(items, providerToolCapabilities(provider)...)
	}
	return items, nil
}

func (s *CapabilitiesService) loadToolSpecs(ctx context.Context) ([]core.ToolSpec, error) {
	specs, err := s.toolSpecs(ctx)
	if err != nil {
		return nil, fmt.Errorf("build tool catalog: %w", err)
	}
	return specs, nil
}

func providerToolCapabilities(provider SystemMCPProviderCapability) []SystemToolCapability {
	toolNames := provider.DiscoveredToolNames
	if len(toolNames) == 0 {
		toolNames = provider.ConfiguredToolNames
	}
	health, reason := providerToolHealth(provider)
	items := make([]SystemToolCapability, 0, len(toolNames))
	for _, toolName := range toolNames {
		items = append(items, SystemToolCapability{
			Name:         toolName,
			Source:       provider.Name,
			Kind:         string(core.ToolKindMCP),
			Category:     string(core.ToolCategoryIntegration),
			Enabled:      health == core.HealthStateHealthy || health == core.HealthStateUnknown,
			HealthState:  string(health),
			HealthReason: reason,
			Risk:         "integration",
		})
	}
	return items
}

func toolCapabilityFromSpec(spec core.ToolSpec) SystemToolCapability {
	return SystemToolCapability{
		Name:         spec.Name,
		Source:       spec.Source,
		Kind:         string(spec.Kind),
		Category:     string(spec.Category),
		Enabled:      spec.Enabled(),
		HealthState:  string(spec.Health.State),
		HealthReason: spec.Health.Reason,
		Risk:         toolRisk(spec),
	}
}

func toolRisk(spec core.ToolSpec) string {
	switch spec.Category {
	case core.ToolCategoryRead, core.ToolCategoryInspect:
		return "read_only"
	case core.ToolCategoryWrite:
		return "mutation"
	case core.ToolCategoryExecute:
		return "escape_hatch"
	case core.ToolCategoryMemory:
		return "memory"
	default:
		return "integration"
	}
}

// providerToolHealth reads the health of a provider's tools from the
// provider's readiness, so tools and readiness never disagree.
func providerToolHealth(provider SystemMCPProviderCapability) (core.HealthState, string) {
	readiness := providerReadinessFromCapability(provider)
	switch {
	case !provider.Configured || !provider.Enabled:
		return core.HealthStateDisabled, readiness.Reason
	case readiness.Status == ProviderReadinessFailed:
		return core.HealthStateDegraded, readiness.Reason
	case readiness.Status == ProviderReadinessBlocked:
		return core.HealthStateUnknown, readiness.Reason
	default:
		return core.HealthStateHealthy, ""
	}
}
