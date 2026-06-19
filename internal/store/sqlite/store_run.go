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
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/store"
)

func (s *Store) CreateRun(ctx context.Context, runID, input, checkpointID string) error {
	return s.CreateRunWithParams(ctx, store.RunCreateParams{
		RunID:             runID,
		Input:             input,
		CheckpointID:      checkpointID,
		OrchestrationMode: events.ModeDirectResponse,
	})
}

func (s *Store) CreateRunWithSession(ctx context.Context, runID, sessionID string, turnIndex int, input, checkpointID string) error {
	return s.CreateRunWithParams(ctx, store.RunCreateParams{
		RunID:             runID,
		SessionID:         sessionID,
		TurnIndex:         turnIndex,
		Input:             input,
		CheckpointID:      checkpointID,
		OrchestrationMode: events.ModeDirectResponse,
	})
}

func (s *Store) CreateRunWithParams(ctx context.Context, params store.RunCreateParams) error {
	mode := params.OrchestrationMode.Normalize()
	if strings.TrimSpace(string(mode)) == "" {
		return fmt.Errorf("orchestration mode is required")
	}
	now := formatTimestamp(time.Now())
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO runs(run_id, session_id, turn_index, status, input_text, output_text, error_text, checkpoint_id, orchestration_mode, parent_run_id, depth, created_at, updated_at) VALUES(?, ?, ?, ?, ?, '', '', ?, ?, ?, ?, ?, ?)`,
		params.RunID,
		params.SessionID,
		params.TurnIndex,
		string(events.RunStatusRunning),
		params.Input,
		params.CheckpointID,
		string(mode),
		params.ParentRunID,
		params.Depth,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}
	return nil
}

func (s *Store) CreateBoundRun(ctx context.Context, runID, sessionID string, turnIndex int, input, checkpointID string) error {
	return s.CreateBoundRunWithParams(ctx, store.RunCreateParams{
		RunID:             runID,
		SessionID:         sessionID,
		TurnIndex:         turnIndex,
		Input:             input,
		CheckpointID:      checkpointID,
		OrchestrationMode: events.ModeDirectResponse,
	})
}

func (s *Store) CreateBoundRunWithParams(ctx context.Context, params store.RunCreateParams) error {
	if err := s.CreateRunWithParams(ctx, params); err != nil {
		return err
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil
	}
	var bindErr error
	if params.BoundMessageID > 0 {
		bindErr = s.BindUserMessageRunIDByID(ctx, params.BoundMessageID, params.RunID)
	} else {
		bindErr = s.BindLatestUserMessageRunID(ctx, params.SessionID, params.TurnIndex, params.RunID)
	}
	if bindErr != nil {
		if _, cleanupErr := s.db.Exec(`DELETE FROM runs WHERE run_id = ?`, params.RunID); cleanupErr != nil {
			return fmt.Errorf("bind user message run id: %w (cleanup failed: %v)", bindErr, cleanupErr)
		}
		return bindErr
	}
	return nil
}

func (s *Store) AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return events.EventRecord{}, fmt.Errorf("marshal event payload: %w", err)
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO events(run_id, kind, payload_json, created_at) VALUES(?, ?, ?, ?)`,
		runID,
		kind,
		string(body),
		formatTimestamp(now),
	)
	if err != nil {
		return events.EventRecord{}, fmt.Errorf("append event: %w", err)
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return events.EventRecord{}, fmt.Errorf("read event sequence: %w", err)
	}
	return events.EventRecord{Sequence: seq, RunID: runID, Kind: kind, Payload: payload, CreatedAt: now}, nil
}

func (s *Store) FinishRunContext(ctx context.Context, runID string, status events.RunStatus, output, errText string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET status = ?, output_text = ?, error_text = ?, updated_at = ? WHERE run_id = ?`,
		string(status),
		output,
		errText,
		formatTimestamp(time.Now()),
		runID,
	)
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	return nil
}

func (s *Store) UpdateRunOutputContext(ctx context.Context, runID, output string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET output_text = ?, updated_at = ? WHERE run_id = ?`,
		output,
		formatTimestamp(time.Now()),
		runID,
	)
	if err != nil {
		return fmt.Errorf("update run output: %w", err)
	}
	return nil
}

func (s *Store) MarkInterruptedContext(ctx context.Context, runID, output string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET status = ?, output_text = ?, updated_at = ? WHERE run_id = ?`,
		string(events.RunStatusInterrupted),
		output,
		formatTimestamp(time.Now()),
		runID,
	)
	if err != nil {
		return fmt.Errorf("mark interrupted: %w", err)
	}
	return nil
}

func (s *Store) LoadRun(ctx context.Context, runID string) (*events.RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, checkpoint_id, orchestration_mode, parent_run_id, depth, created_at, updated_at FROM runs WHERE run_id = ?`, runID)
	rec, err := scanRunRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", store.ErrRunNotFound, runID)
		}
		return nil, fmt.Errorf("load run: %w", err)
	}
	return rec, nil
}

func (s *Store) ListActiveRuns(ctx context.Context, limit int) ([]events.RunRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, checkpoint_id, orchestration_mode, parent_run_id, depth, created_at, updated_at
			 FROM runs
			 WHERE status = ? AND session_id <> '' AND parent_run_id = ''
			 ORDER BY updated_at DESC
			 LIMIT ?`,
		string(events.RunStatusRunning),
		normalizeRunListLimit(limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list active runs: %w", err)
	}
	return scanRunRows(rows, "list active runs")
}

func (s *Store) ListRecentTerminalRuns(ctx context.Context, limit int) ([]events.RunRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, checkpoint_id, orchestration_mode, parent_run_id, depth, created_at, updated_at
			 FROM runs
			 WHERE status IN (?, ?, ?) AND session_id <> '' AND parent_run_id = ''
			 ORDER BY updated_at DESC
			 LIMIT ?`,
		string(events.RunStatusSucceeded),
		string(events.RunStatusInterrupted),
		string(events.RunStatusFailed),
		normalizeRunListLimit(limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list recent terminal runs: %w", err)
	}
	return scanRunRows(rows, "list recent terminal runs")
}

func normalizeRunListLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	return limit
}

func scanRunRows(rows *sql.Rows, source string) ([]events.RunRecord, error) {
	defer rows.Close()
	items := make([]events.RunRecord, 0)
	for rows.Next() {
		record, err := scanRunRecord(rows)
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

func (s *Store) LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence, kind, payload_json, created_at FROM events WHERE run_id = ? ORDER BY sequence ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	return scanEventRows(rows, runID)
}

func (s *Store) LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]events.EventRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence, kind, payload_json, created_at FROM events WHERE run_id = ? AND sequence > ? ORDER BY sequence ASC`, runID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("query events after: %w", err)
	}
	return scanEventRows(rows, runID)
}

func scanEventRows(rows *sql.Rows, runID string) ([]events.EventRecord, error) {
	defer rows.Close()
	items := make([]events.EventRecord, 0)
	for rows.Next() {
		var (
			seq     int64
			kind    string
			payload string
			created string
			body    any
		)
		if err := rows.Scan(&seq, &kind, &payload, &created); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if err := json.Unmarshal([]byte(payload), &body); err != nil {
			return nil, fmt.Errorf("unmarshal event payload run_id=%s sequence=%d kind=%s: %w", runID, seq, kind, err)
		}
		parsed, err := parseTimestamp(time.RFC3339Nano, created, "event.created_at")
		if err != nil {
			return nil, err
		}
		items = append(items, events.EventRecord{Sequence: seq, RunID: runID, Kind: kind, Payload: body, CreatedAt: parsed})
	}
	return items, rows.Err()
}

func (s *Store) Set(ctx context.Context, key string, value []byte) error {
	now := formatTimestamp(time.Now())
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO checkpoints(checkpoint_id, run_id, payload, created_at, updated_at) VALUES(?, ?, ?, ?, ?)
         ON CONFLICT(checkpoint_id) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at`,
		key,
		key,
		value,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("save checkpoint %s: %w", key, err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT payload FROM checkpoints WHERE checkpoint_id = ?`, key)
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load checkpoint %s: %w", key, err)
	}
	return payload, true, nil
}

func (s *Store) UpsertRunArchive(ctx context.Context, archive model.RunArchive) error {
	touchedPathsJSON, err := json.Marshal(archive.TouchedPaths)
	if err != nil {
		return fmt.Errorf("marshal archive touched paths: %w", err)
	}
	toolNamesJSON, err := json.Marshal(archive.ToolNames)
	if err != nil {
		return fmt.Errorf("marshal archive tool names: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO run_archives(run_id, session_id, input_excerpt, output_excerpt, touched_paths_json, tool_names_json, run_status, created_at)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(run_id) DO UPDATE SET
			     session_id = excluded.session_id,
			     input_excerpt = excluded.input_excerpt,
			     output_excerpt = excluded.output_excerpt,
			     touched_paths_json = excluded.touched_paths_json,
			     tool_names_json = excluded.tool_names_json,
			     run_status = excluded.run_status,
			     created_at = excluded.created_at`,
		archive.RunID,
		archive.SessionID,
		archive.InputExcerpt,
		archive.OutputExcerpt,
		string(touchedPathsJSON),
		string(toolNamesJSON),
		archive.RunStatus,
		formatTimestamp(archive.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert run archive: %w", err)
	}
	return nil
}

func (s *Store) GetRunArchive(ctx context.Context, runID string) (*model.RunArchive, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT run_id, session_id, input_excerpt, output_excerpt, touched_paths_json, tool_names_json, run_status, created_at
			 FROM run_archives WHERE run_id = ?`,
		runID,
	)
	archive, err := scanRunArchive(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get run archive: %w", err)
	}
	return archive, nil
}

func scanRunArchive(scanner interface{ Scan(dest ...any) error }) (*model.RunArchive, error) {
	var (
		archive         model.RunArchive
		touchedPathsRaw string
		toolNamesRaw    string
		createdAt       string
	)
	if err := scanner.Scan(&archive.RunID, &archive.SessionID, &archive.InputExcerpt, &archive.OutputExcerpt, &touchedPathsRaw, &toolNamesRaw, &archive.RunStatus, &createdAt); err != nil {
		return nil, err
	}
	if strings.TrimSpace(touchedPathsRaw) != "" {
		if err := json.Unmarshal([]byte(touchedPathsRaw), &archive.TouchedPaths); err != nil {
			return nil, fmt.Errorf("unmarshal run archive touched paths: %w", err)
		}
	}
	if strings.TrimSpace(toolNamesRaw) != "" {
		if err := json.Unmarshal([]byte(toolNamesRaw), &archive.ToolNames); err != nil {
			return nil, fmt.Errorf("unmarshal run archive tool names: %w", err)
		}
	}
	created, err := parseTimestamp(fixedTimestampLayout, createdAt, "run_archive.created_at")
	if err != nil {
		return nil, err
	}
	archive.CreatedAt = created
	return &archive, nil
}

func (s *Store) SaveRunContextSnapshot(ctx context.Context, snapshot model.RunContextSnapshot) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO run_context_snapshots(run_id, working_checkpoint_content, working_checkpoint_skill_id, decision_profile_hash, decision_action, decision_skill_id, created_at)
				 VALUES(?, ?, ?, ?, ?, ?, ?)
				 ON CONFLICT(run_id) DO UPDATE SET
				     working_checkpoint_content = excluded.working_checkpoint_content,
				     working_checkpoint_skill_id = excluded.working_checkpoint_skill_id,
				     decision_profile_hash = excluded.decision_profile_hash,
				     decision_action = excluded.decision_action,
				     decision_skill_id = excluded.decision_skill_id,
			     created_at = excluded.created_at`,
		snapshot.RunID,
		snapshot.WorkingCheckpointContent,
		snapshot.WorkingCheckpointSkillID,
		snapshot.DecisionProfileHash,
		snapshot.DecisionAction,
		snapshot.DecisionSkillID,
		formatTimestamp(snapshot.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("save run context snapshot: %w", err)
	}
	return nil
}

func (s *Store) LoadRunContextSnapshot(ctx context.Context, runID string) (*model.RunContextSnapshot, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT run_id, working_checkpoint_content, working_checkpoint_skill_id, decision_profile_hash, decision_action, decision_skill_id, created_at
				 FROM run_context_snapshots WHERE run_id = ?`,
		runID,
	)
	var (
		snapshot  model.RunContextSnapshot
		createdAt string
	)
	if err := row.Scan(
		&snapshot.RunID,
		&snapshot.WorkingCheckpointContent,
		&snapshot.WorkingCheckpointSkillID,
		&snapshot.DecisionProfileHash,
		&snapshot.DecisionAction,
		&snapshot.DecisionSkillID,
		&createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load run context snapshot: %w", err)
	}
	created, err := parseTimestamp(fixedTimestampLayout, createdAt, "run_context_snapshot.created_at")
	if err != nil {
		return nil, err
	}
	snapshot.CreatedAt = created
	return &snapshot, nil
}

func (s *Store) SaveContextBoundary(ctx context.Context, boundary model.ContextBoundary) error {
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

func scanContextBoundary(scanner interface{ Scan(dest ...any) error }) (*model.ContextBoundary, error) {
	var (
		boundary  model.ContextBoundary
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
