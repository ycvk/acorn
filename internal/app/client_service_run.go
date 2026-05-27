package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ycvk/acorn/internal/events"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/stream"
)

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

func (s *clientRunStartSignal) Sink(item stream.StreamItem) error {
	if item.Kind == stream.StreamKindRunStarted {
		s.hasStarted.Store(true)
		s.closeOnce.Do(func() { close(s.started) })
	}
	return nil
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

func (s *ClientService) executeRun(ctx context.Context, exec executorHandle, req runtimeapi.ExecuteRequest, started *clientRunStartSignal) {
	result, err := exec.ExecuteMessages(ctx, req, started.Sink)
	if err != nil {
		if started.MarkFailed(err) {
			return
		}
		if persistErr := s.recordStartedRunFailure(ctx, req.RunID, err); persistErr != nil {
			s.reportBackgroundRunFailure(ctx, req.RunID, err, persistErr)
		}
		return
	}
	if result == nil {
		err := errors.New("client executor returned nil result")
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
