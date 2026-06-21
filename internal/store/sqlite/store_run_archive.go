package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/model"
)

func (s *Store) UpsertRunArchive(ctx context.Context, archive model.RunArchive) error {
	touchedPathsJSON, toolNamesJSON, err := marshalRunArchiveJSON(archive)
	if err != nil {
		return err
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

// marshalRunArchiveJSON marshals the touched-paths and tool-names slices of an
// archive into the JSON strings stored in run_archives.
func marshalRunArchiveJSON(archive model.RunArchive) (string, string, error) {
	touchedPathsJSON, err := json.Marshal(archive.TouchedPaths)
	if err != nil {
		return "", "", fmt.Errorf("marshal archive touched paths: %w", err)
	}
	toolNamesJSON, err := json.Marshal(archive.ToolNames)
	if err != nil {
		return "", "", fmt.Errorf("marshal archive tool names: %w", err)
	}
	return string(touchedPathsJSON), string(toolNamesJSON), nil
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
	if err := unmarshalRunArchiveJSON(&archive, touchedPathsRaw, toolNamesRaw); err != nil {
		return nil, err
	}
	created, err := parseTimestamp(fixedTimestampLayout, createdAt, "run_archive.created_at")
	if err != nil {
		return nil, err
	}
	archive.CreatedAt = created
	return &archive, nil
}

func unmarshalRunArchiveJSON(archive *model.RunArchive, touchedPathsRaw, toolNamesRaw string) error {
	if strings.TrimSpace(touchedPathsRaw) != "" {
		if err := json.Unmarshal([]byte(touchedPathsRaw), &archive.TouchedPaths); err != nil {
			return fmt.Errorf("unmarshal run archive touched paths: %w", err)
		}
	}
	if strings.TrimSpace(toolNamesRaw) != "" {
		if err := json.Unmarshal([]byte(toolNamesRaw), &archive.ToolNames); err != nil {
			return fmt.Errorf("unmarshal run archive tool names: %w", err)
		}
	}
	return nil
}

func (s *Store) SaveRunContextSnapshot(ctx context.Context, snapshot model.RunContextSnapshot) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO run_context_snapshots(run_id, working_checkpoint_content, working_checkpoint_skill_id, created_at)
				 VALUES(?, ?, ?, ?)
				 ON CONFLICT(run_id) DO UPDATE SET
				     working_checkpoint_content = excluded.working_checkpoint_content,
				     working_checkpoint_skill_id = excluded.working_checkpoint_skill_id,
			     created_at = excluded.created_at`,
		snapshot.RunID,
		snapshot.WorkingCheckpointContent,
		snapshot.WorkingCheckpointSkillID,
		formatTimestamp(snapshot.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("save run context snapshot: %w", err)
	}
	return nil
}

func (s *Store) LoadRunContextSnapshot(ctx context.Context, runID string) (*model.RunContextSnapshot, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT run_id, working_checkpoint_content, working_checkpoint_skill_id, created_at
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
