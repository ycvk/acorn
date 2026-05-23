package runtime

import (
	"context"

	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/crystallization"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/runtimehistory"
)

// RunBuilder is the interface required by Executor to build and manage runs.
type RunBuilder interface {
	New(ctx context.Context, req RunnerBuildRequest) (*ActiveRunner, error)
	Registry() *Registry
	ConsumeEventError(runID string) error
	Config() *config.Config
	MemoryModule() memorymodule.Service
	SessionSummarySvc() *runtimehistory.SessionSummaryService
	NewChatModel(ctx context.Context) (einomodel.BaseChatModel, error)
	Crystallizer() crystallization.Service
}
