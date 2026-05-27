package model

import "time"

type PlanStepStatus string

const (
	PlanStepPending    PlanStepStatus = "pending"
	PlanStepInProgress PlanStepStatus = "in_progress"
	PlanStepCompleted  PlanStepStatus = "completed"
	PlanStepFailed     PlanStepStatus = "failed"
	PlanStepSkipped    PlanStepStatus = "skipped"
)

type PlanStepRisk string

const (
	PlanStepRiskRead     PlanStepRisk = "read"
	PlanStepRiskWrite    PlanStepRisk = "write"
	PlanStepRiskExecute  PlanStepRisk = "execute"
	PlanStepRiskDelegate PlanStepRisk = "delegate"
)

type PlanRepoTarget struct {
	Path       string `json:"path"`
	Symbol     string `json:"symbol,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	EndLine    int    `json:"end_line,omitempty"`
	Reason     string `json:"reason"`
	Confidence string `json:"confidence"`
}

type VerificationIntent struct {
	Kind    string   `json:"kind"`
	Command []string `json:"command,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Reason  string   `json:"reason"`
}

type EvidenceKind string

const (
	EvidenceKindTool       EvidenceKind = "tool"
	EvidenceKindCommand    EvidenceKind = "command"
	EvidenceKindDiff       EvidenceKind = "diff"
	EvidenceKindCheckpoint EvidenceKind = "checkpoint"
	EvidenceKindRollback   EvidenceKind = "rollback"
	EvidenceKindTest       EvidenceKind = "test"
	EvidenceKindSubagent   EvidenceKind = "subagent"
	EvidenceKindVerifier   EvidenceKind = "verifier"
	EvidenceKindManual     EvidenceKind = "manual"
)

type EvidenceStatus string

const (
	EvidenceStatusRecorded  EvidenceStatus = "recorded"
	EvidenceStatusPassed    EvidenceStatus = "passed"
	EvidenceStatusFailed    EvidenceStatus = "failed"
	EvidenceStatusConfirmed EvidenceStatus = "confirmed"
)

type PlanEvidence struct {
	ID            string         `json:"id"`
	StepID        string         `json:"step_id"`
	Kind          EvidenceKind   `json:"kind"`
	Status        EvidenceStatus `json:"status"`
	Summary       string         `json:"summary"`
	ToolResultRef string         `json:"tool_result_ref,omitempty"`
	ToolName      string         `json:"tool_name,omitempty"`
	Command       []string       `json:"command,omitempty"`
	Paths         []string       `json:"paths,omitempty"`
	DiffRef       string         `json:"diff_ref,omitempty"`
	ChildRunID    string         `json:"child_run_id,omitempty"`
	Error         string         `json:"error,omitempty"`
	SourceRunID   string         `json:"source_run_id,omitempty"`
	RecordedAt    time.Time      `json:"recorded_at"`
}

type PlanStep struct {
	ID                 string               `json:"id"`
	Action             string               `json:"action"`
	Status             PlanStepStatus       `json:"status"`
	DependsOn          []string             `json:"depends_on,omitempty"`
	RepoTargets        []PlanRepoTarget     `json:"repo_targets,omitempty"`
	VerificationIntent []VerificationIntent `json:"verification_intent,omitempty"`
	Risk               PlanStepRisk         `json:"risk,omitempty"`
	ToolHints          []string             `json:"tool_hints,omitempty"`
	Evidence           []PlanEvidence       `json:"evidence,omitempty"`
}

type Plan struct {
	PlanID    string     `json:"plan_id"`
	SessionID string     `json:"session_id"`
	RunID     string     `json:"run_id"`
	Steps     []PlanStep `json:"steps"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
