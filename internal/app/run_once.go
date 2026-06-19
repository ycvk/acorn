package app

import (
	"context"
	"errors"
	"strings"

	"github.com/ycvk/acorn/internal/runtime"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
)

// RunOnceResult is the terminal outcome of an owner-local smoke run.
type RunOnceResult struct {
	RunID  string
	Status string
	Output string
	Error  string
}

// RunOnce executes a single owner-local run synchronously and returns its
// terminal result. It is an operator smoke probe: it drives the exact runtime
// execution path (Executor -> RunnerFactory -> ContextPlane -> memory prepare),
// so any readiness gap (binary built without FAISS, unconfigured embedding,
// prepare failure) surfaces here as a real error or failed result instead of
// staying hidden until the first remote-client message.
//
// An empty mode resolves to direct_response. Only the public root modes are
// accepted; the internal single_agent mode is rejected by parseClientRunMode.
func (c *Container) RunOnce(ctx context.Context, input, mode string) (*RunOnceResult, error) {
	if c == nil {
		return nil, errors.New("container is nil")
	}
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, errors.New("run input is required")
	}
	orchestrationMode, err := parseClientRunMode(mode)
	if err != nil {
		return nil, err
	}
	exec, err := runtime.NewExecutorWithRunRuntimeAndController(c.cfg, c.store, c.runnerFactory, c.runController)
	if err != nil {
		return nil, err
	}
	result, err := exec.ExecuteMessages(ctx, runtimeapi.ExecuteRequest{
		Input:             trimmed,
		OrchestrationMode: orchestrationMode,
	}, nil)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("runtime executor returned nil result")
	}
	return &RunOnceResult{
		RunID:  result.RunID,
		Status: string(result.Status),
		Output: result.Output,
		Error:  result.Error,
	}, nil
}
