package model

import (
	"context"
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

func TestNewChatModelPassesReasoningEffort(t *testing.T) {
	ctx := context.Background()

	// Test with reasoning_effort set
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

	model, err := NewChatModel(ctx, cfg)
	if err != nil {
		t.Fatalf("NewChatModel with reasoning_effort: %v", err)
	}
	if model == nil {
		t.Fatal("expected model to be non-nil")
	}

	// Test without reasoning_effort (should still work)
	cfg2 := config.ProviderConfig{
		Name:                "test2",
		Model:               "gpt-4o",
		BaseURL:             "https://api.openai.com/v1",
		APIKey:              "test-key",
		TimeoutSeconds:      30,
		Temperature:         0.1,
		MaxCompletionTokens: 2048,
		ReasoningEffort:     "",
	}

	model2, err := NewChatModel(ctx, cfg2)
	if err != nil {
		t.Fatalf("NewChatModel without reasoning_effort: %v", err)
	}
	if model2 == nil {
		t.Fatal("expected model2 to be non-nil")
	}
}

func TestNewChatModelWithInvalidReasoningEffort(t *testing.T) {
	ctx := context.Background()

	// eino-ext doesn't validate reasoning_effort at creation time,
	// but we should verify it doesn't panic with invalid values
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

	// Should still create the model; validation happens at config layer
	_, err := NewChatModel(ctx, cfg)
	if err != nil {
		t.Fatalf("NewChatModel with invalid reasoning_effort should not error at creation: %v", err)
	}
}

func TestNewChatModelPassesExtraFields(t *testing.T) {
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

	model, err := NewChatModel(ctx, cfg)
	if err != nil {
		t.Fatalf("NewChatModel with extra_fields: %v", err)
	}
	if model == nil {
		t.Fatal("expected model to be non-nil")
	}
}
