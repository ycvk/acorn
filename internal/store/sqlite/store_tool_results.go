package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/toolresult"
)

func (s *Store) Append(ctx context.Context, req toolresult.AppendRequest) (toolresult.Record, error) {
	req, err := toolresult.NormalizeAppendRequest(req)
	if err != nil {
		return toolresult.Record{}, err
	}
	ref := toolresult.BuildRef(req.RunID, req.CallID)
	preview := toolresult.Preview(req.FullText, 240)
	sideEffectsJSON, err := json.Marshal(req.SideEffects)
	if err != nil {
		return toolresult.Record{}, fmt.Errorf("marshal tool result side effects: %w", err)
	}
	evidenceRefsJSON, err := json.Marshal(req.EvidenceRefs)
	if err != nil {
		return toolresult.Record{}, fmt.Errorf("marshal tool result evidence refs: %w", err)
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
		return toolresult.Record{}, fmt.Errorf("append tool result %s: %w", ref, err)
	}
	return s.Load(ctx, ref)
}

func (s *Store) Load(ctx context.Context, ref string) (toolresult.Record, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return toolresult.Record{}, fmt.Errorf("tool result ref is required")
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
		if err == sql.ErrNoRows {
			return toolresult.Record{}, toolresult.ErrToolResultNotFound
		}
		return toolresult.Record{}, err
	}
	return record, nil
}

func (s *Store) ListByRun(ctx context.Context, runID string) ([]toolresult.Record, error) {
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

	var items []toolresult.Record
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

func (s *Store) AppendEvidenceRef(ctx context.Context, resultRef string, ref toolresult.EvidenceRef) (toolresult.Record, error) {
	ref, err := toolresult.NormalizeEvidenceRef(ref)
	if err != nil {
		return toolresult.Record{}, err
	}
	record, err := s.Load(ctx, resultRef)
	if err != nil {
		return toolresult.Record{}, err
	}
	for _, existing := range record.EvidenceRefs {
		if existing.Kind == ref.Kind && existing.Ref == ref.Ref {
			return record, nil
		}
	}
	record.EvidenceRefs = append(record.EvidenceRefs, ref)
	payload, err := json.Marshal(record.EvidenceRefs)
	if err != nil {
		return toolresult.Record{}, fmt.Errorf("marshal tool result evidence refs: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE tool_results SET evidence_refs_json = ? WHERE result_ref = ?`, string(payload), record.ResultRef); err != nil {
		return toolresult.Record{}, fmt.Errorf("append evidence ref to tool result %s: %w", record.ResultRef, err)
	}
	return s.Load(ctx, record.ResultRef)
}

type toolResultScanner interface {
	Scan(dest ...any) error
}

func scanToolResult(scanner toolResultScanner) (toolresult.Record, error) {
	var record toolresult.Record
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
		return toolresult.Record{}, err
	}
	record.Status = toolresult.Status(status)
	if err := json.Unmarshal([]byte(sideEffectsJSON), &record.SideEffects); err != nil {
		return toolresult.Record{}, fmt.Errorf("decode tool result side effects %s: %w", record.ResultRef, err)
	}
	if err := json.Unmarshal([]byte(evidenceRefsJSON), &record.EvidenceRefs); err != nil {
		return toolresult.Record{}, fmt.Errorf("decode tool result evidence refs %s: %w", record.ResultRef, err)
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "tool_result.created_at")
	if err != nil {
		return toolresult.Record{}, err
	}
	record.CreatedAt = parsed
	return record, nil
}
