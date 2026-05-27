package compaction

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/providers"
)

type CompactRequest struct {
	Trigger            contextplane.CompactTrigger
	Messages           []adk.Message
	ToolInfos          []*schema.ToolInfo
	ToolState          *contextplane.ToolLifecycleState
	Pressure           contextplane.BudgetPressure
	PreviousSummary    string
	PreservePolicy     contextplane.PreservePolicy
	CurrentPlan        string
	RecentTouchedPaths []string
}

type CompactionResult struct {
	Messages         []adk.Message
	Summary          adk.Message
	PreservedTail    []adk.Message
	Rehydrated       []adk.Message
	RehydratePlan    *RehydratePlan
	SummaryInput     []adk.Message
	SummaryText      string
	RewriteFirst     int
	RewriteLast      int
	Outcome          contextplane.CompressionOutcome
	Pressure         contextplane.BudgetPressure
	CompactTrigger   contextplane.CompactTrigger
	OriginalMessages []adk.Message
}

type compactionEngine interface {
	Compact(context.Context, CompactRequest) (*CompactionResult, error)
}

type CompactionEngineOptions struct {
	Model                einomodel.BaseChatModel
	ModelOptions         []einomodel.Option
	TokenCounter         *contextplane.CompressionTokenCounter
	HandoffFrameDisabled bool
	MaxSummaryTokens     int
}

type CompactionEngine struct {
	model                einomodel.BaseChatModel
	modelOptions         []einomodel.Option
	tokenCounter         *contextplane.CompressionTokenCounter
	handoffFrameDisabled bool
	maxSummaryTokens     int
}

var requiredContinuationSummarySections = []string{
	"Primary Request / Intent",
	"Current Work",
	"Next Step",
}

func NewDefaultCompactionEngine(opts CompactionEngineOptions) *CompactionEngine {
	return &CompactionEngine{
		model:                opts.Model,
		modelOptions:         append([]einomodel.Option(nil), opts.ModelOptions...),
		tokenCounter:         opts.TokenCounter,
		handoffFrameDisabled: opts.HandoffFrameDisabled,
		maxSummaryTokens:     opts.MaxSummaryTokens,
	}
}

func (e *CompactionEngine) Compact(ctx context.Context, req CompactRequest) (*CompactionResult, error) {
	if e == nil {
		return nil, errors.New("compaction engine is not initialized")
	}
	if e.model == nil {
		return nil, errors.New("compaction engine model is required")
	}
	if e.tokenCounter == nil {
		return nil, errors.New("compaction engine token counter is required")
	}
	if req.Trigger == "" {
		return nil, errors.New("compaction trigger is required")
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("compaction messages are required")
	}
	if req.PreservePolicy.RecentTurns <= 0 {
		return nil, errors.New("compaction preserve policy recent turns must be positive")
	}

	summaryInput, err := buildSummarizerInput(req.PreviousSummary, req.Messages, req.PreservePolicy)
	if err != nil {
		return nil, err
	}

	tokensBefore, err := e.countMessages(ctx, summaryInput)
	if err != nil {
		return nil, fmt.Errorf("count summary input tokens: %w", err)
	}
	summaryBudget := computeSummaryBudget(tokensBefore)
	if e.maxSummaryTokens > 0 && summaryBudget > e.maxSummaryTokens {
		summaryBudget = e.maxSummaryTokens
	}
	generateOpts := append([]einomodel.Option(nil), e.modelOptions...)
	generateOpts = append(generateOpts, einomodel.WithMaxTokens(summaryBudget))

	rawSummary, err := e.model.Generate(providers.WithCallSite(ctx, providers.CallSiteCompaction), summaryInput, generateOpts...)
	if err != nil {
		return nil, fmt.Errorf("generate compaction summary: %w", err)
	}
	if rawSummary == nil {
		return nil, errors.New("compaction summary model returned nil message")
	}
	if len(rawSummary.ToolCalls) > 0 {
		return nil, errors.New("compaction summary model returned tool calls")
	}

	summaryText := contextplane.RedactSecrets(summaryMessageText(rawSummary))
	if err := validateStructuredContinuationSummary(summaryText); err != nil {
		return nil, err
	}

	finalSummary := contextplane.MarkCompressionSummary(contextplane.SanitizeSummaryMessage(contextplane.CompactionSummaryMessage(summaryText)))
	if !e.handoffFrameDisabled {
		frame := buildHandoffFrame(req.Messages)
		if frame != "" {
			finalSummary = contextplane.MarkCompressionSummary(contextplane.SanitizeSummaryMessage(appendToMessage(finalSummary, frame)))
		}
	}

	systemMessages, contextMessages := splitLeadingSystemMessages(req.Messages)
	preservedTail := preservedConversationTail(contextMessages, req.PreservePolicy)
	rehydratePlan, err := e.buildRehydratePlan(ctx, RehydrateRequest{
		Messages:           req.Messages,
		ToolState:          req.ToolState,
		CurrentPlan:        req.CurrentPlan,
		RecentTouchedPaths: req.RecentTouchedPaths,
		TokenCounter:       e.tokenCounter,
	})
	if err != nil {
		return nil, fmt.Errorf("plan post-compact rehydration: %w", err)
	}
	rehydratedMessages := BuildRehydrateMessages(rehydratePlan)
	finalMessages := make([]adk.Message, 0, len(systemMessages)+1+len(rehydratedMessages)+len(preservedTail))
	finalMessages = append(finalMessages, contextplane.CloneContextSessionMessages(systemMessages)...)
	finalMessages = append(finalMessages, finalSummary)
	finalMessages = append(finalMessages, contextplane.CloneContextSessionMessages(rehydratedMessages)...)
	finalMessages = append(finalMessages, contextplane.CloneContextSessionMessages(preservedTail)...)

	firstIndex, lastIndex := compressionRewriteRange(req.Messages, req.PreservePolicy)
	if firstIndex < 0 || lastIndex < firstIndex {
		return nil, errors.New("compression rewrite range is empty")
	}
	tokensBefore, err = e.tokenCounter.CountMessages(ctx, req.Messages, req.ToolInfos)
	if err != nil {
		return nil, fmt.Errorf("count compression tokens before rewrite: %w", err)
	}
	tokensAfter, err := e.tokenCounter.CountMessages(ctx, finalMessages, req.ToolInfos)
	if err != nil {
		return nil, fmt.Errorf("count compression tokens after rewrite: %w", err)
	}

	outcome := contextplane.CompressionOutcome{
		FirstIndex:     firstIndex,
		LastIndex:      lastIndex,
		TokensBefore:   tokensBefore,
		TokensAfter:    tokensAfter,
		Summary:        summaryText,
		SummarySnippet: contextplane.Snippet(summaryText, 200),
	}
	return &CompactionResult{
		Messages:         contextplane.CloneContextSessionMessages(finalMessages),
		Summary:          contextplane.CloneContextSessionMessage(finalSummary),
		PreservedTail:    contextplane.CloneContextSessionMessages(preservedTail),
		Rehydrated:       contextplane.CloneContextSessionMessages(rehydratedMessages),
		RehydratePlan:    cloneRehydratePlan(rehydratePlan),
		SummaryInput:     contextplane.CloneContextSessionMessages(summaryInput),
		SummaryText:      summaryText,
		RewriteFirst:     firstIndex,
		RewriteLast:      lastIndex,
		Outcome:          outcome,
		Pressure:         req.Pressure,
		CompactTrigger:   req.Trigger,
		OriginalMessages: contextplane.CloneContextSessionMessages(req.Messages),
	}, nil
}

func validateStructuredContinuationSummary(summary string) error {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		return errors.New("compression summary content is empty")
	}
	if len(trimmed) < 50 {
		return errors.New("compression summary content is too short")
	}
	for _, section := range requiredContinuationSummarySections {
		content, ok := continuationSummarySection(trimmed, section)
		if !ok {
			return fmt.Errorf("compression summary missing required section %q", section)
		}
		if isEmptySummarySection(content) {
			return fmt.Errorf("compression summary section %q is empty", section)
		}
	}
	return nil
}

func continuationSummarySection(summary string, section string) (string, bool) {
	headingRe := regexp.MustCompile(`(?im)^#{1,6}\s*` + regexp.QuoteMeta(section) + `\s*:?\s*$`)
	loc := headingRe.FindStringIndex(summary)
	if loc == nil {
		return "", false
	}
	rest := summary[loc[1]:]
	nextHeadingRe := regexp.MustCompile(`(?m)^#{1,6}\s+`)
	next := nextHeadingRe.FindStringIndex(rest)
	if next != nil {
		rest = rest[:next[0]]
	}
	return strings.TrimSpace(rest), true
}

func isEmptySummarySection(content string) bool {
	trimmed := strings.Trim(strings.ToLower(strings.TrimSpace(content)), " \t\r\n-*:()")
	switch trimmed {
	case "", "none", "n/a", "na", "not applicable", "nothing":
		return true
	default:
		return false
	}
}

func splitLeadingSystemMessages(messages []adk.Message) ([]adk.Message, []adk.Message) {
	index := 0
	for index < len(messages) {
		if messages[index] == nil || messages[index].Role != schema.System {
			break
		}
		index++
	}
	return contextplane.CloneContextSessionMessages(messages[:index]), contextplane.CloneContextSessionMessages(messages[index:])
}

func (e *CompactionEngine) countMessages(ctx context.Context, messages []*schema.Message) (int, error) {
	if e.tokenCounter == nil {
		return 0, errors.New("token counter is nil")
	}
	return e.tokenCounter.CountMessages(ctx, schemaMessagesToADK(messages), nil)
}

func computeSummaryBudget(tokens int) int {
	budget := tokens * 20 / 100
	if budget < 2000 {
		budget = 2000
	}
	if budget > 12000 {
		budget = 12000
	}
	return budget
}

func schemaMessagesToADK(messages []*schema.Message) []adk.Message {
	result := make([]adk.Message, 0, len(messages))
	for _, msg := range messages {
		if msg != nil {
			result = append(result, msg)
		}
	}
	return result
}

func (e *CompactionEngine) buildRehydratePlan(ctx context.Context, req RehydrateRequest) (*RehydratePlan, error) {
	if req.TokenCounter == nil {
		return nil, errors.New("rehydration planner token counter is required")
	}
	tokenBudget := req.TokenBudget
	if tokenBudget <= 0 {
		tokenBudget = defaultRehydratePlanTokenBudget
	}
	builder := rehydratePlanBuilder{
		ctx:          ctx,
		tokenCounter: req.TokenCounter,
		plan:         RehydratePlan{TokenBudget: tokenBudget},
	}

	memoryContext := extractTaggedContent(req.Messages, "memory-context")
	if err := builder.append(RehydrateWorkingCheckpoint, "memory-context/working-checkpoint", extractTaggedBlock(memoryContext, "working-checkpoint")); err != nil {
		return nil, err
	}
	if err := builder.append(RehydrateSelectedSkill, "skill-context", extractTaggedContent(req.Messages, "skill-context")); err != nil {
		return nil, err
	}
	if err := builder.append(RehydrateSkillCatalog, "skill-catalog", extractTaggedContent(req.Messages, "skill-catalog")); err != nil {
		return nil, err
	}
	if err := builder.append(RehydrateToolState, "tool-lifecycle-state", formatToolStatePacket(req.ToolState)); err != nil {
		return nil, err
	}
	if err := builder.append(RehydrateSessionSummary, "memory-context/session-summary", extractTaggedBlock(memoryContext, "session-summary")); err != nil {
		return nil, err
	}
	if err := builder.append(RehydratePreparedMemory, "memory-context/prepared-memory", extractPreparedMemoryPacket(memoryContext)); err != nil {
		return nil, err
	}
	if err := builder.append(RehydratePlanState, "request/current-plan", req.CurrentPlan); err != nil {
		return nil, err
	}
	if err := builder.append(RehydrateRecentFiles, "request/recent-touched-paths", formatRecentTouchedPaths(req.RecentTouchedPaths)); err != nil {
		return nil, err
	}

	return &builder.plan, nil
}
