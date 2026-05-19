package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/orchestrationmode"
	"github.com/ycvk/acorn/internal/runtime"
	storecore "github.com/ycvk/acorn/internal/store"
)

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

func (s *ClientService) LoadRunEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]RunEvent, error) {
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
	events := make([]RunEvent, 0, len(records))
	for _, record := range records {
		event, err := projectRunEvent(record)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *ClientService) LoadRunEventsForDetail(ctx context.Context, runID string) (*RunEventDetail, error) {
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
	events := make([]RunEvent, 0, len(records))
	unsupported := make([]UnsupportedRunEvent, 0)
	for _, record := range records {
		event, err := projectRunEvent(record)
		if err != nil {
			unsupported = append(unsupported, projectUnsupportedRunEvent(record))
			continue
		}
		events = append(events, event)
	}
	return &RunEventDetail{
		Events:      events,
		Unsupported: unsupported,
		Trace:       runtime.BuildTraceSummary(records),
	}, nil
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

func (s *ClientService) EventPollInterval() time.Duration {
	if s == nil || s.eventPoll <= 0 {
		return 100 * time.Millisecond
	}
	return s.eventPoll
}

func (s *ClientService) CreateRun(ctx context.Context, threadID, skillID, mode string) (*Run, error) {
	if s == nil || s.store == nil || s.executors == nil || s.newRunID == nil {
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
		if errors.Is(err, storecore.ErrSessionMessageNotFound) {
			return nil, fmt.Errorf("%w: thread %s", ErrClientNoPendingMessage, threadID)
		}
		return nil, err
	}
	history, err := s.store.ListSessionMessages(ctx, threadID, chatHistoryLimit)
	if err != nil {
		return nil, err
	}
	handle, err := s.executors.New(ctx)
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(s.newRunID())
	if runID == "" {
		return nil, errors.New("client run id is empty")
	}
	started := newRunStartSignal()
	req := runtime.ExecuteRequest{
		RunID:             runID,
		SessionID:         threadID,
		TurnIndex:         message.TurnIndex,
		Input:             message.Content,
		SkillID:           skillID,
		Messages:          buildChatMessages(history),
		OrchestrationMode: orchestrationMode,
	}
	runCtx := context.WithoutCancel(ctx)
	go s.executeRun(runCtx, handle, req, started)

	select {
	case <-started.Started():
		return s.GetRun(ctx, runID)
	case err := <-started.Failed():
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func parseClientRunMode(raw string) (orchestrationmode.Mode, error) {
	mode := orchestrationmode.Mode(strings.TrimSpace(raw))
	switch mode {
	case "":
		return "", nil
	case orchestrationmode.DirectResponse, orchestrationmode.PlanExecute:
		return mode, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrClientInvalidRunMode, raw)
	}
}

func (s *ClientService) executeRun(ctx context.Context, handle executorHandle, req runtime.ExecuteRequest, started *clientRunStartSignal) {
	result, err := handle.ExecuteMessages(ctx, req, started.Sink)
	if err != nil {
		if started.MarkFailed(err) {
			return
		}
		if persistErr := s.recordStartedRunFailure(ctx, req.RunID, err); persistErr != nil {
			panic(persistErr)
		}
		return
	}
	if result == nil {
		err := errors.New("client executor returned nil result")
		if started.MarkFailed(err) {
			return
		}
		if persistErr := s.recordStartedRunFailure(ctx, req.RunID, err); persistErr != nil {
			panic(persistErr)
		}
		return
	}
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
	if record.Status != events.RunStatusRunning {
		return nil
	}
	if err := s.store.FinishRunContext(ctx, runID, events.RunStatusFailed, "", cause.Error()); err != nil {
		return fmt.Errorf("mark client run failed after background error: %w", err)
	}
	if _, err := s.store.AppendEventContext(ctx, runID, "run.failed", map[string]any{"error": cause.Error()}); err != nil {
		return fmt.Errorf("append client run failed event after background error: %w", err)
	}
	return nil
}
