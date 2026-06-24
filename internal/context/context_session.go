package context

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"encoding/json"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/localit-io/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
)

// ContextSession is the sole owner of root-run model input. It bootstraps
// from an assembly result, applies observation masking + auto-compact before
// each model call, and records assistant/tool messages.
type ContextSession interface {
	ID() ContextSessionID
	Bootstrap(context.Context, BootstrapRequest) (*ModelInput, error)
	BeforeModelCall(context.Context, ModelCallRequest) (*ModelInput, error)
	RecordMessages(context.Context, []adk.Message) error
	RecordAssistant(context.Context, adk.Message) error
	RecordToolResults(context.Context, []adk.Message) error
}

type ContextSessionID struct {
	SessionID string
	RunID     string
}

type BootstrapRequest struct {
	SessionID       string
	RunID           string
	TurnIndex       int
	InitialMessages []adk.Message
	Assembly        *AssembleResult
}

// ModelCallRequest carries per-call metadata. AllowCompact is gone:
// auto-compact is always permitted when the token threshold is crossed.
type ModelCallRequest struct {
	CallID    string
	ToolInfos []*schema.ToolInfo
}

// ModelInput is the prepared message list handed to the model.
type ModelInput struct {
	Messages []adk.Message
}

// ContextSessionOptions configures a defaultContextSession.
type ContextSessionOptions struct {
	TokenCounter        TokenCounter
	Model               einomodel.BaseChatModel // used for auto-compact summary generation; nil disables compact
	WindowTokens        int                     // provider context window (effective)
	CompactMargin       int                     // auto-compact triggers when tokens exceed WindowTokens - CompactMargin
	MaskAfterTurns      int                     // tool results older than this many turns are masked
	PreserveRecentTurns int                     // recent turns kept verbatim after compact
}

type defaultContextSession struct {
	id                  ContextSessionID
	turnIndex           int
	messages            []adk.Message
	tokenCounter        TokenCounter
	compactor           *autoCompactor
	windowTokens        int
	compactMargin       int
	maskAfterTurns      int
	preserveRecentTurns int
	bootstrapped        bool
}

func NewDefaultContextSession(opts ContextSessionOptions) ContextSession {
	s := &defaultContextSession{
		tokenCounter:        opts.TokenCounter,
		windowTokens:        opts.WindowTokens,
		compactMargin:       opts.CompactMargin,
		maskAfterTurns:      opts.MaskAfterTurns,
		preserveRecentTurns: opts.PreserveRecentTurns,
	}
	if opts.Model != nil && opts.TokenCounter != nil {
		s.compactor = newAutoCompactor(opts.Model, opts.TokenCounter, opts.PreserveRecentTurns)
	}
	return s
}

func (s *defaultContextSession) ID() ContextSessionID {
	return s.id
}

func (s *defaultContextSession) Bootstrap(ctx context.Context, req BootstrapRequest) (*ModelInput, error) {
	if s.tokenCounter == nil {
		return nil, errors.New("context session token counter is required")
	}
	id, err := validateContextSessionIdentity(req.SessionID, req.RunID)
	if err != nil {
		return nil, err
	}
	if req.TurnIndex < 0 {
		return nil, errors.New("context session turn index must be non-negative")
	}
	messages := make([]adk.Message, 0, len(req.InitialMessages))
	if req.Assembly != nil {
		for _, msg := range req.Assembly.Messages {
			if msg != nil {
				messages = append(messages, CloneContextSessionMessage(msg))
			}
		}
	}
	for _, msg := range req.InitialMessages {
		if msg == nil {
			continue
		}
		messages = append(messages, CloneContextSessionMessage(msg))
	}
	s.id = id
	s.turnIndex = req.TurnIndex
	s.messages = messages
	s.bootstrapped = true
	return s.modelInput(), nil
}

func (s *defaultContextSession) BeforeModelCall(ctx context.Context, req ModelCallRequest) (*ModelInput, error) {
	if !s.bootstrapped {
		return nil, errors.New("context session must be bootstrapped before model calls")
	}
	// 1. Apply observation masking to old tool results.
	masked := applyMasking(s.messages, s.turnIndex, s.maskAfterTurns)
	// 2. Count tokens; if over threshold, auto-compact.
	total, err := s.tokenCounter.CountMessages(ctx, masked, req.ToolInfos)
	if err != nil {
		return nil, fmt.Errorf("count context tokens: %w", err)
	}
	threshold := s.compactThreshold()
	if total > threshold && s.compactor != nil {
		compacted, compactErr := s.compactor.compact(ctx, masked)
		if compactErr == nil {
			masked = compacted
		}
		// compact failure is non-fatal: fall through with masked messages.
		// The circuit breaker inside compactor tracks consecutive failures.
	}
	s.messages = masked
	return s.modelInput(), nil
}

func (s *defaultContextSession) RecordAssistant(_ context.Context, msg adk.Message) error {
	if !s.bootstrapped {
		return errors.New("context session must be bootstrapped before recording assistant messages")
	}
	if msg == nil {
		return errors.New("context session assistant message is required")
	}
	s.messages = append(s.messages, s.annotateTurnIndex(CloneContextSessionMessage(msg)))
	return nil
}

func (s *defaultContextSession) RecordMessages(_ context.Context, messages []adk.Message) error {
	if !s.bootstrapped {
		return errors.New("context session must be bootstrapped before recording messages")
	}
	for _, msg := range messages {
		if msg == nil {
			return errors.New("context session message is required")
		}
		s.messages = append(s.messages, s.annotateTurnIndex(CloneContextSessionMessage(msg)))
	}
	return nil
}

func (s *defaultContextSession) RecordToolResults(_ context.Context, results []adk.Message) error {
	if !s.bootstrapped {
		return errors.New("context session must be bootstrapped before recording tool results")
	}
	for _, result := range results {
		if result == nil {
			return errors.New("context session tool result message is required")
		}
		s.messages = append(s.messages, s.annotateTurnIndex(CloneContextSessionMessage(result)))
	}
	return nil
}

func (s *defaultContextSession) annotateTurnIndex(msg adk.Message) adk.Message {
	return AnnotateMessageTurn(msg, s.turnIndex)
}

func (s *defaultContextSession) modelInput() *ModelInput {
	return &ModelInput{
		Messages: CloneContextSessionMessages(s.messages),
	}
}

func (s *defaultContextSession) compactThreshold() int {
	if s.windowTokens <= 0 {
		return 1 << 30 // effectively unlimited if not configured
	}
	margin := s.compactMargin
	if margin <= 0 {
		margin = 13000
	}
	threshold := s.windowTokens - margin
	if threshold <= 0 {
		threshold = s.windowTokens * 9 / 10
	}
	return threshold
}

func validateContextSessionIdentity(sessionID, runID string) (ContextSessionID, error) {
	id := ContextSessionID{
		SessionID: strings.TrimSpace(sessionID),
		RunID:     strings.TrimSpace(runID),
	}
	if id.SessionID == "" {
		return ContextSessionID{}, errors.New("context session id is required")
	}
	if id.RunID == "" {
		return ContextSessionID{}, errors.New("context session run id is required")
	}
	return id, nil
}

func CloneContextSessionMessages(messages []adk.Message) []adk.Message {
	result := make([]adk.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		result = append(result, CloneContextSessionMessage(msg))
	}
	return result
}

func CloneContextSessionMessage(msg adk.Message) adk.Message {
	return CloneMessage(msg)
}

func AnnotateMessageTurn(msg adk.Message, turnIndex int) adk.Message {
	if msg == nil {
		return msg
	}
	if msg.Extra == nil {
		msg.Extra = make(map[string]any)
	}
	msg.Extra[TurnIndexExtraKey] = turnIndex
	return msg
}

type contextSessionContextKey struct{}

func WithContextSession(ctx context.Context, session ContextSession) context.Context {
	return context.WithValue(ctx, contextSessionContextKey{}, session)
}

func ContextSessionFromContext(ctx context.Context) ContextSession {
	if ctx == nil {
		return nil
	}
	session, ok := ctx.Value(contextSessionContextKey{}).(ContextSession)
	if !ok {
		return nil
	}
	return session
}

// applyMasking replaces tool result messages older than maskAfterTurns with a
// compact placeholder. This is the first-line defense against context bloat.
// It is a pure in-memory transformation — nothing is persisted.
func applyMasking(messages []adk.Message, currentTurn int, maskAfterTurns int) []adk.Message {
	if maskAfterTurns <= 0 || len(messages) == 0 {
		return messages
	}
	result := make([]adk.Message, len(messages))
	copy(result, messages)
	for i, msg := range result {
		if msg == nil {
			continue
		}
		if msg.Role != schema.Tool {
			continue
		}
		callID := strings.TrimSpace(msg.ToolCallID)
		if callID == "" {
			continue
		}
		msgTurn := turnIndexFromMessage(msg)
		if currentTurn-msgTurn <= maskAfterTurns {
			continue // keep recent tool results
		}
		result[i] = maskToolMessage(msg)
	}
	return result
}

// maskToolMessage replaces the tool result content with a compact placeholder.
func maskToolMessage(msg adk.Message) adk.Message {
	clone := msg
	clone.Content = fmt.Sprintf("[tool result elided: call_id=%s]", msg.ToolCallID)
	return clone
}

// turnIndexFromMessage extracts the turn index annotated by AnnotateMessageTurn.
func turnIndexFromMessage(msg adk.Message) int {
	if msg == nil || msg.Extra == nil {
		return 0
	}
	v, ok := msg.Extra[TurnIndexExtraKey]
	if !ok {
		return 0
	}
	if t, ok := v.(int); ok {
		return t
	}
	return 0
}

const (
	autoCompactMaxFailures   = 3
	autoCompactSummaryPrompt = "Summarize the conversation so far, preserving key decisions, facts, and pending work. Be concise."
)

// autoCompactor generates a conversation summary via a model call, then
// replaces old messages with [summary + recent turns]. A circuit breaker
// stops further compaction attempts after autoCompactMaxFailures consecutive
// failures.
type autoCompactor struct {
	model               einomodel.BaseChatModel
	tokenCounter        TokenCounter
	preserveRecentTurns int
	failures            int
}

func newAutoCompactor(model einomodel.BaseChatModel, counter TokenCounter, preserveRecentTurns int) *autoCompactor {
	return &autoCompactor{
		model:               model,
		tokenCounter:        counter,
		preserveRecentTurns: preserveRecentTurns,
	}
}

// compact splits messages into old (to summarize) and recent (to keep),
// generates a summary, and returns [summary message + recent messages].
// On failure, returns the original messages and a non-nil error; the circuit
// breaker increments and will short-circuit after maxFailures.
func (c *autoCompactor) compact(ctx context.Context, messages []adk.Message) ([]adk.Message, error) {
	if c.failures >= autoCompactMaxFailures {
		return messages, nil // circuit breaker tripped
	}
	preserve := c.preserveRecentTurns
	if preserve <= 0 {
		preserve = 3
	}
	// Each turn is approximately user + assistant (2 messages). Keep the
	// last preserve*2 messages as recent context.
	recentCount := preserve * 2
	if recentCount >= len(messages) {
		return messages, nil // nothing to compact
	}
	splitAt := len(messages) - recentCount
	oldMessages := messages[:splitAt]
	recentMessages := messages[splitAt:]

	summary, err := c.generateSummary(ctx, oldMessages)
	if err != nil {
		c.failures++
		return messages, fmt.Errorf("auto-compact summary generation: %w", err)
	}
	c.failures = 0 // reset on success

	summaryMsg := adk.Message(schema.SystemMessage("Conversation summary:\n" + summary))
	result := make([]adk.Message, 0, len(recentMessages)+1)
	result = append(result, summaryMsg)
	result = append(result, recentMessages...)
	return result, nil
}

func (c *autoCompactor) generateSummary(ctx context.Context, messages []adk.Message) (string, error) {
	serialized := serializeMessagesForSummary(messages)
	prompt := autoCompactSummaryPrompt + "\n\n---\n\n" + serialized
	req := []*schema.Message{schema.UserMessage(prompt)}
	resp, err := c.model.Generate(ctx, req)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("auto-compact model returned nil response")
	}
	return strings.TrimSpace(resp.Content), nil
}

func serializeMessagesForSummary(messages []adk.Message) string {
	var b strings.Builder
	for _, m := range messages {
		if m == nil {
			continue
		}
		b.WriteString(string(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

var tokenLoaderOnce sync.Once

// TokenCounter counts tokens for text and messages.
type TokenCounter interface {
	CountText(context.Context, string) (int, error)
	CountMessages(context.Context, []adk.Message, []*schema.ToolInfo) (int, error)
}

// tiktokenCounter is the production TokenCounter backed by tiktoken-go.
type tiktokenCounter struct {
	encodingName string
	encoder      *tiktoken.Tiktoken
}

// NewTokenCounter creates a tiktoken-backed TokenCounter using o200k_base
// (the encoding used by GPT-4o / o1 and a reasonable approximation for other providers).
func NewTokenCounter() (TokenCounter, error) {
	if err := ensureTokenLoader(); err != nil {
		return nil, err
	}
	encoding := "o200k_base"
	encoder, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		return nil, fmt.Errorf("initialize tiktoken encoding %q: %w", encoding, err)
	}
	return &tiktokenCounter{
		encodingName: encoding,
		encoder:      encoder,
	}, nil
}

func (c *tiktokenCounter) CountText(_ context.Context, text string) (int, error) {
	return len(c.encoder.Encode(text, nil, nil)), nil
}

func (c *tiktokenCounter) CountMessages(ctx context.Context, messages []adk.Message, tools []*schema.ToolInfo) (int, error) {
	total := 0
	for _, msg := range messages {
		payload, err := json.Marshal(normalizeMessage(msg))
		if err != nil {
			return 0, fmt.Errorf("marshal message for token count: %w", err)
		}
		total += len(c.encoder.Encode(string(payload), nil, nil))
	}
	for _, tool := range tools {
		payload, err := json.Marshal(normalizeTool(tool))
		if err != nil {
			return 0, fmt.Errorf("marshal tool for token count: %w", err)
		}
		total += len(c.encoder.Encode(string(payload), nil, nil))
	}
	return total, nil
}

func ensureTokenLoader() error {
	tokenLoaderOnce.Do(func() {
		tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	})
	return nil
}

func normalizeMessage(msg adk.Message) *schema.Message {
	if msg == nil {
		return &schema.Message{}
	}
	return &schema.Message{
		Role:                     msg.Role,
		Content:                  msg.Content,
		UserInputMultiContent:    append([]schema.MessageInputPart(nil), msg.UserInputMultiContent...),
		AssistantGenMultiContent: append([]schema.MessageOutputPart(nil), msg.AssistantGenMultiContent...),
		Name:                     msg.Name,
		ToolCalls:                append([]schema.ToolCall(nil), msg.ToolCalls...),
		ToolCallID:               msg.ToolCallID,
		ToolName:                 msg.ToolName,
		ReasoningContent:         msg.ReasoningContent,
	}
}

func normalizeTool(tool *schema.ToolInfo) *schema.ToolInfo {
	if tool == nil {
		return &schema.ToolInfo{}
	}
	clone := *tool
	clone.Extra = nil
	return &clone
}
