package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/contextplane/compaction"
	"github.com/ycvk/acorn/internal/events"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
)

func (e *Executor) bootstrapContextSessionMessages(
	ctx context.Context,
	req runtimeapi.ExecuteRequest,
	runID string,
	mode events.OrchestrationMode,
	active *ActiveRunner,
) ([]adk.Message, error) {
	if e == nil || e.runRuntime == nil || e.runRuntime.Config() == nil {
		return nil, fmt.Errorf("context session bootstrap requires runtime config")
	}
	if active == nil {
		return nil, fmt.Errorf("context session bootstrap requires active runner")
	}
	contextPolicy, err := e.runRuntime.Config().ContextPolicy()
	if err != nil {
		return nil, fmt.Errorf("context policy: %w", err)
	}
	modelProfile := contextplane.ModelProfileFromContextPolicy(contextPolicy)
	counter, err := contextplane.NewCompressionTokenCounter(contextPolicy)
	if err != nil {
		return nil, fmt.Errorf("build context session token counter: %w", err)
	}
	pipeline := compaction.NewDefaultContextCompressionPipeline(compaction.CompressionPipelineOptions{
		Governor: contextplane.NewBudgetGovernor(counter),
		CompactionEngine: compaction.NewDefaultCompactionEngine(compaction.CompactionEngineOptions{
			Model:                active.ChatModel,
			ModelOptions:         []einomodel.Option{einomodel.WithMaxTokens(contextPolicy.SummaryMaxTokens)},
			TokenCounter:         counter,
			HandoffFrameDisabled: contextPolicy.HandoffFrameDisabled,
			MaxSummaryTokens:     contextPolicy.SummaryMaxTokens,
		}),
		TokenCounter: counter,
		ModelProfile: modelProfile,
	})

	session := contextplane.NewDefaultContextSession(contextplane.ContextSessionOptions{
		BudgetGovernor: contextplane.NewBudgetGovernor(counter),
		Pipeline:       pipeline,
		BoundaryStore:  e.store,
		PreservePolicy: contextplane.PreservePolicy{
			RecentTurns:       contextPolicy.PreserveRecentTurns,
			PreserveToolPairs: true,
		},
		State: active.CompressionState,
	})
	initialMessages := append([]adk.Message(nil), req.Messages...)
	if mode == events.ModeDirectResponse {
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
