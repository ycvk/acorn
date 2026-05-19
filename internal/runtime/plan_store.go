package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	storecore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/toolresult"
)

type PlanStore interface {
	OrchestrationPlanStore()
	LoadPlan(ctx context.Context, sessionID string) (*Plan, error)
	SavePlan(ctx context.Context, plan *Plan) error
	AppendStepEvidence(ctx context.Context, sessionID string, runID string, stepID string, evidence PlanEvidence) (*Plan, error)
	AppendToolResultEvidenceRef(ctx context.Context, resultRef string, ref toolresult.EvidenceRef) error
}

type durablePlanStore struct {
	store planRecordStore
}

func newPlanStore(store planRecordStore) PlanStore {
	if store == nil {
		return nil
	}
	return &durablePlanStore{store: store}
}

func (s *durablePlanStore) OrchestrationPlanStore() {}

func (s *durablePlanStore) LoadPlan(ctx context.Context, sessionID string) (*Plan, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("plan store is not available")
	}
	record, err := s.store.LoadPlanBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return planFromStoreRecord(record), nil
}

func (s *durablePlanStore) SavePlan(ctx context.Context, plan *Plan) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("plan store is not available")
	}
	return s.store.SavePlan(ctx, storeRecordFromPlan(plan))
}

func (s *durablePlanStore) AppendStepEvidence(
	ctx context.Context,
	sessionID string,
	runID string,
	stepID string,
	evidence PlanEvidence,
) (*Plan, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(stepID) == "" {
		return nil, fmt.Errorf("plan step id is required")
	}
	plan, err := s.LoadPlan(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	stepIndex := -1
	for i, step := range plan.Steps {
		if step.ID == stepID {
			stepIndex = i
			break
		}
	}
	if stepIndex < 0 {
		return nil, fmt.Errorf("plan step %s no longer exists", stepID)
	}
	if err := validatePlanEvidence(stepID, evidence); err != nil {
		return nil, err
	}
	plan.Steps[stepIndex].Evidence = append(plan.Steps[stepIndex].Evidence, evidence)
	plan.RunID = strings.TrimSpace(runID)
	plan.UpdatedAt = time.Now().UTC()
	if err := s.SavePlan(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (s *durablePlanStore) AppendToolResultEvidenceRef(ctx context.Context, resultRef string, ref toolresult.EvidenceRef) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("plan store is not available")
	}
	_, err := s.store.AppendEvidenceRef(ctx, resultRef, ref)
	return err
}

func planFromStoreRecord(record *storecore.PlanRecord) *Plan {
	if record == nil {
		return nil
	}
	steps := make([]PlanStep, 0, len(record.Steps))
	for _, step := range record.Steps {
		steps = append(steps, PlanStep{
			ID:                 step.ID,
			Action:             step.Action,
			Status:             PlanStepStatus(step.Status),
			DependsOn:          append([]string(nil), step.DependsOn...),
			RepoTargets:        planRepoTargetsFromStore(step.RepoTargets),
			VerificationIntent: verificationIntentsFromStore(step.VerificationIntent),
			Risk:               PlanStepRisk(step.Risk),
			ToolHints:          append([]string(nil), step.ToolHints...),
			Evidence:           planEvidenceFromStore(step.Evidence),
		})
	}
	return &Plan{
		PlanID:    record.PlanID,
		SessionID: record.SessionID,
		RunID:     record.RunID,
		Steps:     steps,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
}

func storeRecordFromPlan(plan *Plan) *storecore.PlanRecord {
	if plan == nil {
		return nil
	}
	steps := make([]storecore.PlanStep, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		steps = append(steps, storecore.PlanStep{
			ID:                 step.ID,
			Action:             step.Action,
			Status:             string(step.Status),
			DependsOn:          append([]string(nil), step.DependsOn...),
			RepoTargets:        storePlanRepoTargets(step.RepoTargets),
			VerificationIntent: storeVerificationIntents(step.VerificationIntent),
			Risk:               string(step.Risk),
			ToolHints:          append([]string(nil), step.ToolHints...),
			Evidence:           storePlanEvidence(step.Evidence),
		})
	}
	return &storecore.PlanRecord{
		PlanID:    plan.PlanID,
		SessionID: plan.SessionID,
		RunID:     plan.RunID,
		Steps:     steps,
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	}
}

func planRepoTargetsFromStore(items []storecore.PlanRepoTarget) []PlanRepoTarget {
	result := make([]PlanRepoTarget, 0, len(items))
	for _, item := range items {
		result = append(result, PlanRepoTarget{
			Path:       item.Path,
			Symbol:     item.Symbol,
			StartLine:  item.StartLine,
			EndLine:    item.EndLine,
			Reason:     item.Reason,
			Confidence: item.Confidence,
		})
	}
	return result
}

func storePlanRepoTargets(items []PlanRepoTarget) []storecore.PlanRepoTarget {
	result := make([]storecore.PlanRepoTarget, 0, len(items))
	for _, item := range items {
		result = append(result, storecore.PlanRepoTarget{
			Path:       item.Path,
			Symbol:     item.Symbol,
			StartLine:  item.StartLine,
			EndLine:    item.EndLine,
			Reason:     item.Reason,
			Confidence: item.Confidence,
		})
	}
	return result
}

func verificationIntentsFromStore(items []storecore.VerificationIntent) []VerificationIntent {
	result := make([]VerificationIntent, 0, len(items))
	for _, item := range items {
		result = append(result, VerificationIntent{
			Kind:    item.Kind,
			Command: append([]string(nil), item.Command...),
			Paths:   append([]string(nil), item.Paths...),
			Reason:  item.Reason,
		})
	}
	return result
}

func storeVerificationIntents(items []VerificationIntent) []storecore.VerificationIntent {
	result := make([]storecore.VerificationIntent, 0, len(items))
	for _, item := range items {
		result = append(result, storecore.VerificationIntent{
			Kind:    item.Kind,
			Command: append([]string(nil), item.Command...),
			Paths:   append([]string(nil), item.Paths...),
			Reason:  item.Reason,
		})
	}
	return result
}

func planEvidenceFromStore(items []storecore.PlanEvidence) []PlanEvidence {
	result := make([]PlanEvidence, 0, len(items))
	for _, item := range items {
		result = append(result, PlanEvidence{
			ID:            item.ID,
			StepID:        item.StepID,
			Kind:          EvidenceKind(item.Kind),
			Status:        EvidenceStatus(item.Status),
			Summary:       item.Summary,
			ToolResultRef: item.ToolResultRef,
			ToolName:      item.ToolName,
			Command:       append([]string(nil), item.Command...),
			Paths:         append([]string(nil), item.Paths...),
			DiffRef:       item.DiffRef,
			ChildRunID:    item.ChildRunID,
			Error:         item.Error,
			SourceRunID:   item.SourceRunID,
			RecordedAt:    item.RecordedAt,
		})
	}
	return result
}

func storePlanEvidence(items []PlanEvidence) []storecore.PlanEvidence {
	result := make([]storecore.PlanEvidence, 0, len(items))
	for _, item := range items {
		result = append(result, storecore.PlanEvidence{
			ID:            item.ID,
			StepID:        item.StepID,
			Kind:          string(item.Kind),
			Status:        string(item.Status),
			Summary:       item.Summary,
			ToolResultRef: item.ToolResultRef,
			ToolName:      item.ToolName,
			Command:       append([]string(nil), item.Command...),
			Paths:         append([]string(nil), item.Paths...),
			DiffRef:       item.DiffRef,
			ChildRunID:    item.ChildRunID,
			Error:         item.Error,
			SourceRunID:   item.SourceRunID,
			RecordedAt:    item.RecordedAt,
		})
	}
	return result
}
