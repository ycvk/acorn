package providers

import (
	"context"
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

func TestNewOpenAIChatModelPassesReasoningEffort(t *testing.T) {
	ctx := context.Background()
	cfg := config.ProviderConfig{
		Name:                "test",
		Model:               "o3-mini",
		BaseURL:             "https://api.openai.com/v1",
		APIKey:              "test-key",
		TimeoutSeconds:      30,
		Temperature:         0.1,
		MaxCompletionTokens: 2048,
		ReasoningEffort:     "high",
	}

	model, err := NewOpenAIChatModel(ctx, cfg)
	if err != nil {
		t.Fatalf("NewOpenAIChatModel with reasoning_effort: %v", err)
	}
	if model == nil {
		t.Fatal("expected model to be non-nil")
	}

	cfg.ReasoningEffort = ""
	model, err = NewOpenAIChatModel(ctx, cfg)
	if err != nil {
		t.Fatalf("NewOpenAIChatModel without reasoning_effort: %v", err)
	}
	if model == nil {
		t.Fatal("expected model to be non-nil")
	}
}

func TestNewOpenAIChatModelWithInvalidReasoningEffort(t *testing.T) {
	ctx := context.Background()
	cfg := config.ProviderConfig{
		Name:                "test",
		Model:               "o3-mini",
		BaseURL:             "https://api.openai.com/v1",
		APIKey:              "test-key",
		TimeoutSeconds:      30,
		Temperature:         0.1,
		MaxCompletionTokens: 2048,
		ReasoningEffort:     "invalid",
	}

	if _, err := NewOpenAIChatModel(ctx, cfg); err != nil {
		t.Fatalf("NewOpenAIChatModel with invalid reasoning_effort should not error at creation: %v", err)
	}
}

func TestNewOpenAIChatModelPassesExtraFields(t *testing.T) {
	ctx := context.Background()
	cfg := config.ProviderConfig{
		Name:                "test",
		Model:               "claude-3-7-sonnet",
		BaseURL:             "https://api.anthropic.com/v1",
		APIKey:              "test-key",
		TimeoutSeconds:      30,
		Temperature:         0.1,
		MaxCompletionTokens: 4096,
		ExtraFields: map[string]any{
			"enable_thinking": true,
			"thinking_budget": 2048,
		},
	}

	model, err := NewOpenAIChatModel(ctx, cfg)
	if err != nil {
		t.Fatalf("NewOpenAIChatModel with extra_fields: %v", err)
	}
	if model == nil {
		t.Fatal("expected model to be non-nil")
	}
}
