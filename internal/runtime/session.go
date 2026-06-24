package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/localit-io/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
)

// Session is the sole owner of root-run model input. It bootstraps
// from an assembly result, applies observation masking + auto-compact before
// each model call, and records assistant/tool messages.
//
// Concurrency contract: Session methods are NOT safe for concurrent use.
// The direct_response agent loop calls BeforeModelCall, RecordAssistant, and
// RecordToolResults sequentially from a single goroutine. The auto-compact
// circuit breaker (failure counter) relies on this single-writer invariant.
type Session interface {
	ID() SessionID
	Bootstrap(context.Context, BootstrapRequest) (*ModelInput, error)
	BeforeModelCall(context.Context, ModelCallRequest) (*ModelInput, error)
	RecordMessages(context.Context, []adk.Message) error
	RecordAssistant(context.Context, adk.Message) error
	RecordToolResults(context.Context, []adk.Message) error
}

type SessionID struct {
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

// SessionOptions configures a defaultContextSession.
type SessionOptions struct {
	TokenCounter        TokenCounter
	Model               einomodel.BaseChatModel // used for auto-compact summary generation; nil disables compact
	WindowTokens        int                     // provider context window (effective)
	CompactMargin       int                     // auto-compact triggers when tokens exceed WindowTokens - CompactMargin
	MaskAfterTurns      int                     // tool results older than this many turns are masked
	PreserveRecentTurns int                     // recent turns kept verbatim after compact
}

type defaultContextSession struct {
	id                  SessionID
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

func NewDefaultSession(opts SessionOptions) Session {
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

func (s *defaultContextSession) ID() SessionID {
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

func validateContextSessionIdentity(sessionID, runID string) (SessionID, error) {
	id := SessionID{
		SessionID: strings.TrimSpace(sessionID),
		RunID:     strings.TrimSpace(runID),
	}
	if id.SessionID == "" {
		return SessionID{}, errors.New("context session id is required")
	}
	if id.RunID == "" {
		return SessionID{}, errors.New("context session run id is required")
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

func WithSession(ctx context.Context, session Session) context.Context {
	return context.WithValue(ctx, contextSessionContextKey{}, session)
}

func SessionFromContext(ctx context.Context) Session {
	if ctx == nil {
		return nil
	}
	session, ok := ctx.Value(contextSessionContextKey{}).(Session)
	if !ok {
		return nil
	}
	return session
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
