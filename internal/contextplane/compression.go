package contextplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/config"
)

type CompressionBuildOptions struct {
	RuntimeStorageDir string
	TokenCounter      *CompressionTokenCounter
	State             any
	EmitCompressed    func(context.Context, CompressionOutcome) error
	EmitPressure      func(context.Context, BudgetPressure) error
}

type CompressionPipeline struct{}

func NewCompressionPipeline() *CompressionPipeline {
	return &CompressionPipeline{}
}

func (p *defaultContextPlane) BuildHandlers(
	ctx context.Context,
	cfg config.ContextConfig,
	chatModel einomodel.BaseChatModel,
	opts CompressionBuildOptions,
) ([]adk.ChatModelAgentMiddleware, error) {
	if p == nil {
		return nil, errors.New("context plane is not initialized")
	}
	return p.compressionPipeline.Build(ctx, cfg, chatModel, opts)
}

func (*CompressionPipeline) Build(
	ctx context.Context,
	cfg config.ContextConfig,
	chatModel einomodel.BaseChatModel,
	opts CompressionBuildOptions,
) ([]adk.ChatModelAgentMiddleware, error) {
	_ = cfg
	_ = chatModel
	_ = opts

	patchMW, err := patchtoolcalls.New(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("build patchtoolcalls middleware: %w", err)
	}
	lifecycleMW := newToolLifecycleMiddleware()
	return []adk.ChatModelAgentMiddleware{patchMW, lifecycleMW}, nil
}
