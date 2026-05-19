package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

func (s *Store) CreatePendingAction(ctx context.Context, input CreatePendingActionInput) (*events.PendingActionRecord, error) {
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
			return nil, ErrPendingActionExists
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
			return ErrPendingActionExists
		}
		return fmt.Errorf("attach pending action interrupt: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("attach pending action interrupt rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("attach pending action interrupt: %w: %s", ErrPendingActionNotFound, actionID)
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
			return nil, fmt.Errorf("%w: %s", ErrPendingActionNotFound, actionID)
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
			return nil, fmt.Errorf("%w: %s", ErrPendingActionNotFound, interruptID)
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
			return nil, fmt.Errorf("decide pending action: %w: %s", ErrPendingActionNotFound, actionID)
		}
		return nil, fmt.Errorf("decide pending action load: %w", err)
	}
	if record.Status != events.PendingActionStatusPending {
		return nil, fmt.Errorf("decide pending action: %w: status %q", ErrPendingActionDecided, record.Status)
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
		return nil, fmt.Errorf("decide pending action: %w", ErrPendingActionDecided)
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
		return fmt.Errorf("resolve pending action: %w: %s", ErrPendingActionNotFound, actionID)
	}
	return nil
}

func normalizePendingActionKind(kind events.PendingActionKind) (events.PendingActionKind, error) {
	switch strings.TrimSpace(string(kind)) {
	case string(events.PendingActionKindElicitation):
		return events.PendingActionKindElicitation, nil
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
