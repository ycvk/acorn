package app

import (
	"context"

	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
)

type runtimePlanRecordStore interface {
	LoadPlanBySession(ctx context.Context, sessionID string) (*store.PlanRecord, error)
}

type runtimePlanStoreAdapter struct {
	store runtimePlanRecordStore
}

func newRuntimePlanStoreAdapter(store runtimePlanRecordStore) runtimeWorkbenchPlanStore {
	if store == nil {
		return nil
	}
	return runtimePlanStoreAdapter{store: store}
}

func (s runtimePlanStoreAdapter) LoadRuntimePlan(ctx context.Context, sessionID string) (*runtime.Plan, error) {
	record, err := s.store.LoadPlanBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return runtimePlanFromStoreRecord(record), nil
}

func runtimePlanFromStoreRecord(record *store.PlanRecord) *runtime.Plan {
	if record == nil {
		return nil
	}
	plan := &runtime.Plan{
		PlanID:    record.PlanID,
		SessionID: record.SessionID,
		RunID:     record.RunID,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
	steps := make([]runtime.PlanStep, 0, len(record.Steps))
	for _, step := range record.Steps {
		current := runtime.PlanStep{
			ID:        step.ID,
			Action:    step.Action,
			Status:    runtime.PlanStepStatus(step.Status),
			DependsOn: append([]string(nil), step.DependsOn...),
			Risk:      runtime.PlanStepRisk(step.Risk),
			ToolHints: append([]string(nil), step.ToolHints...),
		}
		for _, target := range step.RepoTargets {
			current.RepoTargets = append(current.RepoTargets, runtime.PlanRepoTarget{
				Path:       target.Path,
				Symbol:     target.Symbol,
				StartLine:  target.StartLine,
				EndLine:    target.EndLine,
				Reason:     target.Reason,
				Confidence: target.Confidence,
			})
		}
		for _, intent := range step.VerificationIntent {
			current.VerificationIntent = append(current.VerificationIntent, runtime.VerificationIntent{
				Kind:    intent.Kind,
				Command: append([]string(nil), intent.Command...),
				Paths:   append([]string(nil), intent.Paths...),
				Reason:  intent.Reason,
			})
		}
		for _, evidence := range step.Evidence {
			current.Evidence = append(current.Evidence, runtime.PlanEvidence{
				ID:            evidence.ID,
				StepID:        evidence.StepID,
				Kind:          runtime.EvidenceKind(evidence.Kind),
				Status:        runtime.EvidenceStatus(evidence.Status),
				Summary:       evidence.Summary,
				ToolResultRef: evidence.ToolResultRef,
				ToolName:      evidence.ToolName,
				Command:       append([]string(nil), evidence.Command...),
				Paths:         append([]string(nil), evidence.Paths...),
				DiffRef:       evidence.DiffRef,
				ChildRunID:    evidence.ChildRunID,
				Error:         evidence.Error,
				SourceRunID:   evidence.SourceRunID,
				RecordedAt:    evidence.RecordedAt,
			})
		}
		steps = append(steps, current)
	}
	plan.Steps = steps
	return plan
}

// RuntimeWorkbench aggregates all runtime state for a session.
