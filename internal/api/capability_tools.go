package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/tools"
)

func (s *CapabilitiesService) snapshotTools(ctx context.Context, providers []SystemMCPProviderCapability) ([]SystemToolCapability, error) {
	specs, err := s.loadToolSpecs(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]SystemToolCapability, 0, len(specs)+providerToolCount(providers))
	for _, spec := range specs {
		items = append(items, toolCapabilityFromSpec(spec))
	}
	for _, provider := range providers {
		providerItems, err := s.providerToolCapabilities(provider)
		if err != nil {
			return nil, err
		}
		items = append(items, providerItems...)
	}
	return items, nil
}

func (s *CapabilitiesService) loadToolSpecs(ctx context.Context) ([]core.ToolSpec, error) {
	if s.catalogBuilder != nil {
		specs, err := s.catalogBuilder.BuildCapabilitySpecs(ctx)
		if err != nil {
			return nil, fmt.Errorf("build tool catalog: %w", err)
		}
		return specs, nil
	}
	return tools.ConfiguredLocalSpecs(), nil
}

func (s *CapabilitiesService) providerToolCapabilities(provider SystemMCPProviderCapability) ([]SystemToolCapability, error) {
	toolNames := provider.DiscoveredToolNames
	if len(toolNames) == 0 {
		toolNames = provider.ConfiguredToolNames
	}
	items := make([]SystemToolCapability, 0, len(toolNames))
	for _, toolName := range toolNames {
		items = append(items, SystemToolCapability{
			Name:         toolName,
			Source:       provider.Name,
			Kind:         string(core.ToolKindMCP),
			Category:     string(core.ToolCategoryIntegration),
			Enabled:      provider.Enabled && provider.Error == "",
			HealthState:  providerHealthState(provider),
			HealthReason: strings.TrimSpace(provider.Error),
			Risk:         "integration",
		})
	}
	return items, nil
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

func providerHealthState(provider SystemMCPProviderCapability) string {
	switch {
	case !provider.Enabled:
		return string(core.HealthStateDisabled)
	case strings.TrimSpace(provider.Error) != "":
		return string(core.HealthStateDegraded)
	default:
		return string(core.HealthStateHealthy)
	}
}

func providerToolCount(providers []SystemMCPProviderCapability) int {
	total := 0
	for _, provider := range providers {
		if len(provider.DiscoveredToolNames) > 0 {
			total += len(provider.DiscoveredToolNames)
			continue
		}
		total += len(provider.ConfiguredToolNames)
	}
	return total
}
