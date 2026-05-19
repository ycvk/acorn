package app

import (
	"context"
	"errors"

	"github.com/ycvk/acorn/internal/runtime"
)

type RunService struct {
	executors  executorFactory
	controller *runtime.RunController
}

func NewRunService(executors executorFactory, controller *runtime.RunController) *RunService {
	return &RunService{executors: executors, controller: controller}
}

func (s *RunService) Run(ctx context.Context, input, skillID string, sink runtime.StreamSink) (*runtime.Result, error) {
	if s == nil || s.executors == nil {
		return nil, errors.New("run executor factory is nil")
	}
	handle, err := s.executors.New(ctx)
	if err != nil {
		return nil, err
	}
	return handle.Run(ctx, input, skillID, sink)
}

func (s *RunService) InterruptRun(ctx context.Context, runID string) error {
	_ = ctx
	if s == nil || s.controller == nil {
		return errors.New("run controller is nil")
	}
	return s.controller.Interrupt(runID)
}
