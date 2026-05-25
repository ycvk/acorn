package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/providerusage"
	"github.com/ycvk/acorn/internal/store"
)

func (s *Store) GetOAuthToken(ctx context.Context, providerName string) (*store.OAuthToken, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT provider_name, access_token, refresh_token, expiry, updated_at
	     FROM mcp_oauth_tokens
	     WHERE provider_name = ?`,
		providerName,
	)
	var (
		token   store.OAuthToken
		expiry  string
		updated string
	)
	if err := row.Scan(&token.ProviderName, &token.AccessToken, &token.RefreshToken, &expiry, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrOAuthTokenNotFound
		}
		return nil, fmt.Errorf("get oauth token: %w", err)
	}
	expiryAt, err := parseTimestamp(fixedTimestampLayout, expiry, "mcp_oauth_token.expiry")
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTimestamp(fixedTimestampLayout, updated, "mcp_oauth_token.updated_at")
	if err != nil {
		return nil, err
	}
	token.Expiry = expiryAt
	token.UpdatedAt = updatedAt
	return &token, nil
}

func (s *Store) SaveOAuthToken(ctx context.Context, token *store.OAuthToken) error {
	now := formatTimestamp(time.Now())
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO mcp_oauth_tokens(provider_name, access_token, refresh_token, expiry, updated_at)
	     VALUES(?, ?, ?, ?, ?)
	     ON CONFLICT(provider_name) DO UPDATE SET
	        access_token = excluded.access_token,
	        refresh_token = excluded.refresh_token,
	        expiry = excluded.expiry,
	        updated_at = excluded.updated_at`,
		token.ProviderName,
		token.AccessToken,
		token.RefreshToken,
		formatTimestamp(token.Expiry),
		now,
	)
	if err != nil {
		return fmt.Errorf("save oauth token: %w", err)
	}
	return nil
}

func (s *Store) DeleteOAuthToken(ctx context.Context, providerName string) error {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_oauth_tokens WHERE provider_name = ?`,
		providerName,
	)
	if err != nil {
		return fmt.Errorf("delete oauth token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete oauth token rows affected: %w", err)
	}
	if affected == 0 {
		return store.ErrOAuthTokenNotFound
	}
	return nil
}

func (s *Store) CreatePendingAction(ctx context.Context, input store.CreatePendingActionInput) (*events.PendingActionRecord, error) {
	kind, err := normalizePendingActionKind(input.Kind)
	if err != nil {
		return nil, fmt.Errorf("create pending action: %w", err)
	}
	status, err := normalizePendingActionStatus(input.Status)
	if err != nil {
		return nil, fmt.Errorf("create pending action: %w", err)
	}
	mode, err := normalizePendingActionMode(input.Mode)
	if err != nil {
		return nil, fmt.Errorf("create pending action: %w", err)
	}
	if _, err := s.LoadRun(ctx, input.RunID); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	record := &events.PendingActionRecord{
		ActionID:      strings.TrimSpace(input.ActionID),
		RequestID:     strings.TrimSpace(input.ActionID),
		RunID:         strings.TrimSpace(input.RunID),
		InterruptID:   strings.TrimSpace(input.InterruptID),
		Kind:          kind,
		Subject:       strings.TrimSpace(input.Subject),
		ToolName:      strings.TrimSpace(input.Subject),
		PayloadJSON:   input.PayloadJSON,
		ArgumentsJSON: input.PayloadJSON,
		Status:        status,
		Mode:          mode,
		Reason:        strings.TrimSpace(input.Reason),
		Rule:          strings.TrimSpace(input.Rule),
		CreatedAt:     now,
	}
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO pending_actions(action_id, run_id, interrupt_id, kind, subject, payload_json, status, mode, reason, rule, decision_json, created_at, decided_at, resolved_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, '', '')`,
		record.ActionID,
		record.RunID,
		record.InterruptID,
		string(record.Kind),
		record.Subject,
		record.PayloadJSON,
		string(record.Status),
		string(record.Mode),
		record.Reason,
		record.Rule,
		formatTimestamp(record.CreatedAt),
	)
	if err != nil {
		if isPendingActionUniqueConstraint(err) {
			return nil, store.ErrPendingActionExists
		}
		return nil, fmt.Errorf("create pending action: %w", err)
	}
	return record, nil
}

func (s *Store) AttachPendingActionInterrupt(ctx context.Context, actionID, interruptID string) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE pending_actions SET interrupt_id = ? WHERE action_id = ?`,
		strings.TrimSpace(interruptID),
		strings.TrimSpace(actionID),
	)
	if err != nil {
		if isPendingActionUniqueConstraint(err) {
			return store.ErrPendingActionExists
		}
		return fmt.Errorf("attach pending action interrupt: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("attach pending action interrupt rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("attach pending action interrupt: %w: %s", store.ErrPendingActionNotFound, actionID)
	}
	return nil
}

func (s *Store) LoadPendingAction(ctx context.Context, actionID string) (*events.PendingActionRecord, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT action_id, run_id, interrupt_id, kind, subject, payload_json, status, mode, reason, rule, decision_json, created_at, decided_at, resolved_at
		FROM pending_actions
		WHERE action_id = ?`,
		actionID,
	)
	record, err := scanPendingActionRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", store.ErrPendingActionNotFound, actionID)
		}
		return nil, fmt.Errorf("load pending action: %w", err)
	}
	return record, nil
}

func (s *Store) LoadPendingActionByInterrupt(ctx context.Context, interruptID string) (*events.PendingActionRecord, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT action_id, run_id, interrupt_id, kind, subject, payload_json, status, mode, reason, rule, decision_json, created_at, decided_at, resolved_at
		FROM pending_actions
		WHERE interrupt_id = ?`,
		interruptID,
	)
	record, err := scanPendingActionRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", store.ErrPendingActionNotFound, interruptID)
		}
		return nil, fmt.Errorf("load pending action by interrupt: %w", err)
	}
	return record, nil
}

func (s *Store) ListPendingActions(ctx context.Context, limit int) ([]events.PendingActionRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT action_id, run_id, interrupt_id, kind, subject, payload_json, status, mode, reason, rule, decision_json, created_at, decided_at, resolved_at
		FROM pending_actions
		WHERE status = ?
		ORDER BY created_at DESC
		LIMIT ?`,
		string(events.PendingActionStatusPending),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending actions: %w", err)
	}
	defer rows.Close()

	items := make([]events.PendingActionRecord, 0)
	for rows.Next() {
		record, err := scanPendingActionRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("list pending actions: %w", err)
		}
		items = append(items, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending actions: %w", err)
	}
	return items, nil
}

func (s *Store) ListPendingActionsByRun(ctx context.Context, runID string) ([]events.PendingActionRecord, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT action_id, run_id, interrupt_id, kind, subject, payload_json, status, mode, reason, rule, decision_json, created_at, decided_at, resolved_at
		FROM pending_actions
		WHERE run_id = ?
		ORDER BY created_at ASC`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending actions by run: %w", err)
	}
	defer rows.Close()

	items := make([]events.PendingActionRecord, 0)
	for rows.Next() {
		record, err := scanPendingActionRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("list pending actions by run: %w", err)
		}
		items = append(items, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending actions by run: %w", err)
	}
	return items, nil
}

func (s *Store) DecidePendingAction(ctx context.Context, actionID string, status events.PendingActionStatus, mode events.PendingActionDecisionMode, decisionJSON string) (out *events.PendingActionRecord, err error) {
	normalizedStatus, err := normalizePendingActionDecision(status)
	if err != nil {
		return nil, fmt.Errorf("decide pending action: %w", err)
	}
	normalizedMode, err := normalizePendingActionMode(mode)
	if err != nil {
		return nil, fmt.Errorf("decide pending action: %w", err)
	}
	decidedAt := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("decide pending action begin tx: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("decide pending action rollback: %w", rollbackErr))
		}
	}()

	record, err := scanPendingActionRecord(tx.QueryRowContext(ctx,
		`SELECT action_id, run_id, interrupt_id, kind, subject, payload_json, status, mode, reason, rule, decision_json, created_at, decided_at, resolved_at FROM pending_actions WHERE action_id = ?`,
		actionID,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("decide pending action: %w: %s", store.ErrPendingActionNotFound, actionID)
		}
		return nil, fmt.Errorf("decide pending action load: %w", err)
	}
	if record.Status != events.PendingActionStatusPending {
		return nil, fmt.Errorf("decide pending action: %w: status %q", store.ErrPendingActionDecided, record.Status)
	}

	result, err := tx.ExecContext(
		ctx,
		`UPDATE pending_actions SET status = ?, mode = ?, decision_json = ?, decided_at = ? WHERE action_id = ? AND status = ?`,
		string(normalizedStatus),
		string(normalizedMode),
		decisionJSON,
		formatTimestamp(decidedAt),
		actionID,
		string(events.PendingActionStatusPending),
	)
	if err != nil {
		return nil, fmt.Errorf("decide pending action: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("decide pending action rows affected: %w", err)
	}
	if affected == 0 {
		return nil, fmt.Errorf("decide pending action: %w", store.ErrPendingActionDecided)
	}

	eventPayload, err := json.Marshal(map[string]any{
		"action_id":    record.ActionID,
		"interrupt_id": record.InterruptID,
		"kind":         record.Kind,
		"subject":      record.Subject,
		"decision":     string(normalizedStatus),
		"mode":         string(normalizedMode),
		"reason":       record.Reason,
		"rule":         record.Rule,
		"decided_at":   decidedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal action.decided event: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO events(run_id, kind, payload_json, created_at) VALUES(?, ?, ?, ?)`,
		record.RunID,
		"action.decided",
		string(eventPayload),
		formatTimestamp(decidedAt),
	); err != nil {
		return nil, fmt.Errorf("insert action.decided event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("decide pending action commit: %w", err)
	}

	record.Status = normalizedStatus
	record.Mode = normalizedMode
	record.DecisionJSON = decisionJSON
	record.DecidedAt = &decidedAt
	return record, nil
}

func (s *Store) ResolvePendingAction(ctx context.Context, actionID string) error {
	resolvedAt := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE pending_actions SET status = ?, resolved_at = ? WHERE action_id = ?`,
		string(events.PendingActionStatusResolved),
		formatTimestamp(resolvedAt),
		actionID,
	)
	if err != nil {
		return fmt.Errorf("resolve pending action: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve pending action rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("resolve pending action: %w: %s", store.ErrPendingActionNotFound, actionID)
	}
	return nil
}

func normalizePendingActionKind(kind events.PendingActionKind) (events.PendingActionKind, error) {
	switch strings.TrimSpace(string(kind)) {
	case string(events.PendingActionKindElicitation):
		return events.PendingActionKindElicitation, nil
	case string(events.PendingActionKindOperatorQuestion):
		return events.PendingActionKindOperatorQuestion, nil
	default:
		return "", fmt.Errorf("unsupported pending action kind %q", kind)
	}
}

func normalizePendingActionMode(mode events.PendingActionDecisionMode) (events.PendingActionDecisionMode, error) {
	switch strings.TrimSpace(string(mode)) {
	case "":
		return "", nil
	case string(events.PendingActionModeInline):
		return events.PendingActionModeInline, nil
	case string(events.PendingActionModeDeferred):
		return events.PendingActionModeDeferred, nil
	default:
		return "", fmt.Errorf("unsupported pending action mode %q", mode)
	}
}

func normalizePendingActionStatus(status events.PendingActionStatus) (events.PendingActionStatus, error) {
	switch strings.TrimSpace(string(status)) {
	case "":
		return events.PendingActionStatusPending, nil
	case string(events.PendingActionStatusPending):
		return events.PendingActionStatusPending, nil
	case string(events.PendingActionStatusApproved):
		return events.PendingActionStatusApproved, nil
	case string(events.PendingActionStatusRejected):
		return events.PendingActionStatusRejected, nil
	case string(events.PendingActionStatusResolved):
		return events.PendingActionStatusResolved, nil
	default:
		return "", fmt.Errorf("unsupported pending action status %q", status)
	}
}

func normalizePendingActionDecision(status events.PendingActionStatus) (events.PendingActionStatus, error) {
	switch strings.TrimSpace(string(status)) {
	case string(events.PendingActionStatusApproved):
		return events.PendingActionStatusApproved, nil
	case string(events.PendingActionStatusRejected):
		return events.PendingActionStatusRejected, nil
	default:
		return "", fmt.Errorf("unsupported pending action decision status %q", status)
	}
}

func isPendingActionUniqueConstraint(err error) bool {
	message := err.Error()
	return strings.Contains(message, "UNIQUE constraint failed: pending_actions.action_id") ||
		strings.Contains(message, "UNIQUE constraint failed: pending_actions.interrupt_id")
}

func (s *Store) SaveRunDecision(ctx context.Context, record decision.Record) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO run_decisions(run_id, session_id, action, intent, selected_skill_id, decision_reason, decision_profile_hash, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(run_id) DO UPDATE SET
		     session_id = excluded.session_id,
		     action = excluded.action,
		     intent = excluded.intent,
		     selected_skill_id = excluded.selected_skill_id,
		     decision_reason = excluded.decision_reason,
		     decision_profile_hash = excluded.decision_profile_hash,
		     created_at = excluded.created_at`,
		strings.TrimSpace(record.RunID),
		strings.TrimSpace(record.SessionID),
		string(record.Action),
		strings.TrimSpace(record.Intent),
		strings.TrimSpace(record.SelectedSkillID),
		strings.TrimSpace(record.DecisionReason),
		strings.TrimSpace(record.DecisionProfileHash),
		formatTimestamp(record.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("save run decision: %w", err)
	}
	return nil
}

func (s *Store) LoadRunDecision(ctx context.Context, runID string) (*decision.Record, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT run_id, session_id, action, intent, selected_skill_id, decision_reason, decision_profile_hash, created_at
		 FROM run_decisions WHERE run_id = ?`,
		strings.TrimSpace(runID),
	)
	var (
		record    decision.Record
		action    string
		createdAt string
	)
	if err := row.Scan(
		&record.RunID,
		&record.SessionID,
		&action,
		&record.Intent,
		&record.SelectedSkillID,
		&record.DecisionReason,
		&record.DecisionProfileHash,
		&createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load run decision: %w", err)
	}
	record.Action = decision.Action(strings.TrimSpace(action))
	parsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "run_decision.created_at")
	if err != nil {
		return nil, err
	}
	record.CreatedAt = parsed
	return &record, nil
}

// SavePlan upserts a plan. If the plan_id already exists, it updates; otherwise inserts.
// Empty steps = delete the plan row.
func (s *Store) SavePlan(ctx context.Context, plan *store.PlanRecord) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	if len(plan.Steps) == 0 {
		_, err := s.db.ExecContext(ctx, "DELETE FROM plan_steps WHERE plan_id = ?", plan.PlanID)
		if err != nil {
			return fmt.Errorf("delete plan %s: %w", plan.PlanID, err)
		}
		return nil
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

// LoadPlanBySession loads the most recent plan for a session.
// Returns store.ErrPlanNotFound if no plan exists.
func (s *Store) LoadPlanBySession(ctx context.Context, sessionID string) (*store.PlanRecord, error) {
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
func (s *Store) LoadPlanByRun(ctx context.Context, runID string) (*store.PlanRecord, error) {
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

func (s *Store) scanPlan(row *sql.Row) (*store.PlanRecord, error) {
	var (
		planID    string
		sessionID string
		runID     string
		stepsJSON string
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&planID, &sessionID, &runID, &stepsJSON, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: session/run", store.ErrPlanNotFound)
		}
		return nil, fmt.Errorf("scan plan: %w", err)
	}
	var steps []store.PlanStep
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
	return &store.PlanRecord{
		PlanID:    planID,
		SessionID: sessionID,
		RunID:     runID,
		Steps:     steps,
		CreatedAt: created,
		UpdatedAt: updated,
	}, nil
}

func (s *Store) AppendProviderUsage(ctx context.Context, record providerusage.Record) error {
	normalized, err := providerusage.NormalizeRecord(record)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO provider_usages (
			usage_id, run_id, session_id, call_site, provider_name, model_name,
			prompt_tokens, completion_tokens, total_tokens, cached_tokens,
			reasoning_tokens, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, normalized.UsageID, normalized.RunID, normalized.SessionID, normalized.CallSite,
		normalized.ProviderName, normalized.ModelName, normalized.PromptTokens,
		normalized.CompletionTokens, normalized.TotalTokens, normalized.CachedTokens,
		normalized.ReasoningTokens, formatTimestamp(normalized.CreatedAt))
	if err != nil {
		return fmt.Errorf("append provider usage %s: %w", normalized.UsageID, err)
	}
	return nil
}

func (s *Store) ListProviderUsagesByRun(ctx context.Context, runID string) ([]providerusage.Record, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("provider usage run_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT usage_id, run_id, session_id, call_site, provider_name, model_name,
		       prompt_tokens, completion_tokens, total_tokens, cached_tokens,
		       reasoning_tokens, created_at
		FROM provider_usages
		WHERE run_id = ?
		ORDER BY created_at ASC, usage_id ASC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list provider usages for run %s: %w", runID, err)
	}
	defer rows.Close()

	var items []providerusage.Record
	for rows.Next() {
		record, err := scanProviderUsage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider usages for run %s: %w", runID, err)
	}
	return items, nil
}

func scanProviderUsage(scanner interface{ Scan(dest ...any) error }) (providerusage.Record, error) {
	var record providerusage.Record
	var createdAt string
	if err := scanner.Scan(
		&record.UsageID,
		&record.RunID,
		&record.SessionID,
		&record.CallSite,
		&record.ProviderName,
		&record.ModelName,
		&record.PromptTokens,
		&record.CompletionTokens,
		&record.TotalTokens,
		&record.CachedTokens,
		&record.ReasoningTokens,
		&createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return providerusage.Record{}, err
		}
		return providerusage.Record{}, err
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "provider_usage.created_at")
	if err != nil {
		return providerusage.Record{}, err
	}
	record.CreatedAt = parsed
	return record, nil
}
