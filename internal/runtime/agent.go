package runtime

import (
	"context"
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
	"github.com/ycvk/acorn/internal/tools"
)

type agentRunnerRequest struct {
	RunID             string
	ChatModel         einomodel.BaseChatModel
	Catalog           *tools.Catalog
	Instruction       string
	AllowedToolNames  []string
	ExcludedToolNames []string
}

// buildAgentRunner assembles the per-run ChatModelAgent. Context management,
// deferred tool discovery and approvals are middleware; checkpoints persist
// through the session store so interrupted runs resume after a restart.
func buildAgentRunner(ctx context.Context, deps RuntimeDeps, req agentRunnerRequest) (*adk.Runner, error) {
	built, err := BuildAuditedTools(ctx, deps.Store, req.Catalog.EnabledSpecs(), req.ExcludedToolNames, req.AllowedToolNames, req.RunID)
	if err != nil {
		return nil, err
	}
	eager, deferred, err := splitToolsByLoading(ctx, req.Catalog.EnabledSpecs(), built)
	if err != nil {
		return nil, err
	}
	handlers, err := buildAgentHandlers(ctx, deps, req.ChatModel, deferred)
	if err != nil {
		return nil, err
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        deps.Config.Agent.Name,
		Description: deps.Config.Agent.Description,
		Instruction: req.Instruction,
		Model:       req.ChatModel,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               eager,
			ExecuteSequentially: true,
			UnknownToolsHandler: unknownToolResult,
		}},
		MaxIterations: deps.Config.Agent.MaxIterations,
		Handlers:      handlers,
	})
	if err != nil {
		return nil, fmt.Errorf("build chat model agent: %w", err)
	}
	return adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
		CheckPointStore: storeCheckpointStore{store: deps.Store},
	}), nil
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

func buildAgentHandlers(ctx context.Context, deps RuntimeDeps, chatModel einomodel.BaseChatModel, deferred []einotool.BaseTool) ([]adk.ChatModelAgentMiddleware, error) {
	counter, err := NewTokenCounter()
	if err != nil {
		return nil, fmt.Errorf("token counter: %w", err)
	}
	window := deps.Config.Context.WindowTokens
	patch, err := patchtoolcalls.New(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("patchtoolcalls middleware: %w", err)
	}
	summarize, err := summarization.New(ctx, &summarization.Config{
		Model:   chatModel,
		Trigger: &summarization.TriggerCondition{ContextTokens: window - deps.Config.Context.CompactMarginTokens},
		TokenCounter: func(ctx context.Context, in *summarization.TokenCounterInput) (int, error) {
			return counter.CountMessages(ctx, in.Messages, in.Tools)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("summarization middleware: %w", err)
	}
	reduce, err := reduction.New(ctx, &reduction.Config{
		SkipTruncation:            true,
		MaxTokensForClear:         int64(window / 2),
		ClearRetentionSuffixLimit: deps.Config.Context.MaskAfterTurns,
		TokenCounter: func(ctx context.Context, msgs []*schema.Message, tools []*schema.ToolInfo) (int64, error) {
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
	handlers := []adk.ChatModelAgentMiddleware{patch, summarize, reduce}
	if len(deferred) > 0 {
		search, err := toolsearch.New(ctx, &toolsearch.Config{DynamicTools: deferred})
		if err != nil {
			return nil, fmt.Errorf("toolsearch middleware: %w", err)
		}
		handlers = append(handlers, search)
	}
	// Earlier handlers wrap later ones. Approval sits outside the tool error
	// handler so a failure to record an approval fails the run instead of
	// reaching the model as an ordinary tool error.
	handlers = append(handlers, approval, newToolErrorMiddleware())
	return append(handlers, deps.Handlers...), nil
}

// buildAgentInstruction joins the stable instruction with the per-run context
// envelopes (memory, skill catalog). Keeping them in the system instruction
// keeps them out of summarization and gives the model a stable prefix.
func buildAgentInstruction(base, suffix string, contextMessages []*schema.Message) string {
	parts := []string{buildStableInstruction(base, suffix)}
	for _, msg := range contextMessages {
		if content := strings.TrimSpace(msg.Content); content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n")
}
