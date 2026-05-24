package config

import "testing"

func TestContextPolicyDerivesInternalDefaults(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers[0].MaxCompletionTokens = 8192
	cfg.Context = ContextConfig{
		WindowTokens:        200000,
		CompactMarginTokens: 13000,
		PreserveRecentTurns: 3,
		SummaryMaxTokens:    2048,
	}

	policy, err := cfg.ContextPolicy()
	if err != nil {
		t.Fatalf("ContextPolicy: %v", err)
	}

	if got, want := policy.WindowTokens, 200000; got != want {
		t.Fatalf("WindowTokens = %d, want %d", got, want)
	}
	if got, want := policy.ReservedOutputTokens, 8192; got != want {
		t.Fatalf("ReservedOutputTokens = %d, want provider max_completion_tokens %d", got, want)
	}
	if got, want := policy.CompactMarginTokens, 13000; got != want {
		t.Fatalf("CompactMarginTokens = %d, want %d", got, want)
	}
	if got, want := policy.TokenEncoding, "o200k_base"; got != want {
		t.Fatalf("TokenEncoding = %q, want %q", got, want)
	}
}

func TestContextPolicyRequiresProviderOutputCap(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers[0].MaxCompletionTokens = 0

	if _, err := cfg.ContextPolicy(); err == nil {
		t.Fatal("expected missing provider max_completion_tokens to fail")
	}
}
