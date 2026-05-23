package stream

import (
	"time"

	"github.com/ycvk/acorn/internal/runtime/api"
)

type PlanStepPayload struct {
	PlanID    string        `json:"plan_id"`
	SessionID string        `json:"session_id"`
	RunID     string        `json:"run_id"`
	Plan      *StreamPlan   `json:"plan"`
	Step      *api.PlanStep `json:"step"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type PlanStepStartedPayload struct {
	PlanStepPayload
}

func (p *PlanStepStartedPayload) StreamKind() StreamItemKind { return StreamKindStepStarted }

type PlanStepCompletedPayload struct {
	PlanStepPayload
}

func (p *PlanStepCompletedPayload) StreamKind() StreamItemKind { return StreamKindStepCompleted }

type PlanStepFailedPayload struct {
	PlanStepPayload
	Error string `json:"error,omitempty"`
}

func (p *PlanStepFailedPayload) StreamKind() StreamItemKind { return StreamKindStepFailed }
