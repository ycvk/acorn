package api

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/store"
)

// --- Plan types ---

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

// --- PlanStore interface ---

type PlanStore interface {
	OrchestrationPlanStore()
	LoadPlan(ctx context.Context, sessionID string) (*Plan, error)
	SavePlan(ctx context.Context, plan *Plan) error
	AppendStepEvidence(ctx context.Context, sessionID string, runID string, stepID string, evidence PlanEvidence) (*Plan, error)
	AppendToolResultEvidenceRef(ctx context.Context, resultRef string, ref store.EvidenceRef) error
}

// --- PlanRecordStore interface ---

type PlanRecordStore interface {
	LoadPlanBySession(ctx context.Context, sessionID string) (*store.PlanRecord, error)
	SavePlan(ctx context.Context, plan *store.PlanRecord) error
	AppendEvidenceRef(ctx context.Context, resultRef string, ref store.EvidenceRef) (store.ToolResultRecord, error)
}
