package web

import (
	"encoding/json"
	"time"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/skills"
)

type PairDeviceRequest struct {
	PairingCode string `json:"pairing_code"`
	DeviceName  string `json:"device_name"`
	Platform    string `json:"platform"`
}

type PairDeviceResponse struct {
	Device      DeviceDTO `json:"device"`
	AccessToken string    `json:"access_token"`
}

type DeviceDTO struct {
	DeviceID   string  `json:"device_id"`
	Name       string  `json:"name"`
	Platform   string  `json:"platform"`
	CreatedAt  string  `json:"created_at"`
	LastSeenAt string  `json:"last_seen_at"`
	RevokedAt  *string `json:"revoked_at,omitempty"`
}

type DeviceListResponse struct {
	Items []DeviceDTO `json:"items"`
}

type RegisterDevicePushTokenRequest struct {
	Provider string `json:"provider"`
	Platform string `json:"platform"`
	Token    string `json:"token"`
}

type DevicePushTokenDTO struct {
	DeviceID  string `json:"device_id"`
	Provider  string `json:"provider"`
	Platform  string `json:"platform"`
	UpdatedAt string `json:"updated_at"`
}

func deviceDTOFromView(view app.DeviceView) DeviceDTO {
	return DeviceDTO{
		DeviceID:   view.DeviceID,
		Name:       view.Name,
		Platform:   view.Platform,
		CreatedAt:  view.CreatedAt.UTC().Format(time.RFC3339Nano),
		LastSeenAt: view.LastSeenAt.UTC().Format(time.RFC3339Nano),
		RevokedAt:  optionalDeviceTime(view.RevokedAt),
	}
}

func devicePushTokenDTOFromView(view app.DevicePushTokenView) DevicePushTokenDTO {
	return DevicePushTokenDTO{
		DeviceID:  view.DeviceID,
		Provider:  view.Provider,
		Platform:  view.Platform,
		UpdatedAt: view.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func optionalDeviceTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	return new(value.UTC().Format(time.RFC3339Nano))
}

type MemoryRecordDTO struct {
	Ref          string                    `json:"ref"`
	Kind         string                    `json:"kind"`
	Title        string                    `json:"title"`
	Status       string                    `json:"status"`
	Scope        string                    `json:"scope,omitempty"`
	Tags         []string                  `json:"tags,omitempty"`
	Origin       string                    `json:"origin,omitempty"`
	TaskPattern  string                    `json:"task_pattern,omitempty"`
	Path         string                    `json:"path"`
	Body         string                    `json:"body"`
	Created      string                    `json:"created,omitempty"`
	Updated      string                    `json:"updated,omitempty"`
	ValidFrom    string                    `json:"valid_from,omitempty"`
	ValidUntil   string                    `json:"valid_until,omitempty"`
	SourceRun    string                    `json:"source_run,omitempty"`
	SourceRefs   []string                  `json:"source_refs,omitempty"`
	EvidenceRefs []string                  `json:"evidence_refs,omitempty"`
	Relations    []MemoryRecordRelationDTO `json:"relations,omitempty"`
}

type MemoryRecordRelationDTO struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Reason string `json:"reason,omitempty"`
}

type MemoryRecordListResponse struct {
	Items []MemoryRecordDTO `json:"items"`
}

type MemorySearchItemDTO struct {
	Ref          string                    `json:"ref"`
	Kind         string                    `json:"kind"`
	Title        string                    `json:"title"`
	Status       string                    `json:"status"`
	Scope        string                    `json:"scope,omitempty"`
	Tags         []string                  `json:"tags,omitempty"`
	Origin       string                    `json:"origin,omitempty"`
	TaskPattern  string                    `json:"task_pattern,omitempty"`
	Path         string                    `json:"path"`
	Snippet      string                    `json:"snippet"`
	Score        float64                   `json:"score"`
	Created      string                    `json:"created,omitempty"`
	Updated      string                    `json:"updated,omitempty"`
	ValidFrom    string                    `json:"valid_from,omitempty"`
	ValidUntil   string                    `json:"valid_until,omitempty"`
	SourceRun    string                    `json:"source_run,omitempty"`
	SourceRefs   []string                  `json:"source_refs,omitempty"`
	EvidenceRefs []string                  `json:"evidence_refs,omitempty"`
	Relations    []MemoryRecordRelationDTO `json:"relations,omitempty"`
}

type MemorySearchResponse struct {
	Items []MemorySearchItemDTO `json:"items"`
}

type WorkingCheckpointEnvelope struct {
	Item *WorkingCheckpointDTO `json:"item,omitempty"`
}

type UpdateWorkingCheckpointRequest struct {
	Content        string `json:"content"`
	RelatedSkillID string `json:"related_skill_id"`
}

type WorkingCheckpointDTO struct {
	ThreadID       string    `json:"thread_id"`
	Content        string    `json:"content"`
	RelatedSkillID string    `json:"related_skill_id"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func workingCheckpointDTOFromView(view *app.WorkingCheckpointView) *WorkingCheckpointDTO {
	if view == nil {
		return nil
	}
	return &WorkingCheckpointDTO{
		ThreadID:       view.ThreadID,
		Content:        view.Content,
		RelatedSkillID: view.RelatedSkillID,
		UpdatedAt:      view.UpdatedAt,
	}
}

func memoryRecordDTOsFromDomain(records []memorymodule.Record) []MemoryRecordDTO {
	items := make([]MemoryRecordDTO, 0, len(records))
	for _, record := range records {
		items = append(items, MemoryRecordDTO{
			Ref:          record.Ref,
			Kind:         string(record.Kind),
			Title:        record.Title,
			Status:       string(record.Status),
			Scope:        record.Scope,
			Tags:         append([]string(nil), record.Tags...),
			Origin:       record.Origin,
			TaskPattern:  record.TaskPattern,
			Path:         record.RelPath,
			Body:         record.Body,
			Created:      record.Created,
			Updated:      record.Updated,
			ValidFrom:    record.ValidFrom,
			ValidUntil:   record.ValidUntil,
			SourceRun:    record.SourceRun,
			SourceRefs:   append([]string(nil), record.SourceRefs...),
			EvidenceRefs: append([]string(nil), record.EvidenceRefs...),
			Relations:    memoryRecordRelationDTOsFromDomain(record.Relations),
		})
	}
	return items
}

func memorySearchItemDTOsFromDomain(records []memorymodule.SearchItem) []MemorySearchItemDTO {
	items := make([]MemorySearchItemDTO, 0, len(records))
	for _, record := range records {
		items = append(items, MemorySearchItemDTO{
			Ref:          record.Ref,
			Kind:         record.Kind,
			Title:        record.Title,
			Status:       record.Status,
			Scope:        record.Scope,
			Tags:         append([]string(nil), record.Tags...),
			Origin:       record.Origin,
			TaskPattern:  record.TaskPattern,
			Path:         record.Path,
			Snippet:      record.Snippet,
			Score:        record.Score,
			Created:      record.Created,
			Updated:      record.Updated,
			ValidFrom:    record.ValidFrom,
			ValidUntil:   record.ValidUntil,
			SourceRun:    record.SourceRun,
			SourceRefs:   append([]string(nil), record.SourceRefs...),
			EvidenceRefs: append([]string(nil), record.EvidenceRefs...),
			Relations:    memoryRecordRelationDTOsFromDomain(record.Relations),
		})
	}
	return items
}

func memoryRecordRelationDTOsFromDomain(items []memorymodule.RecordRelation) []MemoryRecordRelationDTO {
	out := make([]MemoryRecordRelationDTO, 0, len(items))
	for _, item := range items {
		out = append(out, MemoryRecordRelationDTO{
			Type:   string(item.Type),
			Target: item.Target,
			Reason: item.Reason,
		})
	}
	return out
}

type PlanStepDTO struct {
	ID                 string                  `json:"id"`
	Action             string                  `json:"action"`
	Status             string                  `json:"status"`
	DependsOn          []string                `json:"depends_on,omitempty"`
	RepoTargets        []PlanRepoTargetDTO     `json:"repo_targets,omitempty"`
	VerificationIntent []VerificationIntentDTO `json:"verification_intent,omitempty"`
	Risk               string                  `json:"risk,omitempty"`
	ToolHints          []string                `json:"tool_hints,omitempty"`
	Evidence           []PlanEvidenceDTO       `json:"evidence,omitempty"`
}

type PlanRepoTargetDTO struct {
	Path       string `json:"path"`
	Symbol     string `json:"symbol,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	EndLine    int    `json:"end_line,omitempty"`
	Reason     string `json:"reason"`
	Confidence string `json:"confidence"`
}

type VerificationIntentDTO struct {
	Kind    string   `json:"kind"`
	Command []string `json:"command,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Reason  string   `json:"reason"`
}

type PlanEvidenceDTO struct {
	ID          string   `json:"id"`
	StepID      string   `json:"step_id"`
	Kind        string   `json:"kind"`
	Status      string   `json:"status"`
	Summary     string   `json:"summary"`
	ToolName    string   `json:"tool_name,omitempty"`
	Command     []string `json:"command,omitempty"`
	Paths       []string `json:"paths,omitempty"`
	DiffRef     string   `json:"diff_ref,omitempty"`
	ChildRunID  string   `json:"child_run_id,omitempty"`
	Error       string   `json:"error,omitempty"`
	SourceRunID string   `json:"source_run_id,omitempty"`
	RecordedAt  string   `json:"recorded_at,omitempty"`
}

type PlanDTO struct {
	PlanID    string        `json:"plan_id"`
	SessionID string        `json:"session_id"`
	RunID     string        `json:"run_id"`
	Steps     []PlanStepDTO `json:"steps"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func planDTOFromRuntime(plan *runtime.Plan) *PlanDTO {
	if plan == nil {
		return nil
	}
	steps := make([]PlanStepDTO, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		steps = append(steps, PlanStepDTO{
			ID:                 step.ID,
			Action:             step.Action,
			Status:             string(step.Status),
			DependsOn:          append([]string(nil), step.DependsOn...),
			RepoTargets:        planRepoTargetDTOsFromRuntime(step.RepoTargets),
			VerificationIntent: verificationIntentDTOsFromRuntime(step.VerificationIntent),
			Risk:               string(step.Risk),
			ToolHints:          append([]string(nil), step.ToolHints...),
			Evidence:           planEvidenceDTOsFromRuntime(step.Evidence),
		})
	}
	return &PlanDTO{
		PlanID:    plan.PlanID,
		SessionID: plan.SessionID,
		RunID:     plan.RunID,
		Steps:     steps,
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	}
}

func planRepoTargetDTOsFromRuntime(items []runtime.PlanRepoTarget) []PlanRepoTargetDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]PlanRepoTargetDTO, 0, len(items))
	for _, item := range items {
		result = append(result, PlanRepoTargetDTO{
			Path:       item.Path,
			Symbol:     item.Symbol,
			StartLine:  item.StartLine,
			EndLine:    item.EndLine,
			Reason:     item.Reason,
			Confidence: item.Confidence,
		})
	}
	return result
}

func verificationIntentDTOsFromRuntime(items []runtime.VerificationIntent) []VerificationIntentDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]VerificationIntentDTO, 0, len(items))
	for _, item := range items {
		result = append(result, VerificationIntentDTO{
			Kind:    item.Kind,
			Command: append([]string(nil), item.Command...),
			Paths:   append([]string(nil), item.Paths...),
			Reason:  item.Reason,
		})
	}
	return result
}

func planEvidenceDTOsFromRuntime(items []runtime.PlanEvidence) []PlanEvidenceDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]PlanEvidenceDTO, 0, len(items))
	for _, item := range items {
		recordedAt := ""
		if !item.RecordedAt.IsZero() {
			recordedAt = item.RecordedAt.Format(time.RFC3339)
		}
		result = append(result, PlanEvidenceDTO{
			ID:          item.ID,
			StepID:      item.StepID,
			Kind:        string(item.Kind),
			Status:      string(item.Status),
			Summary:     item.Summary,
			ToolName:    item.ToolName,
			Command:     append([]string(nil), item.Command...),
			Paths:       append([]string(nil), item.Paths...),
			DiffRef:     item.DiffRef,
			ChildRunID:  item.ChildRunID,
			Error:       item.Error,
			SourceRunID: item.SourceRunID,
			RecordedAt:  recordedAt,
		})
	}
	return result
}

type SkillRequirementsDTO struct {
	Tools    []string `json:"tools,omitempty"`
	Toolsets []string `json:"toolsets,omitempty"`
	Bins     []string `json:"bins,omitempty"`
	Env      []string `json:"env,omitempty"`
}

type SkillSummaryDTO struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	Category        string                 `json:"category,omitempty"`
	Source          string                 `json:"source"`
	Origin          skills.Origin          `json:"origin"`
	LifecycleStatus skills.LifecycleStatus `json:"lifecycle_status"`
	TaskPattern     string                 `json:"task_pattern,omitempty"`
	Summary         string                 `json:"summary,omitempty"`
	PromotedFrom    string                 `json:"promoted_from,omitempty"`
	Eligible        bool                   `json:"eligible"`
	Requirements    SkillRequirementsDTO   `json:"requirements,omitempty"`
	DisabledReasons []string               `json:"disabled_reasons,omitempty"`
	CreatedByRunID  string                 `json:"created_by_run_id,omitempty"`
	UpdatedByRunID  string                 `json:"updated_by_run_id,omitempty"`
	EvidenceRefs    []string               `json:"evidence_refs,omitempty"`
	Replaces        []string               `json:"replaces,omitempty"`
	ReplacedBy      []string               `json:"replaced_by,omitempty"`
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
		ID:              item.ID,
		Name:            item.Name,
		Version:         item.Version,
		Category:        item.Category,
		Source:          item.Source,
		Origin:          item.Origin,
		LifecycleStatus: item.LifecycleStatus,
		TaskPattern:     item.TaskPattern,
		Summary:         item.Summary,
		PromotedFrom:    item.PromotedFrom,
		Eligible:        item.Eligible,
		Requirements:    skillRequirementsDTOFromDomain(item.Requires),
		DisabledReasons: append([]string(nil), item.DisabledReasons...),
		CreatedByRunID:  item.CreatedByRunID,
		UpdatedByRunID:  item.UpdatedByRunID,
		EvidenceRefs:    append([]string(nil), item.EvidenceRefs...),
		Replaces:        append([]string(nil), item.Replaces...),
		ReplacedBy:      append([]string(nil), item.ReplacedBy...),
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
	return SkillRequirementsDTO{
		Tools:    append([]string(nil), item.Tools...),
		Toolsets: append([]string(nil), item.Toolsets...),
		Bins:     append([]string(nil), item.Bins...),
		Env:      append([]string(nil), item.Env...),
	}
}

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
	TraceDebug      bool `json:"trace_debug"`
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
	Name           string   `json:"name"`
	Source         string   `json:"source"`
	Kind           string   `json:"kind"`
	Category       string   `json:"category"`
	ResourceScope  string   `json:"resource_scope,omitempty"`
	Profiles       []string `json:"profiles,omitempty"`
	Enabled        bool     `json:"enabled"`
	HealthState    string   `json:"health_state"`
	HealthReason   string   `json:"health_reason,omitempty"`
	ParallelPolicy string   `json:"parallel_policy,omitempty"`
	PlanPolicy     string   `json:"plan_policy,omitempty"`
	FactPolicy     string   `json:"fact_policy,omitempty"`
	Risk           string   `json:"risk"`
	RootDir        string   `json:"root_dir,omitempty"`
	WorkDir        string   `json:"work_dir,omitempty"`
	DefaultTimeout int      `json:"default_timeout,omitempty"`
}

func capabilitiesSummaryDTOFromSnapshot(snapshot app.SystemCapabilitySummary) CapabilitiesSummaryDTO {
	return CapabilitiesSummaryDTO{
		ToolCount:                  snapshot.ToolCount,
		EnabledToolCount:           snapshot.EnabledToolCount,
		SkillCount:                 snapshot.SkillCount,
		EligibleSkillCount:         snapshot.EligibleSkillCount,
		IneligibleSkillCount:       snapshot.IneligibleSkillCount,
		InvalidSkillCount:          snapshot.InvalidSkillCount,
		MCPConfiguredProviderCount: snapshot.MCPConfiguredProviderCount,
		MCPEnabledProviderCount:    snapshot.MCPEnabledProviderCount,
		MCPHealthyProviderCount:    snapshot.MCPHealthyProviderCount,
	}
}

func capabilitiesModelDTOFromSnapshot(snapshot app.SystemModelCapabilities) CapabilitiesModelDTO {
	return CapabilitiesModelDTO{Name: snapshot.Name}
}

func capabilitiesFeaturesDTOFromSnapshot(snapshot app.SystemFeatureCapabilities) CapabilitiesFeaturesDTO {
	return CapabilitiesFeaturesDTO(snapshot)
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
	if len(snapshot) == 0 {
		return nil
	}
	items := make([]ProviderReadinessDTO, 0, len(snapshot))
	for _, item := range snapshot {
		items = append(items, ProviderReadinessDTO{
			Scope:         item.Scope,
			Provider:      item.Provider,
			Status:        string(item.Status),
			Reason:        item.Reason,
			StartupStatus: item.StartupStatus,
			AuthStatus:    item.AuthStatus,
		})
	}
	return items
}

func capabilitiesToolsDTOFromSnapshot(snapshot []app.SystemToolCapability) []CapabilitiesToolDTO {
	items := make([]CapabilitiesToolDTO, 0, len(snapshot))
	for _, item := range snapshot {
		items = append(items, CapabilitiesToolDTO{
			Name:           item.Name,
			Source:         item.Source,
			Kind:           item.Kind,
			Category:       item.Category,
			ResourceScope:  item.ResourceScope,
			Profiles:       append([]string(nil), item.Profiles...),
			Enabled:        item.Enabled,
			HealthState:    item.HealthState,
			HealthReason:   item.HealthReason,
			ParallelPolicy: item.ParallelPolicy,
			PlanPolicy:     item.PlanPolicy,
			FactPolicy:     item.FactPolicy,
			Risk:           item.Risk,
			RootDir:        item.RootDir,
			WorkDir:        item.WorkDir,
			DefaultTimeout: item.DefaultTimeout,
		})
	}
	return items
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
type RunSummaryDTO struct {
	RunID          string    `json:"run_id"`
	ThreadID       string    `json:"thread_id"`
	ThreadTitle    string    `json:"thread_title"`
	Status         string    `json:"status"`
	Mode           string    `json:"mode"`
	Preview        string    `json:"preview"`
	LastEventLabel string    `json:"last_event_label"`
	AttentionLevel string    `json:"attention_level"`
	DurationMS     int64     `json:"duration_ms"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PendingActionSummaryDTO is a lightweight summary of a pending action.
type PendingActionSummaryDTO struct {
	ActionID  string                   `json:"action_id"`
	RunID     string                   `json:"run_id"`
	ThreadID  string                   `json:"thread_id"`
	Kind      string                   `json:"kind"`
	Status    string                   `json:"status"`
	Title     string                   `json:"title"`
	Body      string                   `json:"body,omitempty"`
	Options   []PendingActionOptionDTO `json:"options"`
	CreatedAt time.Time                `json:"created_at"`
}

// ToolSummaryDTO is an alias to the capabilities tool DTO.
type ToolSummaryDTO = CapabilitiesToolDTO

// ToolListResponse is the response body for listing tools.
type ToolListResponse struct {
	Items []ToolSummaryDTO `json:"items"`
	Total int              `json:"total"`
}

func inboxDTOFromDomain(inbox app.MobileInbox, workspaceRoot string) InboxResponse {
	return InboxResponse{
		PendingActions:     pendingActionSummaryDTOsFromDomain(inbox.PendingActions),
		ActiveRuns:         runSummaryDTOsFromDomain(inbox.ActiveRuns),
		RecentTerminalRuns: runSummaryDTOsFromDomain(inbox.RecentTerminalRuns),
		System:             systemStatusDTOFromSnapshot(inbox.System, workspaceRoot),
	}
}

func runSummaryDTOsFromDomain(items []app.RunSummary) []RunSummaryDTO {
	result := make([]RunSummaryDTO, 0, len(items))
	for _, item := range items {
		result = append(result, RunSummaryDTO{
			RunID:          item.RunID,
			ThreadID:       item.ThreadID,
			ThreadTitle:    item.ThreadTitle,
			Status:         item.Status,
			Mode:           item.Mode,
			Preview:        item.Preview,
			LastEventLabel: item.LastEventLabel,
			AttentionLevel: item.AttentionLevel,
			DurationMS:     item.DurationMS,
			CreatedAt:      item.CreatedAt,
			UpdatedAt:      item.UpdatedAt,
		})
	}
	return result
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

// MessageContentDTO represents the content block of a message.
type MessageContentDTO struct {
	Type  string           `json:"type"`
	Text  string           `json:"text"`
	Parts []MessagePartDTO `json:"parts,omitempty"`
}

// MessageDTO represents a single message in a thread.
type MessageDTO struct {
	ID        string            `json:"id"`
	ThreadID  string            `json:"thread_id"`
	Role      string            `json:"role"`
	Content   MessageContentDTO `json:"content"`
	CreatedAt time.Time         `json:"created_at"`
	RunID     string            `json:"run_id,omitempty"`
}

// MessageListResponse is the response body for listing messages.
type MessageListResponse struct {
	Items []MessageDTO `json:"items"`
}

// CreateMessageRequest is the body for creating a message.
type CreateMessageRequest struct {
	Content MessageContentDTO `json:"content" validate:"required"`
}

// MessagePartDTO represents a rich-content part inside a message.
type MessagePartDTO struct {
	Kind             string              `json:"kind"`
	Text             string              `json:"text,omitempty"`
	Reasoning        string              `json:"reasoning,omitempty"`
	Status           string              `json:"status,omitempty"`
	Title            string              `json:"title,omitempty"`
	Summary          string              `json:"summary,omitempty"`
	Changed          []string            `json:"changed,omitempty"`
	Verified         []string            `json:"verified,omitempty"`
	Risks            []string            `json:"risks,omitempty"`
	Items            []DisclosureItemDTO `json:"items,omitempty"`
	DetailRunID      string              `json:"detail_run_id,omitempty"`
	RunID            string              `json:"run_id,omitempty"`
	Label            string              `json:"label,omitempty"`
	DecisionID       string              `json:"decision_id,omitempty"`
	Question         string              `json:"question,omitempty"`
	SelectedOptionID string              `json:"selected_option_id,omitempty"`
	Answer           string              `json:"answer,omitempty"`
	Options          []DecisionOptionDTO `json:"options,omitempty"`
	Action           *MessageActionDTO   `json:"action,omitempty"`
}

// DisclosureItemDTO represents a single disclosure item inside a result part.
type DisclosureItemDTO struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Tone    string `json:"tone"`
	SkillID string `json:"skill_id,omitempty"`
}

// DecisionOptionDTO represents a single option in a decision part.
type DecisionOptionDTO struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// MessageActionDTO represents an action attached to a message part.
type MessageActionDTO struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
	Label string `json:"label"`
}

func messageDTOFromDomain(message app.Message) MessageDTO {
	return MessageDTO{
		ID:       message.ID,
		ThreadID: message.ThreadID,
		Role:     message.Role,
		Content: MessageContentDTO{
			Type:  message.Content.Type,
			Text:  message.Content.Text,
			Parts: messagePartDTOsFromDomain(message.Content.Parts),
		},
		CreatedAt: message.CreatedAt,
		RunID:     message.RunID,
	}
}

func messagePartDTOsFromDomain(parts []app.MessagePart) []MessagePartDTO {
	if len(parts) == 0 {
		return nil
	}
	items := make([]MessagePartDTO, 0, len(parts))
	for _, part := range parts {
		item := MessagePartDTO{
			Kind:             part.Kind,
			Text:             part.Text,
			Reasoning:        part.Reasoning,
			Status:           part.Status,
			Title:            part.Title,
			Summary:          part.Summary,
			Changed:          part.Changed,
			Verified:         part.Verified,
			Risks:            part.Risks,
			Items:            disclosureItemDTOsFromDomain(part.Items),
			DetailRunID:      part.DetailRunID,
			RunID:            part.RunID,
			Label:            part.Label,
			DecisionID:       part.DecisionID,
			Question:         part.Question,
			SelectedOptionID: part.SelectedOptionID,
			Answer:           part.Answer,
			Options:          decisionOptionDTOsFromDomain(part.Options),
			Action:           messageActionDTOFromDomain(part.Action),
		}
		if part.Kind == "result" {
			item.Changed = nonNilStrings(item.Changed)
			item.Verified = nonNilStrings(item.Verified)
			item.Risks = nonNilStrings(item.Risks)
		}
		items = append(items, item)
	}
	return items
}

func disclosureItemDTOsFromDomain(items []app.DisclosureItem) []DisclosureItemDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]DisclosureItemDTO, 0, len(items))
	for _, item := range items {
		result = append(result, DisclosureItemDTO{
			Kind:    item.Kind,
			Label:   item.Label,
			Detail:  item.Detail,
			Tone:    item.Tone,
			SkillID: item.SkillID,
		})
	}
	return result
}

func messageActionDTOFromDomain(action *app.MessageAction) *MessageActionDTO {
	if action == nil {
		return nil
	}
	return &MessageActionDTO{
		Kind:  action.Kind,
		RunID: action.RunID,
		Label: action.Label,
	}
}

func messageDTOsFromDomain(items []app.Message) []MessageDTO {
	result := make([]MessageDTO, 0, len(items))
	for _, item := range items {
		result = append(result, messageDTOFromDomain(item))
	}
	return result
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// DecidePendingActionRequest is the body for approving or rejecting a pending action.
type DecidePendingActionRequest struct {
	Decision         string `json:"decision"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
	Answer           string `json:"answer,omitempty"`
}

// PendingActionDecisionDTO represents the operator's decision on a pending action.
type PendingActionDecisionDTO struct {
	ActionID         string     `json:"action_id"`
	RunID            string     `json:"run_id"`
	Status           string     `json:"status"`
	Decision         string     `json:"decision"`
	SelectedOptionID string     `json:"selected_option_id,omitempty"`
	Answer           string     `json:"answer,omitempty"`
	DecidedAt        *time.Time `json:"decided_at,omitempty"`
}

// PendingActionListResponse is the response body for listing pending actions.
type PendingActionListResponse struct {
	Items []PendingActionSummaryDTO `json:"items"`
}

// PendingActionDetailDTO represents the full detail of a pending action.
type PendingActionDetailDTO struct {
	ActionID  string                   `json:"action_id"`
	RunID     string                   `json:"run_id"`
	ThreadID  string                   `json:"thread_id"`
	Kind      string                   `json:"kind"`
	Status    string                   `json:"status"`
	Title     string                   `json:"title"`
	Body      string                   `json:"body,omitempty"`
	Options   []PendingActionOptionDTO `json:"options"`
	Payload   map[string]any           `json:"payload"`
	Reason    string                   `json:"reason,omitempty"`
	Rule      string                   `json:"rule,omitempty"`
	CreatedAt time.Time                `json:"created_at"`
}

// PendingActionOptionDTO represents a single option in a pending action.
type PendingActionOptionDTO struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

func pendingActionDecisionDTOFromDomain(record events.PendingActionRecord) PendingActionDecisionDTO {
	status := string(record.Status)
	decision, selectedOptionID, answer := parsePendingActionDecision(record)
	return PendingActionDecisionDTO{
		ActionID:         record.ActionID,
		RunID:            record.RunID,
		Status:           status,
		Decision:         decision,
		SelectedOptionID: selectedOptionID,
		Answer:           answer,
		DecidedAt:        record.DecidedAt,
	}
}

func parsePendingActionDecision(record events.PendingActionRecord) (string, string, string) {
	var payload struct {
		Action           string `json:"action"`
		SelectedOptionID string `json:"selected_option_id"`
		Answer           string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(record.DecisionJSON), &payload); err == nil {
		return payload.Action, payload.SelectedOptionID, payload.Answer
	}
	return "", "", ""
}

func pendingActionDetailDTOFromDomain(item app.PendingActionDetail) PendingActionDetailDTO {
	return PendingActionDetailDTO{
		ActionID:  item.ActionID,
		RunID:     item.RunID,
		ThreadID:  item.ThreadID,
		Kind:      item.Kind,
		Status:    item.Status,
		Title:     item.Title,
		Body:      item.Body,
		Options:   pendingActionOptionDTOsFromDomain(item.Options),
		Payload:   item.Payload,
		Reason:    item.Reason,
		Rule:      item.Rule,
		CreatedAt: item.CreatedAt,
	}
}

func pendingActionListResponseFromDomain(items []app.PendingActionSummary) PendingActionListResponse {
	return PendingActionListResponse{Items: pendingActionSummaryDTOsFromDomain(items)}
}

func decisionOptionDTOsFromDomain(options []app.DecisionOption) []DecisionOptionDTO {
	if len(options) == 0 {
		return nil
	}
	items := make([]DecisionOptionDTO, 0, len(options))
	for _, option := range options {
		items = append(items, DecisionOptionDTO{
			ID:          option.ID,
			Label:       option.Label,
			Description: option.Description,
		})
	}
	return items
}

func pendingActionOptionDTOsFromDomain(items []app.PendingActionOption) []PendingActionOptionDTO {
	result := make([]PendingActionOptionDTO, 0, len(items))
	for _, item := range items {
		result = append(result, PendingActionOptionDTO{
			ID:          item.ID,
			Label:       item.Label,
			Description: item.Description,
		})
	}
	return result
}

func pendingActionSummaryDTOsFromDomain(items []app.PendingActionSummary) []PendingActionSummaryDTO {
	result := make([]PendingActionSummaryDTO, 0, len(items))
	for _, item := range items {
		result = append(result, PendingActionSummaryDTO{
			ActionID:  item.ActionID,
			RunID:     item.RunID,
			ThreadID:  item.ThreadID,
			Kind:      item.Kind,
			Status:    item.Status,
			Title:     item.Title,
			Body:      item.Body,
			Options:   pendingActionOptionDTOsFromDomain(item.Options),
			CreatedAt: item.CreatedAt,
		})
	}
	return result
}

// CreateRunRequest is the body for creating a new run.
type CreateRunRequest struct {
	SkillID string `json:"skill_id,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

// ResumeRunRequest is the body for resuming an interrupted run.
type ResumeRunRequest struct{}

// RunDTO represents a client-visible run summary.
type RunDTO struct {
	ID          string     `json:"id"`
	ThreadID    string     `json:"thread_id"`
	Status      string     `json:"status"`
	Mode        string     `json:"mode"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// RunEventDTO represents a single event in a run event stream.
type RunEventDTO struct {
	EventID string    `json:"event_id"`
	RunID   string    `json:"run_id"`
	Seq     int64     `json:"seq"`
	TS      time.Time `json:"ts"`
	Type    string    `json:"type"`
	Data    any       `json:"data"`
}

// UnsupportedRunEventDTO captures events the client does not yet understand.
type UnsupportedRunEventDTO struct {
	EventID string         `json:"event_id"`
	RunID   string         `json:"run_id"`
	Seq     int64          `json:"seq"`
	TS      time.Time      `json:"ts"`
	Type    string         `json:"type"`
	Raw     map[string]any `json:"raw,omitempty"`
	Reason  string         `json:"reason"`
}

// RunDetailDTO aggregates a run, its thread, events, and workbench.
type RunDetailDTO struct {
	Run       RunDTO                `json:"run"`
	Thread    ThreadDTO             `json:"thread"`
	Events    []RunEventDTO         `json:"events"`
	Workbench *RuntimeWorkbenchDTO  `json:"workbench"`
	Trace     *runtime.TraceSummary `json:"trace"`
	Raw       *RunDetailRawDTO      `json:"raw,omitempty"`
}

// RunDetailRawDTO holds raw unsupported events for diagnostic purposes.
type RunDetailRawDTO struct {
	UnsupportedEvents []UnsupportedRunEventDTO `json:"unsupported_events"`
}

// InterruptRunResponse is returned after requesting a run interruption.
type InterruptRunResponse struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

func runDTOFromDomain(run app.Run) RunDTO {
	dto := RunDTO{
		ID:        run.ID,
		ThreadID:  run.ThreadID,
		Status:    run.Status,
		Mode:      run.Mode,
		CreatedAt: run.CreatedAt,
	}
	if !run.CompletedAt.IsZero() {
		dto.CompletedAt = &run.CompletedAt
	}
	return dto
}

func runEventDTOFromDomain(event app.RunEvent) RunEventDTO {
	return RunEventDTO{
		EventID: event.EventID,
		RunID:   event.RunID,
		Seq:     event.Seq,
		TS:      event.TS,
		Type:    event.Type,
		Data:    event.Data,
	}
}

func runEventDTOsFromDomain(items []app.RunEvent) []RunEventDTO {
	result := make([]RunEventDTO, 0, len(items))
	for _, item := range items {
		result = append(result, runEventDTOFromDomain(item))
	}
	return result
}

func unsupportedRunEventDTOFromDomain(event app.UnsupportedRunEvent) UnsupportedRunEventDTO {
	return UnsupportedRunEventDTO{
		EventID: event.EventID,
		RunID:   event.RunID,
		Seq:     event.Seq,
		TS:      event.TS,
		Type:    event.Type,
		Raw:     event.Raw,
		Reason:  event.Reason,
	}
}

func unsupportedRunEventDTOsFromDomain(items []app.UnsupportedRunEvent) []UnsupportedRunEventDTO {
	result := make([]UnsupportedRunEventDTO, 0, len(items))
	for _, item := range items {
		result = append(result, unsupportedRunEventDTOFromDomain(item))
	}
	return result
}

type RuntimeWorkbenchDTO struct {
	SessionID           string                  `json:"session_id"`
	Title               string                  `json:"title"`
	State               runtime.SessionState    `json:"state,omitempty"`
	LatestRunID         string                  `json:"latest_run_id,omitempty"`
	LatestRunStatus     string                  `json:"latest_run_status,omitempty"`
	LatestRunMode       string                  `json:"latest_run_mode,omitempty"`
	LatestRunDepth      int                     `json:"latest_run_depth,omitempty"`
	ParentRunID         string                  `json:"parent_run_id,omitempty"`
	Resumable           bool                    `json:"resumable"`
	ResumeReason        string                  `json:"resume_reason,omitempty"`
	TraceSummary        *runtime.TraceSummary   `json:"trace_summary,omitempty"`
	SelectedSkill       *SelectedSkillDTO       `json:"selected_skill,omitempty"`
	LatestDecision      *RunDecisionDTO         `json:"latest_decision,omitempty"`
	SessionSummary      string                  `json:"session_summary,omitempty"`
	SummaryStatus       string                  `json:"summary_status,omitempty"`
	SummarySourceRunID  string                  `json:"summary_source_run_id,omitempty"`
	SummaryUpdatedAt    *time.Time              `json:"summary_updated_at,omitempty"`
	WorkspaceRoot       string                  `json:"workspace_root"`
	GitStatus           WorkspaceGitStatusDTO   `json:"git_status"`
	MutationCheckpoints []MutationCheckpointDTO `json:"mutation_checkpoints,omitempty"`
	RollbackResults     []RollbackSummaryDTO    `json:"rollback_results,omitempty"`
	ContextEconomy      ContextEconomyDTO       `json:"context_economy"`
	ProviderUsage       ProviderUsageDTO        `json:"provider_usage"`
	Artifacts           []ArtifactSummaryDTO    `json:"artifacts,omitempty"`
	TerminalSessions    []TerminalSessionDTO    `json:"terminal_sessions,omitempty"`
	Plan                *PlanDTO                `json:"plan,omitempty"`
	Evidence            []PlanEvidenceDTO       `json:"evidence,omitempty"`
	Subagents           []SubagentRunDTO        `json:"subagents,omitempty"`
	NextStepHint        string                  `json:"next_step_hint,omitempty"`
}

type WorkspaceGitStatusDTO struct {
	WorkspaceRoot string                 `json:"workspace_root"`
	Available     bool                   `json:"available"`
	Branch        string                 `json:"branch,omitempty"`
	Clean         bool                   `json:"clean"`
	Error         string                 `json:"error,omitempty"`
	Entries       []WorkspaceGitEntryDTO `json:"entries,omitempty"`
}

type WorkspaceGitEntryDTO struct {
	Path           string `json:"path"`
	IndexStatus    string `json:"index_status,omitempty"`
	WorktreeStatus string `json:"worktree_status,omitempty"`
}

type SubagentRunDTO struct {
	SubRunID          string    `json:"sub_run_id"`
	ParentRunID       string    `json:"parent_run_id"`
	SessionID         string    `json:"session_id,omitempty"`
	Depth             int       `json:"depth"`
	Task              string    `json:"task"`
	ChildRunMode      string    `json:"child_run_mode,omitempty"`
	WorkspaceMode     string    `json:"workspace_mode,omitempty"`
	WorktreePath      string    `json:"worktree_path,omitempty"`
	ContextMessages   int       `json:"context_messages,omitempty"`
	OrchestrationMode string    `json:"orchestration_mode,omitempty"`
	ParentStepID      string    `json:"parent_step_id,omitempty"`
	State             string    `json:"state"`
	FinalStatus       string    `json:"final_status,omitempty"`
	AcceptanceStatus  string    `json:"acceptance_status,omitempty"`
	AcceptanceReasons []string  `json:"acceptance_reasons,omitempty"`
	EvidenceRefs      []string  `json:"evidence_refs,omitempty"`
	Summary           string    `json:"summary,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type MutationCheckpointDTO struct {
	CheckpointID     string    `json:"checkpoint_id"`
	ToolResultRef    string    `json:"tool_result_ref"`
	ToolName         string    `json:"tool_name"`
	Status           string    `json:"status"`
	Paths            []string  `json:"paths,omitempty"`
	Summary          string    `json:"summary,omitempty"`
	VerifiedDiffStat string    `json:"verified_diff_stat,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type RollbackSummaryDTO struct {
	RollbackID    string    `json:"rollback_id"`
	CheckpointID  string    `json:"checkpoint_id,omitempty"`
	ToolResultRef string    `json:"tool_result_ref"`
	ToolName      string    `json:"tool_name"`
	Status        string    `json:"status"`
	RestoredPaths []string  `json:"restored_paths,omitempty"`
	ConflictPaths []string  `json:"conflict_paths,omitempty"`
	Summary       string    `json:"summary,omitempty"`
	Error         string    `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type ContextEconomyDTO struct {
	LatestPressure          *ContextPressureDTO    `json:"latest_pressure,omitempty"`
	LatestCompression       *ContextCompressionDTO `json:"latest_compression,omitempty"`
	ToolResults             []ContextToolResultDTO `json:"tool_results,omitempty"`
	ToolResultCount         int                    `json:"tool_result_count"`
	ElidedToolResultCount   int                    `json:"elided_tool_result_count"`
	ToolResultTokenEstimate int                    `json:"tool_result_token_estimate"`
	MemoryRefs              []string               `json:"memory_refs,omitempty"`
	ProcedureRefs           []string               `json:"procedure_refs,omitempty"`
}

type ContextPressureDTO struct {
	State                 string `json:"state,omitempty"`
	EstimatedInputTokens  int    `json:"estimated_input_tokens,omitempty"`
	EffectiveWindowTokens int    `json:"effective_window_tokens,omitempty"`
	PercentUsed           int    `json:"percent_used,omitempty"`
}

type ContextCompressionDTO struct {
	BoundaryID   string `json:"boundary_id,omitempty"`
	TokensBefore int    `json:"tokens_before,omitempty"`
	TokensAfter  int    `json:"tokens_after,omitempty"`
	Summary      string `json:"summary,omitempty"`
}

type ContextToolResultDTO struct {
	ResultRef     string   `json:"result_ref"`
	ToolName      string   `json:"tool_name"`
	Status        string   `json:"status"`
	Preview       string   `json:"preview,omitempty"`
	TokenEstimate int      `json:"token_estimate"`
	FullTextBytes int      `json:"full_text_bytes"`
	Elided        bool     `json:"elided"`
	EvidenceRefs  []string `json:"evidence_refs,omitempty"`
}

type ProviderUsageDTO struct {
	CallCount        int                    `json:"call_count"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	TotalTokens      int                    `json:"total_tokens"`
	CachedTokens     int                    `json:"cached_tokens"`
	ReasoningTokens  int                    `json:"reasoning_tokens"`
	Records          []ProviderUsageCallDTO `json:"records,omitempty"`
}

type ProviderUsageCallDTO struct {
	UsageID          string    `json:"usage_id"`
	CallSite         string    `json:"call_site"`
	ProviderName     string    `json:"provider_name"`
	ModelName        string    `json:"model_name"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CachedTokens     int       `json:"cached_tokens"`
	ReasoningTokens  int       `json:"reasoning_tokens"`
	CreatedAt        time.Time `json:"created_at"`
}

type ArtifactSummaryDTO struct {
	ArtifactID          string    `json:"artifact_id"`
	RunID               string    `json:"run_id"`
	SessionID           string    `json:"session_id,omitempty"`
	SourceToolResultRef string    `json:"source_tool_result_ref,omitempty"`
	Kind                string    `json:"kind"`
	Title               string    `json:"title,omitempty"`
	MIMEType            string    `json:"mime_type,omitempty"`
	SizeBytes           int64     `json:"size_bytes"`
	SHA256              string    `json:"sha256"`
	CreatedAt           time.Time `json:"created_at"`
}

type TerminalSessionDTO struct {
	TerminalSessionID string                  `json:"terminal_session_id"`
	RunID             string                  `json:"run_id"`
	SessionID         string                  `json:"session_id,omitempty"`
	Label             string                  `json:"label,omitempty"`
	CommandJSON       string                  `json:"command_json"`
	Cwd               string                  `json:"cwd"`
	Interactive       bool                    `json:"interactive"`
	PTY               bool                    `json:"pty"`
	Status            string                  `json:"status"`
	ProcessID         *int                    `json:"pid,omitempty"`
	ProcessGroupID    *int                    `json:"process_group_id,omitempty"`
	ExitCode          *int                    `json:"exit_code,omitempty"`
	Signal            string                  `json:"signal,omitempty"`
	StdoutArtifactID  string                  `json:"stdout_artifact_id,omitempty"`
	StderrArtifactID  string                  `json:"stderr_artifact_id,omitempty"`
	PTYArtifactID     string                  `json:"pty_artifact_id,omitempty"`
	StartedAt         *time.Time              `json:"started_at,omitempty"`
	EndedAt           *time.Time              `json:"ended_at,omitempty"`
	CreatedAt         time.Time               `json:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at"`
	Logs              []TerminalSessionLogDTO `json:"logs,omitempty"`
}

type TerminalSessionLogDTO struct {
	LogID             string    `json:"log_id"`
	TerminalSessionID string    `json:"terminal_session_id"`
	Stream            string    `json:"stream"`
	ArtifactID        string    `json:"artifact_id"`
	StartOffset       int64     `json:"start_offset"`
	SizeBytes         int64     `json:"size_bytes"`
	CreatedAt         time.Time `json:"created_at"`
}

func runtimeWorkbenchDTOFromDomain(item *app.RuntimeWorkbench) RuntimeWorkbenchDTO {
	if item == nil {
		return RuntimeWorkbenchDTO{}
	}
	dto := RuntimeWorkbenchDTO{
		SessionID:           item.SessionID,
		Title:               item.Title,
		State:               item.State,
		LatestRunID:         item.LatestRunID,
		LatestRunStatus:     string(item.LatestRunStatus),
		LatestRunMode:       item.LatestRunMode,
		LatestRunDepth:      item.LatestRunDepth,
		ParentRunID:         item.ParentRunID,
		Resumable:           item.Resumable,
		ResumeReason:        item.ResumeReason,
		TraceSummary:        item.TraceSummary,
		SelectedSkill:       selectedSkillDTOFromRuntime(item.SelectedSkill),
		LatestDecision:      runDecisionDTOFromDomain(item.LatestDecision),
		SessionSummary:      summaryText(item.SessionSummary),
		SummaryStatus:       summaryStatus(item.SessionSummary),
		SummarySourceRunID:  summarySourceRunID(item.SessionSummary),
		SummaryUpdatedAt:    summaryUpdatedAt(item.SessionSummary),
		WorkspaceRoot:       item.WorkspaceRoot,
		GitStatus:           workspaceGitStatusDTOFromDomain(item.GitStatus),
		MutationCheckpoints: mutationCheckpointDTOsFromDomain(item.MutationCheckpoints),
		RollbackResults:     rollbackSummaryDTOsFromDomain(item.RollbackResults),
		ContextEconomy:      contextEconomyDTOFromDomain(item.ContextEconomy),
		ProviderUsage:       providerUsageDTOFromDomain(item.ProviderUsage),
		Artifacts:           artifactSummaryDTOsFromDomain(item.Artifacts),
		TerminalSessions:    terminalSessionDTOsFromDomain(item.TerminalSessions),
		Plan:                planDTOFromRuntime(item.Plan),
		Evidence:            planEvidenceDTOsFromRuntime(item.Evidence),
		Subagents:           subagentRunDTOsFromDomain(item.Subagents),
		NextStepHint:        item.NextStepHint,
	}
	return dto
}

func terminalSessionDTOsFromDomain(items []app.TerminalSessionSummary) []TerminalSessionDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]TerminalSessionDTO, 0, len(items))
	for _, item := range items {
		result = append(result, TerminalSessionDTO{
			TerminalSessionID: item.TerminalSessionID,
			RunID:             item.RunID,
			SessionID:         item.SessionID,
			Label:             item.Label,
			CommandJSON:       item.CommandJSON,
			Cwd:               item.Cwd,
			Interactive:       item.Interactive,
			PTY:               item.PTY,
			Status:            item.Status,
			ProcessID:         copyOptionalInt(item.ProcessID),
			ProcessGroupID:    copyOptionalInt(item.ProcessGroupID),
			ExitCode:          copyOptionalInt(item.ExitCode),
			Signal:            item.Signal,
			StdoutArtifactID:  item.StdoutArtifactID,
			StderrArtifactID:  item.StderrArtifactID,
			PTYArtifactID:     item.PTYArtifactID,
			StartedAt:         copyOptionalTime(item.StartedAt),
			EndedAt:           copyOptionalTime(item.EndedAt),
			CreatedAt:         item.CreatedAt,
			UpdatedAt:         item.UpdatedAt,
			Logs:              terminalSessionLogDTOsFromDomain(item.Logs),
		})
	}
	return result
}

func terminalSessionLogDTOsFromDomain(items []app.TerminalSessionLogSummary) []TerminalSessionLogDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]TerminalSessionLogDTO, 0, len(items))
	for _, item := range items {
		result = append(result, TerminalSessionLogDTO{
			LogID:             item.LogID,
			TerminalSessionID: item.TerminalSessionID,
			Stream:            item.Stream,
			ArtifactID:        item.ArtifactID,
			StartOffset:       item.StartOffset,
			SizeBytes:         item.SizeBytes,
			CreatedAt:         item.CreatedAt,
		})
	}
	return result
}

func artifactSummaryDTOsFromDomain(items []app.ArtifactSummary) []ArtifactSummaryDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]ArtifactSummaryDTO, 0, len(items))
	for _, item := range items {
		result = append(result, ArtifactSummaryDTO{
			ArtifactID:          item.ArtifactID,
			RunID:               item.RunID,
			SessionID:           item.SessionID,
			SourceToolResultRef: item.SourceToolResultRef,
			Kind:                item.Kind,
			Title:               item.Title,
			MIMEType:            item.MIMEType,
			SizeBytes:           item.SizeBytes,
			SHA256:              item.SHA256,
			CreatedAt:           item.CreatedAt,
		})
	}
	return result
}

func copyOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	return new(*value)
}

func copyOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return new(value.UTC())
}

func workspaceGitStatusDTOFromDomain(item app.WorkspaceGitStatus) WorkspaceGitStatusDTO {
	dto := WorkspaceGitStatusDTO{
		WorkspaceRoot: item.WorkspaceRoot,
		Available:     item.Available,
		Branch:        item.Branch,
		Clean:         item.Clean,
		Error:         item.Error,
	}
	if len(item.Entries) == 0 {
		return dto
	}
	dto.Entries = make([]WorkspaceGitEntryDTO, 0, len(item.Entries))
	for _, entry := range item.Entries {
		dto.Entries = append(dto.Entries, WorkspaceGitEntryDTO{
			Path:           entry.Path,
			IndexStatus:    entry.IndexStatus,
			WorktreeStatus: entry.WorktreeStatus,
		})
	}
	return dto
}

func subagentRunDTOsFromDomain(items []app.SubagentRun) []SubagentRunDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]SubagentRunDTO, 0, len(items))
	for _, item := range items {
		result = append(result, SubagentRunDTO{
			SubRunID:          item.SubRunID,
			ParentRunID:       item.ParentRunID,
			SessionID:         item.SessionID,
			Depth:             item.Depth,
			Task:              item.Task,
			ChildRunMode:      item.ChildRunMode,
			WorkspaceMode:     item.WorkspaceMode,
			WorktreePath:      item.WorktreePath,
			ContextMessages:   item.ContextMessages,
			OrchestrationMode: item.OrchestrationMode,
			ParentStepID:      item.ParentStepID,
			State:             item.State,
			FinalStatus:       item.FinalStatus,
			AcceptanceStatus:  item.AcceptanceStatus,
			AcceptanceReasons: append([]string(nil), item.AcceptanceReasons...),
			EvidenceRefs:      append([]string(nil), item.EvidenceRefs...),
			Summary:           item.Summary,
			UpdatedAt:         item.UpdatedAt,
		})
	}
	return result
}

func contextEconomyDTOFromDomain(item app.ContextEconomySummary) ContextEconomyDTO {
	dto := ContextEconomyDTO{
		ToolResultCount:         item.ToolResultCount,
		ElidedToolResultCount:   item.ElidedToolResultCount,
		ToolResultTokenEstimate: item.ToolResultTokenEstimate,
		MemoryRefs:              append([]string(nil), item.MemoryRefs...),
		ProcedureRefs:           append([]string(nil), item.ProcedureRefs...),
	}
	if item.LatestPressure != nil {
		dto.LatestPressure = &ContextPressureDTO{
			State:                 item.LatestPressure.State,
			EstimatedInputTokens:  item.LatestPressure.EstimatedInputTokens,
			EffectiveWindowTokens: item.LatestPressure.EffectiveWindowTokens,
			PercentUsed:           item.LatestPressure.PercentUsed,
		}
	}
	if item.LatestCompression != nil {
		dto.LatestCompression = &ContextCompressionDTO{
			BoundaryID:   item.LatestCompression.BoundaryID,
			TokensBefore: item.LatestCompression.TokensBefore,
			TokensAfter:  item.LatestCompression.TokensAfter,
			Summary:      item.LatestCompression.Summary,
		}
	}
	if len(item.ToolResults) > 0 {
		dto.ToolResults = make([]ContextToolResultDTO, 0, len(item.ToolResults))
		for _, record := range item.ToolResults {
			dto.ToolResults = append(dto.ToolResults, ContextToolResultDTO{
				ResultRef:     record.ResultRef,
				ToolName:      record.ToolName,
				Status:        record.Status,
				Preview:       record.Preview,
				TokenEstimate: record.TokenEstimate,
				FullTextBytes: record.FullTextBytes,
				Elided:        record.Elided,
				EvidenceRefs:  append([]string(nil), record.EvidenceRefs...),
			})
		}
	}
	return dto
}

func providerUsageDTOFromDomain(item app.ProviderUsageSummary) ProviderUsageDTO {
	dto := ProviderUsageDTO{
		CallCount:        item.CallCount,
		PromptTokens:     item.PromptTokens,
		CompletionTokens: item.CompletionTokens,
		TotalTokens:      item.TotalTokens,
		CachedTokens:     item.CachedTokens,
		ReasoningTokens:  item.ReasoningTokens,
	}
	if len(item.Records) > 0 {
		dto.Records = make([]ProviderUsageCallDTO, 0, len(item.Records))
		for _, record := range item.Records {
			dto.Records = append(dto.Records, ProviderUsageCallDTO{
				UsageID:          record.UsageID,
				CallSite:         record.CallSite,
				ProviderName:     record.ProviderName,
				ModelName:        record.ModelName,
				PromptTokens:     record.PromptTokens,
				CompletionTokens: record.CompletionTokens,
				TotalTokens:      record.TotalTokens,
				CachedTokens:     record.CachedTokens,
				ReasoningTokens:  record.ReasoningTokens,
				CreatedAt:        record.CreatedAt,
			})
		}
	}
	return dto
}

func mutationCheckpointDTOsFromDomain(items []app.MutationCheckpointSummary) []MutationCheckpointDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]MutationCheckpointDTO, 0, len(items))
	for _, item := range items {
		result = append(result, MutationCheckpointDTO{
			CheckpointID:     item.CheckpointID,
			ToolResultRef:    item.ToolResultRef,
			ToolName:         item.ToolName,
			Status:           item.Status,
			Paths:            append([]string(nil), item.Paths...),
			Summary:          item.Summary,
			VerifiedDiffStat: item.VerifiedDiffStat,
			CreatedAt:        item.CreatedAt,
		})
	}
	return result
}

func rollbackSummaryDTOsFromDomain(items []app.RollbackSummary) []RollbackSummaryDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]RollbackSummaryDTO, 0, len(items))
	for _, item := range items {
		result = append(result, RollbackSummaryDTO{
			RollbackID:    item.RollbackID,
			CheckpointID:  item.CheckpointID,
			ToolResultRef: item.ToolResultRef,
			ToolName:      item.ToolName,
			Status:        item.Status,
			RestoredPaths: append([]string(nil), item.RestoredPaths...),
			ConflictPaths: append([]string(nil), item.ConflictPaths...),
			Summary:       item.Summary,
			Error:         item.Error,
			CreatedAt:     item.CreatedAt,
		})
	}
	return result
}

type SelectedSkillDTO struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Source       string               `json:"source,omitempty"`
	Origin       string               `json:"origin,omitempty"`
	TaskPattern  string               `json:"task_pattern,omitempty"`
	Summary      string               `json:"summary,omitempty"`
	PromotedFrom string               `json:"promoted_from,omitempty"`
	Requirements SkillRequirementsDTO `json:"requirements,omitempty"`
	Score        int                  `json:"score,omitempty"`
	MatchedTerms []string             `json:"matched_terms,omitempty"`
}

type RunDecisionDTO struct {
	RunID               string    `json:"run_id"`
	Action              string    `json:"action"`
	Intent              string    `json:"intent,omitempty"`
	SelectedSkillID     string    `json:"selected_skill_id,omitempty"`
	DecisionReason      string    `json:"decision_reason,omitempty"`
	DecisionProfileHash string    `json:"decision_profile_hash,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

func summaryText(summary *runtimehistory.SessionSummary) string {
	if summary == nil {
		return ""
	}
	return summary.Summary
}

func summaryStatus(summary *runtimehistory.SessionSummary) string {
	if summary == nil {
		return ""
	}
	return summary.RunStatus
}

func summarySourceRunID(summary *runtimehistory.SessionSummary) string {
	if summary == nil {
		return ""
	}
	return summary.SourceRunID
}

func summaryUpdatedAt(summary *runtimehistory.SessionSummary) *time.Time {
	if summary == nil {
		return nil
	}
	return &summary.UpdatedAt
}

func runDecisionDTOFromDomain(record *decision.Record) *RunDecisionDTO {
	if record == nil {
		return nil
	}
	return &RunDecisionDTO{
		RunID:               record.RunID,
		Action:              string(record.Action),
		Intent:              record.Intent,
		SelectedSkillID:     record.SelectedSkillID,
		DecisionReason:      record.DecisionReason,
		DecisionProfileHash: record.DecisionProfileHash,
		CreatedAt:           record.CreatedAt,
	}
}

func selectedSkillDTOFromRuntime(skill *runtime.SelectedSkill) *SelectedSkillDTO {
	if skill == nil {
		return nil
	}
	return &SelectedSkillDTO{
		ID:           skill.Skill.ID,
		Name:         skill.Skill.Name,
		Source:       skill.Skill.Source,
		Origin:       string(skill.Skill.Origin),
		TaskPattern:  skill.Skill.TaskPattern,
		Summary:      skill.Skill.Summary,
		PromotedFrom: skill.Skill.PromotedFrom,
		Requirements: skillRequirementsDTOFromDomain(skill.Skill.Requires),
		Score:        skill.Score,
		MatchedTerms: append([]string(nil), skill.MatchedTerms...),
	}
}

// ClientProviderSettingsDTO represents a provider configuration exposed to the client.
type ClientProviderSettingsDTO struct {
	Name                string  `json:"name"`
	Model               string  `json:"model"`
	BaseURL             string  `json:"base_url,omitempty"`
	ReasoningEffort     string  `json:"reasoning_effort,omitempty"`
	TimeoutSeconds      int     `json:"timeout_seconds,omitempty"`
	Temperature         float32 `json:"temperature,omitempty"`
	MaxCompletionTokens int     `json:"max_completion_tokens,omitempty"`
	Enabled             bool    `json:"enabled"`
}

// ClientRuntimeSettingsDTO represents runtime configuration exposed to the client.
type ClientRuntimeSettingsDTO struct {
	StorageDir        string `json:"storage_dir"`
	RunTimeoutSeconds int    `json:"run_timeout_seconds"`
}

// ClientWebSettingsDTO represents web server configuration exposed to the client.
type ClientWebSettingsDTO struct {
	ListenAddr     string   `json:"listen_addr"`
	AllowedOrigins []string `json:"allowed_origins,omitempty"`
}

// ClientSettingsDTO aggregates all client-visible settings.
type ClientSettingsDTO struct {
	ConfigPath    string                      `json:"config_path,omitempty"`
	ConfigDir     string                      `json:"config_dir,omitempty"`
	WorkspaceRoot string                      `json:"workspace_root"`
	Providers     []ClientProviderSettingsDTO `json:"providers"`
	Runtime       ClientRuntimeSettingsDTO    `json:"runtime"`
	Web           ClientWebSettingsDTO        `json:"web"`
}

func clientSettingsDTOFromConfig(cfg *config.Config) ClientSettingsDTO {
	providers := make([]ClientProviderSettingsDTO, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		providers = append(providers, ClientProviderSettingsDTO{
			Name:                p.Name,
			Model:               p.Model,
			BaseURL:             p.BaseURL,
			ReasoningEffort:     p.ReasoningEffort,
			TimeoutSeconds:      p.TimeoutSeconds,
			Temperature:         p.Temperature,
			MaxCompletionTokens: p.MaxCompletionTokens,
			Enabled:             p.Enabled,
		})
	}
	return ClientSettingsDTO{
		ConfigPath:    cfg.ConfigPath,
		ConfigDir:     cfg.ConfigDir,
		WorkspaceRoot: cfg.WorkspaceRoot(),
		Providers:     providers,
		Runtime: ClientRuntimeSettingsDTO{
			StorageDir:        cfg.Runtime.StorageDir,
			RunTimeoutSeconds: cfg.Runtime.RunTimeoutSeconds,
		},
		Web: ClientWebSettingsDTO{
			ListenAddr:     cfg.Web.ListenAddr,
			AllowedOrigins: append([]string(nil), cfg.Web.AllowedOrigins...),
		},
	}
}

// ThreadDTO represents a client-visible thread summary.
type ThreadDTO struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	WorkspaceRoot string    `json:"workspace_root"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	LatestRunID   string    `json:"latest_run_id,omitempty"`
	State         string    `json:"state"`
}

// ThreadListResponse is the response body for listing threads.
type ThreadListResponse struct {
	Items []ThreadDTO `json:"items"`
}

// CreateThreadRequest is the body for creating a new thread.
type CreateThreadRequest struct {
	Title string `json:"title,omitempty"`
}

// UpdateThreadRequest is the body for updating a thread.
type UpdateThreadRequest struct {
	Title string `json:"title"`
}

func threadDTOFromDomain(thread app.Thread) ThreadDTO {
	return ThreadDTO{
		ID:            thread.ID,
		Title:         thread.Title,
		WorkspaceRoot: thread.WorkspaceRoot,
		CreatedAt:     thread.CreatedAt,
		UpdatedAt:     thread.UpdatedAt,
		LatestRunID:   thread.LatestRunID,
		State:         thread.State,
	}
}

func threadDTOsFromDomain(items []app.Thread) []ThreadDTO {
	result := make([]ThreadDTO, 0, len(items))
	for _, item := range items {
		result = append(result, threadDTOFromDomain(item))
	}
	return result
}
