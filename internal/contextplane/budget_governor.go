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
	PressureWarning     BudgetPressureState = "warning"
	PressureAutoCompact BudgetPressureState = "auto_compact"
	PressureBlocking    BudgetPressureState = "blocking"
)

type budgetGovernor interface {
	Evaluate(context.Context, BudgetEvaluateRequest) (BudgetPressure, error)
	AutoCompactThreshold(ModelProfile) (int, error)
}

type ModelProfile struct {
	Name                        string
	ContextWindowTokens         int
	ReservedOutputTokens        int
	ReservedSummaryOutputTokens int
	StaticOverheadTokens        int
	WarningBufferTokens         int
	AutoCompactBufferTokens     int
	BlockingBufferTokens        int
}

type BudgetEvaluateRequest struct {
	Profile  ModelProfile
	Messages []adk.Message
	Tools    []*schema.ToolInfo
}

type BudgetPressure struct {
	EstimatedInputTokens       int
	EffectiveWindowTokens      int
	WarningThresholdTokens     int
	AutoCompactThresholdTokens int
	BlockingThresholdTokens    int
	PercentUsed                int
	State                      BudgetPressureState
}

type BudgetGovernor struct {
	tokenCounter *CompressionTokenCounter
}

func NewBudgetGovernor(tokenCounter *CompressionTokenCounter) *BudgetGovernor {
	return &BudgetGovernor{tokenCounter: tokenCounter}
}

const (
	defaultStaticOverheadTokens = 4096
	defaultWarningGapTokens     = 7000
	defaultBlockingBufferMax    = 3000
)

func ModelProfileFromContextPolicy(cfg config.ContextConfig) ModelProfile {
	blockingBuffer := cfg.CompactMarginTokens / 4
	if blockingBuffer < 1 {
		blockingBuffer = 1
	}
	if blockingBuffer > defaultBlockingBufferMax {
		blockingBuffer = defaultBlockingBufferMax
	}
	return ModelProfile{
		ContextWindowTokens:         cfg.WindowTokens,
		ReservedOutputTokens:        cfg.ReservedOutputTokens,
		ReservedSummaryOutputTokens: cfg.SummaryMaxTokens,
		StaticOverheadTokens:        defaultStaticOverheadTokens,
		WarningBufferTokens:         cfg.CompactMarginTokens + defaultWarningGapTokens,
		AutoCompactBufferTokens:     cfg.CompactMarginTokens,
		BlockingBufferTokens:        blockingBuffer,
	}
}

func ContextAssemblyTokenLimitFromContextPolicy(cfg config.ContextConfig) (int, error) {
	thresholds, err := pressureThresholds(ModelProfileFromContextPolicy(cfg))
	if err != nil {
		return 0, err
	}
	return thresholds.warning, nil
}

func (g *BudgetGovernor) Evaluate(ctx context.Context, req BudgetEvaluateRequest) (BudgetPressure, error) {
	if g == nil {
		return BudgetPressure{}, errors.New("budget governor is not initialized")
	}
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
	pressure := BudgetPressure{
		EstimatedInputTokens:       estimated,
		EffectiveWindowTokens:      thresholds.effectiveWindow,
		WarningThresholdTokens:     thresholds.warning,
		AutoCompactThresholdTokens: thresholds.autoCompact,
		BlockingThresholdTokens:    thresholds.blocking,
		PercentUsed:                estimated * 100 / thresholds.effectiveWindow,
		State:                      pressureState(estimated, thresholds),
	}
	return pressure, nil
}

func (g *BudgetGovernor) AutoCompactThreshold(profile ModelProfile) (int, error) {
	if g == nil {
		return 0, errors.New("budget governor is not initialized")
	}
	thresholds, err := pressureThresholds(profile)
	if err != nil {
		return 0, err
	}
	return thresholds.autoCompact, nil
}

type pressureThresholdSet struct {
	effectiveWindow int
	warning         int
	autoCompact     int
	blocking        int
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
	if profile.BlockingBufferTokens <= 0 {
		return pressureThresholdSet{}, errors.New("model profile blocking buffer tokens must be positive")
	}
	if profile.WarningBufferTokens <= profile.AutoCompactBufferTokens {
		return pressureThresholdSet{}, errors.New("model profile warning buffer must be greater than auto compact buffer")
	}
	if profile.AutoCompactBufferTokens <= profile.BlockingBufferTokens {
		return pressureThresholdSet{}, errors.New("model profile auto compact buffer must be greater than blocking buffer")
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
		blocking:        effectiveWindow - profile.BlockingBufferTokens,
	}, nil
}

func pressureState(tokens int, thresholds pressureThresholdSet) BudgetPressureState {
	switch {
	case tokens >= thresholds.blocking:
		return PressureBlocking
	case tokens >= thresholds.autoCompact:
		return PressureAutoCompact
	case tokens >= thresholds.warning:
		return PressureWarning
	default:
		return PressureOK
	}
}
