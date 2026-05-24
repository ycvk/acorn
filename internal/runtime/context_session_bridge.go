package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/orchestrationmode"
)

func (e *Executor) bootstrapContextSessionMessages(
	ctx context.Context,
	req ExecuteRequest,
	runID string,
	mode orchestrationmode.Mode,
	active *ActiveRunner,
) ([]adk.Message, error) {
	if e == nil || e.runBuilder == nil || e.runBuilder.Config() == nil {
		return nil, fmt.Errorf("context session bootstrap requires runtime config")
	}
	if active == nil {
		return nil, fmt.Errorf("context session bootstrap requires active runner")
	}
	contextPolicy, err := e.runBuilder.Config().ContextPolicy()
	if err != nil {
		return nil, fmt.Errorf("context policy: %w", err)
	}
	modelProfile := contextplane.ModelProfileFromContextPolicy(contextPolicy)
	counter, err := contextplane.NewCompressionTokenCounter(contextPolicy)
	if err != nil {
		return nil, fmt.Errorf("build context session token counter: %w", err)
	}
	pipeline := contextplane.NewDefaultContextCompressionPipeline(contextplane.CompressionPipelineOptions{
		Governor: contextplane.NewBudgetGovernor(counter),
		CompactionEngine: contextplane.NewDefaultCompactionEngine(contextplane.CompactionEngineOptions{
			Model:                active.ChatModel,
			ModelOptions:         []einomodel.Option{einomodel.WithMaxTokens(contextPolicy.MaxSummaryTokens)},
			TokenCounter:         counter,
			HandoffFrameDisabled: contextPolicy.HandoffFrameDisabled,
			MaxSummaryTokens:     contextPolicy.MaxSummaryTokens,
		}),
		TokenCounter:         counter,
		Catalog:              active.ToolCatalog,
		MicrocompactInterval: 5,
		ModelProfile:         modelProfile,
	})

	session := contextplane.NewDefaultContextSession(contextplane.ContextSessionOptions{
		BudgetGovernor: contextplane.NewBudgetGovernor(counter),
		Pipeline:       pipeline,
		PreservePolicy: contextplane.PreservePolicy{
			RecentTurns:       contextPolicy.PreserveRecentTurns,
			PreserveToolPairs: true,
		},
		State: active.CompressionState,
		EmitCompressed: func(ctx context.Context, outcome contextplane.CompressionOutcome) error {
			return EmitContextCompressedEvent(ctx, e.store, outcome)
		},
		EmitPressure: func(ctx context.Context, pressure contextplane.BudgetPressure) error {
			return EmitContextPressureEvent(ctx, e.store, pressure)
		},
	})
	initialMessages := append([]adk.Message(nil), req.Messages...)
	if mode == orchestrationmode.DirectResponse {
		if instruction := strings.TrimSpace(active.Instruction); instruction != "" {
			initialMessages = append([]adk.Message{schema.SystemMessage(instruction)}, initialMessages...)
		}
	}
	input, err := session.Bootstrap(ctx, contextplane.BootstrapRequest{
		SessionID:       req.SessionID,
		RunID:           runID,
		TurnIndex:       req.TurnIndex,
		Mode:            string(mode),
		InitialMessages: initialMessages,
		Assembly:        active.ContextResult,
		ModelProfile:    modelProfile,
	})
	if err != nil {
		return nil, err
	}
	active.ContextSession = session
	return input.Messages, nil
}
