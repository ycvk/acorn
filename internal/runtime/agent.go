package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/dynamictool/toolsearch"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
)

type agentRunnerRequest struct {
	RunID       string
	ChatModel   einomodel.AgenticModel
	Catalog     *tools.Catalog
	Skills      *skills.Snapshot
	Instruction string
	FailedCalls *failedToolCalls
}

// buildAgentRunner assembles the per-run ChatModelAgent. Context management,
// deferred tool discovery and approvals are middleware; checkpoints persist
// through the session store so interrupted runs resume after a restart.
func buildAgentRunner(ctx context.Context, deps RuntimeDeps, req agentRunnerRequest) (*adk.TypedRunner[*schema.AgenticMessage], error) {
	built, err := BuildAuditedTools(ctx, req.Catalog.EnabledSpecs())
	if err != nil {
		return nil, err
	}
	eager, deferred, err := splitToolsByLoading(ctx, req.Catalog.EnabledSpecs(), built)
	if err != nil {
		return nil, err
	}
	handlers, err := buildAgentHandlers(ctx, deps, req, deferred)
	if err != nil {
		return nil, err
	}
	agent, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        deps.Config.Agent.Name,
		Description: deps.Config.Agent.Description,
		Instruction: req.Instruction,
		Model:       req.ChatModel,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               eager,
			ExecuteSequentially: true,
			UnknownToolsHandler: unknownToolResult,
		}},
		MaxIterations:    deps.Config.Agent.MaxIterations,
		Handlers:         handlers,
		ModelRetryConfig: modelRetryConfig(),
	})
	if err != nil {
		return nil, fmt.Errorf("build chat model agent: %w", err)
	}
	return adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:           agent,
		EnableStreaming: true,
		CheckPointStore: storeCheckpointStore{store: deps.Store},
	}), nil
}

// modelRetryConfig retries a failed main model call up to three times. Deltas
// already streamed from a failed attempt stay with the client; the assistant
// message of the successful attempt replaces them.
func modelRetryConfig() *adk.TypedModelRetryConfig[*schema.AgenticMessage] {
	return &adk.TypedModelRetryConfig[*schema.AgenticMessage]{
		MaxRetries: 3,
		ShouldRetry: func(_ context.Context, retry *adk.TypedRetryContext[*schema.AgenticMessage]) *adk.TypedRetryDecision[*schema.AgenticMessage] {
			err := retry.Err
			return &adk.TypedRetryDecision[*schema.AgenticMessage]{Retry: err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)}
		},
	}
}

// splitToolsByLoading separates eager tools from deferred ones; deferred tools
// are only reachable through the tool_search middleware.
func splitToolsByLoading(ctx context.Context, specs []core.ToolSpec, built []einotool.BaseTool) (eager, deferred []einotool.BaseTool, err error) {
	modes := make(map[string]core.ToolLoadingMode, len(specs))
	for _, spec := range specs {
		modes[strings.TrimSpace(spec.Name)] = spec.Loading.Mode
	}
	for _, t := range built {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("read tool info: %w", err)
		}
		switch modes[info.Name] {
		case core.ToolLoadingModeDeferred:
			deferred = append(deferred, t)
		case core.ToolLoadingModeHidden:
		default:
			eager = append(eager, t)
		}
	}
	return eager, deferred, nil
}

func buildAgentHandlers(ctx context.Context, deps RuntimeDeps, req agentRunnerRequest, deferred []einotool.BaseTool) ([]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	counter, err := NewTokenCounter()
	if err != nil {
		return nil, fmt.Errorf("token counter: %w", err)
	}
	inputBudget, err := deps.Config.InputTokenBudget()
	if err != nil {
		return nil, err
	}
	patch, err := patchtoolcalls.NewTyped[*schema.AgenticMessage](ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("patchtoolcalls middleware: %w", err)
	}
	summarize, err := summarization.NewTyped[*schema.AgenticMessage](ctx, &summarization.TypedConfig[*schema.AgenticMessage]{
		Model:   req.ChatModel,
		Trigger: &summarization.TriggerCondition{ContextTokens: inputBudget},
		TokenCounter: func(ctx context.Context, in *summarization.TypedTokenCounterInput[*schema.AgenticMessage]) (int, error) {
			return counter.CountMessages(ctx, in.Messages, in.Tools)
		},
		// A transient provider error while summarizing would otherwise fail the run.
		Retry: &summarization.TypedRetryConfig[*schema.AgenticMessage]{},
	})
	if err != nil {
		return nil, fmt.Errorf("summarization middleware: %w", err)
	}
	reduce, err := reduction.NewTyped[*schema.AgenticMessage](ctx, &reduction.TypedConfig[*schema.AgenticMessage]{
		SkipTruncation:            true,
		MaxTokensForClear:         int64(inputBudget * 3 / 4),
		ClearRetentionSuffixLimit: deps.Config.Context.MaskAfterTurns,
		TokenCounter: func(ctx context.Context, msgs []*schema.AgenticMessage, tools []*schema.ToolInfo) (int64, error) {
			n, err := counter.CountMessages(ctx, msgs, tools)
			return int64(n), err
		},
	})
	if err != nil {
		return nil, fmt.Errorf("reduction middleware: %w", err)
	}
	approval, err := newApprovalMiddleware(deps.Config.Approval.Require, deps.Store)
	if err != nil {
		return nil, err
	}
	present, err := newPresenceMiddleware(deps, counter, req.RunID)
	if err != nil {
		return nil, err
	}
	skillHandler, err := newSkillMiddleware(ctx, req.Skills)
	if err != nil {
		return nil, err
	}
	handlers := []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{patch, summarize, reduce}
	if len(deferred) > 0 {
		search, err := toolsearch.NewTyped[*schema.AgenticMessage](ctx, &toolsearch.Config{DynamicTools: deferred})
		if err != nil {
			return nil, fmt.Errorf("toolsearch middleware: %w", err)
		}
		handlers = append(handlers, search)
	}
	// Earlier handlers wrap later ones. The skill handler adds its tool and
	// instruction before presence reads the final instruction. Presence
	// follows summarization so the present is not counted toward compaction
	// (presence.max_tokens bounds it). Approval sits outside the tool error
	// handler so a failure to record an approval fails the run instead of
	// reaching the model as a tool error.
	handlers = append(handlers, skillHandler, present, approval, newToolErrorMiddleware(req.FailedCalls))
	return handlers, nil
}
