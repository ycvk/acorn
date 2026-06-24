package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) SaveArtifact(ctx context.Context, record ArtifactRecord) (ArtifactRecord, error) {
	normalized, err := NormalizeArtifactRecord(record)
	if err != nil {
		return ArtifactRecord{}, err
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
		return ArtifactRecord{}, fmt.Errorf("save artifact %s: %w", normalized.ArtifactID, err)
	}
	return s.LoadArtifact(ctx, normalized.ArtifactID)
}

func (s *Store) LoadArtifact(ctx context.Context, artifactID string) (ArtifactRecord, error) {
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return ArtifactRecord{}, fmt.Errorf("artifact_id is required")
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
			return ArtifactRecord{}, fmt.Errorf("%w: %s", ErrArtifactNotFound, artifactID)
		}
		return ArtifactRecord{}, err
	}
	return record, nil
}

func (s *Store) ListArtifactsByRun(ctx context.Context, runID string) ([]ArtifactRecord, error) {
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

	var items []ArtifactRecord
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

func (s *Store) ListArtifactsBySession(ctx context.Context, sessionID string) ([]ArtifactRecord, error) {
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

	var items []ArtifactRecord
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

func scanArtifact(scanner interface{ Scan(dest ...any) error }) (ArtifactRecord, error) {
	var record ArtifactRecord
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
		return ArtifactRecord{}, err
	}
	record.Kind = kind
	parsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "artifact.created_at")
	if err != nil {
		return ArtifactRecord{}, err
	}
	record.CreatedAt = parsed
	return NormalizeArtifactRecord(record)
}
