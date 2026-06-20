package runtime

import (
	"context"
	"errors"
	"fmt"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
)

type Result struct {
	RunID       string           `json:"run_id"`
	Status      events.RunStatus `json:"status"`
	Output      string           `json:"output,omitempty"`
	Error       string           `json:"error,omitempty"`
	Interrupted map[string]any   `json:"interrupted,omitempty"`
}

type Executor struct {
	store             ExecutorStore
	runRuntime        RunRuntime
	controller        *RunController
	newChatModel      func(ctx context.Context) (einomodel.BaseChatModel, error)
	archiveRunFunc    func(ctx context.Context, runID string, runStatus events.RunStatus) error
	sessionSummarySvc *model.SessionSummaryService
}

func NewExecutorWithRunRuntimeAndController(cfg *config.Config, store ExecutorStore, runRuntime RunRuntime, controller *RunController) (*Executor, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if store == nil {
		return nil, errors.New("store is required")
	}
	if runRuntime == nil {
		return nil, errors.New("run runtime is required")
	}
	if controller == nil {
		controller = NewRunController()
	}
	if err := cfg.ValidateExecutionReady(); err != nil {
		return nil, fmt.Errorf("%w: %v", runtimeapi.ErrExecutionNotReady, err)
	}
	exec := &Executor{
		store:             store,
		runRuntime:        runRuntime,
		controller:        controller,
		sessionSummarySvc: runRuntime.SessionSummarySvc(),
		newChatModel:      runRuntime.NewChatModel,
	}
	exec.archiveRunFunc = exec.archiveRun
	return exec, nil
}
