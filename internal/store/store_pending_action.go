package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Store) CreatePendingAction(ctx context.Context, input core.PendingActionInput) (*core.PendingActionRecord, error) {
	kind, status, err := normalizeCreatePendingActionInput(input)
	if err != nil {
		return nil, err
	}
	if _, err := s.LoadRun(ctx, input.RunID); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	record := &core.PendingActionRecord{
		ActionID:    strings.TrimSpace(input.ActionID),
		RunID:       strings.TrimSpace(input.RunID),
		InterruptID: strings.TrimSpace(input.InterruptID),
		Kind:        kind,
		Subject:     strings.TrimSpace(input.Subject),
		PayloadJSON: input.PayloadJSON,
		Status:      status,
		Reason:      strings.TrimSpace(input.Reason),
		CreatedAt:   now,
	}
	if err := s.insertPendingAction(ctx, record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Store) insertPendingAction(ctx context.Context, record *core.PendingActionRecord) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO pending_actions(action_id, run_id, interrupt_id, kind, subject, payload_json, status, reason, decision_json, created_at, resolved_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, '', ?, '')`,
		record.ActionID,
		record.RunID,
		record.InterruptID,
		string(record.Kind),
		record.Subject,
		record.PayloadJSON,
		string(record.Status),
		record.Reason,
		formatTimestamp(record.CreatedAt),
	)
	if err != nil {
		if isPendingActionUniqueConstraint(err) {
			return core.ErrPendingActionExists
		}
		return fmt.Errorf("create pending action: %w", err)
	}
	return nil
}

// normalizeCreatePendingActionInput normalizes the kind/status fields of a
// PendingActionInput, returning them together with any error.
func normalizeCreatePendingActionInput(input core.PendingActionInput) (core.PendingActionKind, core.PendingActionStatus, error) {
	kind, err := normalizePendingActionKind(input.Kind)
	if err != nil {
		return "", "", fmt.Errorf("create pending action: %w", err)
	}
	status, err := normalizePendingActionStatus(input.Status)
	if err != nil {
		return "", "", fmt.Errorf("create pending action: %w", err)
	}
	return kind, status, nil
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
			return core.ErrPendingActionExists
		}
		return fmt.Errorf("attach pending action interrupt: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("attach pending action interrupt rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("attach pending action interrupt: %w: %s", core.ErrPendingActionNotFound, actionID)
	}
	return nil
}

func (s *Store) LoadPendingAction(ctx context.Context, actionID string) (*core.PendingActionRecord, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT `+pendingActionColumns+` FROM pending_actions WHERE action_id = ?`,
		actionID,
	)
	record, err := scanPendingActionRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", core.ErrPendingActionNotFound, actionID)
		}
		return nil, fmt.Errorf("load pending action: %w", err)
	}
	return record, nil
}

func (s *Store) LoadPendingActionByInterrupt(ctx context.Context, interruptID string) (*core.PendingActionRecord, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT `+pendingActionColumns+` FROM pending_actions WHERE interrupt_id = ?`,
		interruptID,
	)
	record, err := scanPendingActionRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", core.ErrPendingActionNotFound, interruptID)
		}
		return nil, fmt.Errorf("load pending action by interrupt: %w", err)
	}
	return record, nil
}

func (s *Store) ListPendingActions(ctx context.Context, limit int) ([]core.PendingActionRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT `+pendingActionColumns+` FROM pending_actions WHERE status = ? ORDER BY created_at DESC LIMIT ?`,
		string(core.PendingActionStatusPending),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending actions: %w", err)
	}
	return scanPendingActionRows(rows, "list pending actions")
}

// pendingActionColumns is the shared column list for all pending_actions
// SELECTs, matching the field order scanned by scanPendingActionRecord.
const pendingActionColumns = `action_id, run_id, interrupt_id, kind, subject, payload_json, status, reason, decision_json, created_at, resolved_at`

// scanPendingActionRows scans all rows from a pending_actions query into a
// slice, closing the rows and wrapping the scan error with source.
func scanPendingActionRows(rows *sql.Rows, source string) ([]core.PendingActionRecord, error) {
	defer rows.Close()
	items := make([]core.PendingActionRecord, 0)
	for rows.Next() {
		record, err := scanPendingActionRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		items = append(items, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return items, nil
}

func (s *Store) ListPendingActionsByRun(ctx context.Context, runID string) ([]core.PendingActionRecord, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT `+pendingActionColumns+` FROM pending_actions WHERE run_id = ? ORDER BY created_at ASC`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending actions by run: %w", err)
	}
	return scanPendingActionRows(rows, "list pending actions by run")
}

// DecidePendingAction records a decision on a pending action inside a
// transaction: it loads and validates the action, applies the decision update,
// appends an action.decided event, and commits.
func (s *Store) DecidePendingAction(ctx context.Context, actionID string, status core.PendingActionStatus, decisionJSON string) (out *core.PendingActionRecord, err error) {
	normalizedStatus, err := normalizePendingActionDecisionInput(status)
	if err != nil {
		return nil, err
	}
	resolvedAt := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("decide pending action begin tx: %w", err)
	}
	defer rollbackOnErr(tx, &err, "decide pending action")

	record, err := decideLoadPendingAction(ctx, tx, actionID)
	if err != nil {
		return nil, err
	}
	if err := decideApplyUpdate(ctx, tx, record, normalizedStatus, decisionJSON, resolvedAt); err != nil {
		return nil, err
	}
	if err := decideAppendEvent(tx, record, normalizedStatus, resolvedAt); err != nil {
		return nil, err
	}
	return decideCommit(tx, record, normalizedStatus, decisionJSON, resolvedAt)
}

// normalizePendingActionDecisionInput normalizes the decision status,
// wrapping any error with the "decide pending action" context.
func normalizePendingActionDecisionInput(status core.PendingActionStatus) (core.PendingActionStatus, error) {
	normalizedStatus, err := normalizePendingActionDecision(status)
	if err != nil {
		return "", fmt.Errorf("decide pending action: %w", err)
	}
	return normalizedStatus, nil
}

// decideCommit commits the tx and returns the finalized record.
func decideCommit(tx *sql.Tx, record *core.PendingActionRecord, status core.PendingActionStatus, decisionJSON string, resolvedAt time.Time) (*core.PendingActionRecord, error) {
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("decide pending action commit: %w", err)
	}
	record.Status = status
	record.DecisionJSON = decisionJSON
	record.ResolvedAt = &resolvedAt
	return record, nil
}

// decideLoadPendingAction loads and validates that the action is still pending.
func decideLoadPendingAction(ctx context.Context, tx *sql.Tx, actionID string) (*core.PendingActionRecord, error) {
	record, err := scanPendingActionRecord(tx.QueryRowContext(ctx,
		`SELECT `+pendingActionColumns+` FROM pending_actions WHERE action_id = ?`,
		actionID,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("decide pending action: %w: %s", core.ErrPendingActionNotFound, actionID)
		}
		return nil, fmt.Errorf("decide pending action load: %w", err)
	}
	if record.Status != core.PendingActionStatusPending {
		return nil, fmt.Errorf("decide pending action: %w: status %q", core.ErrPendingActionDecided, record.Status)
	}
	return record, nil
}

// decideApplyUpdate runs the conditional status update within the tx.
func decideApplyUpdate(ctx context.Context, tx *sql.Tx, record *core.PendingActionRecord, status core.PendingActionStatus, decisionJSON string, resolvedAt time.Time) error {
	result, err := tx.ExecContext(
		ctx,
		`UPDATE pending_actions SET status = ?, decision_json = ?, resolved_at = ? WHERE action_id = ? AND status = ?`,
		string(status),
		decisionJSON,
		formatTimestamp(resolvedAt),
		record.ActionID,
		string(core.PendingActionStatusPending),
	)
	if err != nil {
		return fmt.Errorf("decide pending action: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("decide pending action rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("decide pending action: %w", core.ErrPendingActionDecided)
	}
	return nil
}

// decideAppendEvent marshals and inserts the action.decided event row.
func decideAppendEvent(tx *sql.Tx, record *core.PendingActionRecord, status core.PendingActionStatus, resolvedAt time.Time) error {
	eventPayload, err := json.Marshal(map[string]any{
		"action_id":    record.ActionID,
		"interrupt_id": record.InterruptID,
		"kind":         record.Kind,
		"subject":      record.Subject,
		"decision":     string(status),
		"reason":       record.Reason,
		"resolved_at":  resolvedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return fmt.Errorf("marshal action.decided event: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO events(run_id, kind, payload_json, created_at) VALUES(?, ?, ?, ?)`,
		record.RunID,
		"action.decided",
		string(eventPayload),
		formatTimestamp(resolvedAt),
	); err != nil {
		return fmt.Errorf("insert action.decided event: %w", err)
	}
	return nil
}

func (s *Store) ResolvePendingAction(ctx context.Context, actionID string) error {
	resolvedAt := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE pending_actions SET status = ?, resolved_at = ? WHERE action_id = ?`,
		string(core.PendingActionStatusResolved),
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
		return fmt.Errorf("resolve pending action: %w: %s", core.ErrPendingActionNotFound, actionID)
	}
	return nil
}

func normalizePendingActionKind(kind core.PendingActionKind) (core.PendingActionKind, error) {
	switch strings.TrimSpace(string(kind)) {
	case string(core.PendingActionKindElicitation):
		return core.PendingActionKindElicitation, nil
	case string(core.PendingActionKindOperatorQuestion):
		return core.PendingActionKindOperatorQuestion, nil
	default:
		return "", fmt.Errorf("unsupported pending action kind %q", kind)
	}
}

func normalizePendingActionStatus(status core.PendingActionStatus) (core.PendingActionStatus, error) {
	switch strings.TrimSpace(string(status)) {
	case "":
		return core.PendingActionStatusPending, nil
	case string(core.PendingActionStatusPending):
		return core.PendingActionStatusPending, nil
	case string(core.PendingActionStatusApproved):
		return core.PendingActionStatusApproved, nil
	case string(core.PendingActionStatusRejected):
		return core.PendingActionStatusRejected, nil
	case string(core.PendingActionStatusResolved):
		return core.PendingActionStatusResolved, nil
	default:
		return "", fmt.Errorf("unsupported pending action status %q", status)
	}
}

func normalizePendingActionDecision(status core.PendingActionStatus) (core.PendingActionStatus, error) {
	switch strings.TrimSpace(string(status)) {
	case string(core.PendingActionStatusApproved):
		return core.PendingActionStatusApproved, nil
	case string(core.PendingActionStatusRejected):
		return core.PendingActionStatusRejected, nil
	default:
		return "", fmt.Errorf("unsupported pending action decision status %q", status)
	}
}

func isPendingActionUniqueConstraint(err error) bool {
	message := err.Error()
	return strings.Contains(message, "UNIQUE constraint failed: pending_actions.action_id") ||
		strings.Contains(message, "UNIQUE constraint failed: pending_actions.interrupt_id")
}
