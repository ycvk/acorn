package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/model"
)

func (s *Store) SaveContextBoundary(ctx context.Context, boundary model.ContextBoundary) error {
	if err := validateContextBoundary(boundary); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO context_boundaries(boundary_id, session_id, run_id, sequence, turn_index, mode, trigger, first_index, last_index, covered_first_message_id, covered_last_message_id, previous_boundary_id, summary_message_id, transcript_ref, preserved_from_index, preserved_to_index, preserved_head_message_id, preserved_anchor_message_id, preserved_tail_message_id, tokens_before, tokens_after, effective_window_tokens, summary, summary_snippet, created_at)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		contextBoundaryInsertArgs(boundary)...,
	)
	if err != nil {
		return fmt.Errorf("save context boundary: %w", err)
	}
	return nil
}

// contextBoundaryInsertArgs returns the positional arguments for a
// context_boundaries INSERT, in column order, with string fields trimmed.
func contextBoundaryInsertArgs(b model.ContextBoundary) []any {
	return []any{
		strings.TrimSpace(b.BoundaryID),
		strings.TrimSpace(b.SessionID),
		strings.TrimSpace(b.RunID),
		b.Sequence,
		b.TurnIndex,
		strings.TrimSpace(b.Mode),
		strings.TrimSpace(b.Trigger),
		b.FirstIndex,
		b.LastIndex,
		strings.TrimSpace(b.CoveredFirstMessageID),
		strings.TrimSpace(b.CoveredLastMessageID),
		strings.TrimSpace(b.PreviousBoundaryID),
		strings.TrimSpace(b.SummaryMessageID),
		strings.TrimSpace(b.TranscriptRef),
		b.PreservedFromIndex,
		b.PreservedToIndex,
		strings.TrimSpace(b.PreservedHeadMessageID),
		strings.TrimSpace(b.PreservedAnchorMessageID),
		strings.TrimSpace(b.PreservedTailMessageID),
		b.TokensBefore,
		b.TokensAfter,
		b.EffectiveWindowTokens,
		strings.TrimSpace(b.Summary),
		strings.TrimSpace(b.SummarySnippet),
		formatTimestamp(b.CreatedAt),
	}
}

func (s *Store) LoadContextBoundary(ctx context.Context, boundaryID string) (*model.ContextBoundary, error) {
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

func (s *Store) LoadLatestContextBoundary(ctx context.Context, sessionID string) (*model.ContextBoundary, error) {
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

func (s *Store) ListContextBoundaries(ctx context.Context, sessionID string) ([]model.ContextBoundary, error) {
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

	items := make([]model.ContextBoundary, 0)
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

func validateContextBoundary(boundary model.ContextBoundary) error {
	if err := validateBoundaryIdentifiers(boundary); err != nil {
		return err
	}
	if err := validateBoundaryIndexes(boundary); err != nil {
		return err
	}
	if err := validateBoundaryMessageRefs(boundary); err != nil {
		return err
	}
	return validateBoundaryTokens(boundary)
}

func validateBoundaryIdentifiers(b model.ContextBoundary) error {
	if strings.TrimSpace(b.BoundaryID) == "" {
		return errors.New("context boundary id is required")
	}
	if strings.TrimSpace(b.SessionID) == "" {
		return errors.New("context boundary session id is required")
	}
	if strings.TrimSpace(b.RunID) == "" {
		return errors.New("context boundary run id is required")
	}
	if b.Sequence <= 0 {
		return errors.New("context boundary sequence must be positive")
	}
	if b.TurnIndex < 0 {
		return errors.New("context boundary turn index must be non-negative")
	}
	if strings.TrimSpace(b.Mode) == "" {
		return errors.New("context boundary mode is required")
	}
	if strings.TrimSpace(b.Trigger) == "" {
		return errors.New("context boundary trigger is required")
	}
	return nil
}

func validateBoundaryIndexes(b model.ContextBoundary) error {
	if b.FirstIndex < 0 {
		return errors.New("context boundary first index must be non-negative")
	}
	if b.LastIndex < b.FirstIndex {
		return errors.New("context boundary last index must be greater than or equal to first index")
	}
	if b.PreservedFromIndex < 0 || b.PreservedToIndex < 0 {
		return errors.New("context boundary preserved indexes must be non-negative")
	}
	if b.PreservedToIndex < b.PreservedFromIndex {
		return errors.New("context boundary preserved to index must be greater than or equal to preserved from index")
	}
	return nil
}

func validateBoundaryMessageRefs(b model.ContextBoundary) error {
	if strings.TrimSpace(b.CoveredFirstMessageID) == "" {
		return errors.New("context boundary covered first message id is required")
	}
	if strings.TrimSpace(b.CoveredLastMessageID) == "" {
		return errors.New("context boundary covered last message id is required")
	}
	if strings.TrimSpace(b.SummaryMessageID) == "" {
		return errors.New("context boundary summary message id is required")
	}
	if strings.TrimSpace(b.TranscriptRef) == "" {
		return errors.New("context boundary transcript ref is required")
	}
	if strings.TrimSpace(b.PreservedHeadMessageID) == "" {
		return errors.New("context boundary preserved head message id is required")
	}
	if strings.TrimSpace(b.PreservedAnchorMessageID) == "" {
		return errors.New("context boundary preserved anchor message id is required")
	}
	if strings.TrimSpace(b.PreservedTailMessageID) == "" {
		return errors.New("context boundary preserved tail message id is required")
	}
	if strings.TrimSpace(b.Summary) == "" {
		return errors.New("context boundary summary is required")
	}
	if b.CreatedAt.IsZero() {
		return errors.New("context boundary created_at is required")
	}
	return nil
}

func validateBoundaryTokens(b model.ContextBoundary) error {
	if b.TokensBefore <= 0 {
		return errors.New("context boundary tokens before must be positive")
	}
	if b.TokensAfter <= 0 {
		return errors.New("context boundary tokens after must be positive")
	}
	if b.TokensAfter >= b.TokensBefore {
		return errors.New("context boundary tokens after must be less than tokens before")
	}
	if b.EffectiveWindowTokens <= 0 {
		return errors.New("context boundary effective window tokens must be positive")
	}
	if b.TokensAfter > b.EffectiveWindowTokens {
		return errors.New("context boundary tokens after must not exceed effective window tokens")
	}
	return nil
}

func scanContextBoundary(scanner interface{ Scan(dest ...any) error }) (*model.ContextBoundary, error) {
	var (
		boundary  model.ContextBoundary
		createdAt string
	)
	if err := scanContextBoundaryFields(scanner, &boundary, &createdAt); err != nil {
		return nil, err
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "context_boundary.created_at")
	if err != nil {
		return nil, err
	}
	boundary.CreatedAt = parsed
	return &boundary, nil
}

func scanContextBoundaryFields(scanner interface{ Scan(dest ...any) error }, b *model.ContextBoundary, createdAt *string) error {
	return scanner.Scan(
		&b.BoundaryID,
		&b.SessionID,
		&b.RunID,
		&b.Sequence,
		&b.TurnIndex,
		&b.Mode,
		&b.Trigger,
		&b.FirstIndex,
		&b.LastIndex,
		&b.CoveredFirstMessageID,
		&b.CoveredLastMessageID,
		&b.PreviousBoundaryID,
		&b.SummaryMessageID,
		&b.TranscriptRef,
		&b.PreservedFromIndex,
		&b.PreservedToIndex,
		&b.PreservedHeadMessageID,
		&b.PreservedAnchorMessageID,
		&b.PreservedTailMessageID,
		&b.TokensBefore,
		&b.TokensAfter,
		&b.EffectiveWindowTokens,
		&b.Summary,
		&b.SummarySnippet,
		createdAt,
	)
}
