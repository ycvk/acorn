package web

import (
	"time"

	"github.com/ycvk/acorn/internal/app"
)

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
