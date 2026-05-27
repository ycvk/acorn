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
	return DefaultConverter.workspaceGitStatusDTOFromDomain(item)
}

func subagentRunDTOsFromDomain(items []app.SubagentRun) []SubagentRunDTO {
	return DefaultConverter.subagentRunDTOsFromDomain(items)
}

func contextEconomyDTOFromDomain(item app.ContextEconomySummary) ContextEconomyDTO {
	return DefaultConverter.contextEconomyDTOFromDomain(item)
}

func providerUsageDTOFromDomain(item app.ProviderUsageSummary) ProviderUsageDTO {
	return DefaultConverter.providerUsageDTOFromDomain(item)
}

func mutationCheckpointDTOsFromDomain(items []app.MutationCheckpointSummary) []MutationCheckpointDTO {
	return DefaultConverter.mutationCheckpointDTOsFromDomain(items)
}

func rollbackSummaryDTOsFromDomain(items []app.RollbackSummary) []RollbackSummaryDTO {
	return DefaultConverter.rollbackSummaryDTOsFromDomain(items)
}
