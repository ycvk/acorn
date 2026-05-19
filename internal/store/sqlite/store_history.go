package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/runtimehistory"
)

func (s *Store) GetConversationHistorySegment(ctx context.Context, segmentID int64) (*runtimehistory.HistoryHit, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, run_id, run_status, user_content || char(10) || assistant_content, created_at
		 FROM conversation_segments WHERE id = ?`,
		segmentID,
	)
	hit, err := scanHistoryHit(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation history segment: %w", err)
	}
	return hit, nil
}

func (s *Store) GetConversationHistorySegmentByRunID(ctx context.Context, runID string) (*runtimehistory.HistoryHit, error) {
	trimmedRunID := strings.TrimSpace(runID)
	if trimmedRunID == "" {
		return nil, errors.New("run id is required")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, run_id, run_status, user_content || char(10) || assistant_content, created_at
		 FROM conversation_segments WHERE run_id = ?`,
		trimmedRunID,
	)
	hit, err := scanHistoryHit(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation history segment by run: %w", err)
	}
	return hit, nil
}

func scanHistoryHit(scanner interface{ Scan(dest ...any) error }) (*runtimehistory.HistoryHit, error) {
	var (
		hit       runtimehistory.HistoryHit
		createdAt string
	)
	if err := scanner.Scan(&hit.SegmentID, &hit.SessionID, &hit.RunID, &hit.RunStatus, &hit.Content, &createdAt); err != nil {
		return nil, err
	}
	timestamp, err := parseTimestamp(fixedTimestampLayout, createdAt, "conversation_segment.created_at")
	if err != nil {
		return nil, err
	}
	hit.Timestamp = timestamp
	return &hit, nil
}
