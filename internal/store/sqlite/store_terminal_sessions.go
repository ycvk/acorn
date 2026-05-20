package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/terminalsession"
)

func (s *Store) SaveTerminalSession(ctx context.Context, record terminalsession.SessionRecord) (terminalsession.SessionRecord, error) {
	normalized, err := terminalsession.NormalizeSessionRecord(record)
	if err != nil {
		return terminalsession.SessionRecord{}, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO terminal_sessions (
			terminal_session_id, run_id, session_id, label, command_json, cwd,
			interactive, pty, status, pid, process_group_id, exit_code, signal,
			stdout_artifact_id, stderr_artifact_id, pty_artifact_id,
			started_at, ended_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(terminal_session_id) DO UPDATE SET
			run_id = excluded.run_id,
			session_id = excluded.session_id,
			label = excluded.label,
			command_json = excluded.command_json,
			cwd = excluded.cwd,
			interactive = excluded.interactive,
			pty = excluded.pty,
			status = excluded.status,
			pid = excluded.pid,
			process_group_id = excluded.process_group_id,
			exit_code = excluded.exit_code,
			signal = excluded.signal,
			stdout_artifact_id = excluded.stdout_artifact_id,
			stderr_artifact_id = excluded.stderr_artifact_id,
			pty_artifact_id = excluded.pty_artifact_id,
			started_at = excluded.started_at,
			ended_at = excluded.ended_at,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`, normalized.TerminalSessionID, normalized.RunID, normalized.SessionID, normalized.Label,
		normalized.CommandJSON, normalized.Cwd, boolToSQLite(normalized.Interactive), boolToSQLite(normalized.PTY),
		string(normalized.Status), nullableInt(normalized.ProcessID), nullableInt(normalized.ProcessGroupID),
		nullableInt(normalized.ExitCode), normalized.Signal, normalized.StdoutArtifactID,
		normalized.StderrArtifactID, normalized.PTYArtifactID, nullableTimestamp(normalized.StartedAt),
		nullableTimestamp(normalized.EndedAt), formatTimestamp(normalized.CreatedAt),
		formatTimestamp(normalized.UpdatedAt))
	if err != nil {
		return terminalsession.SessionRecord{}, fmt.Errorf("save terminal session %s: %w", normalized.TerminalSessionID, err)
	}
	return s.LoadTerminalSession(ctx, normalized.TerminalSessionID)
}

func (s *Store) LoadTerminalSession(ctx context.Context, terminalSessionID string) (terminalsession.SessionRecord, error) {
	terminalSessionID = strings.TrimSpace(terminalSessionID)
	if terminalSessionID == "" {
		return terminalsession.SessionRecord{}, fmt.Errorf("terminal_session_id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT terminal_session_id, run_id, session_id, label, command_json, cwd,
		       interactive, pty, status, pid, process_group_id, exit_code, signal,
		       stdout_artifact_id, stderr_artifact_id, pty_artifact_id,
		       started_at, ended_at, created_at, updated_at
		FROM terminal_sessions
		WHERE terminal_session_id = ?
	`, terminalSessionID)
	record, err := scanTerminalSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return terminalsession.SessionRecord{}, fmt.Errorf("%w: %s", terminalsession.ErrSessionNotFound, terminalSessionID)
		}
		return terminalsession.SessionRecord{}, err
	}
	return record, nil
}

func (s *Store) ListTerminalSessionsByRun(ctx context.Context, runID string) ([]terminalsession.SessionRecord, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("terminal session run_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT terminal_session_id, run_id, session_id, label, command_json, cwd,
		       interactive, pty, status, pid, process_group_id, exit_code, signal,
		       stdout_artifact_id, stderr_artifact_id, pty_artifact_id,
		       started_at, ended_at, created_at, updated_at
		FROM terminal_sessions
		WHERE run_id = ?
		ORDER BY created_at ASC, terminal_session_id ASC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list terminal sessions for run %s: %w", runID, err)
	}
	defer rows.Close()

	var items []terminalsession.SessionRecord
	for rows.Next() {
		record, err := scanTerminalSession(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate terminal sessions for run %s: %w", runID, err)
	}
	return items, nil
}

func (s *Store) SaveTerminalSessionLog(ctx context.Context, record terminalsession.LogRecord) (terminalsession.LogRecord, error) {
	normalized, err := terminalsession.NormalizeLogRecord(record)
	if err != nil {
		return terminalsession.LogRecord{}, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO terminal_session_logs (
			log_id, terminal_session_id, stream, artifact_id, start_offset, size_bytes, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(log_id) DO UPDATE SET
			terminal_session_id = excluded.terminal_session_id,
			stream = excluded.stream,
			artifact_id = excluded.artifact_id,
			start_offset = excluded.start_offset,
			size_bytes = excluded.size_bytes,
			created_at = excluded.created_at
	`, normalized.LogID, normalized.TerminalSessionID, string(normalized.Stream),
		normalized.ArtifactID, normalized.StartOffset, normalized.SizeBytes,
		formatTimestamp(normalized.CreatedAt))
	if err != nil {
		return terminalsession.LogRecord{}, fmt.Errorf("save terminal session log %s: %w", normalized.LogID, err)
	}
	return normalized, nil
}

func (s *Store) ListTerminalSessionLogs(ctx context.Context, terminalSessionID string) ([]terminalsession.LogRecord, error) {
	terminalSessionID = strings.TrimSpace(terminalSessionID)
	if terminalSessionID == "" {
		return nil, fmt.Errorf("terminal_session_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT log_id, terminal_session_id, stream, artifact_id, start_offset, size_bytes, created_at
		FROM terminal_session_logs
		WHERE terminal_session_id = ?
		ORDER BY stream ASC, created_at ASC, log_id ASC
	`, terminalSessionID)
	if err != nil {
		return nil, fmt.Errorf("list terminal session logs for session %s: %w", terminalSessionID, err)
	}
	defer rows.Close()

	var items []terminalsession.LogRecord
	for rows.Next() {
		record, err := scanTerminalSessionLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate terminal session logs for session %s: %w", terminalSessionID, err)
	}
	return items, nil
}

type terminalSessionScanner interface {
	Scan(dest ...any) error
}

func scanTerminalSession(scanner terminalSessionScanner) (terminalsession.SessionRecord, error) {
	var record terminalsession.SessionRecord
	var interactive int
	var pty int
	var status string
	var pid sql.NullInt64
	var processGroupID sql.NullInt64
	var exitCode sql.NullInt64
	var startedAt sql.NullString
	var endedAt sql.NullString
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
		&record.TerminalSessionID,
		&record.RunID,
		&record.SessionID,
		&record.Label,
		&record.CommandJSON,
		&record.Cwd,
		&interactive,
		&pty,
		&status,
		&pid,
		&processGroupID,
		&exitCode,
		&record.Signal,
		&record.StdoutArtifactID,
		&record.StderrArtifactID,
		&record.PTYArtifactID,
		&startedAt,
		&endedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return terminalsession.SessionRecord{}, err
	}
	record.Interactive = interactive != 0
	record.PTY = pty != 0
	record.Status = terminalsession.Status(status)
	record.ProcessID = intFromSQLite(pid)
	record.ProcessGroupID = intFromSQLite(processGroupID)
	record.ExitCode = intFromSQLite(exitCode)
	parsedStartedAt, err := parseOptionalFixedTimestamp(startedAt, "terminal_session.started_at")
	if err != nil {
		return terminalsession.SessionRecord{}, err
	}
	record.StartedAt = parsedStartedAt
	parsedEndedAt, err := parseOptionalFixedTimestamp(endedAt, "terminal_session.ended_at")
	if err != nil {
		return terminalsession.SessionRecord{}, err
	}
	record.EndedAt = parsedEndedAt
	parsedCreatedAt, err := parseTimestamp(fixedTimestampLayout, createdAt, "terminal_session.created_at")
	if err != nil {
		return terminalsession.SessionRecord{}, err
	}
	record.CreatedAt = parsedCreatedAt
	parsedUpdatedAt, err := parseTimestamp(fixedTimestampLayout, updatedAt, "terminal_session.updated_at")
	if err != nil {
		return terminalsession.SessionRecord{}, err
	}
	record.UpdatedAt = parsedUpdatedAt
	return terminalsession.NormalizeSessionRecord(record)
}

func scanTerminalSessionLog(scanner terminalSessionScanner) (terminalsession.LogRecord, error) {
	var record terminalsession.LogRecord
	var stream string
	var createdAt string
	if err := scanner.Scan(
		&record.LogID,
		&record.TerminalSessionID,
		&stream,
		&record.ArtifactID,
		&record.StartOffset,
		&record.SizeBytes,
		&createdAt,
	); err != nil {
		return terminalsession.LogRecord{}, err
	}
	record.Stream = terminalsession.LogStream(stream)
	parsedCreatedAt, err := parseTimestamp(fixedTimestampLayout, createdAt, "terminal_session_log.created_at")
	if err != nil {
		return terminalsession.LogRecord{}, err
	}
	record.CreatedAt = parsedCreatedAt
	return terminalsession.NormalizeLogRecord(record)
}

func boolToSQLite(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func intFromSQLite(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}

func nullableTimestamp(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTimestamp(*value)
}

func parseOptionalFixedTimestamp(value sql.NullString, field string) (*time.Time, error) {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil, nil
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, value.String, field)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
