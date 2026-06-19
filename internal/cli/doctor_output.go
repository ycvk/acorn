package cli

import (
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/app"
)

func printDoctorOutput(snapshot app.SystemCapabilities, configPath string, jsonMode bool) error {
	if jsonMode {
		return printJSON(snapshot)
	}
	fmt.Println(renderDoctorSummary(snapshot, configPath))
	return nil
}

// doctorRemediationLines tells the owner WHAT to type to fix a not-ready verdict,
// naming the active config path. JSON output is untouched (printJSON returns first).
func doctorRemediationLines(configPath string) []string {
	cfgPath := strings.TrimSpace(configPath)
	if cfgPath == "" {
		cfgPath = "your config"
	}
	return []string{
		fmt.Sprintf("  Fix:   edit %s, then re-run 'acorn doctor' (or 'acorn smoke \"hello\"' to test a real run).", cfgPath),
		"         api_key fields read env vars (e.g. OPENAI_API_KEY; systemd installs read ~/.acorn/acorn.env — restart with 'sudo systemctl restart acorn').",
		"         No config yet? run 'acorn init'.",
	}
}

func renderDoctorSummary(snapshot app.SystemCapabilities, configPath string) string {
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
		lines = append(lines, doctorRemediationLines(configPath)...)
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

func doctorRuntimeReadinessLabel(readiness *app.RuntimeReadiness) string {
	if readiness != nil && readiness.Status == app.RuntimeReadinessReady {
		return "ready"
	}
	return "not ready"
}

func runtimeReadinessReason(readiness *app.RuntimeReadiness) string {
	if readiness == nil {
		return ""
	}
	return strings.TrimSpace(readiness.Reason)
}

func renderDoctorToolLine(item app.SystemToolCapability) string {
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
	if item.DefaultTimeout > 0 {
		parts = append(parts, fmt.Sprintf("timeout=%ds", item.DefaultTimeout))
	}
	if strings.TrimSpace(item.RootDir) != "" {
		parts = append(parts, "root="+item.RootDir)
	}
	if strings.TrimSpace(item.WorkDir) != "" {
		parts = append(parts, "workdir="+item.WorkDir)
	}
	return strings.Join(parts, " ")
}

func renderDoctorSkillLine(item app.SystemSkillSummary) string {
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

func renderDoctorSkillProblem(problem app.SystemSkillProblem) string {
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

func renderDoctorProviderLine(provider app.SystemMCPProviderCapability) string {
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
