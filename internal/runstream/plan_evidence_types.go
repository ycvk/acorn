package runstream

import "time"

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
	SourceRunID   string         `json:"source_run_id"`
	RecordedAt    time.Time      `json:"recorded_at"`
}
