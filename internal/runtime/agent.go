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
	Memory      *runMemory
	ChatModel   einomodel.AgenticModel
	Catalog     *tools.Catalog
	Skills      *skills.Snapshot
	SkillDir    string
	Instruction string
	FailedCalls *failedToolCalls
	// Settings are the owner's settings the run started with.
	Settings core.Settings
	Reports  *subRunReportMiddleware
}

// subRunRunnerRequest is what a sub-run's agent is built from.
type subRunRunnerRequest struct {
	ChatModel   einomodel.AgenticModel
	Catalog     *tools.Catalog
	Skills      *skills.Snapshot
	SkillDir    string
	Rules       core.ApprovalRules
	FailedCalls *failedToolCalls
}

// The run's agent.
const (
	agentName        = "acorn"
	agentDescription = "Self-hosted personal agent that works on its owner's behalf and asks for approval before sensitive actions."
	subAgentName     = "acorn_sub_run"
	subAgentDesc     = "Sub-agent that works on one brief another Acorn run delegated."
	// maxIterations bounds the model calls of one run.
	maxIterations = 100
	// maskAfterTurns is how many of the most recent tool-call rounds stay
	// verbatim when older tool results are cleared to save context.
	maskAfterTurns = 8
)

// buildAgentRunner assembles the per-run ChatModelAgent. Context management,
// deferred tool discovery and approvals are middleware; checkpoints persist
// through the session store so interrupted runs resume after a restart.
func buildAgentRunner(ctx context.Context, deps RuntimeDeps, req agentRunnerRequest) (*adk.TypedRunner[*schema.AgenticMessage], error) {
	eager, deferred, err := buildRunTools(ctx, req.Catalog)
	if err != nil {
		return nil, err
	}
	handlers, err := buildAgentHandlers(ctx, deps, req, deferred)
	if err != nil {
		return nil, err
	}
	agent, err := newChatModelAgent(ctx, agentName, agentDescription, req.Instruction, req.ChatModel, eager, handlers)
	if err != nil {
		return nil, err
	}
	return adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:           agent,
		EnableStreaming: true,
		CheckPointStore: storeCheckpointStore{store: deps.Store, memory: deps.MemoryStore, visibility: req.Memory},
	}), nil
}

// buildSubRunRunner assembles a sub-run's agent: no memory, presence or
// owner messages, and no checkpoints, since a sub-run never waits for the
// owner.
func buildSubRunRunner(ctx context.Context, deps RuntimeDeps, req subRunRunnerRequest) (*adk.TypedRunner[*schema.AgenticMessage], error) {
	eager, deferred, err := buildRunTools(ctx, req.Catalog)
	if err != nil {
		return nil, err
	}
	handlers, err := buildSubRunHandlers(ctx, deps, req, deferred)
	if err != nil {
		return nil, err
	}
	agent, err := newChatModelAgent(ctx, subAgentName, subAgentDesc, subRunInstruction, req.ChatModel, eager, handlers)
	if err != nil {
		return nil, err
	}
	return adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: agent, EnableStreaming: true}), nil
}

func buildRunTools(ctx context.Context, catalog *tools.Catalog) (eager, deferred []einotool.BaseTool, err error) {
	built, err := BuildAuditedTools(ctx, catalog.EnabledSpecs())
	if err != nil {
		return nil, nil, err
	}
	return splitToolsByLoading(ctx, catalog.EnabledSpecs(), built)
}

func newChatModelAgent(ctx context.Context, name, description, instruction string, chatModel einomodel.AgenticModel, eager []einotool.BaseTool, handlers []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]) (adk.TypedAgent[*schema.AgenticMessage], error) {
	agent, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        name,
		Description: description,
		Instruction: instruction,
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               eager,
			ExecuteSequentially: true,
			UnknownToolsHandler: unknownToolResult,
		}},
		MaxIterations:    maxIterations,
		Handlers:         handlers,
		ModelRetryConfig: modelRetryConfig(),
	})
	if err != nil {
		return nil, fmt.Errorf("build chat model agent: %w", err)
	}
	return agent, nil
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
	if req.Reports == nil {
		return nil, errors.New("run handlers require the sub-run report middleware")
	}
	counter, err := NewTokenCounter()
	if err != nil {
		return nil, fmt.Errorf("token counter: %w", err)
	}
	inputBudget, err := deps.Config.InputTokenBudget()
	if err != nil {
		return nil, err
	}
	inputBudget -= req.Memory.budget + 64
	contextHandlers, err := newContextHandlers(ctx, deps, req.ChatModel, counter, inputBudget)
	if err != nil {
		return nil, err
	}
	approval, err := newApprovalMiddleware(req.Settings.Approval, deps.Reviewer, deps.Describer, deps.Store)
	if err != nil {
		return nil, err
	}
	present, err := newPresenceMiddleware(deps, counter, req.RunID, req.Settings.Location)
	if err != nil {
		return nil, err
	}
	present.runMemory = req.Memory
	skillHandler, err := newSkillMiddleware(ctx, req.Skills, req.SkillDir)
	if err != nil {
		return nil, err
	}
	handlers := []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{
		&memoryVisibilityMiddleware{TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{}, memory: req.Memory},
		newInterjectionMiddleware(deps.Store, req.RunID, req.Memory),
		req.Reports,
	}
	handlers = append(handlers, contextHandlers...)
	if handlers, err = appendToolSearch(ctx, handlers, deferred); err != nil {
		return nil, err
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

// buildSubRunHandlers is the run's chain without memory visibility, owner
// messages, sub-run reports and presence; approval refuses what would wait
// for the owner.
func buildSubRunHandlers(ctx context.Context, deps RuntimeDeps, req subRunRunnerRequest, deferred []einotool.BaseTool) ([]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	counter, err := NewTokenCounter()
	if err != nil {
		return nil, fmt.Errorf("token counter: %w", err)
	}
	inputBudget, err := deps.Config.InputTokenBudget()
	if err != nil {
		return nil, err
	}
	handlers, err := newContextHandlers(ctx, deps, req.ChatModel, counter, inputBudget)
	if err != nil {
		return nil, err
	}
	if handlers, err = appendToolSearch(ctx, handlers, deferred); err != nil {
		return nil, err
	}
	approval, err := newApprovalMiddleware(req.Rules, deps.Reviewer, deps.Describer, deps.Store)
	if err != nil {
		return nil, err
	}
	approval.subRun = true
	skillHandler, err := newSkillMiddleware(ctx, req.Skills, req.SkillDir)
	if err != nil {
		return nil, err
	}
	return append(handlers, skillHandler, approval, newToolErrorMiddleware(req.FailedCalls)), nil
}

// newContextHandlers keep the agent's messages within inputBudget: missing
// tool results patched, older rounds summarized, and old tool results
// cleared.
func newContextHandlers(ctx context.Context, deps RuntimeDeps, chatModel einomodel.AgenticModel, counter TokenCounter, inputBudget int) ([]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	patch, err := patchtoolcalls.NewTyped[*schema.AgenticMessage](ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("patchtoolcalls middleware: %w", err)
	}
	summarize, err := newCompactionMiddleware(ctx, deps.Memory.MeterCompaction(chatModel), inputBudget, counter)
	if err != nil {
		return nil, fmt.Errorf("summarization middleware: %w", err)
	}
	reduce, err := reduction.NewTyped[*schema.AgenticMessage](ctx, &reduction.TypedConfig[*schema.AgenticMessage]{
		SkipTruncation:            true,
		MaxTokensForClear:         int64(inputBudget * 3 / 4),
		ClearRetentionSuffixLimit: maskAfterTurns,
		TokenCounter: func(ctx context.Context, msgs []*schema.AgenticMessage, tools []*schema.ToolInfo) (int64, error) {
			n, err := counter.CountMessages(ctx, msgs, tools)
			return int64(n), err
		},
	})
	if err != nil {
		return nil, fmt.Errorf("reduction middleware: %w", err)
	}
	return []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{patch, summarize, reduce}, nil
}

func appendToolSearch(ctx context.Context, handlers []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], deferred []einotool.BaseTool) ([]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	if len(deferred) == 0 {
		return handlers, nil
	}
	search, err := toolsearch.NewTyped[*schema.AgenticMessage](ctx, &toolsearch.Config{DynamicTools: deferred})
	if err != nil {
		return nil, fmt.Errorf("toolsearch middleware: %w", err)
	}
	return append(handlers, search), nil
}
