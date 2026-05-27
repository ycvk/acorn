package app

import (
	"context"

	"github.com/ycvk/acorn/internal/model"
)

type runtimePlanPersistenceStore interface {
	LoadPlanBySession(ctx context.Context, sessionID string) (*model.Plan, error)
}

type runtimePlanStoreAdapter struct {
	store runtimePlanPersistenceStore
}

func newRuntimePlanStoreAdapter(store runtimePlanPersistenceStore) runtimeWorkbenchPlanStore {
	if store == nil {
		return nil
	}
	return runtimePlanStoreAdapter{store: store}
}

func (s runtimePlanStoreAdapter) LoadRuntimePlan(ctx context.Context, sessionID string) (*model.Plan, error) {
	return s.store.LoadPlanBySession(ctx, sessionID)
}

// RuntimeWorkbench aggregates all runtime state for a session.
