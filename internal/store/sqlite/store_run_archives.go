package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/runtimehistory"
)

func (s *Store) UpsertRunArchive(ctx context.Context, archive runtimehistory.RunArchive) error {
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

func (s *Store) GetRunArchive(ctx context.Context, runID string) (*runtimehistory.RunArchive, error) {
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

func (s *Store) ListAllRunArchives(ctx context.Context) ([]runtimehistory.RunArchive, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id, session_id, input_excerpt, output_excerpt, touched_paths_json, tool_names_json, run_status, created_at
		 FROM run_archives
		 ORDER BY created_at, run_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all run archives: %w", err)
	}
	defer rows.Close()

	items := make([]runtimehistory.RunArchive, 0)
	for rows.Next() {
		archive, err := scanRunArchive(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run archive: %w", err)
		}
		items = append(items, *archive)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run archives: %w", err)
	}
	return items, nil
}

func scanRunArchive(scanner interface{ Scan(dest ...any) error }) (*runtimehistory.RunArchive, error) {
	var (
		archive         runtimehistory.RunArchive
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
