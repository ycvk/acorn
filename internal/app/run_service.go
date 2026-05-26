package app

import (
	"context"
	"errors"

	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/stream"
)

type RunService struct {
	newExecutor func(context.Context) (executorHandle, error)
	controller  *runtime.RunController
}

func NewRunService(newExecutor func(context.Context) (executorHandle, error), controller *runtime.RunController) *RunService {
	return &RunService{newExecutor: newExecutor, controller: controller}
}

func (s *RunService) Run(ctx context.Context, input, skillID string, sink stream.StreamSink) (*runtime.Result, error) {
	if s == nil || s.newExecutor == nil {
		return nil, errors.New("run executor factory is nil")
	}
	exec, err := s.newExecutor(ctx)
	if err != nil {
		return nil, err
	}
	return exec.Run(ctx, input, skillID, sink)
}

func (s *RunService) InterruptRun(ctx context.Context, runID string) error {
	_ = ctx
	if s == nil || s.controller == nil {
		return errors.New("run controller is nil")
	}
	return s.controller.Interrupt(runID)
}

type ResumeService struct {
	trace       *TraceService
	newExecutor func(context.Context) (executorHandle, error)
	pending     runtime.PendingResumeStore
}

func NewResumeService(trace *TraceService, newExecutor func(context.Context) (executorHandle, error), pending runtime.PendingResumeStore) *ResumeService {
	return &ResumeService{trace: trace, newExecutor: newExecutor, pending: pending}
}

func (s *ResumeService) FindPendingResume(ctx context.Context) (*runtime.PendingResumeInfo, error) {
	if s == nil || s.pending == nil {
		return nil, errors.New("resume pending store is nil")
	}
	return runtime.FindPendingResume(ctx, s.pending)
}

func (s *ResumeService) Resume(ctx context.Context, runID string, sink stream.StreamSink) (*runtime.Result, error) {
	if s == nil || s.trace == nil {
		return nil, errors.New("resume trace service is nil")
	}
	if s.newExecutor == nil {
		return nil, errors.New("resume executor factory is nil")
	}
	targets, err := s.trace.InferResumeTargets(ctx, runID)
	if err != nil {
		return nil, err
	}
	exec, err := s.newExecutor(ctx)
	if err != nil {
		return nil, err
	}
	return exec.ResumeWithTargets(ctx, runID, targets, sink)
}
