package orchestration

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/orchestrationmode"
)

type ChildAgentOrigin string
type ChildRunMode string
type ChildWorkspaceMode string

const (
	ChildAgentOriginDelegateTask ChildAgentOrigin = "delegate_task"
	ChildAgentOriginMCPSampling  ChildAgentOrigin = "mcp_sampling"
	ChildAgentOriginPlanExecute  ChildAgentOrigin = "plan_execute"
	ChildAgentOriginVerifier     ChildAgentOrigin = "verifier"
)

const (
	ChildRunModeFork ChildRunMode = "fork"
)

const (
	ChildWorkspaceModeShared   ChildWorkspaceMode = "shared"
	ChildWorkspaceModeWorktree ChildWorkspaceMode = "worktree"
)

func NormalizeChildRunMode(mode ChildRunMode) ChildRunMode {
	switch mode {
	case ChildRunModeFork:
		return mode
	default:
		return ChildRunModeFork
	}
}

func NormalizeChildWorkspaceMode(mode ChildWorkspaceMode) ChildWorkspaceMode {
	switch mode {
	case ChildWorkspaceModeShared, ChildWorkspaceModeWorktree:
		return mode
	default:
		return ChildWorkspaceModeShared
	}
}

type ChildAgentRequest struct {
	ParentRunID        string
	ParentSessionID    string
	ParentStepID       string
	Task               string
	ChildRunMode       ChildRunMode
	WorkspaceMode      ChildWorkspaceMode
	ContextMessages    []*schema.Message
	AllowedToolNames   []string
	AcceptanceCriteria []string
	ExpectedEvidence   []string
	Origin             ChildAgentOrigin
	RequestedMode      orchestrationmode.Mode
}

type ChildAgentResult struct {
	ChildRunID         string               `json:"child_run_id"`
	ChildSessionID     string               `json:"child_session_id"`
	ChildRunMode       ChildRunMode         `json:"child_run_mode"`
	WorkspaceMode      ChildWorkspaceMode   `json:"workspace_mode"`
	WorktreePath       string               `json:"worktree_path,omitempty"`
	FinalStatus        string               `json:"final_status"`
	OutputSummary      string               `json:"output_summary,omitempty"`
	Acceptance         ChildAgentAcceptance `json:"acceptance"`
	EvidenceSummaries  []string             `json:"evidence_summaries,omitempty"`
	EvidenceRefs       []string             `json:"evidence_refs,omitempty"`
	EffectiveToolNames []string             `json:"effective_tool_names,omitempty"`
}

type ChildAgentAcceptance struct {
	Status  string   `json:"status"`
	Reasons []string `json:"reasons,omitempty"`
}

type ChildAgentExecutor interface {
	Execute(ctx context.Context, req ChildAgentRequest) (*ChildAgentResult, error)
}

func (r ChildAgentResult) MarshalJSON() ([]byte, error) {
	type alias ChildAgentResult
	return json.Marshal(alias(r))
}
