package runtime

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"time"

	"github.com/cloudwego/eino-ext/components/model/agenticclaude"
	"github.com/cloudwego/eino-ext/components/model/agenticopenai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/openai/openai-go/v3/responses"

	"github.com/ycvk/acorn/internal/config"
)

// newChatModel builds a chat model from the configured primary provider.
func newChatModel(ctx context.Context, cfg *config.Config) (einomodel.AgenticModel, error) {
	if cfg == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	return newRuntimeChatModel(ctx, cfg, nil, nil)
}

// chatModelBuilder constructs a chat model for a given provider config.
type chatModelBuilder func(context.Context, config.ProviderConfig) (einomodel.AgenticModel, error)

func buildRuntimeChatModel(ctx context.Context, cfg *config.Config, newModel chatModelBuilder) (einomodel.AgenticModel, error) {
	model, _, err := buildRuntimeChatModelWithProvider(ctx, cfg, newModel)
	return model, err
}

func buildRuntimeChatModelWithProvider(ctx context.Context, cfg *config.Config, newModel chatModelBuilder) (einomodel.AgenticModel, config.ProviderConfig, error) {
	if cfg == nil {
		return nil, config.ProviderConfig{}, errors.New("config is required")
	}
	if newModel == nil {
		newModel = newProviderModel
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
) (einomodel.AgenticModel, error) {
	return buildRuntimeChatModel(ctx, cfg, newModel)
}

// newProviderModel uses the provider's native message protocol. Agentic messages
// preserve reasoning signatures and tool-call blocks through Eino checkpoints.
func newProviderModel(ctx context.Context, cfg config.ProviderConfig) (einomodel.AgenticModel, error) {
	client := &http.Client{
		Timeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
		Transport: &modelTransport{base: http.DefaultTransport, idle: time.Duration(cfg.ModelIdleTimeoutSeconds()) * time.Second},
	}
	extra := maps.Clone(cfg.ExtraFields)
	if extra == nil {
		extra = make(map[string]any)
	}
	switch cfg.APIProtocol() {
	case "responses":
		request := &agenticopenai.ResponsesConfig{
			APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model, HTTPClient: client,
			MaxRetries: new(0), MaxTokens: cfg.MaxOutputTokens, Temperature: cfg.Temperature,
			Store: new(false), Include: []responses.ResponseIncludable{"reasoning.encrypted_content"}, ExtraFields: extra,
		}
		if cfg.ReasoningEffort != "" {
			request.Reasoning = &responses.ReasoningParam{Effort: responses.ReasoningEffort(cfg.ReasoningEffort)}
		}
		return agenticopenai.NewResponsesModel(ctx, request)
	case "chat_completions":
		if cfg.ReasoningEffort != "" {
			extra["reasoning_effort"] = cfg.ReasoningEffort
		}
		return agenticopenai.NewChatModel(ctx, &agenticopenai.ChatConfig{
			APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model, HTTPClient: client,
			MaxCompletionTokens: cfg.MaxOutputTokens, Temperature: cfg.Temperature, ExtraFields: extra,
		})
	case "anthropic":
		if cfg.MaxOutputTokens == nil {
			return nil, errors.New("anthropic requires max_output_tokens")
		}
		if cfg.Temperature != nil {
			extra["temperature"] = *cfg.Temperature
		}
		if cfg.ReasoningEffort != "" {
			extra["output_config"] = map[string]any{"effort": cfg.ReasoningEffort}
		}
		model, err := agenticclaude.New(ctx, &agenticclaude.Config{
			APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model, HTTPClient: client,
			MaxTokens: *cfg.MaxOutputTokens, ExtraFields: extra,
		})
		if err != nil {
			return nil, err
		}
		return &streamingGenerationModel{AgenticModel: model}, nil
	default:
		return nil, fmt.Errorf("unsupported provider api %q", cfg.API)
	}
}
