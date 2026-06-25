package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
)

func (s *CapabilitiesService) snapshotSkills(ctx context.Context) SystemSkillCapabilities {
	if s == nil || s.skills == nil {
		return SystemSkillCapabilities{}
	}
	snapshot, err := s.skills(ctx)
	if err != nil {
		return SystemSkillCapabilities{
			LoadError: fmt.Sprintf("load stable skills: %v", err),
		}
	}
	out := make([]SystemSkillSummary, 0, len(snapshot.Skills))
	eligibleCount := 0
	for _, item := range snapshot.Skills {
		if item.Eligible {
			eligibleCount++
		}
		out = append(out, SystemSkillSummary{
			ID:              item.ID,
			Name:            item.Name,
			Version:         item.Version,
			Source:          item.Source,
			Origin:          string(item.Origin),
			TaskPattern:     item.TaskPattern,
			Summary:         item.Summary,
			PromotedFrom:    item.PromotedFrom,
			Eligible:        item.Eligible,
			DisabledReasons: append([]string(nil), item.DisabledReasons...),
		})
	}
	problems := make([]SystemSkillProblem, 0, len(snapshot.Problems))
	for _, item := range snapshot.Problems {
		problems = append(problems, SystemSkillProblem{
			ID:     item.ID,
			Name:   item.Name,
			Source: item.Source,
			Path:   item.Path,
			Error:  item.Error,
		})
	}
	return SystemSkillCapabilities{
		Count:           len(out),
		EligibleCount:   eligibleCount,
		IneligibleCount: len(out) - eligibleCount,
		InvalidCount:    len(problems),
		Items:           out,
		Problems:        problems,
	}
}

func (s *CapabilitiesService) snapshotMCPProviders(ctx context.Context, opts CapabilitySnapshotOptions) []SystemMCPProviderCapability {
	configured := configuredProviderConfigs(s.cfg)
	if len(configured) == 0 {
		return nil
	}
	statuses := s.resolveProviderStatuses(ctx, configured, opts)
	out := make([]SystemMCPProviderCapability, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, SystemMCPProviderCapability{
			Name:                status.Name,
			Configured:          status.Configured,
			Enabled:             status.Enabled,
			Transport:           status.Transport,
			StartupStatus:       status.StartupStatus,
			Command:             status.Command,
			Args:                append([]string(nil), status.Args...),
			WorkDir:             status.WorkDir,
			CommandPath:         status.CommandPath,
			ConfiguredToolNames: append([]string(nil), status.ConfiguredToolNames...),
			DiscoveredToolNames: append([]string(nil), status.DiscoveredToolNames...),
			ToolCount:           status.ToolCount,
			Error:               status.Error,
			AuthStatus:          status.AuthStatus,
		})
	}
	return out
}

func (s *CapabilitiesService) resolveProviderStatuses(ctx context.Context, configured []mcpprovider.ProviderConfig, opts CapabilitySnapshotOptions) []mcpprovider.ProviderStatus {
	if opts.ProbeMCP && s.probeProviders != nil {
		return s.probeProviders(ctx, configured)
	}
	statuses := make([]mcpprovider.ProviderStatus, 0, len(configured))
	for _, cfg := range configured {
		statuses = append(statuses, mcpprovider.ProviderStatus{
			Name:                cfg.Name,
			Configured:          true,
			Enabled:             cfg.Enabled,
			Transport:           cfg.Transport,
			Command:             cfg.Command,
			Args:                append([]string(nil), cfg.Args...),
			WorkDir:             cfg.WorkDir,
			ConfiguredToolNames: append([]string(nil), cfg.ToolNames...),
		})
	}
	return statuses
}

func configuredProviderConfigs(cfg *config.Config) []mcpprovider.ProviderConfig {
	if cfg == nil {
		return nil
	}
	return mcpprovider.ProviderConfigsFromConfig(cfg.MCP.Providers)
}

func enabledToolCount(items []SystemToolCapability) int {
	count := 0
	for _, item := range items {
		if item.Enabled {
			count++
		}
	}
	return count
}

func enabledCapabilityProviderCount(items []SystemMCPProviderCapability) int {
	count := 0
	for _, item := range items {
		if item.Enabled {
			count++
		}
	}
	return count
}

func healthyCapabilityProviderCount(items []SystemMCPProviderCapability) int {
	count := 0
	for _, item := range items {
		if item.Enabled && item.Error == "" {
			count++
		}
	}
	return count
}

func buildProviderReadiness(items []SystemMCPProviderCapability) []ProviderReadinessSummary {
	if len(items) == 0 {
		return nil
	}
	out := make([]ProviderReadinessSummary, 0, len(items))
	for _, item := range items {
		out = append(out, providerReadinessFromCapability(item))
	}
	return out
}

func providerReadinessFromCapability(provider SystemMCPProviderCapability) ProviderReadinessSummary {
	summary := ProviderReadinessSummary{
		Scope:         providerReadinessScopeMCP,
		Provider:      provider.Name,
		StartupStatus: strings.TrimSpace(provider.StartupStatus),
		AuthStatus:    strings.TrimSpace(provider.AuthStatus),
	}

	switch {
	case !provider.Configured:
		summary.Status = ProviderReadinessBlocked
		summary.Reason = "provider is not configured"
	case !provider.Enabled:
		summary.Status = ProviderReadinessBlocked
		summary.Reason = "provider is disabled"
	case strings.TrimSpace(provider.Error) != "":
		summary.Status = ProviderReadinessFailed
		summary.Reason = strings.TrimSpace(provider.Error)
	case summary.AuthStatus == "expired":
		summary.Status = ProviderReadinessFailed
		summary.Reason = "provider auth expired"
	case summary.StartupStatus == "":
		summary.Status = ProviderReadinessBlocked
		summary.Reason = "provider status has not been probed"
	case summary.StartupStatus == "failed" || summary.StartupStatus == "degraded":
		summary.Status = ProviderReadinessFailed
		summary.Reason = providerStartupReason(summary.StartupStatus)
	default:
		summary.Status = ProviderReadinessPassed
	}

	return summary
}

func providerStartupReason(status string) string {
	switch strings.TrimSpace(status) {
	case "failed":
		return "provider startup failed"
	case "degraded":
		return "provider startup degraded"
	default:
		return ""
	}
}
func buildRuntimeReadiness(blockedReason string) *RuntimeReadiness {
	reason := strings.TrimSpace(blockedReason)
	if reason == "" {
		return &RuntimeReadiness{Status: RuntimeReadinessReady}
	}
	return &RuntimeReadiness{
		Status: RuntimeReadinessBlocked,
		Reason: reason,
	}
}

func firstEnabledProviderModel(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	for _, item := range cfg.Providers {
		if !item.Enabled {
			continue
		}
		if name := strings.TrimSpace(item.Name); name != "" {
			return name
		}
	}
	return ""
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
