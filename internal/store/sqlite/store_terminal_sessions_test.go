package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/terminalsession"
)

func TestStoreTerminalSessionsSaveLoadListAndLogs(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	startedAt := time.Unix(1_710_000_000, 0).UTC()
	pid := 234
	pgid := 234
	record, err := store.SaveTerminalSession(context.Background(), terminalsession.SessionRecord{
		TerminalSessionID: "term_1",
		RunID:             "run_1",
		SessionID:         "session_1",
		Label:             "make test",
		CommandJSON:       `["make","test"]`,
		Cwd:               "/workspace",
		Interactive:       true,
		PTY:               true,
		Status:            terminalsession.StatusRunning,
		ProcessID:         &pid,
		ProcessGroupID:    &pgid,
		StdoutArtifactID:  "artifact_stdout",
		StartedAt:         &startedAt,
		CreatedAt:         startedAt,
		UpdatedAt:         startedAt,
	})
	if err != nil {
		t.Fatalf("save terminal session: %v", err)
	}
	if record.ProcessID == nil || *record.ProcessID != pid {
		t.Fatalf("process id = %#v", record.ProcessID)
	}

	loaded, err := store.LoadTerminalSession(context.Background(), "term_1")
	if err != nil {
		t.Fatalf("load terminal session: %v", err)
	}
	if loaded.Status != terminalsession.StatusRunning || !loaded.Interactive || !loaded.PTY {
		t.Fatalf("loaded terminal session = %#v", loaded)
	}

	endedAt := startedAt.Add(time.Minute)
	updated, err := store.SaveTerminalSession(context.Background(), terminalsession.SessionRecord{
		TerminalSessionID: "term_1",
		RunID:             "run_1",
		SessionID:         "session_1",
		Label:             "make test",
		CommandJSON:       `["make","test"]`,
		Cwd:               "/workspace",
		Interactive:       true,
		PTY:               true,
		Status:            terminalsession.StatusExited,
		ProcessID:         &pid,
		ProcessGroupID:    &pgid,
		ExitCode:          new(0),
		StdoutArtifactID:  "artifact_stdout",
		StartedAt:         &startedAt,
		EndedAt:           &endedAt,
		CreatedAt:         startedAt,
		UpdatedAt:         endedAt,
	})
	if err != nil {
		t.Fatalf("update terminal session: %v", err)
	}
	if updated.Status != terminalsession.StatusExited || updated.ExitCode == nil || *updated.ExitCode != 0 {
		t.Fatalf("updated terminal session = %#v", updated)
	}

	byRun, err := store.ListTerminalSessionsByRun(context.Background(), "run_1")
	if err != nil {
		t.Fatalf("list terminal sessions by run: %v", err)
	}
	if len(byRun) != 1 || byRun[0].TerminalSessionID != "term_1" {
		t.Fatalf("by run = %#v", byRun)
	}

	logAt := startedAt.Add(2 * time.Second)
	logRecord, err := store.SaveTerminalSessionLog(context.Background(), terminalsession.LogRecord{
		LogID:             "log_1",
		TerminalSessionID: "term_1",
		Stream:            terminalsession.LogStreamStdout,
		ArtifactID:        "artifact_stdout",
		StartOffset:       0,
		SizeBytes:         128,
		CreatedAt:         logAt,
	})
	if err != nil {
		t.Fatalf("save terminal session log: %v", err)
	}
	if !logRecord.CreatedAt.Equal(logAt) {
		t.Fatalf("log created_at = %s, want %s", logRecord.CreatedAt, logAt)
	}
	logs, err := store.ListTerminalSessionLogs(context.Background(), "term_1")
	if err != nil {
		t.Fatalf("list terminal session logs: %v", err)
	}
	if len(logs) != 1 || logs[0].ArtifactID != "artifact_stdout" {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestStoreTerminalSessionLoadMissing(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.LoadTerminalSession(context.Background(), "missing")
	if !errors.Is(err, terminalsession.ErrSessionNotFound) {
		t.Fatalf("load missing err = %v, want storecore.ErrSessionNotFound", err)
	}
}
