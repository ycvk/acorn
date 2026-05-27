package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/store"
)

type ClientService struct {
	store         clientStore
	newExecutor   func(context.Context) (executorHandle, error)
	workspaceRoot string
	eventPoll     time.Duration
	newThreadID   func() string
	newRunID      func() string
	reportError   func(context.Context, string, error)
}

func BuildClientService(store clientStore, newExecutor func(context.Context) (executorHandle, error), workspaceRoot string) *ClientService {
	return &ClientService{
		store:         store,
		newExecutor:   newExecutor,
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
	case events.RunStatusRunning:
		return false, nil
	case events.RunStatusSucceeded, events.RunStatusInterrupted, events.RunStatusFailed:
		return true, nil
	default:
		return false, projectionError("unknown run status %q", record.Status)
	}
}

func (s *ClientService) CreateRun(ctx context.Context, threadID, skillID, mode string) (*Run, error) {
	if s == nil || s.store == nil || s.newExecutor == nil || s.newRunID == nil {
		return nil, errors.New("client service is not initialized")
	}
	threadID = strings.TrimSpace(threadID)
	skillID = strings.TrimSpace(skillID)
	orchestrationMode, err := parseClientRunMode(mode)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.LoadSession(ctx, threadID); err != nil {
		return nil, err
	}
	message, err := s.store.LoadLatestUnboundUserMessage(ctx, threadID)
	if err != nil {
		if errors.Is(err, store.ErrSessionMessageNotFound) {
			return nil, fmt.Errorf("%w: thread %s", ErrClientNoPendingMessage, threadID)
		}
		return nil, err
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
	req := runtimeapi.ExecuteRequest{
		RunID:             runID,
		SessionID:         threadID,
		TurnIndex:         message.TurnIndex,
		Input:             message.Content,
		SkillID:           skillID,
		Messages:          buildChatMessages(history),
		OrchestrationMode: orchestrationMode,
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

func parseClientRunMode(raw string) (events.OrchestrationMode, error) {
	mode := events.OrchestrationMode(strings.TrimSpace(raw))
	switch mode {
	case "":
		return "", nil
	case events.ModeDirectResponse, events.ModePlanExecute:
		return mode, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrClientInvalidRunMode, raw)
	}
}

func projectRun(record events.RunRecord) (Run, error) {
	status, err := projectRunStatus(record.Status)
	if err != nil {
		return Run{}, err
	}
	mode, err := projectRunMode(record.OrchestrationMode)
	if err != nil {
		return Run{}, err
	}
	run := Run{
		ID:        record.RunID,
		ThreadID:  record.SessionID,
		Status:    status,
		Mode:      mode,
		CreatedAt: record.CreatedAt,
	}
	if record.Status != events.RunStatusRunning {
		run.CompletedAt = record.UpdatedAt
	}
	return run, nil
}
