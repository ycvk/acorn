package terminalsession

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeSessionRecordRunning(t *testing.T) {
	record, err := NormalizeSessionRecord(SessionRecord{
		TerminalSessionID: "term_1",
		RunID:             "run_1",
		SessionID:         "session_1",
		CommandJSON:       `["sh","-lc","make test"]`,
		Cwd:               "/workspace",
		Interactive:       true,
		PTY:               true,
		Status:            StatusRunning,
		ProcessID:         new(123),
		ProcessGroupID:    new(123),
		StartedAt:         new(time.Unix(1_710_000_000, 0).UTC()),
	})
	if err != nil {
		t.Fatalf("normalize session: %v", err)
	}
	if record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		t.Fatalf("timestamps should be populated: %#v", record)
	}
	if record.ProcessID == nil || *record.ProcessID != 123 {
		t.Fatalf("process id = %#v", record.ProcessID)
	}
}

func TestNormalizeSessionRecordRequiresExitFields(t *testing.T) {
	_, err := NormalizeSessionRecord(SessionRecord{
		TerminalSessionID: "term_1",
		RunID:             "run_1",
		CommandJSON:       `["sh","-lc","exit 0"]`,
		Cwd:               "/workspace",
		Status:            StatusExited,
	})
	if err == nil || !strings.Contains(err.Error(), "exit code") {
		t.Fatalf("error = %v, want exit code requirement", err)
	}
}

func TestNormalizeLogRecordValidation(t *testing.T) {
	_, err := NormalizeLogRecord(LogRecord{
		LogID:             "log_1",
		TerminalSessionID: "term_1",
		Stream:            "bad",
		ArtifactID:        "artifact_1",
	})
	if err == nil {
		t.Fatal("expected invalid stream error")
	}
}
