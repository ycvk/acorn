package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/store"
)

func (s *Store) AppendSessionMessage(sessionID string, turnIndex int, role, content, runID string) (*events.SessionMessageRecord, error) {
	return s.AppendSessionMessageWithParts(sessionID, turnIndex, role, content, nil, runID)
}

func (s *Store) AppendSessionMessageWithParts(sessionID string, turnIndex int, role, content string, parts []SessionMessagePart, runID string) (*events.SessionMessageRecord, error) {
	now := time.Now().UTC()
	contentParts, err := encodeSessionMessageParts(parts)
	if err != nil {
		return nil, err
	}
	result, err := s.db.Exec(
		`INSERT INTO session_messages(session_id, turn_index, role, content, content_parts, run_id, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		sessionID,
		turnIndex,
		role,
		content,
		string(contentParts),
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
	if _, err := s.db.Exec(`UPDATE sessions SET updated_at = ? WHERE session_id = ?`, formatTimestamp(now), sessionID); err != nil {
		return nil, fmt.Errorf("touch session updated_at: %w", err)
	}
	return &events.SessionMessageRecord{
		ID:           id,
		SessionID:    sessionID,
		TurnIndex:    turnIndex,
		Role:         role,
		Content:      content,
		ContentParts: contentParts,
		RunID:        runID,
		CreatedAt:    now,
	}, nil
}

func (s *Store) UpdateSessionMessageWithParts(ctx context.Context, id int64, content string, parts []SessionMessagePart) error {
	contentParts, err := encodeSessionMessageParts(parts)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE session_messages SET content = ?, content_parts = ? WHERE id = ?`,
		content,
		string(contentParts),
		id,
	)
	if err != nil {
		return fmt.Errorf("update session message: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update session message rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %d", store.ErrSessionMessageNotFound, id)
	}
	return nil
}

func (s *Store) ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]events.SessionMessageRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, session_id, turn_index, role, content, content_parts, run_id, created_at
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

	items := make([]events.SessionMessageRecord, 0)
	for rows.Next() {
		var (
			rec          events.SessionMessageRecord
			contentParts string
			created      string
		)
		if err := rows.Scan(&rec.ID, &rec.SessionID, &rec.TurnIndex, &rec.Role, &rec.Content, &contentParts, &rec.RunID, &created); err != nil {
			return nil, fmt.Errorf("scan session message: %w", err)
		}
		createdAt, err := parseTimestamp(time.RFC3339Nano, created, "session_message.created_at")
		if err != nil {
			return nil, err
		}
		rec.ContentParts = json.RawMessage(contentParts)
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

func (s *Store) LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*events.SessionMessageRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, turn_index, role, content, content_parts, run_id, created_at
		 FROM session_messages
		 WHERE session_id = ? AND role = 'user' AND run_id = ''
		 ORDER BY id DESC
		 LIMIT 1`,
		sessionID,
	)
	var (
		rec          events.SessionMessageRecord
		contentParts string
		created      string
	)
	if err := row.Scan(&rec.ID, &rec.SessionID, &rec.TurnIndex, &rec.Role, &rec.Content, &contentParts, &rec.RunID, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: latest unbound user message for %s", store.ErrSessionMessageNotFound, sessionID)
		}
		return nil, fmt.Errorf("load latest unbound user message: %w", err)
	}
	createdAt, err := parseTimestamp(time.RFC3339Nano, created, "session_message.created_at")
	if err != nil {
		return nil, err
	}
	rec.ContentParts = json.RawMessage(contentParts)
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
		 WHERE id = ? AND role = 'user' AND run_id = ''`,
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

func encodeSessionMessageParts(parts []SessionMessagePart) (json.RawMessage, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(parts)
	if err != nil {
		return nil, fmt.Errorf("marshal session message parts: %w", err)
	}
	return json.RawMessage(payload), nil
}
