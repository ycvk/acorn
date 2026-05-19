package app

import "strings"

type RuntimeReadinessStatus string

const (
	RuntimeReadinessReady   RuntimeReadinessStatus = "ready"
	RuntimeReadinessBlocked RuntimeReadinessStatus = "blocked"
)

type ProviderReadinessStatus string

const (
	ProviderReadinessPassed  ProviderReadinessStatus = "passed"
	ProviderReadinessFailed  ProviderReadinessStatus = "failed"
	ProviderReadinessBlocked ProviderReadinessStatus = "blocked"
)

const providerReadinessScopeMCP = "mcp"

type RuntimeReadiness struct {
	Status RuntimeReadinessStatus `json:"status"`
	Reason string                 `json:"reason,omitempty"`
}

type ProviderReadinessSummary struct {
	Scope         string                  `json:"scope"`
	Provider      string                  `json:"provider"`
	Status        ProviderReadinessStatus `json:"status"`
	Reason        string                  `json:"reason,omitempty"`
	StartupStatus string                  `json:"startup_status,omitempty"`
	AuthStatus    string                  `json:"auth_status,omitempty"`
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
