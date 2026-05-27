package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/workingstate"
)

func (s *Store) SaveArtifact(ctx context.Context, record store.ArtifactRecord) (store.ArtifactRecord, error) {
	normalized, err := store.NormalizeArtifactRecord(record)
	if err != nil {
		return store.ArtifactRecord{}, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO artifacts (
			artifact_id, run_id, session_id, source_tool_result_ref, kind, title,
			mime_type, relative_path, size_bytes, sha256, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(artifact_id) DO UPDATE SET
			run_id = excluded.run_id,
			session_id = excluded.session_id,
			source_tool_result_ref = excluded.source_tool_result_ref,
			kind = excluded.kind,
			title = excluded.title,
			mime_type = excluded.mime_type,
			relative_path = excluded.relative_path,
			size_bytes = excluded.size_bytes,
			sha256 = excluded.sha256,
			created_at = excluded.created_at
	`, normalized.ArtifactID, normalized.RunID, normalized.SessionID, normalized.SourceToolResultRef,
		string(normalized.Kind), normalized.Title, normalized.MIMEType, normalized.RelativePath,
		normalized.SizeBytes, normalized.SHA256, formatTimestamp(normalized.CreatedAt))
	if err != nil {
		return store.ArtifactRecord{}, fmt.Errorf("save artifact %s: %w", normalized.ArtifactID, err)
	}
	return s.LoadArtifact(ctx, normalized.ArtifactID)
}

func (s *Store) LoadArtifact(ctx context.Context, artifactID string) (store.ArtifactRecord, error) {
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return store.ArtifactRecord{}, fmt.Errorf("artifact_id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT artifact_id, run_id, session_id, source_tool_result_ref, kind, title,
		       mime_type, relative_path, size_bytes, sha256, created_at
		FROM artifacts
		WHERE artifact_id = ?
	`, artifactID)
	record, err := scanArtifact(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.ArtifactRecord{}, fmt.Errorf("%w: %s", store.ErrArtifactNotFound, artifactID)
		}
		return store.ArtifactRecord{}, err
	}
	return record, nil
}

func (s *Store) ListArtifactsByRun(ctx context.Context, runID string) ([]store.ArtifactRecord, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("artifact run_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT artifact_id, run_id, session_id, source_tool_result_ref, kind, title,
		       mime_type, relative_path, size_bytes, sha256, created_at
		FROM artifacts
		WHERE run_id = ?
		ORDER BY created_at ASC, artifact_id ASC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list artifacts for run %s: %w", runID, err)
	}
	defer rows.Close()

	var items []store.ArtifactRecord
	for rows.Next() {
		record, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifacts for run %s: %w", runID, err)
	}
	return items, nil
}

func (s *Store) ListArtifactsBySession(ctx context.Context, sessionID string) ([]store.ArtifactRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("artifact session_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT artifact_id, run_id, session_id, source_tool_result_ref, kind, title,
		       mime_type, relative_path, size_bytes, sha256, created_at
		FROM artifacts
		WHERE session_id = ?
		ORDER BY created_at ASC, artifact_id ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list artifacts for session %s: %w", sessionID, err)
	}
	defer rows.Close()

	var items []store.ArtifactRecord
	for rows.Next() {
		record, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifacts for session %s: %w", sessionID, err)
	}
	return items, nil
}

func scanArtifact(scanner interface{ Scan(dest ...any) error }) (store.ArtifactRecord, error) {
	var record store.ArtifactRecord
	var kind string
	var createdAt string
	if err := scanner.Scan(
		&record.ArtifactID,
		&record.RunID,
		&record.SessionID,
		&record.SourceToolResultRef,
		&kind,
		&record.Title,
		&record.MIMEType,
		&record.RelativePath,
		&record.SizeBytes,
		&record.SHA256,
		&createdAt,
	); err != nil {
		return store.ArtifactRecord{}, err
	}
	record.Kind = store.ArtifactKind(kind)
	parsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "artifact.created_at")
	if err != nil {
		return store.ArtifactRecord{}, err
	}
	record.CreatedAt = parsed
	return store.NormalizeArtifactRecord(record)
}

func (s *Store) Append(ctx context.Context, req store.ToolResultAppendRequest) (store.ToolResultRecord, error) {
	req, err := store.NormalizeToolResultAppendRequest(req)
	if err != nil {
		return store.ToolResultRecord{}, err
	}
	ref := store.BuildToolResultRef(req.RunID, req.CallID)
	preview := store.PreviewToolResult(req.FullText, 240)
	sideEffectsJSON, err := json.Marshal(req.SideEffects)
	if err != nil {
		return store.ToolResultRecord{}, fmt.Errorf("marshal tool result side effects: %w", err)
	}
	evidenceRefsJSON, err := json.Marshal(req.EvidenceRefs)
	if err != nil {
		return store.ToolResultRecord{}, fmt.Errorf("marshal tool result evidence refs: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tool_results (
			result_ref, run_id, session_id, turn_index, call_id, tool_name,
			arguments_json, status, error_reason, preview, full_text, token_estimate,
			side_effects_json, evidence_refs_json, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(result_ref) DO UPDATE SET
			run_id = excluded.run_id,
			session_id = excluded.session_id,
			turn_index = excluded.turn_index,
			call_id = excluded.call_id,
			tool_name = excluded.tool_name,
			arguments_json = excluded.arguments_json,
			status = excluded.status,
			error_reason = excluded.error_reason,
			preview = excluded.preview,
			full_text = excluded.full_text,
			token_estimate = excluded.token_estimate,
			side_effects_json = excluded.side_effects_json,
			evidence_refs_json = excluded.evidence_refs_json,
			created_at = excluded.created_at
	`, ref, req.RunID, req.SessionID, req.TurnIndex, req.CallID, req.ToolName,
		req.ArgumentsJSON, string(req.Status), req.ErrorReason, preview, req.FullText, req.TokenEstimate,
		string(sideEffectsJSON), string(evidenceRefsJSON), formatTimestamp(req.CreatedAt))
	if err != nil {
		return store.ToolResultRecord{}, fmt.Errorf("append tool result %s: %w", ref, err)
	}
	return s.Load(ctx, ref)
}

func (s *Store) Load(ctx context.Context, ref string) (store.ToolResultRecord, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return store.ToolResultRecord{}, fmt.Errorf("tool result ref is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT result_ref, run_id, session_id, turn_index, call_id, tool_name,
		       arguments_json, status, error_reason, preview, full_text, token_estimate,
		       side_effects_json, evidence_refs_json, created_at
		FROM tool_results
		WHERE result_ref = ?
	`, ref)
	record, err := scanToolResult(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.ToolResultRecord{}, store.ErrToolResultNotFound
		}
		return store.ToolResultRecord{}, err
	}
	return record, nil
}

func (s *Store) ListByRun(ctx context.Context, runID string) ([]store.ToolResultRecord, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("tool result run_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT result_ref, run_id, session_id, turn_index, call_id, tool_name,
		       arguments_json, status, error_reason, preview, full_text, token_estimate,
		       side_effects_json, evidence_refs_json, created_at
		FROM tool_results
		WHERE run_id = ?
		ORDER BY created_at ASC, result_ref ASC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list tool results for run %s: %w", runID, err)
	}
	defer rows.Close()

	var items []store.ToolResultRecord
	for rows.Next() {
		record, err := scanToolResult(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tool results for run %s: %w", runID, err)
	}
	return items, nil
}

func (s *Store) AppendEvidenceRef(ctx context.Context, resultRef string, ref store.EvidenceRef) (store.ToolResultRecord, error) {
	ref, err := store.NormalizeEvidenceRef(ref)
	if err != nil {
		return store.ToolResultRecord{}, err
	}
	record, err := s.Load(ctx, resultRef)
	if err != nil {
		return store.ToolResultRecord{}, err
	}
	for _, existing := range record.EvidenceRefs {
		if existing.Kind == ref.Kind && existing.Ref == ref.Ref {
			return record, nil
		}
	}
	record.EvidenceRefs = append(record.EvidenceRefs, ref)
	payload, err := json.Marshal(record.EvidenceRefs)
	if err != nil {
		return store.ToolResultRecord{}, fmt.Errorf("marshal tool result evidence refs: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE tool_results SET evidence_refs_json = ? WHERE result_ref = ?`, string(payload), record.ResultRef); err != nil {
		return store.ToolResultRecord{}, fmt.Errorf("append evidence ref to tool result %s: %w", record.ResultRef, err)
	}
	return s.Load(ctx, record.ResultRef)
}

func scanToolResult(scanner interface{ Scan(dest ...any) error }) (store.ToolResultRecord, error) {
	var record store.ToolResultRecord
	var status string
	var sideEffectsJSON string
	var evidenceRefsJSON string
	var createdAt string
	if err := scanner.Scan(
		&record.ResultRef,
		&record.RunID,
		&record.SessionID,
		&record.TurnIndex,
		&record.CallID,
		&record.ToolName,
		&record.ArgumentsJSON,
		&status,
		&record.ErrorReason,
		&record.Preview,
		&record.FullText,
		&record.TokenEstimate,
		&sideEffectsJSON,
		&evidenceRefsJSON,
		&createdAt,
	); err != nil {
		return store.ToolResultRecord{}, err
	}
	record.Status = store.ToolResultStatus(status)
	if err := json.Unmarshal([]byte(sideEffectsJSON), &record.SideEffects); err != nil {
		return store.ToolResultRecord{}, fmt.Errorf("decode tool result side effects %s: %w", record.ResultRef, err)
	}
	if err := json.Unmarshal([]byte(evidenceRefsJSON), &record.EvidenceRefs); err != nil {
		return store.ToolResultRecord{}, fmt.Errorf("decode tool result evidence refs %s: %w", record.ResultRef, err)
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "tool_result.created_at")
	if err != nil {
		return store.ToolResultRecord{}, err
	}
	record.CreatedAt = parsed
	return record, nil
}

func (s *Store) GetWorkingCheckpoint(ctx context.Context, threadID string) (*workingstate.Checkpoint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT session_id, content, related_skill_id, updated_at
		 FROM working_checkpoints WHERE session_id = ?`,
		strings.TrimSpace(threadID),
	)
	var (
		checkpoint workingstate.Checkpoint
		updatedAt  string
	)
	if err := row.Scan(&checkpoint.ThreadID, &checkpoint.Content, &checkpoint.RelatedSkillID, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get working checkpoint: %w", err)
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, updatedAt, "working_checkpoint.updated_at")
	if err != nil {
		return nil, err
	}
	checkpoint.UpdatedAt = parsed
	return &checkpoint, nil
}

func (s *Store) UpsertWorkingCheckpoint(ctx context.Context, checkpoint workingstate.Checkpoint) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO working_checkpoints(session_id, content, related_skill_id, updated_at)
		 VALUES(?, ?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		     content = excluded.content,
		     related_skill_id = excluded.related_skill_id,
		     updated_at = excluded.updated_at`,
		checkpoint.ThreadID,
		checkpoint.Content,
		checkpoint.RelatedSkillID,
		formatTimestamp(checkpoint.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert working checkpoint: %w", err)
	}
	return nil
}

func (s *Store) DeleteWorkingCheckpoint(ctx context.Context, threadID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM working_checkpoints WHERE session_id = ?`, strings.TrimSpace(threadID)); err != nil {
		return fmt.Errorf("delete working checkpoint: %w", err)
	}
	return nil
}
