package cli

import (
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
	"github.com/ycvk/acorn/internal/wire"
)

func renderDoctorKnowledge(status knowledge.Status) string {
	return strings.Join([]string{
		"",
		"Knowledge base",
		fmt.Sprintf("  Notes: %d", status.Notes),
		fmt.Sprintf("  Attachments: %s", status.Attachments),
	}, "\n")
}

// doctorRemediationLines tells the owner WHAT to type to fix a not-ready verdict,
// naming the active config path. The hint is tailored to the failure reason so it
// is not misleading for non-api_key failures (e.g. max_iterations, workspace).
// JSON output is untouched (printJSON returns first).
func doctorRemediationLines(reason, configPath string) []string {
	cfgPath := strings.TrimSpace(configPath)
	if cfgPath == "" {
		cfgPath = "your config"
	}
	lines := []string{
		fmt.Sprintf("  Fix:   edit %s, then re-run 'acorn doctor' (or 'acorn smoke \"hello\"' to test a real run). No config yet? run 'acorn init'.", cfgPath),
	}
	switch {
	case strings.Contains(reason, "api_key"):
		lines = append(lines, "         api_key fields read env vars (e.g. OPENAI_API_KEY; systemd installs read ~/.acorn/acorn.env — restart with 'sudo systemctl restart acorn').")
	}
	return lines
}

func renderDoctorSummary(snapshot api.SystemCapabilities, configPath string) string {
	lines := []string{
		"Acorn doctor",
		"",
		"Execution",
		fmt.Sprintf("  Ready: %s", doctorRuntimeReadinessLabel(snapshot.RuntimeReadiness)),
	}
	if strings.TrimSpace(snapshot.Model.Name) != "" {
		lines = append(lines, fmt.Sprintf("  Model: %s", snapshot.Model.Name))
	}
	lines = append(lines, fmt.Sprintf(
		"  Summary: %d tools, %d skills, %d/%d healthy MCP providers",
		snapshot.Summary.ToolCount,
		snapshot.Summary.SkillCount,
		snapshot.Summary.MCPHealthyProviderCount,
		snapshot.Summary.MCPConfiguredProviderCount,
	))
	if errText := runtimeReadinessReason(snapshot.RuntimeReadiness); errText != "" {
		lines = append(lines, fmt.Sprintf("  Error: %s", errText))
		lines = append(lines, doctorRemediationLines(errText, configPath)...)
	}
	if errText := strings.TrimSpace(snapshot.ToolCatalogError); errText != "" {
		lines = append(lines, fmt.Sprintf("  Tool catalog: %s", errText))
	}

	lines = append(lines, "", "Tools")
	if len(snapshot.Tools) == 0 {
		lines = append(lines, "  - none")
	} else {
		for _, item := range snapshot.Tools {
			lines = append(lines, renderDoctorToolLine(item))
		}
	}

	lines = append(lines, "", "Skills")
	lines = append(lines, fmt.Sprintf(
		"  Summary: %d total, %d eligible, %d ineligible, %d invalid",
		snapshot.Skills.Count,
		snapshot.Skills.EligibleCount,
		snapshot.Skills.IneligibleCount,
		snapshot.Skills.InvalidCount,
	))
	if loadErr := strings.TrimSpace(snapshot.Skills.LoadError); loadErr != "" {
		lines = append(lines, fmt.Sprintf("  Load error: %s", loadErr))
	}
	if len(snapshot.Skills.Items) == 0 {
		lines = append(lines, "  - none")
	} else {
		for _, item := range snapshot.Skills.Items {
			lines = append(lines, renderDoctorSkillLine(item))
		}
	}
	if len(snapshot.Skills.Problems) > 0 {
		lines = append(lines, "  Problems:")
		for _, problem := range snapshot.Skills.Problems {
			lines = append(lines, fmt.Sprintf("  - %s", renderDoctorSkillProblem(problem)))
		}
	}

	lines = append(lines, "", "MCP providers")
	lines = append(lines, fmt.Sprintf(
		"  Summary: %d configured, %d enabled, %d healthy",
		snapshot.Summary.MCPConfiguredProviderCount,
		snapshot.Summary.MCPEnabledProviderCount,
		snapshot.Summary.MCPHealthyProviderCount,
	))
	if len(snapshot.MCPProviders) == 0 {
		lines = append(lines, "  - none")
	} else {
		for _, provider := range snapshot.MCPProviders {
			lines = append(lines, renderDoctorProviderLine(provider))
			if errText := strings.TrimSpace(provider.Error); errText != "" {
				lines = append(lines, fmt.Sprintf("    Error: %s", errText))
			}
		}
	}

	return strings.Join(lines, "\n")
}

func doctorRuntimeReadinessLabel(readiness *api.RuntimeReadiness) string {
	if readiness != nil && readiness.Status == api.RuntimeReadinessReady {
		return "ready"
	}
	return "not ready"
}

func runtimeReadinessReason(readiness *api.RuntimeReadiness) string {
	if readiness == nil {
		return ""
	}
	return strings.TrimSpace(readiness.Reason)
}

func renderDoctorToolLine(item api.SystemToolCapability) string {
	parts := []string{fmt.Sprintf("  - %s:", item.Name)}
	if !item.Enabled {
		parts = append(parts, "disabled")
	} else {
		parts = append(parts, item.Risk)
	}
	if strings.TrimSpace(item.Source) != "" {
		parts = append(parts, "source="+item.Source)
	}
	if strings.TrimSpace(item.Kind) != "" {
		parts = append(parts, "kind="+item.Kind)
	}
	if strings.TrimSpace(item.Category) != "" {
		parts = append(parts, "category="+item.Category)
	}
	if strings.TrimSpace(item.HealthState) != "" {
		parts = append(parts, "health="+item.HealthState)
	}
	if strings.TrimSpace(item.HealthReason) != "" {
		parts = append(parts, "reason="+item.HealthReason)
	}
	return strings.Join(parts, " ")
}

func renderDoctorSkillLine(item api.SystemSkillSummary) string {
	status := "eligible"
	if !item.Eligible {
		status = "ineligible"
	}
	line := fmt.Sprintf("  - %s: %s", item.ID, status)
	if len(item.DisabledReasons) > 0 {
		line += " (" + strings.Join(item.DisabledReasons, "; ") + ")"
	}
	if strings.TrimSpace(item.PromotedFrom) != "" {
		line += " Promoted from: " + item.PromotedFrom
	}
	return line
}

func renderDoctorSkillProblem(problem api.SystemSkillProblem) string {
	parts := make([]string, 0, 4)
	if problem.ID != "" {
		parts = append(parts, problem.ID)
	} else if problem.Name != "" {
		parts = append(parts, problem.Name)
	}
	if problem.Source != "" {
		parts = append(parts, "source="+problem.Source)
	}
	if problem.Error != "" {
		parts = append(parts, "error="+problem.Error)
	}
	return strings.Join(parts, " ")
}

func renderDoctorProviderLine(provider api.SystemMCPProviderCapability) string {
	parts := []string{fmt.Sprintf("  - %s:", provider.Name)}
	switch {
	case !provider.Configured:
		parts = append(parts, "not configured")
	case !provider.Enabled:
		parts = append(parts, "disabled")
	default:
		transport := strings.TrimSpace(provider.Transport)
		if transport == "" {
			parts = append(parts, "[missing transport]")
		} else {
			parts = append(parts, "["+transport+"]")
		}
		switch strings.TrimSpace(provider.StartupStatus) {
		case "healthy":
			parts = append(parts, "healthy")
		case "failed":
			parts = append(parts, "failed")
		case "degraded":
			parts = append(parts, "degraded")
		default:
			if strings.TrimSpace(provider.Error) != "" {
				parts = append(parts, "failed")
			} else {
				parts = append(parts, "healthy")
			}
		}
	}
	parts = append(parts, fmt.Sprintf("tools=%d", provider.ToolCount))
	if len(provider.DiscoveredToolNames) > 0 {
		parts = append(parts, "discovered="+strings.Join(provider.DiscoveredToolNames, ","))
	}
	if len(provider.ConfiguredToolNames) > 0 {
		parts = append(parts, "configured="+strings.Join(provider.ConfiguredToolNames, ","))
	}
	if auth := strings.TrimSpace(provider.AuthStatus); auth != "" {
		parts = append(parts, "auth="+auth)
	}
	return strings.Join(parts, " ")
}

func renderDoctorWatches(cfg *config.Config, watches []core.Watch) string {
	counts := map[core.WatchStatus]int{}
	for _, w := range watches {
		counts[w.Status]++
	}
	rsshub := cfg.Watch.RSSHubBaseURL
	if rsshub == "" {
		rsshub = "not configured (rsshub: watches unavailable)"
	}
	briefing := cfg.Briefing.At
	if briefing == "" {
		briefing = "off"
	} else {
		briefing += " " + cfg.Owner.Timezone
	}
	lines := []string{
		"",
		"Watches",
		fmt.Sprintf("  Summary: %d total, %d active, %d failing, %d paused", len(watches), counts[core.WatchActive], counts[core.WatchFailing], counts[core.WatchPaused]),
		fmt.Sprintf("  RSSHub: %s", rsshub),
		fmt.Sprintf("  Morning briefing: %s", briefing),
	}
	for _, w := range watches {
		if w.Status == core.WatchFailing {
			lines = append(lines, fmt.Sprintf("  - #%d %s failing: %s", w.ID, w.Name, w.LastError))
		}
	}
	return strings.Join(lines, "\n")
}

func renderDoctorThinking(cfg *config.Config, status wire.ThinkingStatus) string {
	night := cfg.Thinking.NightAt
	if night == "" {
		night = "off"
	}
	wander := strings.Join(cfg.Thinking.WanderAt, ", ")
	if wander == "" {
		wander = "off"
	}
	lines := []string{"", "Thinking", fmt.Sprintf("  Night reflection: %s (%s)", night, cfg.Owner.Timezone), "  Idle thoughts: " + wander, fmt.Sprintf("  Autonomous wakes today: %d / %d", status.Wakes, cfg.Wake.DailyLimit), fmt.Sprintf("  Autonomous tokens today: %d / %d", status.Usage.AutonomousTokens, cfg.Wake.DailyTokens), fmt.Sprintf("  All reported tokens today: %d", status.Usage.TotalTokens), fmt.Sprintf("  Calls without usage today: %d", status.Usage.UnreportedCalls), "  Phone notifications (last 24h):"}
	if len(status.Phones) == 0 {
		lines = append(lines, "    none")
	}
	for _, p := range status.Phones {
		lines = append(lines, fmt.Sprintf("    %s (%s): %d", p.App, p.Package, p.Count))
	}
	return strings.Join(lines, "\n")
}
