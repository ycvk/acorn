package compaction

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/adk"

	"github.com/ycvk/acorn/internal/contextplane"
)

var ErrPipelineNotInitialized = errors.New("compression pipeline is not initialized")
var ErrPipelineBudgetGovernorRequired = errors.New("compression pipeline budget governor is required")
var ErrPipelineCompactionEngineRequired = errors.New("compression pipeline compaction engine is required")
var ErrPipelineTokenCounterRequired = errors.New("compression pipeline token counter is required")

type defaultContextCompressionPipeline struct {
	governor     contextplane.BudgetGovernor
	autocompact  compactionEngine
	modelProfile contextplane.ModelProfile
	tokenCounter *contextplane.CompressionTokenCounter
}

type CompressionPipelineOptions struct {
	Governor         contextplane.BudgetGovernor
	CompactionEngine compactionEngine
	TokenCounter     *contextplane.CompressionTokenCounter
	ModelProfile     contextplane.ModelProfile
}

func NewDefaultContextCompressionPipeline(opts CompressionPipelineOptions) *defaultContextCompressionPipeline {
	return &defaultContextCompressionPipeline{
		governor:     opts.Governor,
		autocompact:  opts.CompactionEngine,
		modelProfile: opts.ModelProfile,
		tokenCounter: opts.TokenCounter,
	}
}

func (p *defaultContextCompressionPipeline) Compress(ctx context.Context, req contextplane.PipelineRequest) (*contextplane.PipelineResult, error) {
	if p == nil {
		return nil, ErrPipelineNotInitialized
	}
	if p.governor == nil {
		return nil, ErrPipelineBudgetGovernorRequired
	}
	if p.tokenCounter == nil {
		return nil, ErrPipelineTokenCounterRequired
	}
	if p.autocompact == nil {
		return nil, ErrPipelineCompactionEngineRequired
	}

	messages := contextplane.CloneContextSessionMessages(req.Messages)
	totalFreed := 0
	var finalOutcome *contextplane.CompressionOutcome

	// ── Proactive: Autocompact ──
	if req.Trigger == contextplane.CompactTriggerAuto || req.Trigger == contextplane.CompactTriggerReactive {
		shouldCompact, err := p.shouldAutocompact(ctx, messages, req)
		if err != nil {
			return nil, fmt.Errorf("check autocompact pressure: %w", err)
		}
		if req.Trigger == contextplane.CompactTriggerAuto && !shouldCompact {
			return p.buildResult(messages, totalFreed, finalOutcome), nil
		}

		acResult, err := p.runAutocompact(ctx, messages, req)
		if err != nil {
			return nil, fmt.Errorf("autocompact: %w", err)
		}
		messages = acResult.Messages
		totalFreed += acResult.Outcome.TokensBefore - acResult.Outcome.TokensAfter
		finalOutcome = &acResult.Outcome

		if ok, err := p.pressureOK(ctx, messages, req); err != nil {
			return nil, fmt.Errorf("pressure check after autocompact: %w", err)
		} else if ok {
			return p.buildResult(messages, totalFreed, finalOutcome), nil
		}
	}

	// ── Reactive: Final compact ──
	if req.Trigger == contextplane.CompactTriggerReactive {
		rcResult, err := p.runReactiveCompact(ctx, messages, req)
		if err != nil {
			return nil, fmt.Errorf("reactivecompact: %w", err)
		}
		messages = rcResult.Messages
		totalFreed += rcResult.Outcome.TokensBefore - rcResult.Outcome.TokensAfter
		finalOutcome = &rcResult.Outcome

		if ok, err := p.pressureOK(ctx, messages, req); err != nil {
			return nil, fmt.Errorf("pressure check after reactivecompact: %w", err)
		} else if ok {
			return p.buildResult(messages, totalFreed, finalOutcome), nil
		}
	}

	// If reactive trigger and we still cannot get pressure below the compact-now threshold, fail loudly.
	if req.Trigger == contextplane.CompactTriggerReactive {
		return nil, errors.New("reactive compact exhausted all layers but context pressure still requires compaction")
	}

	return p.buildResult(messages, totalFreed, finalOutcome), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (p *defaultContextCompressionPipeline) shouldAutocompact(ctx context.Context, messages []adk.Message, req contextplane.PipelineRequest) (bool, error) {
	pressure, err := p.governor.Evaluate(ctx, contextplane.BudgetEvaluateRequest{
		Profile:  p.profileForRequest(req),
		Messages: messages,
		Tools:    req.ToolInfos,
	})
	if err != nil {
		return false, err
	}
	return pressure.State == contextplane.PressureAutoCompact, nil
}

func (p *defaultContextCompressionPipeline) runAutocompact(ctx context.Context, messages []adk.Message, req contextplane.PipelineRequest) (*CompactionResult, error) {
	return p.autocompact.Compact(ctx, CompactRequest{
		Trigger:            req.Trigger,
		Messages:           messages,
		ToolInfos:          req.ToolInfos,
		ToolState:          req.ToolState,
		Pressure:           req.Pressure,
		PreviousSummary:    req.PreviousSummary,
		PreservePolicy:     req.PreservePolicy,
		CurrentPlan:        req.CurrentPlan,
		RecentTouchedPaths: req.RecentTouchedPaths,
	})
}

func (p *defaultContextCompressionPipeline) runReactiveCompact(ctx context.Context, messages []adk.Message, req contextplane.PipelineRequest) (*CompactionResult, error) {
	aggressivePolicy := req.PreservePolicy
	aggressivePolicy.RecentTurns = max(1, aggressivePolicy.RecentTurns/2)
	return p.autocompact.Compact(ctx, CompactRequest{
		Trigger:        contextplane.CompactTriggerReactive,
		Messages:       messages,
		ToolInfos:      req.ToolInfos,
		ToolState:      req.ToolState,
		Pressure:       req.Pressure,
		PreservePolicy: aggressivePolicy,
	})
}

func (p *defaultContextCompressionPipeline) pressureOK(ctx context.Context, messages []adk.Message, req contextplane.PipelineRequest) (bool, error) {
	pressure, err := p.governor.Evaluate(ctx, contextplane.BudgetEvaluateRequest{
		Profile:  p.profileForRequest(req),
		Messages: messages,
		Tools:    req.ToolInfos,
	})
	if err != nil {
		return false, err
	}
	return pressure.State == contextplane.PressureOK, nil
}

func (p *defaultContextCompressionPipeline) profileForRequest(req contextplane.PipelineRequest) contextplane.ModelProfile {
	if req.ModelProfile.ContextWindowTokens > 0 {
		return req.ModelProfile
	}
	return p.modelProfile
}

func (p *defaultContextCompressionPipeline) buildResult(messages []adk.Message, tokensFreed int, outcome *contextplane.CompressionOutcome) *contextplane.PipelineResult {
	return &contextplane.PipelineResult{
		Messages:    contextplane.CloneContextSessionMessages(messages),
		TokensFreed: tokensFreed,
		Outcome:     outcome,
	}
}
