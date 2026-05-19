package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/runtimehistory"
)

func (s *Store) SaveContextBoundary(ctx context.Context, boundary runtimehistory.ContextBoundary) error {
	if err := validateContextBoundary(boundary); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO context_boundaries(boundary_id, session_id, run_id, sequence, turn_index, mode, trigger, first_index, last_index, covered_first_message_id, covered_last_message_id, previous_boundary_id, summary_message_id, transcript_ref, preserved_from_index, preserved_to_index, preserved_head_message_id, preserved_anchor_message_id, preserved_tail_message_id, tokens_before, tokens_after, effective_window_tokens, summary, summary_snippet, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(boundary.BoundaryID),
		strings.TrimSpace(boundary.SessionID),
		strings.TrimSpace(boundary.RunID),
		boundary.Sequence,
		boundary.TurnIndex,
		strings.TrimSpace(boundary.Mode),
		strings.TrimSpace(boundary.Trigger),
		boundary.FirstIndex,
		boundary.LastIndex,
		strings.TrimSpace(boundary.CoveredFirstMessageID),
		strings.TrimSpace(boundary.CoveredLastMessageID),
		strings.TrimSpace(boundary.PreviousBoundaryID),
		strings.TrimSpace(boundary.SummaryMessageID),
		strings.TrimSpace(boundary.TranscriptRef),
		boundary.PreservedFromIndex,
		boundary.PreservedToIndex,
		strings.TrimSpace(boundary.PreservedHeadMessageID),
		strings.TrimSpace(boundary.PreservedAnchorMessageID),
		strings.TrimSpace(boundary.PreservedTailMessageID),
		boundary.TokensBefore,
		boundary.TokensAfter,
		boundary.EffectiveWindowTokens,
		strings.TrimSpace(boundary.Summary),
		strings.TrimSpace(boundary.SummarySnippet),
		formatTimestamp(boundary.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("save context boundary: %w", err)
	}
	return nil
}

func (s *Store) LoadContextBoundary(ctx context.Context, boundaryID string) (*runtimehistory.ContextBoundary, error) {
	id := strings.TrimSpace(boundaryID)
	if id == "" {
		return nil, errors.New("context boundary id is required")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT boundary_id, session_id, run_id, sequence, turn_index, mode, trigger, first_index, last_index, covered_first_message_id, covered_last_message_id, previous_boundary_id, summary_message_id, transcript_ref, preserved_from_index, preserved_to_index, preserved_head_message_id, preserved_anchor_message_id, preserved_tail_message_id, tokens_before, tokens_after, effective_window_tokens, summary, summary_snippet, created_at
		 FROM context_boundaries
		 WHERE boundary_id = ?`,
		id,
	)
	boundary, err := scanContextBoundary(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load context boundary: %w", err)
	}
	return boundary, nil
}

func (s *Store) LoadLatestContextBoundary(ctx context.Context, sessionID string) (*runtimehistory.ContextBoundary, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil, errors.New("context boundary session id is required")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT boundary_id, session_id, run_id, sequence, turn_index, mode, trigger, first_index, last_index, covered_first_message_id, covered_last_message_id, previous_boundary_id, summary_message_id, transcript_ref, preserved_from_index, preserved_to_index, preserved_head_message_id, preserved_anchor_message_id, preserved_tail_message_id, tokens_before, tokens_after, effective_window_tokens, summary, summary_snippet, created_at
		 FROM context_boundaries
		 WHERE session_id = ?
		 ORDER BY sequence DESC, created_at DESC
		 LIMIT 1`,
		id,
	)
	boundary, err := scanContextBoundary(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load latest context boundary: %w", err)
	}
	return boundary, nil
}

func (s *Store) ListContextBoundaries(ctx context.Context, sessionID string) ([]runtimehistory.ContextBoundary, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil, errors.New("context boundary session id is required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT boundary_id, session_id, run_id, sequence, turn_index, mode, trigger, first_index, last_index, covered_first_message_id, covered_last_message_id, previous_boundary_id, summary_message_id, transcript_ref, preserved_from_index, preserved_to_index, preserved_head_message_id, preserved_anchor_message_id, preserved_tail_message_id, tokens_before, tokens_after, effective_window_tokens, summary, summary_snippet, created_at
		 FROM context_boundaries
		 WHERE session_id = ?
		 ORDER BY sequence ASC, created_at ASC`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("list context boundaries: %w", err)
	}
	defer rows.Close()

	items := make([]runtimehistory.ContextBoundary, 0)
	for rows.Next() {
		boundary, err := scanContextBoundary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan context boundary: %w", err)
		}
		items = append(items, *boundary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate context boundaries: %w", err)
	}
	return items, nil
}

func validateContextBoundary(boundary runtimehistory.ContextBoundary) error {
	if strings.TrimSpace(boundary.BoundaryID) == "" {
		return errors.New("context boundary id is required")
	}
	if strings.TrimSpace(boundary.SessionID) == "" {
		return errors.New("context boundary session id is required")
	}
	if strings.TrimSpace(boundary.RunID) == "" {
		return errors.New("context boundary run id is required")
	}
	if boundary.Sequence <= 0 {
		return errors.New("context boundary sequence must be positive")
	}
	if boundary.TurnIndex < 0 {
		return errors.New("context boundary turn index must be non-negative")
	}
	if strings.TrimSpace(boundary.Mode) == "" {
		return errors.New("context boundary mode is required")
	}
	if strings.TrimSpace(boundary.Trigger) == "" {
		return errors.New("context boundary trigger is required")
	}
	if boundary.FirstIndex < 0 {
		return errors.New("context boundary first index must be non-negative")
	}
	if boundary.LastIndex < boundary.FirstIndex {
		return errors.New("context boundary last index must be greater than or equal to first index")
	}
	if strings.TrimSpace(boundary.CoveredFirstMessageID) == "" {
		return errors.New("context boundary covered first message id is required")
	}
	if strings.TrimSpace(boundary.CoveredLastMessageID) == "" {
		return errors.New("context boundary covered last message id is required")
	}
	if strings.TrimSpace(boundary.SummaryMessageID) == "" {
		return errors.New("context boundary summary message id is required")
	}
	if strings.TrimSpace(boundary.TranscriptRef) == "" {
		return errors.New("context boundary transcript ref is required")
	}
	if boundary.PreservedFromIndex < 0 || boundary.PreservedToIndex < 0 {
		return errors.New("context boundary preserved indexes must be non-negative")
	}
	if boundary.PreservedToIndex < boundary.PreservedFromIndex {
		return errors.New("context boundary preserved to index must be greater than or equal to preserved from index")
	}
	if strings.TrimSpace(boundary.PreservedHeadMessageID) == "" {
		return errors.New("context boundary preserved head message id is required")
	}
	if strings.TrimSpace(boundary.PreservedAnchorMessageID) == "" {
		return errors.New("context boundary preserved anchor message id is required")
	}
	if strings.TrimSpace(boundary.PreservedTailMessageID) == "" {
		return errors.New("context boundary preserved tail message id is required")
	}
	if boundary.TokensBefore <= 0 {
		return errors.New("context boundary tokens before must be positive")
	}
	if boundary.TokensAfter <= 0 {
		return errors.New("context boundary tokens after must be positive")
	}
	if boundary.TokensAfter >= boundary.TokensBefore {
		return errors.New("context boundary tokens after must be less than tokens before")
	}
	if boundary.EffectiveWindowTokens <= 0 {
		return errors.New("context boundary effective window tokens must be positive")
	}
	if boundary.TokensAfter > boundary.EffectiveWindowTokens {
		return errors.New("context boundary tokens after must not exceed effective window tokens")
	}
	if strings.TrimSpace(boundary.Summary) == "" {
		return errors.New("context boundary summary is required")
	}
	if boundary.CreatedAt.IsZero() {
		return errors.New("context boundary created_at is required")
	}
	return nil
}

func scanContextBoundary(scanner interface{ Scan(dest ...any) error }) (*runtimehistory.ContextBoundary, error) {
	var (
		boundary  runtimehistory.ContextBoundary
		createdAt string
	)
	if err := scanner.Scan(
		&boundary.BoundaryID,
		&boundary.SessionID,
		&boundary.RunID,
		&boundary.Sequence,
		&boundary.TurnIndex,
		&boundary.Mode,
		&boundary.Trigger,
		&boundary.FirstIndex,
		&boundary.LastIndex,
		&boundary.CoveredFirstMessageID,
		&boundary.CoveredLastMessageID,
		&boundary.PreviousBoundaryID,
		&boundary.SummaryMessageID,
		&boundary.TranscriptRef,
		&boundary.PreservedFromIndex,
		&boundary.PreservedToIndex,
		&boundary.PreservedHeadMessageID,
		&boundary.PreservedAnchorMessageID,
		&boundary.PreservedTailMessageID,
		&boundary.TokensBefore,
		&boundary.TokensAfter,
		&boundary.EffectiveWindowTokens,
		&boundary.Summary,
		&boundary.SummarySnippet,
		&createdAt,
	); err != nil {
		return nil, err
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "context_boundary.created_at")
	if err != nil {
		return nil, err
	}
	boundary.CreatedAt = parsed
	return &boundary, nil
}
