package api

import (
	"github.com/ycvk/acorn/internal/skills"
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
	InvalidSkillCount          int `json:"invalid_skill_count"`
	MCPConfiguredProviderCount int `json:"mcp_configured_provider_count"`
	MCPEnabledProviderCount    int `json:"mcp_enabled_provider_count"`
	MCPHealthyProviderCount    int `json:"mcp_healthy_provider_count"`
}

type CapabilitiesModelDTO struct {
	Name string `json:"name"`
}

type CapabilitiesFeaturesDTO struct {
	SessionHistory bool `json:"session_history"`
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
	Name         string `json:"name"`
	Source       string `json:"source"`
	Kind         string `json:"kind"`
	Category     string `json:"category"`
	Enabled      bool   `json:"enabled"`
	HealthState  string `json:"health_state"`
	HealthReason string `json:"health_reason,omitempty"`
	Risk         string `json:"risk"`
}

func runtimeReadinessDTOFromSnapshot(snapshot *RuntimeReadiness) RuntimeReadinessDTO {
	if snapshot == nil {
		return RuntimeReadinessDTO{}
	}
	return RuntimeReadinessDTO{
		Status: string(snapshot.Status),
		Reason: snapshot.Reason,
	}
}

// SystemStatusDTO represents the overall system readiness snapshot.
type SystemStatusDTO struct {
	RuntimeReadiness  RuntimeReadinessDTO     `json:"runtime_readiness"`
	ProviderReadiness []ProviderReadinessDTO  `json:"provider_readiness,omitempty"`
	Model             CapabilitiesModelDTO    `json:"model"`
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

func inboxDTOFromDomain(inbox MobileInbox) InboxResponse {
	return InboxResponse{
		PendingActions:     DefaultConverter.pendingActionSummaryDTOsFromDomain(inbox.PendingActions),
		ActiveRuns:         DefaultConverter.runSummaryDTOsFromDomain(inbox.ActiveRuns),
		RecentTerminalRuns: DefaultConverter.runSummaryDTOsFromDomain(inbox.RecentTerminalRuns),
		System:             systemStatusDTOFromSnapshot(inbox.System),
	}
}

func systemStatusDTOFromSnapshot(snapshot SystemCapabilities) SystemStatusDTO {
	return SystemStatusDTO{
		RuntimeReadiness:  runtimeReadinessDTOFromSnapshot(snapshot.RuntimeReadiness),
		ProviderReadiness: DefaultConverter.providerReadinessDTOsFromSnapshot(snapshot.ProviderReadiness),
		Model:             DefaultConverter.capabilitiesModelDTOFromSnapshot(snapshot.Model),
		Summary:           DefaultConverter.capabilitiesSummaryDTOFromSnapshot(snapshot.Summary),
		Features:          DefaultConverter.capabilitiesFeaturesDTOFromSnapshot(snapshot.Features),
	}
}

// ToolListResponse is the response body for listing toolset.
type ToolListResponse struct {
	Items []CapabilitiesToolDTO `json:"items"`
	Total int                   `json:"total"`
}

type SkillSummaryDTO struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Eligible        bool     `json:"eligible"`
	RequiresTools   []string `json:"requires_tools,omitempty"`
	FileCount       int      `json:"file_count,omitempty"`
	DisabledReasons []string `json:"disabled_reasons,omitempty"`
}

type SkillFileDTO struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type SkillDetailDTO struct {
	SkillSummaryDTO
	Revision      int64          `json:"revision"`
	Instruction   string         `json:"instruction"`
	License       string         `json:"license,omitempty"`
	Compatibility string         `json:"compatibility,omitempty"`
	Files         []SkillFileDTO `json:"files,omitempty"`
}

type SkillListResponse struct {
	Items []SkillSummaryDTO `json:"items"`
	Total int               `json:"total"`
}

type SkillEnvelope struct {
	Item SkillDetailDTO `json:"item"`
}

func skillSummaryDTOFromView(item skills.View) SkillSummaryDTO {
	return SkillSummaryDTO{
		Name:            item.Name,
		Description:     item.Description,
		Eligible:        item.Eligible,
		RequiresTools:   append([]string(nil), item.RequiresTools...),
		FileCount:       len(item.Files),
		DisabledReasons: append([]string(nil), item.DisabledReasons...),
	}
}

func skillDetailDTOFromView(item skills.View) SkillDetailDTO {
	files := make([]SkillFileDTO, 0, len(item.Files))
	for _, f := range item.Files {
		files = append(files, SkillFileDTO{Path: f.Path, Size: f.Size})
	}
	return SkillDetailDTO{
		SkillSummaryDTO: skillSummaryDTOFromView(item),
		Revision:        item.Revision,
		Instruction:     item.Instruction,
		License:         item.License,
		Compatibility:   item.Compatibility,
		Files:           files,
	}
}

func skillSummaryDTOsFromViews(items []skills.View) []SkillSummaryDTO {
	out := make([]SkillSummaryDTO, 0, len(items))
	for _, item := range items {
		out = append(out, skillSummaryDTOFromView(item))
	}
	return out
}
