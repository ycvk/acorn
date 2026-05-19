package model

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/config"
)

func NewChatModel(ctx context.Context, cfg config.ProviderConfig) (einomodel.BaseChatModel, error) {
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
