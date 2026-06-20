package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
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
	if err := e.validateBootstrapDeps(active); err != nil {
		return nil, err
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
	session := e.buildContextSession(active, contextPolicy, modelProfile, counter)
	input, err := e.bootstrapSession(ctx, session, req, runID, mode, active, modelProfile)
	if err != nil {
		return nil, err
	}
	active.ContextSession = session
	return input.Messages, nil
}

func (e *Executor) bootstrapSession(ctx context.Context, session contextplane.ContextSession, req runtimeapi.ExecuteRequest, runID string, mode events.OrchestrationMode, active *ActiveRunner, modelProfile contextplane.ModelProfile) (*contextplane.ModelInput, error) {
	return session.Bootstrap(ctx, contextplane.BootstrapRequest{
		SessionID:       req.SessionID,
		RunID:           runID,
		TurnIndex:       req.TurnIndex,
		Mode:            string(mode),
		InitialMessages: prepareInitialMessages(req, mode, active),
		Assembly:        active.ContextResult,
		ModelProfile:    modelProfile,
	})
}

func (e *Executor) validateBootstrapDeps(active *ActiveRunner) error {
	if e == nil || e.runRuntime == nil || e.runRuntime.Config() == nil {
		return fmt.Errorf("context session bootstrap requires runtime config")
	}
	if active == nil {
		return fmt.Errorf("context session bootstrap requires active runner")
	}
	return nil
}

func (e *Executor) buildContextSession(active *ActiveRunner, contextPolicy config.ContextConfig, modelProfile contextplane.ModelProfile, counter *contextplane.CompressionTokenCounter) contextplane.ContextSession {
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
	return contextplane.NewDefaultContextSession(contextplane.ContextSessionOptions{
		BudgetGovernor: contextplane.NewBudgetGovernor(counter),
		Pipeline:       pipeline,
		BoundaryStore:  e.store,
		PreservePolicy: contextplane.PreservePolicy{
			RecentTurns:       contextPolicy.PreserveRecentTurns,
			PreserveToolPairs: true,
		},
		State: active.CompressionState,
	})
}

func prepareInitialMessages(req runtimeapi.ExecuteRequest, mode events.OrchestrationMode, active *ActiveRunner) []adk.Message {
	initialMessages := append([]adk.Message(nil), req.Messages...)
	if mode == events.ModeDirectResponse {
		if instruction := strings.TrimSpace(active.Instruction); instruction != "" {
			initialMessages = append([]adk.Message{schema.SystemMessage(instruction)}, initialMessages...)
		}
	}
	return initialMessages
}
