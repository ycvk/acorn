package graph

import (
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/runtime/api"
)

// AgentGraphState is the local state managed by the agent graph.
type AgentGraphState struct {
	Messages            []*schema.Message
	Plan                *api.Plan
	ObserveDecision     ObserveDecision
	RemainingIterations int
	AgentName           string
	Phase               GraphPhase
}

type AgentGraphInput struct {
	Messages []*schema.Message
}

type GraphPhase string

const (
	PhasePlan    GraphPhase = "plan"
	PhaseAct     GraphPhase = "act"
	PhaseObserve GraphPhase = "observe"
)

// --- Observe Decision Types ---

type ObserveDecisionType string

const (
	ObserveDecisionNext   ObserveDecisionType = "next"
	ObserveDecisionReplan ObserveDecisionType = "replan"
	ObserveDecisionDone   ObserveDecisionType = "done"
)

type ObserveDecision struct {
	Decision ObserveDecisionType `json:"decision"`
	StepID   string              `json:"step_id,omitempty"`
	Reason   string              `json:"reason,omitempty"`
}
