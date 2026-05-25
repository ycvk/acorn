package contextplane

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
)

type CompressionBuildOptions struct {
	RuntimeStorageDir string
	TokenCounter      *CompressionTokenCounter
	State             any
	EmitCompressed    func(context.Context, CompressionOutcome) error
	EmitPressure      func(context.Context, BudgetPressure) error
}

type CompressionPipeline interface {
	Build(context.Context, config.ContextConfig, einomodel.BaseChatModel, CompressionBuildOptions) ([]adk.ChatModelAgentMiddleware, error)
}

type defaultCompressionPipeline struct{}

func NewCompressionPipeline() CompressionPipeline {
	return defaultCompressionPipeline{}
}

func (p *defaultContextPlane) BuildHandlers(
	ctx context.Context,
	cfg config.ContextConfig,
	chatModel einomodel.BaseChatModel,
	opts CompressionBuildOptions,
) ([]adk.ChatModelAgentMiddleware, error) {
	if p == nil {
		return nil, errors.New("context plane is not initialized")
	}
	return p.compressionPipeline.Build(ctx, cfg, chatModel, opts)
}

func (defaultCompressionPipeline) Build(
	ctx context.Context,
	cfg config.ContextConfig,
	chatModel einomodel.BaseChatModel,
	opts CompressionBuildOptions,
) ([]adk.ChatModelAgentMiddleware, error) {
	if chatModel == nil {
		return nil, errors.New("compression requires a chat model")
	}
	tokenCounter := opts.TokenCounter
	if tokenCounter == nil {
		var err error
		tokenCounter, err = NewCompressionTokenCounter(cfg)
		if err != nil {
			return nil, err
		}
	}
	governor := NewBudgetGovernor(tokenCounter)
	modelProfile := ModelProfileFromContextPolicy(cfg)
	if _, err := governor.AutoCompactThreshold(modelProfile); err != nil {
		return nil, fmt.Errorf("compute compression auto compact threshold: %w", err)
	}
	if cfg.PreserveRecentTurns <= 0 {
		return nil, errors.New("compression preserve recent turns must be positive")
	}

	engine := NewDefaultCompactionEngine(CompactionEngineOptions{
		Model:                chatModel,
		ModelOptions:         []einomodel.Option{einomodel.WithMaxTokens(cfg.SummaryMaxTokens)},
		TokenCounter:         tokenCounter,
		HandoffFrameDisabled: cfg.HandoffFrameDisabled,
		MaxSummaryTokens:     cfg.SummaryMaxTokens,
	})
	pipeline := NewDefaultContextCompressionPipeline(CompressionPipelineOptions{
		Governor:             governor,
		CompactionEngine:     engine,
		TokenCounter:         tokenCounter,
		MicrocompactInterval: 5,
		ModelProfile:         modelProfile,
	})

	compressionMW, err := newPipelineCompressionMiddleware(pipelineCompressionMiddlewareOptions{
		Pipeline:       pipeline,
		Governor:       governor,
		ModelProfile:   modelProfile,
		PreservePolicy: PreservePolicy{RecentTurns: cfg.PreserveRecentTurns, PreserveToolPairs: true},
		State:          opts.State,
		EmitCompressed: opts.EmitCompressed,
		EmitPressure:   opts.EmitPressure,
	})
	if err != nil {
		return nil, fmt.Errorf("build pipeline compression middleware: %w", err)
	}

	patchMW, err := patchtoolcalls.New(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("build patchtoolcalls middleware: %w", err)
	}
	lifecycleMW := newToolLifecycleMiddleware()
	return []adk.ChatModelAgentMiddleware{patchMW, lifecycleMW, compressionMW}, nil
}

type pipelineCompressionMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	pipeline        ContextCompressionPipeline
	governor        BudgetGovernor
	modelProfile    ModelProfile
	preservePolicy  PreservePolicy
	state           any
	emitCompressed  func(context.Context, CompressionOutcome) error
	emitPressure    func(context.Context, BudgetPressure) error
	turnIndex       int
	lastCompactTurn int
	mu              sync.Mutex
}

type pipelineCompressionMiddlewareOptions struct {
	Pipeline       ContextCompressionPipeline
	Governor       BudgetGovernor
	ModelProfile   ModelProfile
	PreservePolicy PreservePolicy
	State          any
	EmitCompressed func(context.Context, CompressionOutcome) error
	EmitPressure   func(context.Context, BudgetPressure) error
}

func newPipelineCompressionMiddleware(opts pipelineCompressionMiddlewareOptions) (*pipelineCompressionMiddleware, error) {
	if opts.Pipeline == nil {
		return nil, errors.New("pipeline compression middleware pipeline is required")
	}
	if opts.Governor == nil {
		return nil, errors.New("pipeline compression middleware governor is required")
	}
	if opts.PreservePolicy.RecentTurns <= 0 {
		return nil, errors.New("pipeline compression middleware preserve policy recent turns must be positive")
	}
	return &pipelineCompressionMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		pipeline:                     opts.Pipeline,
		governor:                     opts.Governor,
		modelProfile:                 opts.ModelProfile,
		preservePolicy:               opts.PreservePolicy,
		state:                        opts.State,
		emitCompressed:               opts.EmitCompressed,
		emitPressure:                 opts.EmitPressure,
	}, nil
}

func (m *pipelineCompressionMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if m == nil {
		return nil, nil, errors.New("pipeline compression middleware is not initialized")
	}
	if state == nil {
		return nil, nil, errors.New("pipeline compression middleware state is required")
	}

	var tools []*schema.ToolInfo
	if mc != nil {
		tools = mc.Tools
	}

	pressure, err := m.governor.Evaluate(ctx, BudgetEvaluateRequest{
		Profile:  m.modelProfile,
		Messages: state.Messages,
		Tools:    tools,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("evaluate pipeline compression pressure: %w", err)
	}
	if err := m.emitPressureEvent(ctx, pressure); err != nil {
		return nil, nil, err
	}
	if !shouldCompactForPressure(pressure.State) {
		return ctx, state, nil
	}

	m.mu.Lock()
	m.turnIndex++
	currentTurn := m.turnIndex
	lastCompact := m.lastCompactTurn
	m.mu.Unlock()

	result, err := m.pipeline.Compress(ctx, PipelineRequest{
		Messages:        state.Messages,
		ToolInfos:       append([]*schema.ToolInfo(nil), tools...),
		Trigger:         CompactTriggerAuto,
		TurnIndex:       currentTurn,
		LastCompactTurn: lastCompact,
		Pressure:        pressure,
		PreservePolicy:  m.preservePolicy,
		ModelProfile:    m.modelProfile,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("pipeline auto compact: %w", err)
	}
	if result == nil {
		return nil, nil, errors.New("pipeline auto compact returned nil")
	}

	m.mu.Lock()
	m.lastCompactTurn = currentTurn
	m.mu.Unlock()

	if result.Outcome != nil {
		outcome := *result.Outcome
		outcome.LayersApplied = append([]CompactLayer(nil), result.LayersApplied...)
		if s, ok := m.state.(*CompressionState); ok && s != nil {
			s.RecordCompression(outcome.Summary)
		}
		if m.emitCompressed != nil {
			if err := m.emitCompressed(ctx, outcome); err != nil {
				return nil, nil, fmt.Errorf("emit pipeline compression event: %w", err)
			}
		}
	}

	afterState := *state
	afterState.Messages = result.Messages
	return ctx, &afterState, nil
}

func (m *pipelineCompressionMiddleware) WrapModel(_ context.Context, model einomodel.BaseChatModel, mc *adk.ModelContext) (einomodel.BaseChatModel, error) {
	if m == nil {
		return nil, errors.New("pipeline compression middleware is not initialized")
	}
	if model == nil {
		return nil, errors.New("pipeline compression middleware model is required")
	}
	var tools []*schema.ToolInfo
	if mc != nil {
		tools = mc.Tools
	}
	return &reactivePipelineModel{
		inner:      model,
		middleware: m,
		tools:      append([]*schema.ToolInfo(nil), tools...),
	}, nil
}

func (m *pipelineCompressionMiddleware) emitPressureEvent(ctx context.Context, pressure BudgetPressure) error {
	if m.emitPressure == nil {
		return nil
	}
	if err := m.emitPressure(ctx, pressure); err != nil {
		return fmt.Errorf("emit pipeline compression pressure event: %w", err)
	}
	return nil
}

type reactivePipelineModel struct {
	inner      einomodel.BaseChatModel
	middleware *pipelineCompressionMiddleware
	tools      []*schema.ToolInfo
}

func (m *reactivePipelineModel) Generate(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	if ContextSessionFromContext(ctx) != nil {
		return m.inner.Generate(ctx, input, opts...)
	}
	compacted, err := m.preFlightCompact(ctx, input)
	if err != nil {
		return nil, err
	}
	msg, err := m.inner.Generate(ctx, compacted, opts...)
	if !IsContextOverflowError(err) {
		return msg, err
	}
	compacted, compactErr := m.reactiveCompact(ctx, compacted)
	if compactErr != nil {
		return nil, compactErr
	}
	msg, retryErr := m.inner.Generate(ctx, compacted, opts...)
	if retryErr != nil {
		return nil, fmt.Errorf("retry model call after reactive pipeline compact: %w", retryErr)
	}
	return msg, nil
}

func (m *reactivePipelineModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	if ContextSessionFromContext(ctx) != nil {
		return m.inner.Stream(ctx, input, opts...)
	}
	compacted, err := m.preFlightCompact(ctx, input)
	if err != nil {
		return nil, err
	}
	stream, err := m.inner.Stream(ctx, compacted, opts...)
	if !IsContextOverflowError(err) {
		return stream, err
	}
	compacted, compactErr := m.reactiveCompact(ctx, compacted)
	if compactErr != nil {
		return nil, compactErr
	}
	stream, retryErr := m.inner.Stream(ctx, compacted, opts...)
	if retryErr != nil {
		return nil, fmt.Errorf("retry model stream after reactive pipeline compact: %w", retryErr)
	}
	return stream, nil
}

func (m *reactivePipelineModel) preFlightCompact(ctx context.Context, input []*schema.Message) ([]*schema.Message, error) {
	if m == nil || m.middleware == nil {
		return input, nil
	}

	m.middleware.mu.Lock()
	m.middleware.turnIndex++
	currentTurn := m.middleware.turnIndex
	lastCompact := m.middleware.lastCompactTurn
	m.middleware.mu.Unlock()

	pressure, err := m.middleware.governor.Evaluate(ctx, BudgetEvaluateRequest{
		Profile:  m.middleware.modelProfile,
		Messages: input,
		Tools:    append([]*schema.ToolInfo(nil), m.tools...),
	})
	if err != nil {
		return nil, fmt.Errorf("evaluate pre-flight pipeline pressure: %w", err)
	}
	if err := m.middleware.emitPressureEvent(ctx, pressure); err != nil {
		return nil, err
	}
	if !shouldCompactForPressure(pressure.State) {
		return input, nil
	}

	return m.executeCompact(ctx, input, pressure, currentTurn, lastCompact, CompactTriggerAuto, "pre-flight")
}

func (m *reactivePipelineModel) reactiveCompact(ctx context.Context, input []*schema.Message) ([]*schema.Message, error) {
	if m == nil || m.middleware == nil {
		return nil, errors.New("reactive pipeline model is not initialized")
	}

	pressure, err := m.middleware.governor.Evaluate(ctx, BudgetEvaluateRequest{
		Profile:  m.middleware.modelProfile,
		Messages: input,
		Tools:    append([]*schema.ToolInfo(nil), m.tools...),
	})
	if err != nil {
		return nil, fmt.Errorf("evaluate reactive pipeline pressure: %w", err)
	}
	if err := m.middleware.emitPressureEvent(ctx, pressure); err != nil {
		return nil, err
	}

	m.middleware.mu.Lock()
	currentTurn := m.middleware.turnIndex
	lastCompact := m.middleware.lastCompactTurn
	m.middleware.mu.Unlock()

	return m.executeCompact(ctx, input, pressure, currentTurn, lastCompact, CompactTriggerReactive, "reactive")
}

func (m *reactivePipelineModel) executeCompact(ctx context.Context, input []*schema.Message, pressure BudgetPressure, currentTurn, lastCompact int, trigger CompactTrigger, phase string) ([]*schema.Message, error) {
	result, err := m.middleware.pipeline.Compress(ctx, PipelineRequest{
		Messages:        input,
		ToolInfos:       append([]*schema.ToolInfo(nil), m.tools...),
		Trigger:         trigger,
		TurnIndex:       currentTurn,
		LastCompactTurn: lastCompact,
		Pressure:        pressure,
		PreservePolicy:  m.middleware.preservePolicy,
		ModelProfile:    m.middleware.modelProfile,
	})
	if err != nil {
		return nil, fmt.Errorf("%s pipeline compact: %w", phase, err)
	}
	if result == nil {
		return nil, fmt.Errorf("%s pipeline compact returned nil", phase)
	}

	m.middleware.mu.Lock()
	m.middleware.lastCompactTurn = currentTurn
	m.middleware.mu.Unlock()

	if result.Outcome != nil {
		outcome := *result.Outcome
		outcome.LayersApplied = append([]CompactLayer(nil), result.LayersApplied...)
		if s, ok := m.middleware.state.(*CompressionState); ok && s != nil {
			s.RecordCompression(outcome.Summary)
		}
		if m.middleware.emitCompressed != nil {
			if err := m.middleware.emitCompressed(ctx, outcome); err != nil {
				return nil, fmt.Errorf("emit %s pipeline compression event: %w", phase, err)
			}
		}
	}

	return result.Messages, nil
}
