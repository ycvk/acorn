package contextplane

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/tooling"
)

var ErrPipelineNotInitialized = errors.New("compression pipeline is not initialized")
var ErrPipelineBudgetGovernorRequired = errors.New("compression pipeline budget governor is required")
var ErrPipelineCompactionEngineRequired = errors.New("compression pipeline compaction engine is required")
var ErrPipelineTokenCounterRequired = errors.New("compression pipeline token counter is required")

const clearedPlaceholder = "[Previous tool result content cleared]"
const turnIndexExtraKey = "acorn_turn_index"

type defaultContextCompressionPipeline struct {
	governor             budgetGovernor
	autocompact          compactionEngine
	microcompactInterval int
	modelProfile         ModelProfile
	catalog              *tooling.Catalog
	tokenCounter         *CompressionTokenCounter
}

type CompressionPipelineOptions struct {
	Governor             budgetGovernor
	CompactionEngine     compactionEngine
	TokenCounter         *CompressionTokenCounter
	Catalog              *tooling.Catalog
	MicrocompactInterval int
	ModelProfile         ModelProfile
}

func NewDefaultContextCompressionPipeline(opts CompressionPipelineOptions) *defaultContextCompressionPipeline {
	if opts.MicrocompactInterval <= 0 {
		opts.MicrocompactInterval = 5
	}
	return &defaultContextCompressionPipeline{
		governor:             opts.Governor,
		autocompact:          opts.CompactionEngine,
		microcompactInterval: opts.MicrocompactInterval,
		modelProfile:         opts.ModelProfile,
		catalog:              opts.Catalog,
		tokenCounter:         opts.TokenCounter,
	}
}

func (p *defaultContextCompressionPipeline) Compress(ctx context.Context, req PipelineRequest) (*PipelineResult, error) {
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

	messages := cloneMessages(req.Messages)
	layers := make([]CompactLayer, 0, 4)
	totalFreed := 0
	var finalOutcome *CompressionOutcome

	// ── Layer 1: Microcompact ──
	if p.shouldMicrocompact(req) {
		mcMessages, mcFreed, err := p.runMicrocompact(ctx, messages, req)
		if err != nil {
			return nil, fmt.Errorf("microcompact: %w", err)
		}
		messages = mcMessages
		totalFreed += mcFreed
		if mcFreed > 0 {
			layers = append(layers, CompactLayerMicrocompact)
		}

		if ok, err := p.pressureOK(ctx, messages, req); err != nil {
			return nil, fmt.Errorf("pressure check after microcompact: %w", err)
		} else if ok {
			return p.buildResult(messages, layers, totalFreed, finalOutcome), nil
		}
	}

	// ── Layer 2: Autocompact ──
	if req.Trigger == CompactTriggerAuto || req.Trigger == CompactTriggerReactive {
		shouldCompact, err := p.shouldAutocompact(ctx, messages, req)
		if err != nil {
			return nil, fmt.Errorf("check autocompact pressure: %w", err)
		}
		if req.Trigger == CompactTriggerAuto && !shouldCompact {
			return p.buildResult(messages, layers, totalFreed, finalOutcome), nil
		}

		acResult, err := p.runAutocompact(ctx, messages, req)
		if err != nil {
			return nil, fmt.Errorf("autocompact: %w", err)
		}
		messages = acResult.Messages
		totalFreed += acResult.Outcome.TokensBefore - acResult.Outcome.TokensAfter
		layers = append(layers, CompactLayerAutocompact)
		finalOutcome = &acResult.Outcome

		if ok, err := p.pressureOK(ctx, messages, req); err != nil {
			return nil, fmt.Errorf("pressure check after autocompact: %w", err)
		} else if ok {
			return p.buildResult(messages, layers, totalFreed, finalOutcome), nil
		}
	}

	// ── Layer 3: ReactiveCompact ──
	if req.Trigger == CompactTriggerReactive {
		rcResult, err := p.runReactiveCompact(ctx, messages, req)
		if err != nil {
			return nil, fmt.Errorf("reactivecompact: %w", err)
		}
		messages = rcResult.Messages
		layers = append(layers, CompactLayerReactive)

		if ok, err := p.pressureOK(ctx, messages, req); err != nil {
			return nil, fmt.Errorf("pressure check after reactivecompact: %w", err)
		} else if ok {
			return p.buildResult(messages, layers, totalFreed, nil), nil
		}
	}

	// If reactive trigger and we still cannot get pressure below blocking, fail loudly.
	if req.Trigger == CompactTriggerReactive {
		return nil, errors.New("reactive compact exhausted all layers but context pressure remains blocking")
	}

	return p.buildResult(messages, layers, totalFreed, finalOutcome), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (p *defaultContextCompressionPipeline) shouldMicrocompact(req PipelineRequest) bool {
	if req.Trigger == CompactTriggerReactive {
		return true
	}
	return req.TurnIndex-req.LastCompactTurn >= p.microcompactInterval
}

func (p *defaultContextCompressionPipeline) runMicrocompact(ctx context.Context, messages []adk.Message, req PipelineRequest) ([]adk.Message, int, error) {
	var cleared []string
	freed := 0

	for i, msg := range messages {
		if !isToolResultMessage(msg) {
			continue
		}
		toolName := extractToolName(msg)
		if toolName == "" {
			continue
		}
		if !p.isCompressibleTool(toolName) {
			continue
		}
		if isRecentResult(msg, req.TurnIndex) {
			continue
		}

		beforeTokens, err := p.countMessage(ctx, msg)
		if err != nil {
			return nil, 0, fmt.Errorf("count tool result before: %w", err)
		}
		messages[i] = replaceWithPlaceholder(msg)
		afterTokens, err := p.countMessage(ctx, messages[i])
		if err != nil {
			return nil, 0, fmt.Errorf("count tool result after: %w", err)
		}
		freed += beforeTokens - afterTokens
		cleared = append(cleared, toolName)
	}

	_ = cleared // unused for now, kept for future logging
	return messages, freed, nil
}

func (p *defaultContextCompressionPipeline) countMessage(ctx context.Context, msg adk.Message) (int, error) {
	if p.tokenCounter == nil {
		return 0, nil
	}
	return p.tokenCounter.CountMessages(ctx, []adk.Message{msg}, nil)
}

func isToolResultMessage(msg adk.Message) bool {
	if msg == nil {
		return false
	}
	return msg.Role == schema.Tool
}

func extractToolName(msg adk.Message) string {
	if msg == nil {
		return ""
	}
	name := strings.TrimSpace(msg.ToolName)
	if name != "" {
		return name
	}
	content := strings.TrimSpace(msg.Content)
	if idx := strings.Index(content, "tool:"); idx == 0 {
		rest := strings.TrimSpace(content[5:])
		if space := strings.IndexFunc(rest, func(r rune) bool { return r == ' ' || r == '\n' }); space > 0 {
			return rest[:space]
		}
	}
	return ""
}

func (p *defaultContextCompressionPipeline) isCompressibleTool(name string) bool {
	if p.catalog != nil {
		return p.catalog.IsCompressible(name)
	}
	return false
}

func isRecentResult(msg adk.Message, currentTurn int) bool {
	if msg == nil || msg.Extra == nil {
		return false
	}
	v, ok := msg.Extra[turnIndexExtraKey]
	if !ok {
		return false
	}
	msgTurn, ok := v.(int)
	if !ok {
		return false
	}
	return msgTurn == currentTurn
}

func replaceWithPlaceholder(msg adk.Message) adk.Message {
	if msg == nil {
		return msg
	}
	clone := *msg
	clone.Content = fmt.Sprintf("%s (tool: %s)", clearedPlaceholder, extractToolName(msg))
	clone.AssistantGenMultiContent = nil
	clone.UserInputMultiContent = nil
	return &clone
}

func (p *defaultContextCompressionPipeline) shouldAutocompact(ctx context.Context, messages []adk.Message, req PipelineRequest) (bool, error) {
	pressure, err := p.governor.Evaluate(ctx, BudgetEvaluateRequest{
		Profile:  p.profileForRequest(req),
		Messages: messages,
		Tools:    req.ToolInfos,
	})
	if err != nil {
		return false, err
	}
	return pressure.State == PressureAutoCompact || pressure.State == PressureBlocking, nil
}

func (p *defaultContextCompressionPipeline) runAutocompact(ctx context.Context, messages []adk.Message, req PipelineRequest) (*CompactionResult, error) {
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

func (p *defaultContextCompressionPipeline) runReactiveCompact(ctx context.Context, messages []adk.Message, req PipelineRequest) (*CompactionResult, error) {
	aggressivePolicy := req.PreservePolicy
	aggressivePolicy.RecentTurns = max(1, aggressivePolicy.RecentTurns/2)
	return p.autocompact.Compact(ctx, CompactRequest{
		Trigger:        CompactTriggerReactive,
		Messages:       messages,
		ToolInfos:      req.ToolInfos,
		ToolState:      req.ToolState,
		Pressure:       req.Pressure,
		PreservePolicy: aggressivePolicy,
	})
}

func (p *defaultContextCompressionPipeline) pressureOK(ctx context.Context, messages []adk.Message, req PipelineRequest) (bool, error) {
	pressure, err := p.governor.Evaluate(ctx, BudgetEvaluateRequest{
		Profile:  p.profileForRequest(req),
		Messages: messages,
		Tools:    req.ToolInfos,
	})
	if err != nil {
		return false, err
	}
	return pressure.State == PressureOK || pressure.State == PressureWarning, nil
}

func (p *defaultContextCompressionPipeline) profileForRequest(req PipelineRequest) ModelProfile {
	if req.ModelProfile.ContextWindowTokens > 0 {
		return req.ModelProfile
	}
	return p.modelProfile
}

func (p *defaultContextCompressionPipeline) buildResult(messages []adk.Message, layers []CompactLayer, tokensFreed int, outcome *CompressionOutcome) *PipelineResult {
	return &PipelineResult{
		Messages:      cloneMessages(messages),
		LayersApplied: append([]CompactLayer(nil), layers...),
		TokensFreed:   tokensFreed,
		Outcome:       outcome,
	}
}
