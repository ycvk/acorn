package contextplane

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
)

func TestBudgetGovernorEvaluatePressureStates(t *testing.T) {
	counter, err := NewCompressionTokenCounter(config.ContextConfig{TokenEncoding: "o200k_base"})
	if err != nil {
		t.Fatalf("NewCompressionTokenCounter: %v", err)
	}
	messages := []adk.Message{schema.UserMessage(strings.Repeat("budget pressure ", 1200))}
	estimated, err := counter.count(context.Background(), messages, nil)
	if err != nil {
		t.Fatalf("count fixture tokens: %v", err)
	}
	if estimated <= 300 {
		t.Fatalf("fixture estimated tokens = %d, want > 300", estimated)
	}

	cases := []struct {
		name    string
		profile ModelProfile
		want    BudgetPressureState
	}{
		{name: "ok", profile: profileForEstimatedState(estimated, PressureOK), want: PressureOK},
		{name: "auto", profile: profileForEstimatedState(estimated, PressureAutoCompact), want: PressureAutoCompact},
	}
	governor := NewBudgetGovernor(counter)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pressure, err := governor.Evaluate(context.Background(), BudgetEvaluateRequest{
				Profile:  tc.profile,
				Messages: messages,
			})
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if pressure.State != tc.want {
				t.Fatalf("state = %q, want %q; pressure=%+v estimated=%d", pressure.State, tc.want, pressure, estimated)
			}
		})
	}
}

func TestPressureThresholds(t *testing.T) {
	profile := ModelProfile{
		ContextWindowTokens:         200000,
		ReservedOutputTokens:        4096,
		ReservedSummaryOutputTokens: 2048,
		StaticOverheadTokens:        4096,
		WarningBufferTokens:         20000,
		AutoCompactBufferTokens:     13000,
	}
	thresholds, err := pressureThresholds(profile)
	if err != nil {
		t.Fatalf("pressureThresholds: %v", err)
	}
	if thresholds.autoCompact != 178808 {
		t.Fatalf("auto threshold = %d, want 178808", thresholds.autoCompact)
	}
	if thresholds.warning != 171808 {
		t.Fatalf("warning (assembly) threshold = %d, want 171808", thresholds.warning)
	}
}

func TestPressureThresholdsRejectsInvalidProfile(t *testing.T) {
	profile := ModelProfile{
		ContextWindowTokens:         100,
		ReservedOutputTokens:        0,
		ReservedSummaryOutputTokens: 0,
		StaticOverheadTokens:        0,
		WarningBufferTokens:         10,
		AutoCompactBufferTokens:     20,
	}
	_, err := pressureThresholds(profile)
	if err == nil || !strings.Contains(err.Error(), "warning buffer must be greater than auto compact buffer") {
		t.Fatalf("error = %v, want warning/auto buffer error", err)
	}
}

func TestBudgetGovernorRequiresTokenCounter(t *testing.T) {
	_, err := NewBudgetGovernor(nil).Evaluate(context.Background(), BudgetEvaluateRequest{
		Profile: profileForEstimatedState(1000, PressureOK),
	})
	if err == nil || !strings.Contains(err.Error(), "token counter is required") {
		t.Fatalf("error = %v, want token counter required", err)
	}
}

func TestContextAssemblyTokenLimitFromContextPolicy(t *testing.T) {
	got, err := ContextAssemblyTokenLimitFromContextPolicy(config.ContextConfig{
		WindowTokens:         200000,
		CompactMarginTokens:  13000,
		PreserveRecentTurns:  3,
		SummaryMaxTokens:     2048,
		ReservedOutputTokens: 4096,
	})
	if err != nil {
		t.Fatalf("ContextAssemblyTokenLimitFromContextPolicy: %v", err)
	}
	if got != 171808 {
		t.Fatalf("assembly limit = %d, want 171808", got)
	}
}

func profileForEstimatedState(estimated int, state BudgetPressureState) ModelProfile {
	const (
		warningBuffer = 300
		autoBuffer    = 200
	)
	effective := estimated + 400
	if state == PressureAutoCompact {
		effective = estimated + autoBuffer
	}
	return ModelProfile{
		ContextWindowTokens:     effective,
		WarningBufferTokens:     warningBuffer,
		AutoCompactBufferTokens: autoBuffer,
	}
}
