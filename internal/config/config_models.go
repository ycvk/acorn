package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// APIProtocol is the explicit wire protocol used by the enabled provider.
func (p ProviderConfig) APIProtocol() string {
	if p.API == "" {
		return "responses"
	}
	return p.API
}

// ModelIdleTimeoutSeconds bounds a stalled response; zero explicitly disables it.
func (p ProviderConfig) ModelIdleTimeoutSeconds() int {
	if p.IdleTimeoutSeconds == nil {
		return 300
	}
	return *p.IdleTimeoutSeconds
}

type modelLimits struct {
	context, output int
	efforts         []string
	sampling        bool
}

// Published model contracts as of 2026-10-09. Unknown model IDs use the owner's
// explicit context and output settings, without guessing from name prefixes.
func modelLimitsFor(name string) modelLimits {
	switch strings.TrimSpace(name) {
	case "gpt-6-astra":
		return modelLimits{1050000, 128000, []string{"low", "medium", "high", "xhigh", "max"}, false}
	case "claude-opus-5-5":
		return modelLimits{1000000, 128000, []string{"low", "medium", "high", "xhigh", "max"}, false}
	case "grok-4.7":
		return modelLimits{500000, 0, []string{"low", "medium", "high", "xhigh"}, true}
	default:
		return modelLimits{efforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}, sampling: true}
	}
}

func (p ProviderConfig) validateModelSettings() error {
	switch p.APIProtocol() {
	case "responses", "chat_completions", "anthropic":
	default:
		return fmt.Errorf("api must be responses, chat_completions or anthropic, got %q", p.API)
	}
	if p.TimeoutSeconds < 0 {
		return errors.New("timeout_seconds must be >= 0 (0 means no total request deadline)")
	}
	if p.ModelIdleTimeoutSeconds() < 0 {
		return errors.New("idle_timeout_seconds must be >= 0")
	}
	if p.MaxOutputTokens != nil && *p.MaxOutputTokens <= 0 {
		return errors.New("max_output_tokens must be positive when set; omit it to use the provider default")
	}
	if p.APIProtocol() == "anthropic" && p.MaxOutputTokens == nil {
		return errors.New("anthropic requires max_output_tokens")
	}
	limits := modelLimitsFor(p.Model)
	if p.MaxOutputTokens != nil && limits.output > 0 && *p.MaxOutputTokens > limits.output {
		return fmt.Errorf("max_output_tokens exceeds %s output limit (%d)", p.Model, limits.output)
	}
	if p.Temperature != nil {
		if !limits.sampling {
			return fmt.Errorf("%s requires temperature to be omitted", p.Model)
		}
		if *p.Temperature < 0 || *p.Temperature > 2 {
			return errors.New("temperature must be between 0 and 2")
		}
		if p.APIProtocol() == "anthropic" && *p.Temperature > 1 {
			return errors.New("anthropic temperature must be between 0 and 1")
		}
	}
	if p.ReasoningEffort != "" && !slices.Contains(limits.efforts, p.ReasoningEffort) {
		return fmt.Errorf("reasoning_effort %q is unsupported for %s; supported: %s", p.ReasoningEffort, p.Model, strings.Join(limits.efforts, ", "))
	}
	if p.Model == "gpt-6-astra" && p.APIProtocol() != "responses" {
		return fmt.Errorf("%s tool calling requires api: responses", p.Model)
	}
	return nil
}
