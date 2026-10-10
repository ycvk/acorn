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
		{"custom model", ProviderConfig{Model: "custom", API: "chat_completions", Temperature: new(float32(0)), MaxOutputTokens: new(64000)}, ""},
		{"idle disabled", ProviderConfig{IdleTimeoutSeconds: new(0)}, ""},
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
	cfg.Context.WindowTokens = 20000
	cfg.Context.CompactMarginTokens = 8000
	if _, err := cfg.InputTokenBudget(); err == nil {
		t.Fatal("accepted a context without room for input")
	}
}

func TestModernDefaultsLeaveSamplingAndDeadlinesOptional(t *testing.T) {
	cfg := defaultConfig()
	p := cfg.Providers[0]
	if p.Temperature != nil || p.TimeoutSeconds != 0 || cfg.Runtime.RunTimeoutSeconds != 0 || cfg.Wake.DailyTokens != 0 {
		t.Fatalf("unexpected defaults: provider=%+v runtime=%+v wake=%+v", p, cfg.Runtime, cfg.Wake)
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
