package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

type SessionMessagePart struct {
	Kind             string                  `json:"kind"`
	Text             string                  `json:"text,omitempty"`
	Reasoning        string                  `json:"reasoning,omitempty"`
	Status           string                  `json:"status,omitempty"`
	Title            string                  `json:"title,omitempty"`
	Summary          string                  `json:"summary,omitempty"`
	Changed          []string                `json:"changed,omitempty"`
	Verified         []string                `json:"verified,omitempty"`
	Risks            []string                `json:"risks,omitempty"`
	Items            []SessionDisclosureItem `json:"items,omitempty"`
	DetailRunID      string                  `json:"detail_run_id,omitempty"`
	RunID            string                  `json:"run_id,omitempty"`
	Label            string                  `json:"label,omitempty"`
	DecisionID       string                  `json:"decision_id,omitempty"`
	Question         string                  `json:"question,omitempty"`
	SelectedOptionID string                  `json:"selected_option_id,omitempty"`
	Answer           string                  `json:"answer,omitempty"`
	Options          []SessionDecisionOption `json:"options,omitempty"`
	Action           *SessionMessageAction   `json:"action,omitempty"`
}

type SessionDisclosureItem struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Tone    string `json:"tone"`
	SkillID string `json:"skill_id,omitempty"`
}

type SessionDecisionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type SessionMessageAction struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
	Label string `json:"label"`
}

func (s *Store) CreateSession(ctx context.Context, sessionID, title string) (*events.SessionRecord, error) {
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
	return &events.SessionRecord{SessionID: sessionID, Title: title, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) LoadSession(ctx context.Context, sessionID string) (*events.SessionRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT session_id, title, created_at, updated_at FROM sessions WHERE session_id = ?`, sessionID)
	var (
		rec     events.SessionRecord
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

func (s *Store) ListSessions(ctx context.Context, limit int) ([]events.SessionRecord, error) {
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

	items := make([]events.SessionRecord, 0)
	for rows.Next() {
		var (
			rec     events.SessionRecord
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
	if _, err := s.db.ExecContext(ctx, `DELETE FROM session_messages WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete session_messages: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM conversation_segments WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete conversation_segments: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
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
