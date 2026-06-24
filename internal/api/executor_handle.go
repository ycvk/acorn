package api

import (
	"context"

	"github.com/ycvk/acorn/internal/core"
)

// ExecutorHandle is the runtime executor contract for run/resume operations.
type ExecutorHandle interface {
	ExecuteMessages(ctx context.Context, req core.ExecuteRequest, observer RunStartObserver) error
	ResumeWithTargets(ctx context.Context, runID string, targets map[string]any) (*ExecutorRunResult, error)
}

// RunStartObserver is called when a run starts.
type RunStartObserver interface {
	RunStarted()
}

// ExecutorRunResult is the terminal outcome of an executor run.
type ExecutorRunResult struct {
	RunID       string
	Status      core.RunStatus
	Output      string
	Error       string
	Interrupted map[string]any
}
