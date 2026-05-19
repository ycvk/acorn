package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

var ErrSegmentNotFound = errors.New("segment not found")

func (s *Store) ListSessionMessagesByRunID(ctx context.Context, runID string) ([]events.SessionMessageRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, turn_index, role, content, run_id, created_at
		 FROM session_messages
		 WHERE run_id = ?
		 ORDER BY id ASC`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("list session messages by run_id: %w", err)
	}
	defer rows.Close()
	var items []events.SessionMessageRecord
	for rows.Next() {
		var rec events.SessionMessageRecord
		var created string
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
	return items, rows.Err()
}

func (s *Store) CreateSegmentFromRun(ctx context.Context, runID string, runStatus events.RunStatus) (int64, error) {
	run, err := s.LoadRun(ctx, runID)
	if err != nil {
		return 0, fmt.Errorf("create segment from run: load run: %w", err)
	}
	messages, err := s.ListSessionMessagesByRunID(ctx, runID)
	if err != nil {
		return 0, fmt.Errorf("create segment from run: list messages: %w", err)
	}
	var userContent strings.Builder
	var assistantContent strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if userContent.Len() > 0 {
				userContent.WriteString("\n")
			}
			userContent.WriteString(msg.Content)
		case "assistant":
			if assistantContent.Len() > 0 {
				assistantContent.WriteString("\n")
			}
			assistantContent.WriteString(msg.Content)
		}
	}
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO conversation_segments(session_id, run_id, user_content, assistant_content, run_status, created_at)
		 VALUES(?, ?, ?, ?, ?, ?)
		 ON CONFLICT(run_id) DO UPDATE SET
		     session_id = excluded.session_id,
		     user_content = excluded.user_content,
		     assistant_content = excluded.assistant_content,
		     run_status = excluded.run_status,
		     created_at = excluded.created_at`,
		run.SessionID,
		runID,
		userContent.String(),
		assistantContent.String(),
		string(runStatus),
		formatTimestamp(time.Now().UTC()),
	)
	if err != nil {
		return 0, fmt.Errorf("create segment from run: insert: %w", err)
	}
	segmentID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("create segment from run: last insert id: %w", err)
	}
	return segmentID, nil
}
