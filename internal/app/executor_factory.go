package app

import (
	"context"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/runtime"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/stream"
)

type executorHandle interface {
	Run(ctx context.Context, input, skillID string, sink stream.StreamSink) (*runtime.Result, error)
	ExecuteMessages(ctx context.Context, req runtimeapi.ExecuteRequest, sink stream.StreamSink) (*runtime.Result, error)
	ResumeWithTargets(ctx context.Context, runID string, targets map[string]any, sink stream.StreamSink) (*runtime.Result, error)
}

func newExecutorFactory(cfg *config.Config, store runtime.ExecutorStore, runnerFactory *runtime.RunnerFactory, controller *runtime.RunController) func(context.Context) (executorHandle, error) {
	return func(_ context.Context) (executorHandle, error) {
		return runtime.NewExecutorWithRunRuntimeAndController(cfg, store, runnerFactory, controller)
	}
}
