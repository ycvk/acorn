package web

import (
	"time"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/clientevents"
)

type RunDetailDTO struct {
	Run       RunDTO                     `json:"run"`
	Thread    ThreadDTO                  `json:"thread"`
	Events    []clientevents.RunEvent    `json:"events"`
	Workbench *RuntimeWorkbenchDTO       `json:"workbench"`
	Trace     *clientevents.TraceSummary `json:"trace"`
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

type RuntimeWorkbenchDTO struct {
	SessionID           string                     `json:"session_id"`
	Title               string                     `json:"title"`
	State               clientevents.SessionState  `json:"state,omitempty"`
	LatestRunID         string                     `json:"latest_run_id,omitempty"`
	LatestRunStatus     string                     `json:"latest_run_status,omitempty"`
	LatestRunMode       string                     `json:"latest_run_mode,omitempty"`
	LatestRunDepth      int                        `json:"latest_run_depth,omitempty"`
	ParentRunID         string                     `json:"parent_run_id,omitempty"`
	Resumable           bool                       `json:"resumable"`
	ResumeReason        string                     `json:"resume_reason,omitempty"`
	TraceSummary        *clientevents.TraceSummary `json:"trace_summary,omitempty"`
	SelectedSkill       *SelectedSkillDTO          `json:"selected_skill,omitempty"`
	LatestDecision      *RunDecisionDTO            `json:"latest_decision,omitempty"`
	SessionSummary      string                     `json:"session_summary,omitempty"`
	SummaryStatus       string                     `json:"summary_status,omitempty"`
	SummarySourceRunID  string                     `json:"summary_source_run_id,omitempty"`
	SummaryUpdatedAt    *time.Time                 `json:"summary_updated_at,omitempty"`
	WorkspaceRoot       string                     `json:"workspace_root"`
	GitStatus           WorkspaceGitStatusDTO      `json:"git_status"`
	MutationCheckpoints []MutationCheckpointDTO    `json:"mutation_checkpoints,omitempty"`
	RollbackResults     []RollbackSummaryDTO       `json:"rollback_results,omitempty"`
	ContextEconomy      ContextEconomyDTO          `json:"context_economy"`
	ProviderUsage       ProviderUsageDTO           `json:"provider_usage"`
	Artifacts           []ArtifactSummaryDTO       `json:"artifacts,omitempty"`
	Plan                *PlanDTO                   `json:"plan,omitempty"`
	Evidence            []PlanEvidenceDTO          `json:"evidence,omitempty"`
	Subagents           []SubagentRunDTO           `json:"subagents,omitempty"`
	NextStepHint        string                     `json:"next_step_hint,omitempty"`
}

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
	return DefaultConverter.threadDTOFromDomain(thread)
}

func threadDTOsFromDomain(items []app.Thread) []ThreadDTO {
	return DefaultConverter.threadDTOsFromDomain(items)
}
