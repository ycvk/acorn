package app

import (
	"time"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/workspace"
)

type RuntimeWorkbench struct {
	SessionID           string
	Title               string
	State               runtime.SessionState
	LatestRunID         string
	LatestRunStatus     events.RunStatus
	LatestRunMode       string
	LatestRunDepth      int
	ParentRunID         string
	Resumable           bool
	ResumeReason        string
	TraceSummary        *runtime.TraceSummary
	SelectedSkill       *runtime.SelectedSkill
	LatestDecision      *decision.Record
	SessionSummary      *runtimehistory.SessionSummary
	WorkspaceRoot       string
	GitStatus           WorkspaceGitStatus
	MutationCheckpoints []MutationCheckpointSummary
	RollbackResults     []RollbackSummary
	ContextEconomy      ContextEconomySummary
	ProviderUsage       ProviderUsageSummary
	Artifacts           []ArtifactSummary
	Plan                *runtime.Plan
	Evidence            []runtime.PlanEvidence
	Subagents           []SubagentRun
	NextStepHint        string
}

// WorkspaceGitStatus captures the git state of the workspace.
type WorkspaceGitStatus struct {
	WorkspaceRoot string
	Available     bool
	Branch        string
	Clean         bool
	Error         string
	Entries       []workspace.GitStatusEntry
}

// SubagentRun represents a child run spawned by a parent run.
type SubagentRun struct {
	SubRunID          string
	ParentRunID       string
	SessionID         string
	Depth             int
	Task              string
	ChildRunMode      string
	WorkspaceMode     string
	WorktreePath      string
	ContextMessages   int
	OrchestrationMode string
	ParentStepID      string
	State             string
	FinalStatus       string
	AcceptanceStatus  string
	AcceptanceReasons []string
	EvidenceRefs      []string
	Summary           string
	UpdatedAt         time.Time
}

// MutationCheckpointSummary represents a workspace mutation checkpoint.
type MutationCheckpointSummary struct {
	CheckpointID     string    `json:"checkpoint_id"`
	ToolResultRef    string    `json:"tool_result_ref"`
	ToolName         string    `json:"tool_name"`
	Status           string    `json:"status"`
	Paths            []string  `json:"paths,omitempty"`
	Summary          string    `json:"summary,omitempty"`
	VerifiedDiffStat string    `json:"verified_diff_stat,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// RollbackSummary represents the result of a workspace rollback.
type RollbackSummary struct {
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

// ContextEconomySummary aggregates context-window usage metrics.
type ContextEconomySummary struct {
	LatestPressure          *ContextPressureSummary
	LatestCompression       *ContextCompressionSummary
	ToolResults             []ContextToolResultSummary
	ToolResultCount         int
	ElidedToolResultCount   int
	ToolResultTokenEstimate int
	MemoryRefs              []string
	ProcedureRefs           []string
}

// ContextPressureSummary captures a single context-pressure measurement.
type ContextPressureSummary struct {
	State                 string
	EstimatedInputTokens  int
	EffectiveWindowTokens int
	PercentUsed           int
}

// ContextCompressionSummary captures a single compression event.
type ContextCompressionSummary struct {
	BoundaryID   string
	TokensBefore int
	TokensAfter  int
	Summary      string
}

// ContextToolResultSummary captures tool-result accounting in context economy.
type ContextToolResultSummary struct {
	ResultRef     string
	ToolName      string
	Status        string
	Preview       string
	TokenEstimate int
	FullTextBytes int
	Elided        bool
	EvidenceRefs  []string
}

// ProviderUsageSummary aggregates LLM provider token usage.
type ProviderUsageSummary struct {
	CallCount        int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
	ReasoningTokens  int
	Records          []ProviderUsageCallSummary
}

// ProviderUsageCallSummary is a single provider call record.
type ProviderUsageCallSummary struct {
	UsageID          string
	CallSite         string
	ProviderName     string
	ModelName        string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
	ReasoningTokens  int
	CreatedAt        time.Time
}

// ArtifactSummary represents a stored artifact.
type ArtifactSummary struct {
	ArtifactID          string
	RunID               string
	SessionID           string
	SourceToolResultRef string
	Kind                string
	Title               string
	MIMEType            string
	SizeBytes           int64
	SHA256              string
	CreatedAt           time.Time
}
