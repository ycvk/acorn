package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/runtime"
)

var (
	ErrClientProjectionFailed = errors.New("client projection failed")
	ErrClientNoPendingMessage = errors.New("client pending user message not found")
)

// projectionError wraps a format string into ErrClientProjectionFailed.
func projectionError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrClientProjectionFailed, fmt.Sprintf(format, args...))
}

func projectRunStatus(status core.RunStatus) (string, error) {
	switch status {
	case core.RunStatusRunning:
		return "running", nil
	case core.RunStatusSucceeded:
		return "completed", nil
	case core.RunStatusInterrupted:
		return "interrupted", nil
	case core.RunStatusFailed:
		return "failed", nil
	default:
		return "", projectionError("unknown run status %q", status)
	}
}

// Run is a user-facing run DTO.
type Run struct {
	ID          string
	ThreadID    string
	Status      string
	Mode        string
	CreatedAt   time.Time
	CompletedAt time.Time
}

// RunService owns run lifecycle: creating runs against a thread, probing run
// terminality, and interrupting in-flight runs. It binds a run to its
// originating user message via the ThreadService and drives the executor.
type RunService struct {
	store       StoreView
	threads     *ThreadService
	newExecutor func(context.Context) (ExecutorHandle, error)
	controller  *runtime.RunController
	newRunID    func() string
	reportError func(context.Context, string, error)
}

// NewRunService constructs a RunService backed by the given store, executor
// factory, and run controller.
func NewRunService(store StoreView, threads *ThreadService, newExecutor func(context.Context) (ExecutorHandle, error), controller *runtime.RunController) *RunService {
	return &RunService{
		store:       store,
		threads:     threads,
		newExecutor: newExecutor,
		controller:  controller,
		newRunID:    newRunID,
		reportError: reportClientBackgroundError,
	}
}

func newRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
}

func (s *RunService) GetRun(ctx context.Context, runID string) (*Run, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	record, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	run, err := projectRun(*record)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *RunService) RunIsTerminal(ctx context.Context, runID string) (bool, error) {
	if s == nil || s.store == nil {
		return false, errors.New("client store is nil")
	}
	record, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return false, err
	}
	switch record.Status {
	case core.RunStatusRunning:
		return false, nil
	case core.RunStatusSucceeded, core.RunStatusInterrupted, core.RunStatusFailed:
		return true, nil
	default:
		return false, projectionError("unknown run status %q", record.Status)
	}
}

func (s *RunService) InterruptRun(ctx context.Context, runID string) error {
	_ = ctx
	if s == nil || s.controller == nil {
		return errors.New("run controller is nil")
	}
	return s.controller.Interrupt(runID)
}

func (s *RunService) CreateRun(ctx context.Context, threadID, skillID, input string) (*Run, error) {
	if s == nil || s.store == nil || s.newExecutor == nil || s.newRunID == nil || s.threads == nil {
		return nil, errors.New("client service is not initialized")
	}
	threadID = strings.TrimSpace(threadID)
	skillID = strings.TrimSpace(skillID)
	if _, err := s.store.LoadSession(ctx, threadID); err != nil {
		return nil, err
	}
	// Resolve the user message this run binds to, then bind by its exact id (see
	// req.BoundMessageID below). Single-step (input set) records a fresh message
	// and binds that id — race-free, no separate POST /messages needed. The
	// two-step flow (empty input, message posted via POST /messages) reads the
	// latest unbound message and binds by its id; concurrent two-step creates on
	// one thread bind the same id and the second fails loud (RowsAffected=0 -> run
	// rolled back), never silently mis-binding.
	var message *core.SessionMessageRecord
	var err error
	if strings.TrimSpace(input) != "" {
		message, err = s.threads.createUserMessage(ctx, threadID, input)
		if err != nil {
			return nil, err
		}
	} else {
		message, err = s.store.LoadLatestUnboundUserMessage(ctx, threadID)
		if err != nil {
			if errors.Is(err, core.ErrSessionMessageNotFound) {
				return nil, fmt.Errorf("%w: thread %s", ErrClientNoPendingMessage, threadID)
			}
			return nil, err
		}
	}
	history, err := s.store.ListSessionMessages(ctx, threadID, chatHistoryLimit)
	if err != nil {
		return nil, err
	}
	exec, err := s.newExecutor(ctx)
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(s.newRunID())
	if runID == "" {
		return nil, errors.New("client run id is empty")
	}
	started := newRunStartSignal()
	req := core.ExecuteRequest{
		RunID:          runID,
		SessionID:      threadID,
		TurnIndex:      message.TurnIndex,
		Input:          message.Content,
		BoundMessageID: message.ID,
		SkillID:        skillID,
		Messages:       buildChatMessages(history),
	}
	runCtx := context.WithoutCancel(ctx)
	go s.executeRun(runCtx, exec, req, started)

	select {
	case <-started.Started():
		return s.GetRun(ctx, runID)
	case err := <-started.Failed():
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func projectRun(record core.RunRecord) (Run, error) {
	status, err := projectRunStatus(record.Status)
	if err != nil {
		return Run{}, err
	}
	run := Run{
		ID:        record.RunID,
		ThreadID:  record.SessionID,
		Status:    status,
		Mode:      "direct",
		CreatedAt: record.CreatedAt,
	}
	if record.Status != core.RunStatusRunning {
		run.CompletedAt = record.FinishedAt
	}
	return run, nil
}

type clientRunStartSignal struct {
	started     chan struct{}
	failed      chan error
	closeOnce   sync.Once
	failureOnce sync.Once
	hasStarted  atomic.Bool
}

func newRunStartSignal() *clientRunStartSignal {
	return &clientRunStartSignal{
		started: make(chan struct{}),
		failed:  make(chan error, 1),
	}
}

func (s *clientRunStartSignal) Started() <-chan struct{} {
	return s.started
}

func (s *clientRunStartSignal) Failed() <-chan error {
	return s.failed
}

func (s *clientRunStartSignal) RunStarted() {
	s.hasStarted.Store(true)
	s.closeOnce.Do(func() { close(s.started) })
}

func (s *clientRunStartSignal) MarkFailed(err error) bool {
	if err == nil || s.hasStarted.Load() {
		return false
	}
	s.failureOnce.Do(func() { s.failed <- err })
	return true
}

func reportClientBackgroundError(ctx context.Context, runID string, err error) {
	slog.Default().ErrorContext(ctx, "client background run failure was not persisted", "run_id", runID, "error", err)
}

func (s *RunService) executeRun(ctx context.Context, exec ExecutorHandle, req core.ExecuteRequest, started *clientRunStartSignal) {
	err := exec.ExecuteMessages(ctx, req, started)
	if err != nil {
		if started.MarkFailed(err) {
			return
		}
		if persistErr := s.recordStartedRunFailure(ctx, req.RunID, err); persistErr != nil {
			s.reportBackgroundRunFailure(ctx, req.RunID, err, persistErr)
		}
		return
	}
}

func (s *RunService) reportBackgroundRunFailure(ctx context.Context, runID string, cause, persistErr error) {
	err := errors.Join(
		fmt.Errorf("client executor failed after run start: %w", cause),
		fmt.Errorf("record started client run failure: %w", persistErr),
	)
	report := reportClientBackgroundError
	if s != nil && s.reportError != nil {
		report = s.reportError
	}
	report(ctx, runID, err)
}

func (s *RunService) recordStartedRunFailure(ctx context.Context, runID string, cause error) error {
	if s == nil || s.store == nil {
		return errors.New("client store is nil")
	}
	if strings.TrimSpace(runID) == "" || cause == nil {
		return nil
	}
	record, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return err
	}
	if record.Status != core.RunStatusRunning {
		return nil
	}
	if err := s.store.FinishRun(ctx, runID, core.RunStatusFailed, "", cause.Error()); err != nil {
		return fmt.Errorf("mark client run failed after background error: %w", err)
	}
	if _, err := s.store.AppendEvent(ctx, runID, "run.failed", map[string]any{"error": cause.Error()}); err != nil {
		return fmt.Errorf("append client run failed event after background error: %w", err)
	}
	return nil
}

const chatHistoryLimit = 12

func buildChatMessages(items []core.SessionMessageRecord) []adk.Message {
	messages := make([]adk.Message, 0, len(items))
	for _, item := range items {
		switch item.Role {
		case "user":
			messages = append(messages, schema.UserMessage(item.Content))
		case "assistant":
			messages = append(messages, schema.AssistantMessage(item.Content, nil))
		}
	}
	return messages
}
