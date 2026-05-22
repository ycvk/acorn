package terminalsession

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/ycvk/acorn/internal/artifacts"
	"github.com/ycvk/acorn/internal/processgroup"
)

type ArtifactService interface {
	Write(context.Context, artifacts.WriteRequest) (artifacts.Record, error)
	ReadRange(context.Context, artifacts.ReadRangeRequest) (artifacts.ReadRangeResult, error)
}

type Service struct {
	store     Store
	artifacts ArtifactService
	mu        sync.Mutex
	active    map[string]*activeSession
}

type StartRequest struct {
	RunID               string
	SessionID           string
	SourceToolResultRef string
	Label               string
	Command             []string
	Cwd                 string
	Env                 []string
	Interactive         bool
	PTY                 bool
	CreatedAt           time.Time
}

type WriteRequest struct {
	TerminalSessionID string
	Input             string
}

type WriteResult struct {
	Record       SessionRecord
	BytesWritten int
}

type ReadRequest struct {
	TerminalSessionID string
	Stream            LogStream
	Offset            int64
	Limit             int64
}

type ReadResult struct {
	Record     SessionRecord
	Log        *LogRecord
	Stream     LogStream
	ArtifactID string
	Offset     int64
	Bytes      int
	SizeBytes  int64
	EOF        bool
	Content    []byte
}

type SignalRequest struct {
	TerminalSessionID string
	Signal            string
}

func NewService(store Store, artifacts ArtifactService) (*Service, error) {
	if store == nil {
		return nil, errors.New("terminal session store is required")
	}
	if artifacts == nil {
		return nil, errors.New("terminal session artifact service is required")
	}
	return &Service{
		store:     store,
		artifacts: artifacts,
		active:    make(map[string]*activeSession),
	}, nil
}

func (s *Service) Start(ctx context.Context, req StartRequest) (SessionRecord, error) {
	if s == nil {
		return SessionRecord{}, errors.New("terminal session service is nil")
	}
	if err := ctx.Err(); err != nil {
		return SessionRecord{}, err
	}
	req = normalizeStartRequest(req)
	if req.RunID == "" {
		return SessionRecord{}, errors.New("terminal session run_id is required")
	}
	if len(req.Command) == 0 {
		return SessionRecord{}, errors.New("terminal session command is required")
	}
	if req.Command[0] == "" {
		return SessionRecord{}, errors.New("terminal session command name is required")
	}
	if req.Cwd == "" {
		return SessionRecord{}, errors.New("terminal session cwd is required")
	}
	terminalSessionID, err := generateTerminalSessionID()
	if err != nil {
		return SessionRecord{}, err
	}
	commandJSON, err := json.Marshal(req.Command)
	if err != nil {
		return SessionRecord{}, fmt.Errorf("marshal terminal session command: %w", err)
	}
	cmd := exec.Command(req.Command[0], req.Command[1:]...)
	processgroup.ConfigureCommand(cmd)
	cmd.Dir = req.Cwd
	cmd.Env = append([]string(nil), req.Env...)

	stdoutBuf := newStreamBuffer()
	stderrBuf := newStreamBuffer()
	ptyBuf := newStreamBuffer()
	active := &activeSession{
		id:                  terminalSessionID,
		sourceToolResultRef: req.SourceToolResultRef,
		cmd:                 cmd,
		stdout:              stdoutBuf,
		stderr:              stderrBuf,
		pty:                 ptyBuf,
		done:                make(chan struct{}),
	}
	startedAt := time.Now().UTC()
	if req.PTY {
		ptyFile, err := pty.Start(cmd)
		if err != nil {
			return SessionRecord{}, fmt.Errorf("start pty terminal session %v: %w", req.Command, err)
		}
		active.stdin = ptyFile
		active.ptyFile = ptyFile
		active.copyWG.Add(1)
		go copyStream(&active.copyWG, ptyBuf, ptyFile)
	} else {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return SessionRecord{}, fmt.Errorf("open terminal session stdin: %w", err)
		}
		cmd.Stdout = stdoutBuf
		cmd.Stderr = stderrBuf
		active.stdin = stdin
		if err := cmd.Start(); err != nil {
			return SessionRecord{}, fmt.Errorf("start terminal session %v: %w", req.Command, err)
		}
	}
	pid := cmd.Process.Pid
	record := SessionRecord{
		TerminalSessionID: terminalSessionID,
		RunID:             req.RunID,
		SessionID:         req.SessionID,
		Label:             req.Label,
		CommandJSON:       string(commandJSON),
		Cwd:               req.Cwd,
		Interactive:       req.Interactive,
		PTY:               req.PTY,
		Status:            StatusRunning,
		ProcessID:         &pid,
		ProcessGroupID:    &pid,
		StartedAt:         &startedAt,
		CreatedAt:         firstTime(req.CreatedAt, startedAt),
		UpdatedAt:         startedAt,
	}
	record, err = s.store.SaveTerminalSession(ctx, record)
	if err != nil {
		return SessionRecord{}, errors.Join(err, cleanupStartedCommand(cmd, active))
	}
	active.setRecord(record)
	s.mu.Lock()
	s.active[terminalSessionID] = active
	s.mu.Unlock()
	go s.finalize(active)
	return record, nil
}

func (s *Service) Write(ctx context.Context, req WriteRequest) (WriteResult, error) {
	if s == nil {
		return WriteResult{}, errors.New("terminal session service is nil")
	}
	if err := ctx.Err(); err != nil {
		return WriteResult{}, err
	}
	active, err := s.activeByID(req.TerminalSessionID)
	if err != nil {
		return WriteResult{}, err
	}
	active.mu.Lock()
	stdin := active.stdin
	record := active.record
	active.mu.Unlock()
	if stdin == nil {
		return WriteResult{}, fmt.Errorf("terminal session %s stdin is closed", req.TerminalSessionID)
	}
	n, err := io.WriteString(stdin, req.Input)
	if err != nil {
		return WriteResult{}, fmt.Errorf("write terminal session %s: %w", req.TerminalSessionID, err)
	}
	return WriteResult{Record: record, BytesWritten: n}, nil
}

func (s *Service) Read(ctx context.Context, req ReadRequest) (ReadResult, error) {
	if s == nil {
		return ReadResult{}, errors.New("terminal session service is nil")
	}
	if err := ctx.Err(); err != nil {
		return ReadResult{}, err
	}
	req.TerminalSessionID = strings.TrimSpace(req.TerminalSessionID)
	if req.TerminalSessionID == "" {
		return ReadResult{}, errors.New("terminal_session_id is required")
	}
	if req.Offset < 0 {
		return ReadResult{}, errors.New("terminal session read offset must be >= 0")
	}
	if req.Limit <= 0 {
		return ReadResult{}, errors.New("terminal session read limit must be > 0")
	}
	if err := validateStream(req.Stream); err != nil {
		return ReadResult{}, err
	}
	if active := s.activeIfPresent(req.TerminalSessionID); active != nil {
		return active.read(req)
	}
	record, err := s.store.LoadTerminalSession(ctx, req.TerminalSessionID)
	if err != nil {
		return ReadResult{}, err
	}
	logs, err := s.store.ListTerminalSessionLogs(ctx, req.TerminalSessionID)
	if err != nil {
		return ReadResult{}, err
	}
	for _, logRecord := range logs {
		if logRecord.Stream != req.Stream {
			continue
		}
		if logRecord.SizeBytes == 0 {
			return ReadResult{Record: record, Log: &logRecord, Stream: req.Stream, ArtifactID: logRecord.ArtifactID, Offset: req.Offset, EOF: true}, nil
		}
		rangeResult, err := s.artifacts.ReadRange(ctx, artifacts.ReadRangeRequest{
			ArtifactID: logRecord.ArtifactID,
			Offset:     req.Offset,
			Limit:      req.Limit,
		})
		if err != nil {
			return ReadResult{}, err
		}
		return ReadResult{
			Record:     record,
			Log:        &logRecord,
			Stream:     req.Stream,
			ArtifactID: logRecord.ArtifactID,
			Offset:     rangeResult.Offset,
			Bytes:      len(rangeResult.Content),
			SizeBytes:  rangeResult.Record.SizeBytes,
			EOF:        rangeResult.EOF,
			Content:    rangeResult.Content,
		}, nil
	}
	if req.Offset > 0 {
		return ReadResult{}, fmt.Errorf("terminal session log %s/%s has size 0; offset %d is invalid", req.TerminalSessionID, req.Stream, req.Offset)
	}
	return ReadResult{Record: record, Stream: req.Stream, Offset: req.Offset, EOF: true}, nil
}

func (s *Service) Signal(ctx context.Context, req SignalRequest) (SessionRecord, error) {
	if s == nil {
		return SessionRecord{}, errors.New("terminal session service is nil")
	}
	if err := ctx.Err(); err != nil {
		return SessionRecord{}, err
	}
	active, err := s.activeByID(req.TerminalSessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	signal, err := processgroup.ParseSignal(strings.ToUpper(strings.TrimSpace(req.Signal)))
	if err != nil {
		return SessionRecord{}, err
	}
	if err := processgroup.SignalCommandGroup(active.cmd, signal); err != nil {
		return SessionRecord{}, err
	}
	active.mu.Lock()
	record := active.record
	active.mu.Unlock()
	return record, nil
}

func (s *Service) Close(ctx context.Context, terminalSessionID string, force bool) (SessionRecord, error) {
	signal := "TERM"
	if force {
		signal = "KILL"
	}
	return s.Signal(ctx, SignalRequest{TerminalSessionID: terminalSessionID, Signal: signal})
}

func (s *Service) Load(ctx context.Context, terminalSessionID string) (SessionRecord, error) {
	if active := s.activeIfPresent(terminalSessionID); active != nil {
		active.mu.Lock()
		defer active.mu.Unlock()
		return active.record, nil
	}
	return s.store.LoadTerminalSession(ctx, terminalSessionID)
}

func (s *Service) ListByRun(ctx context.Context, runID string) ([]SessionRecord, error) {
	return s.store.ListTerminalSessionsByRun(ctx, runID)
}

func (s *Service) Logs(ctx context.Context, terminalSessionID string) ([]LogRecord, error) {
	return s.store.ListTerminalSessionLogs(ctx, terminalSessionID)
}

func (s *Service) activeByID(id string) (*activeSession, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("terminal_session_id is required")
	}
	active := s.activeIfPresent(id)
	if active == nil {
		return nil, fmt.Errorf("%w or no longer active: %s", ErrSessionNotFound, id)
	}
	return active, nil
}

func (s *Service) activeIfPresent(id string) *activeSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[strings.TrimSpace(id)]
}

func (s *Service) finalize(active *activeSession) {
	waitErr := active.cmd.Wait()
	if active.ptyFile != nil {
		_ = active.ptyFile.Close()
	}
	active.copyWG.Wait()

	record := active.currentRecord()
	record.Status, record.ExitCode, record.Signal = statusFromWaitErr(waitErr)
	endedAt := time.Now().UTC()
	record.EndedAt = &endedAt
	record.UpdatedAt = endedAt
	if err := s.persistFinalLogs(context.Background(), active, &record); err != nil {
		record.Status = StatusFailed
		record.ExitCode = nil
		record.Signal = ""
	}
	saved, err := s.store.SaveTerminalSession(context.Background(), record)
	if err == nil {
		active.setRecord(saved)
	}
	s.mu.Lock()
	delete(s.active, active.id)
	s.mu.Unlock()
	close(active.done)
}

func (s *Service) persistFinalLogs(ctx context.Context, active *activeSession, record *SessionRecord) error {
	if record == nil {
		return errors.New("terminal session record is nil")
	}
	if record.PTY {
		return s.persistLog(ctx, active, record, LogStreamPTY, active.pty.snapshot())
	}
	var errs []error
	if err := s.persistLog(ctx, active, record, LogStreamStdout, active.stdout.snapshot()); err != nil {
		errs = append(errs, err)
	}
	if err := s.persistLog(ctx, active, record, LogStreamStderr, active.stderr.snapshot()); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *Service) persistLog(ctx context.Context, active *activeSession, record *SessionRecord, stream LogStream, content []byte) error {
	if len(content) == 0 {
		return nil
	}
	artifact, err := s.artifacts.Write(ctx, artifacts.WriteRequest{
		RunID:               record.RunID,
		SessionID:           record.SessionID,
		SourceToolResultRef: active.sourceToolResultRef,
		Kind:                artifacts.KindLog,
		Title:               fmt.Sprintf("%s %s", record.TerminalSessionID, stream),
		MIMEType:            "text/plain",
		Content:             content,
	})
	if err != nil {
		return err
	}
	switch stream {
	case LogStreamStdout:
		record.StdoutArtifactID = artifact.ArtifactID
	case LogStreamStderr:
		record.StderrArtifactID = artifact.ArtifactID
	case LogStreamPTY:
		record.PTYArtifactID = artifact.ArtifactID
	}
	_, err = s.store.SaveTerminalSessionLog(ctx, LogRecord{
		LogID:             record.TerminalSessionID + "_" + string(stream),
		TerminalSessionID: record.TerminalSessionID,
		Stream:            stream,
		ArtifactID:        artifact.ArtifactID,
		StartOffset:       0,
		SizeBytes:         artifact.SizeBytes,
		CreatedAt:         artifact.CreatedAt,
	})
	return err
}

type activeSession struct {
	id                  string
	sourceToolResultRef string
	cmd                 *exec.Cmd
	stdin               io.Writer
	ptyFile             *os.File
	stdout              *streamBuffer
	stderr              *streamBuffer
	pty                 *streamBuffer
	copyWG              sync.WaitGroup
	done                chan struct{}
	mu                  sync.Mutex
	record              SessionRecord
}

func (s *activeSession) setRecord(record SessionRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record = record
}

func (s *activeSession) currentRecord() SessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record
}

func (s *activeSession) read(req ReadRequest) (ReadResult, error) {
	s.mu.Lock()
	record := s.record
	s.mu.Unlock()
	var buffer *streamBuffer
	switch req.Stream {
	case LogStreamStdout:
		buffer = s.stdout
	case LogStreamStderr:
		buffer = s.stderr
	case LogStreamPTY:
		buffer = s.pty
	default:
		return ReadResult{}, fmt.Errorf("terminal session log stream %q is invalid", req.Stream)
	}
	content, eof, size, err := buffer.readRange(req.Offset, req.Limit)
	if err != nil {
		return ReadResult{}, err
	}
	return ReadResult{
		Record:    record,
		Stream:    req.Stream,
		Offset:    req.Offset,
		Bytes:     len(content),
		SizeBytes: size,
		EOF:       eof,
		Content:   content,
	}, nil
}

type streamBuffer struct {
	mu  sync.Mutex
	buf []byte
	err error
}

func newStreamBuffer() *streamBuffer {
	return &streamBuffer{}
}

func (b *streamBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *streamBuffer) snapshot() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf...)
}

func (b *streamBuffer) readRange(offset int64, limit int64) ([]byte, bool, int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return nil, false, 0, b.err
	}
	size := int64(len(b.buf))
	if offset > size {
		return nil, false, size, fmt.Errorf("terminal session read offset %d exceeds size %d", offset, size)
	}
	end := offset + limit
	if end > size {
		end = size
	}
	content := append([]byte(nil), b.buf[offset:end]...)
	return content, end >= size, size, nil
}

func (b *streamBuffer) setErr(err error) {
	if err == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err == nil {
		b.err = err
	}
}

func copyStream(wg *sync.WaitGroup, dst *streamBuffer, src io.Reader) {
	defer wg.Done()
	if _, err := io.Copy(dst, src); err != nil && !errors.Is(err, os.ErrClosed) {
		dst.setErr(err)
	}
}

func cleanupStartedCommand(cmd *exec.Cmd, active *activeSession) error {
	killErr := processgroup.KillCommandGroup(cmd)
	waitErr := cmd.Wait()
	if active != nil && active.ptyFile != nil {
		closeErr := active.ptyFile.Close()
		active.copyWG.Wait()
		return errors.Join(killErr, waitErr, closeErr)
	}
	if active != nil {
		active.copyWG.Wait()
	}
	return errors.Join(killErr, waitErr)
}

func normalizeStartRequest(req StartRequest) StartRequest {
	req.RunID = strings.TrimSpace(req.RunID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.SourceToolResultRef = strings.TrimSpace(req.SourceToolResultRef)
	req.Label = strings.TrimSpace(req.Label)
	req.Cwd = strings.TrimSpace(req.Cwd)
	for i := range req.Command {
		req.Command[i] = strings.TrimSpace(req.Command[i])
	}
	return req
}

func statusFromWaitErr(waitErr error) (Status, *int, string) {
	if waitErr == nil {
		return StatusExited, new(0), ""
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](waitErr); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return StatusSignaled, nil, status.Signal().String()
		}
		code := exitErr.ExitCode()
		if code >= 0 {
			return StatusExited, &code, ""
		}
	}
	return StatusFailed, nil, ""
}

func firstTime(value time.Time, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value.UTC()
}

func generateTerminalSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate terminal session id: %w", err)
	}
	return "term_" + hex.EncodeToString(raw[:]), nil
}
