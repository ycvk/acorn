package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
)

type providerBuilderTestModel struct{}

func (m *providerBuilderTestModel) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return schema.AssistantMessage("ok", nil), nil
}

func (m *providerBuilderTestModel) Stream(context.Context, []*schema.Message, ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("ok", nil)}), nil
}

func TestBuildRuntimeChatModelUsesSingleEnabledProvider(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name:    "primary",
				Model:   "gpt-test",
				BaseURL: "https://example.invalid/v1",
				APIKey:  "key",
				Enabled: true,
			},
			{
				Name:    "disabled",
				Model:   "gpt-disabled",
				BaseURL: "https://example.invalid/v1",
				APIKey:  "key",
				Enabled: false,
			},
		},
	}

	var selected string
	model, err := buildRuntimeChatModel(context.Background(), cfg, func(_ context.Context, provider config.ProviderConfig) (einomodel.BaseChatModel, error) {
		selected = provider.Name
		return &providerBuilderTestModel{}, nil
	})
	if err != nil {
		t.Fatalf("buildRuntimeChatModel: %v", err)
	}
	if model == nil {
		t.Fatal("expected model")
	}
	if got, want := selected, "primary"; got != want {
		t.Fatalf("selected provider = %q, want %q", got, want)
	}
}

func TestBuildRuntimeChatModelRequiresEnabledProvider(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "disabled", Enabled: false},
		},
	}

	_, err := buildRuntimeChatModel(context.Background(), cfg, func(context.Context, config.ProviderConfig) (einomodel.BaseChatModel, error) {
		return &providerBuilderTestModel{}, nil
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no enabled providers") {
		t.Fatalf("error = %q, want no enabled providers", err.Error())
	}
}

func TestBuildRuntimeChatModelRejectsMultipleEnabledProviders(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "primary", Enabled: true},
			{Name: "secondary", Enabled: true},
		},
	}

	_, err := buildRuntimeChatModel(context.Background(), cfg, func(context.Context, config.ProviderConfig) (einomodel.BaseChatModel, error) {
		return &providerBuilderTestModel{}, nil
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "exactly one provider must be enabled") {
		t.Fatalf("error = %q, want exactly-one provider error", err.Error())
	}
}

func TestBuildRuntimeChatModelReturnsProviderInitError(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name:    "primary",
				Model:   "gpt-test",
				BaseURL: "https://example.invalid/v1",
				APIKey:  "key",
				Enabled: true,
			},
		},
	}

	_, err := buildRuntimeChatModel(context.Background(), cfg, func(context.Context, config.ProviderConfig) (einomodel.BaseChatModel, error) {
		return nil, errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "init provider primary") {
		t.Fatalf("error = %q, want provider context", err.Error())
	}
}
