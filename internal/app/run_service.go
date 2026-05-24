package app

import (
	"context"
	"errors"

	"github.com/ycvk/acorn/internal/runtime"
)

type RunService struct {
	newExecutor func(context.Context) (executorHandle, error)
	controller  *runtime.RunController
}

func NewRunService(newExecutor func(context.Context) (executorHandle, error), controller *runtime.RunController) *RunService {
	return &RunService{newExecutor: newExecutor, controller: controller}
}

func (s *RunService) Run(ctx context.Context, input, skillID string, sink runtime.StreamSink) (*runtime.Result, error) {
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
