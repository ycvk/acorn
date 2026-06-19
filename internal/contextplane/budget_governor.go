package contextplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
)

type BudgetPressureState string

const (
	PressureOK          BudgetPressureState = "ok"
	PressureAutoCompact BudgetPressureState = "auto_compact"
)

type BudgetGovernor interface {
	Evaluate(context.Context, BudgetEvaluateRequest) (BudgetPressure, error)
}

type ModelProfile struct {
	Name                        string
	ContextWindowTokens         int
	ReservedOutputTokens        int
	ReservedSummaryOutputTokens int
	StaticOverheadTokens        int
	WarningBufferTokens         int
	AutoCompactBufferTokens     int
}

type BudgetEvaluateRequest struct {
	Profile  ModelProfile
	Messages []adk.Message
	Tools    []*schema.ToolInfo
}

// BudgetPressure is the binary compaction decision plus the effective window the
// boundary record needs. The pressure ladder is intentionally two-state: OK (no
// action) and AutoCompact (compact now). There is no separate warning/blocking
// state because no consumer ever distinguished them — they only ever asked
// "compact or not". The warning *threshold* still exists (see pressureThresholdSet)
// but feeds the context-assembly budget, not a pressure state.
type BudgetPressure struct {
	EffectiveWindowTokens int
	State                 BudgetPressureState
}

type budgetGovernor struct {
	tokenCounter *CompressionTokenCounter
}

func NewBudgetGovernor(tokenCounter *CompressionTokenCounter) BudgetGovernor {
	return &budgetGovernor{tokenCounter: tokenCounter}
}

const (
	defaultStaticOverheadTokens = 4096
	defaultWarningGapTokens     = 7000
)

func ModelProfileFromContextPolicy(cfg config.ContextConfig) ModelProfile {
	return ModelProfile{
		ContextWindowTokens:         cfg.WindowTokens,
		ReservedOutputTokens:        cfg.ReservedOutputTokens,
		ReservedSummaryOutputTokens: cfg.SummaryMaxTokens,
		StaticOverheadTokens:        defaultStaticOverheadTokens,
		WarningBufferTokens:         cfg.CompactMarginTokens + defaultWarningGapTokens,
		AutoCompactBufferTokens:     cfg.CompactMarginTokens,
	}
}

func ContextAssemblyTokenLimitFromContextPolicy(cfg config.ContextConfig) (int, error) {
	thresholds, err := pressureThresholds(ModelProfileFromContextPolicy(cfg))
	if err != nil {
		return 0, err
	}
	return thresholds.warning, nil
}

func (g *budgetGovernor) Evaluate(ctx context.Context, req BudgetEvaluateRequest) (BudgetPressure, error) {
	if g.tokenCounter == nil {
		return BudgetPressure{}, errors.New("budget governor token counter is required")
	}
	thresholds, err := pressureThresholds(req.Profile)
	if err != nil {
		return BudgetPressure{}, err
	}
	estimated, err := g.tokenCounter.count(ctx, req.Messages, req.Tools)
	if err != nil {
		return BudgetPressure{}, fmt.Errorf("count budget pressure tokens: %w", err)
	}
	return BudgetPressure{
		EffectiveWindowTokens: thresholds.effectiveWindow,
		State:                 pressureState(estimated, thresholds),
	}, nil
}

type pressureThresholdSet struct {
	effectiveWindow int
	warning         int
	autoCompact     int
}

func pressureThresholds(profile ModelProfile) (pressureThresholdSet, error) {
	if profile.ContextWindowTokens <= 0 {
		return pressureThresholdSet{}, errors.New("model profile context window tokens must be positive")
	}
	if profile.ReservedOutputTokens < 0 {
		return pressureThresholdSet{}, errors.New("model profile reserved output tokens must be non-negative")
	}
	if profile.ReservedSummaryOutputTokens < 0 {
		return pressureThresholdSet{}, errors.New("model profile reserved summary output tokens must be non-negative")
	}
	if profile.StaticOverheadTokens < 0 {
		return pressureThresholdSet{}, errors.New("model profile static overhead tokens must be non-negative")
	}
	if profile.WarningBufferTokens <= 0 {
		return pressureThresholdSet{}, errors.New("model profile warning buffer tokens must be positive")
	}
	if profile.AutoCompactBufferTokens <= 0 {
		return pressureThresholdSet{}, errors.New("model profile auto compact buffer tokens must be positive")
	}
	if profile.WarningBufferTokens <= profile.AutoCompactBufferTokens {
		return pressureThresholdSet{}, errors.New("model profile warning buffer must be greater than auto compact buffer")
	}

	reservedOutput := max(profile.ReservedOutputTokens, profile.ReservedSummaryOutputTokens)
	effectiveWindow := profile.ContextWindowTokens - reservedOutput - profile.StaticOverheadTokens
	if effectiveWindow <= profile.WarningBufferTokens {
		return pressureThresholdSet{}, errors.New("model profile effective window must be greater than warning buffer")
	}
	return pressureThresholdSet{
		effectiveWindow: effectiveWindow,
		warning:         effectiveWindow - profile.WarningBufferTokens,
		autoCompact:     effectiveWindow - profile.AutoCompactBufferTokens,
	}, nil
}

func pressureState(tokens int, thresholds pressureThresholdSet) BudgetPressureState {
	if tokens >= thresholds.autoCompact {
		return PressureAutoCompact
	}
	return PressureOK
}
