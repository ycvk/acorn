package app

import (
	"context"
	"errors"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/runtime/eventstream"
)

type executorHandle interface {
	ExecuteMessages(ctx context.Context, req domain.ExecuteRequest, observer runStartObserver) error
	ResumeWithTargets(ctx context.Context, runID string, targets map[string]any) (*executorRunResult, error)
}

type runStartObserver interface {
	RunStarted()
}

type executorRunResult struct {
	RunID       string
	Status      domain.RunStatus
	Output      string
	Error       string
	Interrupted map[string]any
}

type runtimeExecutorHandle struct {
	exec *runtime.Executor
}

func newExecutorFactory(cfg *config.Config, store runtime.ExecutorStore, runnerFactory *runtime.RunnerFactory, controller *runtime.RunController) func(context.Context) (executorHandle, error) {
	return func(_ context.Context) (executorHandle, error) {
		exec, err := runtime.NewExecutorWithRunRuntimeAndController(cfg, store, runnerFactory, controller)
		if err != nil {
			return nil, err
		}
		return runtimeExecutorHandle{exec: exec}, nil
	}
}

func (h runtimeExecutorHandle) ExecuteMessages(ctx context.Context, req domain.ExecuteRequest, observer runStartObserver) error {
	result, err := h.exec.ExecuteMessages(ctx, req, streamSinkForRunStart(observer))
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("runtime executor returned nil result")
	}
	return nil
}

func (h runtimeExecutorHandle) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any) (*executorRunResult, error) {
	result, err := h.exec.ResumeWithTargets(ctx, runID, targets, nil)
	if err != nil {
		return nil, err
	}
	return executorRunResultFromRuntime(result)
}

func streamSinkForRunStart(observer runStartObserver) eventstream.StreamSink {
	if observer == nil {
		return nil
	}
	return func(item eventstream.StreamItem) error {
		if item.Kind == eventstream.StreamKindRunStarted {
			observer.RunStarted()
		}
		return nil
	}
}

func executorRunResultFromRuntime(result *runtime.Result) (*executorRunResult, error) {
	if result == nil {
		return nil, errors.New("runtime executor returned nil result")
	}
	return &executorRunResult{
		RunID:       result.RunID,
		Status:      result.Status,
		Output:      result.Output,
		Error:       result.Error,
		Interrupted: cloneMap(result.Interrupted),
	}, nil
}
