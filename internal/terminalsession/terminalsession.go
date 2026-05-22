package terminalsession

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusExited   Status = "exited"
	StatusSignaled Status = "signaled"
	StatusFailed   Status = "failed"
	StatusClosed   Status = "closed"
)

type LogStream string

const (
	LogStreamStdout LogStream = "stdout"
	LogStreamStderr LogStream = "stderr"
	LogStreamPTY    LogStream = "pty"
)

var (
	ErrSessionNotFound = errors.New("terminal session not found")
	ErrLogNotFound     = errors.New("terminal session log not found")
)

type SessionRecord struct {
	TerminalSessionID string
	RunID             string
	SessionID         string
	Label             string
	CommandJSON       string
	Cwd               string
	Interactive       bool
	PTY               bool
	Status            Status
	ProcessID         *int
	ProcessGroupID    *int
	ExitCode          *int
	Signal            string
	StdoutArtifactID  string
	StderrArtifactID  string
	PTYArtifactID     string
	StartedAt         *time.Time
	EndedAt           *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type LogRecord struct {
	LogID             string
	TerminalSessionID string
	Stream            LogStream
	ArtifactID        string
	StartOffset       int64
	SizeBytes         int64
	CreatedAt         time.Time
}

type Store interface {
	SaveTerminalSession(context.Context, SessionRecord) (SessionRecord, error)
	LoadTerminalSession(context.Context, string) (SessionRecord, error)
	ListTerminalSessionsByRun(context.Context, string) ([]SessionRecord, error)
	SaveTerminalSessionLog(context.Context, LogRecord) (LogRecord, error)
	ListTerminalSessionLogs(context.Context, string) ([]LogRecord, error)
}

func NormalizeSessionRecord(record SessionRecord) (SessionRecord, error) {
	record.TerminalSessionID = strings.TrimSpace(record.TerminalSessionID)
	record.RunID = strings.TrimSpace(record.RunID)
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.Label = strings.TrimSpace(record.Label)
	record.CommandJSON = strings.TrimSpace(record.CommandJSON)
	record.Cwd = strings.TrimSpace(record.Cwd)
	record.Status = Status(strings.TrimSpace(string(record.Status)))
	record.Signal = strings.TrimSpace(record.Signal)
	record.StdoutArtifactID = strings.TrimSpace(record.StdoutArtifactID)
	record.StderrArtifactID = strings.TrimSpace(record.StderrArtifactID)
	record.PTYArtifactID = strings.TrimSpace(record.PTYArtifactID)
	record.StartedAt = normalizeOptionalTime(record.StartedAt)
	record.EndedAt = normalizeOptionalTime(record.EndedAt)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	} else {
		record.CreatedAt = record.CreatedAt.UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	} else {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	if record.TerminalSessionID == "" {
		return SessionRecord{}, fmt.Errorf("terminal_session_id is required")
	}
	if strings.ContainsAny(record.TerminalSessionID, `/\`) {
		return SessionRecord{}, fmt.Errorf("terminal_session_id must be an opaque identifier")
	}
	if record.RunID == "" {
		return SessionRecord{}, fmt.Errorf("terminal session run_id is required")
	}
	if record.CommandJSON == "" {
		return SessionRecord{}, fmt.Errorf("terminal session command_json is required")
	}
	if record.Cwd == "" {
		return SessionRecord{}, fmt.Errorf("terminal session cwd is required")
	}
	if err := validateStatus(record.Status); err != nil {
		return SessionRecord{}, err
	}
	if record.ProcessID != nil && *record.ProcessID <= 0 {
		return SessionRecord{}, fmt.Errorf("terminal session process id must be > 0")
	}
	if record.ProcessGroupID != nil && *record.ProcessGroupID <= 0 {
		return SessionRecord{}, fmt.Errorf("terminal session process group id must be > 0")
	}
	if record.ExitCode != nil && *record.ExitCode < 0 {
		return SessionRecord{}, fmt.Errorf("terminal session exit code must be >= 0")
	}
	if err := validateExitFields(record); err != nil {
		return SessionRecord{}, err
	}
	return record, nil
}

func NormalizeLogRecord(record LogRecord) (LogRecord, error) {
	record.LogID = strings.TrimSpace(record.LogID)
	record.TerminalSessionID = strings.TrimSpace(record.TerminalSessionID)
	record.Stream = LogStream(strings.TrimSpace(string(record.Stream)))
	record.ArtifactID = strings.TrimSpace(record.ArtifactID)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	} else {
		record.CreatedAt = record.CreatedAt.UTC()
	}
	if record.LogID == "" {
		return LogRecord{}, fmt.Errorf("terminal session log_id is required")
	}
	if strings.ContainsAny(record.LogID, `/\`) {
		return LogRecord{}, fmt.Errorf("terminal session log_id must be an opaque identifier")
	}
	if record.TerminalSessionID == "" {
		return LogRecord{}, fmt.Errorf("terminal_session_id is required")
	}
	if record.ArtifactID == "" {
		return LogRecord{}, fmt.Errorf("terminal session log artifact_id is required")
	}
	if err := validateStream(record.Stream); err != nil {
		return LogRecord{}, err
	}
	if record.StartOffset < 0 {
		return LogRecord{}, fmt.Errorf("terminal session log start_offset must be >= 0")
	}
	if record.SizeBytes < 0 {
		return LogRecord{}, fmt.Errorf("terminal session log size_bytes must be >= 0")
	}
	return record, nil
}

func validateStatus(status Status) error {
	switch status {
	case StatusStarting, StatusRunning, StatusExited, StatusSignaled, StatusFailed, StatusClosed:
		return nil
	default:
		return fmt.Errorf("terminal session status %q is invalid", status)
	}
}

func validateStream(stream LogStream) error {
	switch stream {
	case LogStreamStdout, LogStreamStderr, LogStreamPTY:
		return nil
	default:
		return fmt.Errorf("terminal session log stream %q is invalid", stream)
	}
}

func validateExitFields(record SessionRecord) error {
	switch record.Status {
	case StatusExited:
		if record.ExitCode == nil {
			return fmt.Errorf("terminal session exited status requires exit code")
		}
		if record.EndedAt == nil {
			return fmt.Errorf("terminal session exited status requires ended_at")
		}
	case StatusSignaled:
		if record.Signal == "" {
			return fmt.Errorf("terminal session signaled status requires signal")
		}
		if record.EndedAt == nil {
			return fmt.Errorf("terminal session signaled status requires ended_at")
		}
	case StatusFailed, StatusClosed:
		if record.EndedAt == nil {
			return fmt.Errorf("terminal session %s status requires ended_at", record.Status)
		}
	case StatusStarting, StatusRunning:
		if record.EndedAt != nil || record.ExitCode != nil || record.Signal != "" {
			return fmt.Errorf("terminal session active status cannot include exit fields")
		}
	}
	return nil
}

func normalizeOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return new(value.UTC())
}
