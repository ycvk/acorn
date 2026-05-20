package terminalsession

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/artifacts"
)

func TestServiceStartFinalizesLogsAsArtifacts(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required")
	}
	ctx := context.Background()
	store := newServiceMemoryStore()
	artifactStore := newServiceArtifactStore()
	artifactService, err := artifacts.NewService(t.TempDir(), artifactStore)
	if err != nil {
		t.Fatalf("artifacts.NewService: %v", err)
	}
	service, err := NewService(store, artifactService)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	record, err := service.Start(ctx, StartRequest{
		RunID:               "run_1",
		SessionID:           "session_1",
		SourceToolResultRef: "tool_result:run_1:call_start",
		Label:               "fixture",
		Command:             []string{"sh", "-c", "printf out; printf err 1>&2"},
		Cwd:                 t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	final := waitTerminalStatus(t, service, record.TerminalSessionID)
	if final.Status != StatusExited || final.ExitCode == nil || *final.ExitCode != 0 {
		t.Fatalf("final record = %+v", final)
	}
	stdout, err := service.Read(ctx, ReadRequest{TerminalSessionID: record.TerminalSessionID, Stream: LogStreamStdout, Offset: 0, Limit: 32})
	if err != nil {
		t.Fatalf("Read stdout: %v", err)
	}
	if string(stdout.Content) != "out" || stdout.ArtifactID == "" {
		t.Fatalf("stdout = content:%q artifact:%q", stdout.Content, stdout.ArtifactID)
	}
	stderr, err := service.Read(ctx, ReadRequest{TerminalSessionID: record.TerminalSessionID, Stream: LogStreamStderr, Offset: 0, Limit: 32})
	if err != nil {
		t.Fatalf("Read stderr: %v", err)
	}
	if string(stderr.Content) != "err" || stderr.ArtifactID == "" {
		t.Fatalf("stderr = content:%q artifact:%q", stderr.Content, stderr.ArtifactID)
	}
}

func TestServiceWritesInteractiveInput(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required")
	}
	ctx := context.Background()
	store := newServiceMemoryStore()
	artifactStore := newServiceArtifactStore()
	artifactService, err := artifacts.NewService(t.TempDir(), artifactStore)
	if err != nil {
		t.Fatalf("artifacts.NewService: %v", err)
	}
	service, err := NewService(store, artifactService)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	record, err := service.Start(ctx, StartRequest{
		RunID:       "run_1",
		SessionID:   "session_1",
		Label:       "interactive",
		Command:     []string{"sh", "-c", "read line; printf \"got:%s\\n\" \"$line\""},
		Cwd:         t.TempDir(),
		Interactive: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := service.Write(ctx, WriteRequest{TerminalSessionID: record.TerminalSessionID, Input: "hello\n"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	final := waitTerminalStatus(t, service, record.TerminalSessionID)
	if final.Status != StatusExited {
		t.Fatalf("final status = %s", final.Status)
	}
	stdout, err := service.Read(ctx, ReadRequest{TerminalSessionID: record.TerminalSessionID, Stream: LogStreamStdout, Offset: 0, Limit: 64})
	if err != nil {
		t.Fatalf("Read stdout: %v", err)
	}
	if strings.TrimSpace(string(stdout.Content)) != "got:hello" {
		t.Fatalf("stdout = %q", stdout.Content)
	}
}

func TestServiceCapturesPTYLog(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required")
	}
	ctx := context.Background()
	store := newServiceMemoryStore()
	artifactStore := newServiceArtifactStore()
	artifactService, err := artifacts.NewService(t.TempDir(), artifactStore)
	if err != nil {
		t.Fatalf("artifacts.NewService: %v", err)
	}
	service, err := NewService(store, artifactService)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	record, err := service.Start(ctx, StartRequest{
		RunID:     "run_1",
		SessionID: "session_1",
		Label:     "pty",
		Command:   []string{"sh", "-c", "printf pty"},
		Cwd:       t.TempDir(),
		PTY:       true,
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty is not permitted in this environment: %v", err)
		}
		t.Fatalf("Start: %v", err)
	}
	final := waitTerminalStatus(t, service, record.TerminalSessionID)
	if final.Status != StatusExited {
		t.Fatalf("final status = %s", final.Status)
	}
	ptyLog, err := service.Read(ctx, ReadRequest{TerminalSessionID: record.TerminalSessionID, Stream: LogStreamPTY, Offset: 0, Limit: 32})
	if err != nil {
		t.Fatalf("Read pty: %v", err)
	}
	if string(ptyLog.Content) != "pty" || ptyLog.ArtifactID == "" {
		t.Fatalf("pty log = content:%q artifact:%q", ptyLog.Content, ptyLog.ArtifactID)
	}
}

func waitTerminalStatus(t *testing.T, service *Service, terminalSessionID string) SessionRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		record, err := service.Load(context.Background(), terminalSessionID)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		switch record.Status {
		case StatusExited, StatusSignaled, StatusFailed, StatusClosed:
			return record
		}
		if time.Now().After(deadline) {
			t.Fatalf("terminal session %s still %s", terminalSessionID, record.Status)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

type serviceMemoryStore struct {
	mu      sync.Mutex
	records map[string]SessionRecord
	logs    map[string][]LogRecord
}

func newServiceMemoryStore() *serviceMemoryStore {
	return &serviceMemoryStore{
		records: make(map[string]SessionRecord),
		logs:    make(map[string][]LogRecord),
	}
}

func (s *serviceMemoryStore) SaveTerminalSession(_ context.Context, record SessionRecord) (SessionRecord, error) {
	normalized, err := NormalizeSessionRecord(record)
	if err != nil {
		return SessionRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[normalized.TerminalSessionID] = normalized
	return normalized, nil
}

func (s *serviceMemoryStore) LoadTerminalSession(_ context.Context, terminalSessionID string) (SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[strings.TrimSpace(terminalSessionID)]
	if !ok {
		return SessionRecord{}, ErrSessionNotFound
	}
	return record, nil
}

func (s *serviceMemoryStore) ListTerminalSessionsByRun(_ context.Context, runID string) ([]SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []SessionRecord
	for _, record := range s.records {
		if record.RunID == strings.TrimSpace(runID) {
			items = append(items, record)
		}
	}
	return items, nil
}

func (s *serviceMemoryStore) SaveTerminalSessionLog(_ context.Context, record LogRecord) (LogRecord, error) {
	normalized, err := NormalizeLogRecord(record)
	if err != nil {
		return LogRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.logs[normalized.TerminalSessionID]
	for i := range items {
		if items[i].LogID == normalized.LogID {
			items[i] = normalized
			s.logs[normalized.TerminalSessionID] = items
			return normalized, nil
		}
	}
	s.logs[normalized.TerminalSessionID] = append(items, normalized)
	return normalized, nil
}

func (s *serviceMemoryStore) ListTerminalSessionLogs(_ context.Context, terminalSessionID string) ([]LogRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]LogRecord(nil), s.logs[strings.TrimSpace(terminalSessionID)]...), nil
}

type serviceArtifactStore struct {
	mu      sync.Mutex
	records map[string]artifacts.Record
}

func newServiceArtifactStore() *serviceArtifactStore {
	return &serviceArtifactStore{records: make(map[string]artifacts.Record)}
}

func (s *serviceArtifactStore) SaveArtifact(_ context.Context, record artifacts.Record) (artifacts.Record, error) {
	normalized, err := artifacts.NormalizeRecord(record)
	if err != nil {
		return artifacts.Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[normalized.ArtifactID] = normalized
	return normalized, nil
}

func (s *serviceArtifactStore) LoadArtifact(_ context.Context, artifactID string) (artifacts.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[strings.TrimSpace(artifactID)]
	if !ok {
		return artifacts.Record{}, artifacts.ErrArtifactNotFound
	}
	return record, nil
}

func (s *serviceArtifactStore) ListArtifactsByRun(_ context.Context, runID string) ([]artifacts.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []artifacts.Record
	for _, record := range s.records {
		if record.RunID == strings.TrimSpace(runID) {
			items = append(items, record)
		}
	}
	return items, nil
}

func (s *serviceArtifactStore) ListArtifactsBySession(_ context.Context, sessionID string) ([]artifacts.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []artifacts.Record
	for _, record := range s.records {
		if record.SessionID == strings.TrimSpace(sessionID) {
			items = append(items, record)
		}
	}
	return items, nil
}
