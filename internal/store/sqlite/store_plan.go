package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/store"
)

// SavePlan upserts a plan. If the plan_id already exists, it updates; otherwise inserts.
// Empty steps = delete the plan row.
func (s *Store) SavePlan(ctx context.Context, plan *model.Plan) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	if len(plan.Steps) == 0 {
		return s.deletePlanSteps(ctx, plan.PlanID)
	}
	stepsJSON, err := json.Marshal(plan.Steps)
	if err != nil {
		return fmt.Errorf("marshal plan steps: %w", err)
	}
	now := formatTimestamp(time.Now().UTC())
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO plan_steps (plan_id, session_id, run_id, steps_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(plan_id) DO UPDATE SET
			session_id = excluded.session_id,
			run_id = excluded.run_id,
			steps_json = excluded.steps_json,
			updated_at = excluded.updated_at`,
		plan.PlanID, plan.SessionID, plan.RunID, string(stepsJSON), now, now)
	if err != nil {
		return fmt.Errorf("save plan %s: %w", plan.PlanID, err)
	}
	return nil
}

// deletePlanSteps removes the plan_steps row for the given plan id.
func (s *Store) deletePlanSteps(ctx context.Context, planID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM plan_steps WHERE plan_id = ?", planID)
	if err != nil {
		return fmt.Errorf("delete plan %s: %w", planID, err)
	}
	return nil
}

// LoadPlanBySession loads the most recent plan for a session.
// Returns store.ErrPlanNotFound if no plan exists.
func (s *Store) LoadPlanBySession(ctx context.Context, sessionID string) (*model.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT plan_id, session_id, run_id, steps_json, created_at, updated_at
		FROM plan_steps
		WHERE session_id = ?
		ORDER BY updated_at DESC
		LIMIT 1`, sessionID)
	return s.scanPlan(row)
}

// LoadPlanByRun loads a plan by its last modifying run ID.
// Returns store.ErrPlanNotFound if no plan exists.
func (s *Store) LoadPlanByRun(ctx context.Context, runID string) (*model.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT plan_id, session_id, run_id, steps_json, created_at, updated_at
		FROM plan_steps
		WHERE run_id = ?
		ORDER BY updated_at DESC
		LIMIT 1`, runID)
	return s.scanPlan(row)
}

// DeletePlanBySession deletes all plans for a session.
// Used during session cleanup.
func (s *Store) DeletePlanBySession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM plan_steps WHERE session_id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("delete plans for session %s: %w", sessionID, err)
	}
	return nil
}

func (s *Store) scanPlan(row *sql.Row) (*model.Plan, error) {
	planID, sessionID, runID, stepsJSON, createdAt, updatedAt, err := scanPlanFields(row)
	if err != nil {
		return nil, err
	}
	var steps []model.PlanStep
	if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
		return nil, fmt.Errorf("unmarshal plan steps: %w", err)
	}
	created, err := parseTimestamp(fixedTimestampLayout, createdAt, "plan.created_at")
	if err != nil {
		return nil, err
	}
	updated, err := parseTimestamp(fixedTimestampLayout, updatedAt, "plan.updated_at")
	if err != nil {
		return nil, err
	}
	return &model.Plan{
		PlanID:    planID,
		SessionID: sessionID,
		RunID:     runID,
		Steps:     steps,
		CreatedAt: created,
		UpdatedAt: updated,
	}, nil
}

func scanPlanFields(row *sql.Row) (planID, sessionID, runID, stepsJSON, createdAt, updatedAt string, err error) {
	if err := row.Scan(&planID, &sessionID, &runID, &stepsJSON, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", "", "", "", "", fmt.Errorf("%w: session/run", store.ErrPlanNotFound)
		}
		return "", "", "", "", "", "", fmt.Errorf("scan plan: %w", err)
	}
	return planID, sessionID, runID, stepsJSON, createdAt, updatedAt, nil
}
