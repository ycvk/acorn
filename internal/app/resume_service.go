package app

import (
	"context"
	"errors"

	"github.com/ycvk/acorn/internal/runtime"
)

type ResumeService struct {
	trace     *TraceService
	executors executorFactory
	pending   runtime.PendingResumeStore
}

func NewResumeService(trace *TraceService, executors executorFactory, pending runtime.PendingResumeStore) *ResumeService {
	return &ResumeService{trace: trace, executors: executors, pending: pending}
}

func (s *ResumeService) FindPendingResume(ctx context.Context) (*runtime.PendingResumeInfo, error) {
	if s == nil || s.pending == nil {
		return nil, errors.New("resume pending store is nil")
	}
	return runtime.FindPendingResume(ctx, s.pending)
}

func (s *ResumeService) Resume(ctx context.Context, runID string, sink runtime.StreamSink) (*runtime.Result, error) {
	if s == nil || s.trace == nil {
		return nil, errors.New("resume trace service is nil")
	}
	if s.executors == nil {
		return nil, errors.New("resume executor factory is nil")
	}
	targets, err := s.trace.InferResumeTargets(ctx, runID)
	if err != nil {
		return nil, err
	}
	handle, err := s.executors.New(ctx)
	if err != nil {
		return nil, err
	}
	return handle.ResumeWithTargets(ctx, runID, targets, sink)
}
