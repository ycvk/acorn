package web

import (
	"time"

	"github.com/ycvk/acorn/internal/app"
)

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
