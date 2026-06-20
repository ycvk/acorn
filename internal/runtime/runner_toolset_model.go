package runtime

import (
	"context"
	"errors"
	"fmt"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/providers"
)

type chatModelBuilder func(context.Context, config.ProviderConfig) (einomodel.BaseChatModel, error)

func buildRuntimeChatModel(ctx context.Context, cfg *config.Config, newModel chatModelBuilder) (einomodel.BaseChatModel, error) {
	model, _, err := buildRuntimeChatModelWithProvider(ctx, cfg, newModel)
	return model, err
}

func buildRuntimeChatModelWithProvider(ctx context.Context, cfg *config.Config, newModel chatModelBuilder) (einomodel.BaseChatModel, config.ProviderConfig, error) {
	if cfg == nil {
		return nil, config.ProviderConfig{}, errors.New("config is required")
	}
	if newModel == nil {
		newModel = providers.NewOpenAIChatModel
	}

	provider, err := cfg.EnabledProvider()
	if err != nil {
		return nil, config.ProviderConfig{}, err
	}
	model, err := newModel(ctx, provider)
	if err != nil {
		return nil, config.ProviderConfig{}, fmt.Errorf("init provider %s: %w", provider.Name, err)
	}
	return model, provider, nil
}

func newRuntimeChatModel(
	ctx context.Context,
	cfg *config.Config,
	newModel chatModelBuilder,
	_ any,
) (einomodel.BaseChatModel, error) {
	return buildRuntimeChatModel(ctx, cfg, newModel)
}

func (f *RunnerFactory) newChatModel(ctx context.Context) (einomodel.BaseChatModel, error) {
	if f == nil || f.deps.Config == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	return newRuntimeChatModel(ctx, f.deps.Config, nil, nil)
}

// Subagent execution is now handled by SubagentExecutor in subagent_executor.go.
// The samplingExecutorAdapter has been replaced.
