package api

import (
	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/memory"
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

// ToolListResponse is the response body for listing toolset.
type ToolListResponse struct {
	Items []ToolSummaryDTO `json:"items"`
	Total int              `json:"total"`
}

type SkillRequirementsDTO struct {
	Tools    []string `json:"tools,omitempty"`
	Toolsets []string `json:"toolsets,omitempty"`
	Bins     []string `json:"bins,omitempty"`
	Env      []string `json:"env,omitempty"`
}

type SkillSummaryDTO struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Version         string               `json:"version"`
	Category        string               `json:"category,omitempty"`
	Source          string               `json:"source"`
	Origin          skills.Origin        `json:"origin"`
	TaskPattern     string               `json:"task_pattern,omitempty"`
	Summary         string               `json:"summary,omitempty"`
	PromotedFrom    string               `json:"promoted_from,omitempty"`
	Eligible        bool                 `json:"eligible"`
	Requirements    SkillRequirementsDTO `json:"requirements,omitempty"`
	DisabledReasons []string             `json:"disabled_reasons,omitempty"`
	CreatedByRunID  string               `json:"created_by_run_id,omitempty"`
	Replaces        []string             `json:"replaces,omitempty"`
}

type SkillDetailDTO struct {
	SkillSummaryDTO
	Path         string   `json:"path"`
	Instruction  string   `json:"instruction"`
	Scripts      []string `json:"scripts,omitempty"`
	Files        []string `json:"files,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Platforms    []string `json:"platforms,omitempty"`
	TriggerHints []string `json:"trigger_hints,omitempty"`
}

type SkillListResponse struct {
	Items []SkillSummaryDTO `json:"items"`
	Total int               `json:"total"`
}

type SkillEnvelope struct {
	Item SkillDetailDTO `json:"item"`
}

type SkillFileResponse struct {
	Item app.SkillFileView `json:"item"`
}

func skillSummaryDTOFromView(item skills.View) SkillSummaryDTO {
	return SkillSummaryDTO{
		ID:             item.ID,
		Name:           item.Name,
		Version:        item.Version,
		Category:       item.Category,
		Source:         item.Source,
		Origin:         item.Origin,
		TaskPattern:    item.TaskPattern,
		Summary:        item.Summary,
		PromotedFrom:   item.PromotedFrom,
		Eligible:       item.Eligible,
		Requirements:   skillRequirementsDTOFromDomain(item.Requires),
		CreatedByRunID: item.CreatedByRunID,
		Replaces:       append([]string(nil), item.Replaces...),
	}
}

func skillDetailDTOFromView(item skills.View) SkillDetailDTO {
	return SkillDetailDTO{
		SkillSummaryDTO: skillSummaryDTOFromView(item),
		Path:            item.Path,
		Instruction:     item.Instruction,
		Scripts:         append([]string(nil), item.Scripts...),
		Files:           append([]string(nil), item.Files...),
		Tags:            append([]string(nil), item.Tags...),
		Platforms:       append([]string(nil), item.Platforms...),
		TriggerHints:    append([]string(nil), item.TriggerHints...),
	}
}

func skillSummaryDTOsFromViews(items []skills.View) []SkillSummaryDTO {
	out := make([]SkillSummaryDTO, 0, len(items))
	for _, item := range items {
		out = append(out, skillSummaryDTOFromView(item))
	}
	return out
}

func skillRequirementsDTOFromDomain(item skills.Requirements) SkillRequirementsDTO {
	return DefaultConverter.skillRequirementsDTOFromDomain(item)
}

type MemoryRecordDTO struct {
	Ref         string   `json:"ref"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Scope       string   `json:"scope,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Origin      string   `json:"origin,omitempty"`
	TaskPattern string   `json:"task_pattern,omitempty"`
	Path        string   `json:"path"`
	Body        string   `json:"body"`
	Created     string   `json:"created,omitempty"`
	Updated     string   `json:"updated,omitempty"`
	SourceRun   string   `json:"source_run,omitempty"`
	SourceRefs  []string `json:"source_refs,omitempty"`
}

type MemoryRecordListResponse struct {
	Items []MemoryRecordDTO `json:"items"`
}

type MemorySearchItemDTO struct {
	Ref         string   `json:"ref"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Scope       string   `json:"scope,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Origin      string   `json:"origin,omitempty"`
	TaskPattern string   `json:"task_pattern,omitempty"`
	Path        string   `json:"path"`
	Snippet     string   `json:"snippet"`
	Score       float64  `json:"score"`
	Created     string   `json:"created,omitempty"`
	Updated     string   `json:"updated,omitempty"`
	SourceRun   string   `json:"source_run,omitempty"`
	SourceRefs  []string `json:"source_refs,omitempty"`
}

type MemorySearchResponse struct {
	Items []MemorySearchItemDTO `json:"items"`
}

func memoryRecordDTOsFromDomain(records []memory.Record) []MemoryRecordDTO {
	return DefaultConverter.memoryRecordDTOsFromDomain(records)
}

func memorySearchItemDTOsFromDomain(records []memory.SearchItem) []MemorySearchItemDTO {
	return DefaultConverter.memorySearchItemDTOsFromDomain(records)
}
