package web

import (
	"github.com/ycvk/acorn/internal/app"
)

type HealthResponse struct {
	OK bool `json:"ok"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type CapabilitiesSummaryDTO struct {
	ToolCount                  int `json:"tool_count"`
	EnabledToolCount           int `json:"enabled_tool_count"`
	SkillCount                 int `json:"skill_count"`
	EligibleSkillCount         int `json:"eligible_skill_count"`
	IneligibleSkillCount       int `json:"ineligible_skill_count"`
	InvalidSkillCount          int `json:"invalid_count"`
	MCPConfiguredProviderCount int `json:"mcp_configured_provider_count"`
	MCPEnabledProviderCount    int `json:"mcp_enabled_provider_count"`
	MCPHealthyProviderCount    int `json:"mcp_healthy_provider_count"`
}

type CapabilitiesModelDTO struct {
	Name string `json:"name"`
}

type CapabilitiesFeaturesDTO struct {
	InterruptResume bool `json:"interrupt_resume"`
	SessionHistory  bool `json:"session_history"`
}

type RuntimeReadinessDTO struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type ProviderReadinessDTO struct {
	Scope         string `json:"scope"`
	Provider      string `json:"provider"`
	Status        string `json:"status"`
	Reason        string `json:"reason,omitempty"`
	StartupStatus string `json:"startup_status,omitempty"`
	AuthStatus    string `json:"auth_status,omitempty"`
}

type CapabilitiesToolDTO struct {
	Name           string `json:"name"`
	Source         string `json:"source"`
	Kind           string `json:"kind"`
	Category       string `json:"category"`
	Enabled        bool   `json:"enabled"`
	HealthState    string `json:"health_state"`
	HealthReason   string `json:"health_reason,omitempty"`
	ParallelPolicy string `json:"parallel_policy,omitempty"`
	Risk           string `json:"risk"`
	RootDir        string `json:"root_dir,omitempty"`
	WorkDir        string `json:"work_dir,omitempty"`
	DefaultTimeout int    `json:"default_timeout,omitempty"`
}

func capabilitiesSummaryDTOFromSnapshot(snapshot app.SystemCapabilitySummary) CapabilitiesSummaryDTO {
	return DefaultConverter.capabilitiesSummaryDTOFromSnapshot(snapshot)
}

func capabilitiesModelDTOFromSnapshot(snapshot app.SystemModelCapabilities) CapabilitiesModelDTO {
	return DefaultConverter.capabilitiesModelDTOFromSnapshot(snapshot)
}

func capabilitiesFeaturesDTOFromSnapshot(snapshot app.SystemFeatureCapabilities) CapabilitiesFeaturesDTO {
	return DefaultConverter.capabilitiesFeaturesDTOFromSnapshot(snapshot)
}

func runtimeReadinessDTOFromSnapshot(snapshot *app.RuntimeReadiness) RuntimeReadinessDTO {
	if snapshot == nil {
		return RuntimeReadinessDTO{}
	}
	return RuntimeReadinessDTO{
		Status: string(snapshot.Status),
		Reason: snapshot.Reason,
	}
}

func providerReadinessDTOsFromSnapshot(snapshot []app.ProviderReadinessSummary) []ProviderReadinessDTO {
	return DefaultConverter.providerReadinessDTOsFromSnapshot(snapshot)
}

func capabilitiesToolsDTOFromSnapshot(snapshot []app.SystemToolCapability) []CapabilitiesToolDTO {
	return DefaultConverter.capabilitiesToolsDTOFromSnapshot(snapshot)
}

// SystemStatusDTO represents the overall system readiness snapshot.
type SystemStatusDTO struct {
	RuntimeReadiness  RuntimeReadinessDTO     `json:"runtime_readiness"`
	ProviderReadiness []ProviderReadinessDTO  `json:"provider_readiness,omitempty"`
	Model             CapabilitiesModelDTO    `json:"model"`
	WorkspaceRoot     string                  `json:"workspace_root"`
	Summary           CapabilitiesSummaryDTO  `json:"summary"`
	Features          CapabilitiesFeaturesDTO `json:"features"`
}

// InboxResponse is the aggregate view for the mobile inbox.
type InboxResponse struct {
	PendingActions     []PendingActionSummaryDTO `json:"pending_actions"`
	ActiveRuns         []RunSummaryDTO           `json:"active_runs"`
	RecentTerminalRuns []RunSummaryDTO           `json:"recent_terminal_runs"`
	System             SystemStatusDTO           `json:"system"`
}

// RunSummaryDTO is a lightweight summary of a run for list views.

func inboxDTOFromDomain(inbox app.MobileInbox, workspaceRoot string) InboxResponse {
	return InboxResponse{
		PendingActions:     pendingActionSummaryDTOsFromDomain(inbox.PendingActions),
		ActiveRuns:         runSummaryDTOsFromDomain(inbox.ActiveRuns),
		RecentTerminalRuns: runSummaryDTOsFromDomain(inbox.RecentTerminalRuns),
		System:             systemStatusDTOFromSnapshot(inbox.System, workspaceRoot),
	}
}

func systemStatusDTOFromSnapshot(snapshot app.SystemCapabilities, workspaceRoot string) SystemStatusDTO {
	return SystemStatusDTO{
		RuntimeReadiness:  runtimeReadinessDTOFromSnapshot(snapshot.RuntimeReadiness),
		ProviderReadiness: providerReadinessDTOsFromSnapshot(snapshot.ProviderReadiness),
		Model:             capabilitiesModelDTOFromSnapshot(snapshot.Model),
		WorkspaceRoot:     workspaceRoot,
		Summary:           capabilitiesSummaryDTOFromSnapshot(snapshot.Summary),
		Features:          capabilitiesFeaturesDTOFromSnapshot(snapshot.Features),
	}
}

type ToolSummaryDTO = CapabilitiesToolDTO

// ToolListResponse is the response body for listing tools.
type ToolListResponse struct {
	Items []ToolSummaryDTO `json:"items"`
	Total int              `json:"total"`
}
