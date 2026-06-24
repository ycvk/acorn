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

// SessionMessagePart is one renderable fragment of a session message. Its JSON
// shape is the remote client wire contract, previously owned by sessionview.
// The sessionview projection was retired with the architecture refactor; this
// local type keeps the persisted content_parts shape stable.
type SessionMessagePart struct {
	Kind             string   `json:"kind"`
	Text             string   `json:"text,omitempty"`
	Reasoning        string   `json:"reasoning,omitempty"`
	Status           string   `json:"status,omitempty"`
	Title            string   `json:"title,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	Changed          []string `json:"changed,omitempty"`
	Verified         []string `json:"verified,omitempty"`
	Risks            []string `json:"risks,omitempty"`
	DetailRunID      string   `json:"detail_run_id,omitempty"`
	RunID            string   `json:"run_id,omitempty"`
	Label            string   `json:"label,omitempty"`
	DecisionID       string   `json:"decision_id,omitempty"`
	Question         string   `json:"question,omitempty"`
	SelectedOptionID string   `json:"selected_option_id,omitempty"`
	Answer           string   `json:"answer,omitempty"`
}

func (s *Store) CreateSession(ctx context.Context, sessionID, title string) (*core.SessionRecord, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO sessions(session_id, title, created_at, updated_at) VALUES(?, ?, ?, ?)`,
		sessionID,
		title,
		formatTimestamp(now),
		formatTimestamp(now),
	)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &core.SessionRecord{SessionID: sessionID, Title: title, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) LoadSession(ctx context.Context, sessionID string) (*core.SessionRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT session_id, title, created_at, updated_at FROM sessions WHERE session_id = ?`, sessionID)
	var (
		rec     core.SessionRecord
		created string
		updated string
	)
	if err := row.Scan(&rec.SessionID, &rec.Title, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
		}
		return nil, fmt.Errorf("load session: %w", err)
	}
	createdAt, err := parseTimestamp(time.RFC3339Nano, created, "session.created_at")
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTimestamp(time.RFC3339Nano, updated, "session.updated_at")
	if err != nil {
		return nil, err
	}
	rec.CreatedAt = createdAt
	rec.UpdatedAt = updatedAt
	return &rec, nil
}

func (s *Store) ListSessions(ctx context.Context, limit int) ([]core.SessionRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT session_id, title, created_at, updated_at
         FROM sessions
         ORDER BY updated_at DESC, created_at DESC
         LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	items := make([]core.SessionRecord, 0)
	for rows.Next() {
		var (
			rec     core.SessionRecord
			created string
			updated string
		)
		if err := rows.Scan(&rec.SessionID, &rec.Title, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		createdAt, err := parseTimestamp(time.RFC3339Nano, created, "session.created_at")
		if err != nil {
			return nil, err
		}
		updatedAt, err := parseTimestamp(time.RFC3339Nano, updated, "session.updated_at")
		if err != nil {
			return nil, err
		}
		rec.CreatedAt = createdAt
		rec.UpdatedAt = updatedAt
		items = append(items, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if tx == nil {
			return
		}
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("delete session rollback: %w", rollbackErr))
		}
	}()
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_messages WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete session_messages: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete session: %w", err)
	}
	tx = nil
	return nil
}

func (s *Store) UpdateSessionTitleIfEmpty(ctx context.Context, sessionID, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET title = ?, updated_at = ? WHERE session_id = ? AND trim(title) = ''`,
		title,
		formatTimestamp(time.Now()),
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("update session title: %w", err)
	}
	if _, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("update session title: %w", err)
	}
	return nil
}

func (s *Store) UpdateSessionTitle(ctx context.Context, sessionID, title string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET title = ?, updated_at = ? WHERE session_id = ?`,
		strings.TrimSpace(title),
		formatTimestamp(time.Now()),
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("update session title: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update session title rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	return nil
}

func (s *Store) CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error) {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin create fresh session turn: %w", err)
	}
	defer func() {
		if tx == nil {
			return
		}
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("create fresh session turn rollback: %w", rollbackErr))
		}
	}()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO sessions(session_id, title, created_at, updated_at) VALUES(?, ?, ?, ?)`,
		sessionID,
		title,
		formatTimestamp(now),
		formatTimestamp(now),
	); err != nil {
		return 0, fmt.Errorf("create fresh session turn: create session: %w", err)
	}

	const turnIndex = 1
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO session_messages(session_id, turn_index, role, content, content_parts, run_id, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		sessionID,
		turnIndex,
		"user",
		input,
		"",
		"",
		formatTimestamp(now),
	); err != nil {
		return 0, fmt.Errorf("create fresh session turn: append session message: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit create fresh session turn: %w", err)
	}
	tx = nil
	return turnIndex, nil
}

func (s *Store) NextTurnIndex(ctx context.Context, sessionID string) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(turn_index), 0) FROM runs WHERE session_id = ?`, sessionID)
	var current int
	if err := row.Scan(&current); err != nil {
		return 0, fmt.Errorf("next turn index: %w", err)
	}
	return current + 1, nil
}

func (s *Store) PrepareChatTurn(ctx context.Context, sessionID, input, title string, historyLimit int) (int, []core.SessionMessageRecord, error) {
	if _, err := s.LoadSession(ctx, sessionID); err != nil {
		return 0, nil, err
	}
	turnIndex, err := s.NextTurnIndex(ctx, sessionID)
	if err != nil {
		return 0, nil, err
	}
	if _, err := s.AppendSessionMessage(ctx, sessionID, turnIndex, "user", input, ""); err != nil {
		return 0, nil, err
	}
	if err := s.UpdateSessionTitleIfEmpty(ctx, sessionID, title); err != nil {
		return 0, nil, err
	}
	items, err := s.ListSessionMessages(ctx, sessionID, historyLimit)
	if err != nil {
		return 0, nil, err
	}
	return turnIndex, items, nil
}
