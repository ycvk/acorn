package app

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
	"github.com/ycvk/acorn/internal/clientevents"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
)

type ClientService struct {
	store         containerAppStore
	newExecutor   func(context.Context) (executorHandle, error)
	controller    *runtime.RunController
	workspaceRoot string
	eventPoll     time.Duration
	newThreadID   func() string
	newRunID      func() string
	reportError   func(context.Context, string, error)
}

func BuildClientService(store containerAppStore, newExecutor func(context.Context) (executorHandle, error), controller *runtime.RunController, workspaceRoot string) *ClientService {
	return &ClientService{
		store:         store,
		newExecutor:   newExecutor,
		controller:    controller,
		workspaceRoot: workspaceRoot,
		eventPoll:     100 * time.Millisecond,
		newThreadID:   newThreadID,
		newRunID:      newRunID,
		reportError:   reportClientBackgroundError,
	}
}

func newThreadID() string {
	return fmt.Sprintf("session_%d", time.Now().UTC().UnixNano())
}

func newRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
}

func (s *ClientService) GetRun(ctx context.Context, runID string) (*Run, error) {
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

func (s *ClientService) RunIsTerminal(ctx context.Context, runID string) (bool, error) {
	if s == nil || s.store == nil {
		return false, errors.New("client store is nil")
	}
	record, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return false, err
	}
	switch record.Status {
	case domain.RunStatusRunning:
		return false, nil
	case domain.RunStatusSucceeded, domain.RunStatusInterrupted, domain.RunStatusFailed:
		return true, nil
	default:
		return false, projectionError("unknown run status %q", record.Status)
	}
}

func (s *ClientService) InterruptRun(ctx context.Context, runID string) error {
	_ = ctx
	if s == nil || s.controller == nil {
		return errors.New("run controller is nil")
	}
	return s.controller.Interrupt(runID)
}

func (s *ClientService) CreateRun(ctx context.Context, threadID, skillID, input string) (*Run, error) {
	if s == nil || s.store == nil || s.newExecutor == nil || s.newRunID == nil {
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
	var message *domain.SessionMessageRecord
	var err error
	if strings.TrimSpace(input) != "" {
		message, err = s.createUserMessage(ctx, threadID, input)
		if err != nil {
			return nil, err
		}
	} else {
		message, err = s.store.LoadLatestUnboundUserMessage(ctx, threadID)
		if err != nil {
			if errors.Is(err, store.ErrSessionMessageNotFound) {
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
	req := domain.ExecuteRequest{
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

func projectRun(record domain.RunRecord) (Run, error) {
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
	if record.Status != domain.RunStatusRunning {
		run.CompletedAt = record.UpdatedAt
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

func (s *ClientService) executeRun(ctx context.Context, exec executorHandle, req domain.ExecuteRequest, started *clientRunStartSignal) {
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

func (s *ClientService) reportBackgroundRunFailure(ctx context.Context, runID string, cause, persistErr error) {
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

func (s *ClientService) recordStartedRunFailure(ctx context.Context, runID string, cause error) error {
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
	if record.Status != domain.RunStatusRunning {
		return nil
	}
	if err := s.store.FinishRunContext(ctx, runID, domain.RunStatusFailed, "", cause.Error()); err != nil {
		return fmt.Errorf("mark client run failed after background error: %w", err)
	}
	if _, err := s.store.AppendEventContext(ctx, runID, "run.failed", map[string]any{"error": cause.Error()}); err != nil {
		return fmt.Errorf("append client run failed event after background error: %w", err)
	}
	return nil
}

func (s *ClientService) LoadRunEventsAfter(ctx context.Context, runID string, afterSeq int64) (*clientevents.RunEventBatch, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if _, err := s.store.LoadRun(ctx, runID); err != nil {
		return nil, err
	}
	records, err := s.store.LoadEventsAfter(ctx, runID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("%w: load persisted run events: %v", ErrClientProjectionFailed, err)
	}
	events := make([]clientevents.RunEvent, 0, len(records))
	cursorSeq := afterSeq
	for _, record := range records {
		if record.Sequence > cursorSeq {
			cursorSeq = record.Sequence
		}
		if !clientevents.IsLiveRunEventKind(record.Kind) {
			continue
		}
		event, err := clientevents.ProjectRunEvent(record)
		if err != nil {
			return nil, fmt.Errorf("%w: project persisted run event: %v", ErrClientProjectionFailed, err)
		}
		events = append(events, event)
	}
	return &clientevents.RunEventBatch{
		Events:    events,
		CursorSeq: cursorSeq,
	}, nil
}

func (s *ClientService) LoadRunEventsForDetail(ctx context.Context, runID string) (*clientevents.RunEventDetail, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if _, err := s.store.LoadRun(ctx, runID); err != nil {
		return nil, err
	}
	records, err := s.store.LoadEvents(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("%w: load persisted run events: %v", ErrClientProjectionFailed, err)
	}
	events := make([]clientevents.RunEvent, 0, len(records))
	for _, record := range records {
		if !clientevents.IsLiveRunEventKind(record.Kind) {
			continue
		}
		event, err := clientevents.ProjectRunEvent(record)
		if err != nil {
			return nil, fmt.Errorf("%w: project persisted run event: %v", ErrClientProjectionFailed, err)
		}
		events = append(events, event)
	}
	return &clientevents.RunEventDetail{
		Events: events,
	}, nil
}

func (s *ClientService) EventPollInterval() time.Duration {
	if s == nil || s.eventPoll <= 0 {
		return 100 * time.Millisecond
	}
	return s.eventPoll
}

const chatHistoryLimit = 12

func buildChatMessages(items []domain.SessionMessageRecord) []adk.Message {
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
