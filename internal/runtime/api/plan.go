package api

import (
	"context"

	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/store"
)

// --- PlanStore interface ---

type PlanStore interface {
	OrchestrationPlanStore()
	LoadPlan(ctx context.Context, sessionID string) (*model.Plan, error)
	SavePlan(ctx context.Context, plan *model.Plan) error
	AppendStepEvidence(ctx context.Context, sessionID string, runID string, stepID string, evidence model.PlanEvidence) (*model.Plan, error)
	AppendToolResultEvidenceRef(ctx context.Context, resultRef string, ref store.EvidenceRef) error
}

// --- PlanPersistenceStore interface ---

type PlanPersistenceStore interface {
	LoadPlanBySession(ctx context.Context, sessionID string) (*model.Plan, error)
	SavePlan(ctx context.Context, plan *model.Plan) error
	AppendEvidenceRef(ctx context.Context, resultRef string, ref store.EvidenceRef) (store.ToolResultRecord, error)
}
