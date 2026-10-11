package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Store) AppendSessionMessage(ctx context.Context, sessionID string, turnIndex int, role, content, runID string) (*core.SessionMessageRecord, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO session_messages(session_id, turn_index, role, content, run_id, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		sessionID,
		turnIndex,
		role,
		content,
		runID,
		formatTimestamp(now),
	)
	if err != nil {
		return nil, fmt.Errorf("append session message: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read session message id: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE session_id = ?`, formatTimestamp(now), sessionID); err != nil {
		return nil, fmt.Errorf("touch session updated_at: %w", err)
	}
	return &core.SessionMessageRecord{
		ID:        id,
		SessionID: sessionID,
		TurnIndex: turnIndex,
		Role:      role,
		Content:   content,
		RunID:     runID,
		CreatedAt: now,
	}, nil
}

func (s *Store) ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]core.SessionMessageRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, session_id, turn_index, role, content, run_id, created_at
         FROM session_messages
         WHERE session_id = ?
         ORDER BY id DESC
         LIMIT ?`,
		sessionID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list session messages: %w", err)
	}
	defer rows.Close()

	items := make([]core.SessionMessageRecord, 0)
	for rows.Next() {
		var (
			rec     core.SessionMessageRecord
			created string
		)
		if err := rows.Scan(&rec.ID, &rec.SessionID, &rec.TurnIndex, &rec.Role, &rec.Content, &rec.RunID, &created); err != nil {
			return nil, fmt.Errorf("scan session message: %w", err)
		}
		createdAt, err := parseTimestamp(time.RFC3339Nano, created, "session_message.created_at")
		if err != nil {
			return nil, err
		}
		rec.CreatedAt = createdAt
		items = append(items, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, nil
}

func (s *Store) NextSessionMessageTurnIndex(ctx context.Context, sessionID string) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(turn_index), 0) FROM session_messages WHERE session_id = ?`, sessionID)
	var current int
	if err := row.Scan(&current); err != nil {
		return 0, fmt.Errorf("next session message turn index: %w", err)
	}
	return current + 1, nil
}

func (s *Store) LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*core.SessionMessageRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, turn_index, role, content, run_id, created_at
		 FROM session_messages
		 WHERE session_id = ? AND role = 'user' AND run_id = ''
		 ORDER BY id DESC
		 LIMIT 1`,
		sessionID,
	)
	var (
		rec     core.SessionMessageRecord
		created string
	)
	if err := row.Scan(&rec.ID, &rec.SessionID, &rec.TurnIndex, &rec.Role, &rec.Content, &rec.RunID, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: latest unbound user message for %s", core.ErrSessionMessageNotFound, sessionID)
		}
		return nil, fmt.Errorf("load latest unbound user message: %w", err)
	}
	createdAt, err := parseTimestamp(time.RFC3339Nano, created, "session_message.created_at")
	if err != nil {
		return nil, err
	}
	rec.CreatedAt = createdAt
	return &rec, nil
}

func (s *Store) BindLatestUserMessageRunID(ctx context.Context, sessionID string, turnIndex int, runID string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE session_messages
		 SET run_id = ?
		 WHERE id = (
			 SELECT id
			 FROM session_messages
			 WHERE session_id = ?
			   AND turn_index = ?
			   AND role = 'user'
			   AND run_id = ''
			 ORDER BY id DESC
			 LIMIT 1
		 )`,
		runID,
		sessionID,
		turnIndex,
	)
	if err != nil {
		return fmt.Errorf("bind latest user message run id: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("bind latest user message run id rows affected: %w", err)
	}
	if affected == 0 {
		return errors.New("latest user session message not found")
	}
	return nil
}

// BindUserMessageRunIDByID binds a run to an exact user message id. The
// WHERE run_id = ” guard makes it race-free: a concurrent create that already
// bound this message yields RowsAffected = 0 and an error (so the caller rolls
// the run back) instead of silently mis-binding.
func (s *Store) BindUserMessageRunIDByID(ctx context.Context, messageID int64, runID string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE session_messages
		 SET run_id = ?
		 WHERE id = ? AND role IN ('user', 'wake', 'capture') AND run_id = ''`,
		runID,
		messageID,
	)
	if err != nil {
		return fmt.Errorf("bind user message run id by id: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("bind user message run id by id rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("user session message %d not found or already bound", messageID)
	}
	return nil
}

// LoadLatestRepliesForSessions returns each session's newest non-empty owner
// or assistant message, keyed by session id; sessions without one are absent.
func (s *Store) LoadLatestRepliesForSessions(ctx context.Context, sessionIDs []string) (map[string]core.SessionMessageRecord, error) {
	placeholders, args := inClause(sessionIDs)
	result := map[string]core.SessionMessageRecord{}
	if len(args) == 0 {
		return result, nil
	}
	rows, err := s.db.QueryContext(ctx, `WITH ranked AS (
			SELECT id, session_id, turn_index, role, content, run_id, created_at,
				ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY id DESC) AS row_num
			FROM session_messages
			WHERE session_id IN (`+placeholders+`) AND role IN ('user', 'assistant') AND content <> ''
		)
		SELECT id, session_id, turn_index, role, content, run_id, created_at FROM ranked WHERE row_num = 1`, args...)
	if err != nil {
		return nil, fmt.Errorf("load latest replies: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			rec     core.SessionMessageRecord
			created string
		)
		if err := rows.Scan(&rec.ID, &rec.SessionID, &rec.TurnIndex, &rec.Role, &rec.Content, &rec.RunID, &created); err != nil {
			return nil, fmt.Errorf("scan latest reply: %w", err)
		}
		if rec.CreatedAt, err = parseTimestamp(time.RFC3339Nano, created, "session_message.created_at"); err != nil {
			return nil, err
		}
		result[rec.SessionID] = rec
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load latest replies: %w", err)
	}
	return result, nil
}

// inClause returns "?, ?, ..." and the trimmed, de-duplicated non-empty values.
func inClause(values []string) (string, []any) {
	seen := make(map[string]struct{}, len(values))
	args := make([]any, 0, len(values))
	marks := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		args = append(args, trimmed)
		marks = append(marks, "?")
	}
	return strings.Join(marks, ", "), args
}
