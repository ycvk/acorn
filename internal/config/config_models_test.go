package config

import (
	"strings"
	"testing"
)

func TestModernModelContracts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider ProviderConfig
		want     string
	}{
		{"astra", ProviderConfig{Model: "gpt-6-astra", ReasoningEffort: "max"}, ""},
		{"astra sampling", ProviderConfig{Model: "gpt-6-astra", Temperature: new(float32(1))}, "temperature"},
		{"astra tools", ProviderConfig{Model: "gpt-6-astra", API: "chat_completions"}, "requires api: responses"},
		{"opus", ProviderConfig{Model: "claude-opus-5-5", API: "anthropic", MaxOutputTokens: new(128000), ReasoningEffort: "max"}, ""},
		{"opus output", ProviderConfig{Model: "claude-opus-5-5", API: "anthropic"}, "requires max_output_tokens"},
		{"grok", ProviderConfig{Model: "grok-4.7", ReasoningEffort: "xhigh"}, ""},
		{"grok effort", ProviderConfig{Model: "grok-4.7", ReasoningEffort: "max"}, "unsupported"},
		{"output limit", ProviderConfig{Model: "gpt-6-astra", MaxOutputTokens: new(128001)}, "output limit"},
		{"custom model", ProviderConfig{Model: "custom", API: "chat_completions", Temperature: new(float32(0)), MaxOutputTokens: new(64000), ContextWindowTokens: new(128000)}, ""},
		{"custom model without a window", ProviderConfig{Model: "custom", API: "chat_completions"}, "context_window_tokens is required"},
		{"known model with a window", ProviderConfig{Model: "grok-4.7", ContextWindowTokens: new(200000)}, "remove context_window_tokens"},
		{"idle disabled", ProviderConfig{Model: "grok-4.7", IdleTimeoutSeconds: new(0)}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.provider.validateModelSettings()
			if tc.want == "" && err != nil {
				t.Fatal(err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestInputBudgetReservesOutputAndPresence(t *testing.T) {
	cfg := defaultConfig()
	n, err := cfg.InputTokenBudget()
	if err != nil || n != 886000 {
		t.Fatalf("default input budget = %d, %v", n, err)
	}
	cfg.Providers[0].MaxOutputTokens = new(64000)
	n, err = cfg.InputTokenBudget()
	if err != nil || n != 950000 {
		t.Fatalf("explicit output budget = %d, %v", n, err)
	}
	cfg.Providers[0] = ProviderConfig{Name: "small", Model: "custom", ContextWindowTokens: new(40000), MaxOutputTokens: new(8000), Enabled: true}
	if _, err := cfg.InputTokenBudget(); err == nil {
		t.Fatal("accepted a context without room for input")
	}
	cfg.Providers[0].ContextWindowTokens = new(200000)
	if n, err := cfg.InputTokenBudget(); err != nil || n != 156000 {
		t.Fatalf("the provider's own window = %d, %v", n, err)
	}
}

func TestModernDefaultsLeaveSamplingAndDeadlinesOptional(t *testing.T) {
	cfg := defaultConfig()
	p := cfg.Providers[0]
	if p.Temperature != nil || p.TimeoutSeconds != 0 {
		t.Fatalf("unexpected defaults: provider=%+v", p)
	}
	if p.ModelIdleTimeoutSeconds() != 300 {
		t.Fatal("expected idle deadline")
	}
	cfg.Providers[0].APIKey = "test"
	cfg.Memory.Embedding.APIKey = "voyage-test"
	if err := cfg.ValidateExecutionReady(); err != nil {
		t.Fatal(err)
	}
}
