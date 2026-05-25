package contextplane

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/providerusage"
)

type CompactTrigger string

const (
	CompactTriggerAuto     CompactTrigger = "auto"
	CompactTriggerManual   CompactTrigger = "manual"
	CompactTriggerReactive CompactTrigger = "reactive"
)

type PreservePolicy struct {
	RecentTurns       int
	PreserveToolPairs bool
}

type CompactRequest struct {
	Trigger            CompactTrigger
	Messages           []adk.Message
	ToolInfos          []*schema.ToolInfo
	ToolState          *ToolLifecycleState
	Pressure           BudgetPressure
	PreviousSummary    string
	PreservePolicy     PreservePolicy
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
	Outcome          CompressionOutcome
	Pressure         BudgetPressure
	CompactTrigger   CompactTrigger
	OriginalMessages []adk.Message
}

type compactionEngine interface {
	Compact(context.Context, CompactRequest) (*CompactionResult, error)
}

type CompactionEngineOptions struct {
	Model                einomodel.BaseChatModel
	ModelOptions         []einomodel.Option
	TokenCounter         *CompressionTokenCounter
	RehydrationPlanner   *RehydrationPlanner
	HandoffFrameDisabled bool
	MaxSummaryTokens     int
}

type CompactionEngine struct {
	model                einomodel.BaseChatModel
	modelOptions         []einomodel.Option
	tokenCounter         *CompressionTokenCounter
	rehydrationPlanner   *RehydrationPlanner
	handoffFrameDisabled bool
	maxSummaryTokens     int
}

type CompressionOutcome struct {
	BoundaryID     string
	FirstIndex     int
	LastIndex      int
	TokensBefore   int
	TokensAfter    int
	Summary        string
	SummarySnippet string
	LayersApplied  []CompactLayer
}

var requiredContinuationSummarySections = []string{
	"Primary Request / Intent",
	"Current Work",
	"Next Step",
}

func NewDefaultCompactionEngine(opts CompactionEngineOptions) *CompactionEngine {
	planner := opts.RehydrationPlanner
	if planner == nil {
		planner = NewDefaultRehydrationPlanner()
	}
	return &CompactionEngine{
		model:                opts.Model,
		modelOptions:         append([]einomodel.Option(nil), opts.ModelOptions...),
		tokenCounter:         opts.TokenCounter,
		rehydrationPlanner:   planner,
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

	rawSummary, err := e.model.Generate(providerusage.WithCallSite(ctx, providerusage.CallSiteCompaction), summaryInput, generateOpts...)
	if err != nil {
		return nil, fmt.Errorf("generate compaction summary: %w", err)
	}
	if rawSummary == nil {
		return nil, errors.New("compaction summary model returned nil message")
	}
	if len(rawSummary.ToolCalls) > 0 {
		return nil, errors.New("compaction summary model returned tool calls")
	}

	summaryText := redactSecrets(summaryMessageText(rawSummary))
	if err := validateStructuredContinuationSummary(summaryText); err != nil {
		return nil, err
	}

	finalSummary := markCompressionSummary(sanitizeSummaryMessage(compactionSummaryMessage(summaryText)))
	if !e.handoffFrameDisabled {
		frame := buildHandoffFrame(req.Messages)
		if frame != "" {
			finalSummary = markCompressionSummary(sanitizeSummaryMessage(appendToMessage(finalSummary, frame)))
		}
	}

	systemMessages, contextMessages := splitLeadingSystemMessages(req.Messages)
	preservedTail := preservedConversationTail(contextMessages, req.PreservePolicy)
	rehydratePlan, err := e.rehydrationPlanner.Plan(ctx, RehydrateRequest{
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
	finalMessages = append(finalMessages, cloneContextSessionMessages(systemMessages)...)
	finalMessages = append(finalMessages, finalSummary)
	finalMessages = append(finalMessages, cloneContextSessionMessages(rehydratedMessages)...)
	finalMessages = append(finalMessages, cloneContextSessionMessages(preservedTail)...)

	firstIndex, lastIndex := compressionRewriteRange(req.Messages, req.PreservePolicy)
	if firstIndex < 0 || lastIndex < firstIndex {
		return nil, errors.New("compression rewrite range is empty")
	}
	tokensBefore, err = e.tokenCounter.count(ctx, req.Messages, req.ToolInfos)
	if err != nil {
		return nil, fmt.Errorf("count compression tokens before rewrite: %w", err)
	}
	tokensAfter, err := e.tokenCounter.count(ctx, finalMessages, req.ToolInfos)
	if err != nil {
		return nil, fmt.Errorf("count compression tokens after rewrite: %w", err)
	}

	outcome := CompressionOutcome{
		FirstIndex:     firstIndex,
		LastIndex:      lastIndex,
		TokensBefore:   tokensBefore,
		TokensAfter:    tokensAfter,
		Summary:        summaryText,
		SummarySnippet: snippet(summaryText, 200),
	}
	return &CompactionResult{
		Messages:         cloneContextSessionMessages(finalMessages),
		Summary:          cloneContextSessionMessage(finalSummary),
		PreservedTail:    cloneContextSessionMessages(preservedTail),
		Rehydrated:       cloneContextSessionMessages(rehydratedMessages),
		RehydratePlan:    cloneRehydratePlan(rehydratePlan),
		SummaryInput:     cloneContextSessionMessages(summaryInput),
		SummaryText:      summaryText,
		RewriteFirst:     firstIndex,
		RewriteLast:      lastIndex,
		Outcome:          outcome,
		Pressure:         req.Pressure,
		CompactTrigger:   req.Trigger,
		OriginalMessages: cloneContextSessionMessages(req.Messages),
	}, nil
}

func compactionSummaryMessage(summaryText string) adk.Message {
	return &schema.Message{
		Role: schema.User,
		UserInputMultiContent: []schema.MessageInputPart{
			{
				Type: schema.ChatMessagePartTypeText,
				Text: summaryText,
			},
			{
				Type: schema.ChatMessagePartTypeText,
				Text: "Continue the conversation from this context checkpoint. Do not ask the user to repeat already summarized information.",
			},
		},
	}
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
	return cloneContextSessionMessages(messages[:index]), cloneContextSessionMessages(messages[index:])
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
