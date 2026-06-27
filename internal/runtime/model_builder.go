package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/config"
)

// newChatModel builds a chat model from the configured primary provider.
func newChatModel(ctx context.Context, cfg *config.Config) (einomodel.BaseChatModel, error) {
	if cfg == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	return newRuntimeChatModel(ctx, cfg, nil, nil)
}

// NewChatModelWithModel builds a chat model from the configured primary
// provider but overrides the model name. Used by the periodic memory
// reviewer to run on a cheaper model (memory.review.review_model).
// Empty modelName falls back to the provider's configured model.
func NewChatModelWithModel(cfg *config.Config, modelName string) func(ctx context.Context) (einomodel.BaseChatModel, error) {
	return func(ctx context.Context) (einomodel.BaseChatModel, error) {
		if cfg == nil {
			return nil, errors.New("runner factory is not initialized")
		}
		provider, err := cfg.EnabledProvider()
		if err != nil {
			return nil, err
		}
		if m := strings.TrimSpace(modelName); m != "" {
			provider.Model = m
		}
		return newOpenAIChatModel(ctx, provider)
	}
}

// chatModelBuilder constructs a chat model for a given provider config.
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
		newModel = newOpenAIChatModel
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

// buildRunnerAgentHandlers assembles the chat-model middleware chain. With the
// compaction subpackage removed, compression is driven by the context session
// rather than by model-call middleware; this builder now only appends the
// caller-supplied extra handlers.
func buildRunnerAgentHandlers(
	_ context.Context,
	cfg *config.Config,
	_ *ContextPlane,
	extraHandlers []adk.ChatModelAgentMiddleware,
	_ einomodel.BaseChatModel,
	_ any,
) ([]adk.ChatModelAgentMiddleware, error) {
	if cfg == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	handlers := make([]adk.ChatModelAgentMiddleware, 0, len(extraHandlers))
	handlers = append(handlers, extraHandlers...)
	return handlers, nil
}

// newOpenAIChatModel builds an OpenAI-compatible chat model from provider config.
func newOpenAIChatModel(ctx context.Context, cfg config.ProviderConfig) (einomodel.BaseChatModel, error) {
	chatCfg := &openai.ChatModelConfig{
		APIKey:              cfg.APIKey,
		BaseURL:             cfg.BaseURL,
		Model:               cfg.Model,
		Timeout:             time.Duration(cfg.TimeoutSeconds) * time.Second,
		MaxCompletionTokens: new(cfg.MaxCompletionTokens),
		Temperature:         new(cfg.Temperature),
	}
	if cfg.ReasoningEffort != "" {
		chatCfg.ReasoningEffort = openai.ReasoningEffortLevel(cfg.ReasoningEffort)
	}
	if len(cfg.ExtraFields) > 0 {
		chatCfg.ExtraFields = cfg.ExtraFields
	}
	model, err := openai.NewChatModel(ctx, chatCfg)
	if err != nil {
		return nil, fmt.Errorf("build openai-compatible chat model: %w", err)
	}
	return model, nil
}
