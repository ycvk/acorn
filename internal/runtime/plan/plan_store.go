package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/store"
)

type durablePlanStore struct {
	store api.PlanPersistenceStore
}

func NewPlanStore(store api.PlanPersistenceStore) api.PlanStore {
	if store == nil {
		return nil
	}
	return &durablePlanStore{store: store}
}

func (s *durablePlanStore) OrchestrationPlanStore() {}

func (s *durablePlanStore) LoadPlan(ctx context.Context, sessionID string) (*model.Plan, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("plan store is not available")
	}
	record, err := s.store.LoadPlanBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func (s *durablePlanStore) SavePlan(ctx context.Context, plan *model.Plan) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("plan store is not available")
	}
	return s.store.SavePlan(ctx, plan)
}

func (s *durablePlanStore) AppendStepEvidence(
	ctx context.Context,
	sessionID string,
	runID string,
	stepID string,
	evidence model.PlanEvidence,
) (*model.Plan, error) {
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

func (s *durablePlanStore) AppendToolResultEvidenceRef(ctx context.Context, resultRef string, ref store.EvidenceRef) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("plan store is not available")
	}
	_, err := s.store.AppendEvidenceRef(ctx, resultRef, ref)
	return err
}

func toolVerificationCommand(toolName string, argumentsJSON string) []string {
	if toolName != "run_command" && toolName != "run_verification" {
		return nil
	}
	var payload struct {
		Command []string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err == nil && len(payload.Command) > 0 {
		return append([]string(nil), payload.Command...)
	}
	return nil
}
