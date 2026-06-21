package contextplane

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type ContextSession interface {
	ID() ContextSessionID
	Bootstrap(context.Context, BootstrapRequest) (*ModelInput, error)
	BeforeModelCall(context.Context, ModelCallRequest) (*ModelInput, error)
	ReactiveCompact(context.Context, ModelCallRequest, error) (*ModelInput, error)
	RecordMessages(context.Context, []adk.Message) error
	RecordAssistant(context.Context, adk.Message) error
	RecordToolResults(context.Context, []adk.Message) error
	Resume(context.Context, ResumeContextRequest) (*ModelInput, error)
}

type ContextSessionID struct {
	SessionID string
	RunID     string
	Mode      string
}

type BootstrapRequest struct {
	SessionID       string
	RunID           string
	TurnIndex       int
	Mode            string
	InitialMessages []adk.Message
	Assembly        *AssembleResult
	ModelProfile    ModelProfile
}

type ModelCallRequest struct {
	CallID             string
	QuerySource        string
	AllowCompact       bool
	ToolInfos          []*schema.ToolInfo
	ToolState          *ToolLifecycleState
	CurrentPlan        string
	RecentTouchedPaths []string
}

type ResumeContextRequest struct {
	SessionID    string
	RunID        string
	Mode         string
	BoundaryID   string
	ModelProfile ModelProfile
}

type ModelInput struct {
	Messages []adk.Message
	Pressure BudgetPressure
}

type ContextSessionOptions struct {
	BudgetGovernor BudgetGovernor
	Pipeline       CompressionPipeline
	BoundaryStore  ContextBoundaryStore
	PreservePolicy PreservePolicy
	State          any
}

type defaultContextSession struct {
	id               ContextSessionID
	turnIndex        int
	modelProfile     ModelProfile
	messages         []adk.Message
	budgetGovernor   BudgetGovernor
	pipeline         CompressionPipeline
	boundaryStore    ContextBoundaryStore
	preservePolicy   PreservePolicy
	state            any
	lastSummary      string
	lastBoundaryID   string
	boundarySequence int
	lastCompactTurn  int
	bootstrapped     bool
}

func NewDefaultContextSession(opts ContextSessionOptions) ContextSession {
	s := &defaultContextSession{
		budgetGovernor: opts.BudgetGovernor,
		pipeline:       opts.Pipeline,
		boundaryStore:  opts.BoundaryStore,
		preservePolicy: opts.PreservePolicy,
		state:          opts.State,
	}
	if st, ok := opts.State.(*CompressionState); ok && st != nil {
		s.lastSummary = st.LastSummary
	}
	return s
}

func (s *defaultContextSession) ID() ContextSessionID {
	return s.id
}

func (s *defaultContextSession) Bootstrap(ctx context.Context, req BootstrapRequest) (*ModelInput, error) {
	if s.budgetGovernor == nil {
		return nil, errors.New("context session budget governor is required")
	}
	id, err := validateContextSessionIdentity(req.SessionID, req.RunID, req.Mode)
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
	s.modelProfile = req.ModelProfile
	s.messages = messages
	s.bootstrapped = true
	return s.currentInput(ctx, nil)
}

func (s *defaultContextSession) BeforeModelCall(ctx context.Context, req ModelCallRequest) (*ModelInput, error) {
	if !s.bootstrapped {
		return nil, errors.New("context session must be bootstrapped before model calls")
	}
	pressure, err := s.evaluatePressure(ctx, req.ToolInfos)
	if err != nil {
		return nil, err
	}
	if !shouldCompactForPressure(pressure.State) {
		return s.modelInput(pressure), nil
	}
	if !req.AllowCompact {
		return nil, fmt.Errorf("context pressure %s requires compaction but compact is disabled", pressure.State)
	}
	if s.pipeline == nil {
		return nil, errors.New("context session compression pipeline is required")
	}
	return s.compact(ctx, req, pressure, CompactTriggerAuto)
}

func (s *defaultContextSession) ReactiveCompact(ctx context.Context, req ModelCallRequest, cause error) (*ModelInput, error) {
	if !s.bootstrapped {
		return nil, errors.New("context session must be bootstrapped before reactive compact")
	}
	if !IsContextOverflowError(cause) {
		return nil, fmt.Errorf("reactive compact requires context overflow error: %w", cause)
	}
	if !req.AllowCompact {
		return nil, errors.New("reactive compact requires AllowCompact")
	}
	pressure, err := s.evaluatePressure(ctx, req.ToolInfos)
	if err != nil {
		return nil, err
	}
	return s.compact(ctx, req, pressure, CompactTriggerReactive)
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

func (s *defaultContextSession) currentInput(ctx context.Context, tools []*schema.ToolInfo) (*ModelInput, error) {
	pressure, err := s.evaluatePressure(ctx, tools)
	if err != nil {
		return nil, err
	}
	return s.modelInput(pressure), nil
}

func (s *defaultContextSession) evaluatePressure(ctx context.Context, tools []*schema.ToolInfo) (BudgetPressure, error) {
	if s.budgetGovernor == nil {
		return BudgetPressure{}, errors.New("context session budget governor is required")
	}
	pressure, err := s.budgetGovernor.Evaluate(ctx, BudgetEvaluateRequest{
		Profile:  s.modelProfile,
		Messages: CloneContextSessionMessages(s.messages),
		Tools:    append([]*schema.ToolInfo(nil), tools...),
	})
	if err != nil {
		return BudgetPressure{}, fmt.Errorf("evaluate context session pressure: %w", err)
	}
	return pressure, nil
}

func (s *defaultContextSession) modelInput(pressure BudgetPressure) *ModelInput {
	return &ModelInput{
		Messages: CloneContextSessionMessages(s.messages),
		Pressure: pressure,
	}
}

func validateContextSessionIdentity(sessionID, runID, mode string) (ContextSessionID, error) {
	id := ContextSessionID{
		SessionID: strings.TrimSpace(sessionID),
		RunID:     strings.TrimSpace(runID),
		Mode:      strings.TrimSpace(mode),
	}
	if id.SessionID == "" {
		return ContextSessionID{}, errors.New("context session id is required")
	}
	if id.RunID == "" {
		return ContextSessionID{}, errors.New("context session run id is required")
	}
	if id.Mode == "" {
		return ContextSessionID{}, errors.New("context session mode is required")
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

// CompressionState tracks compression history within a single run so later
// compressions can update the latest sanitized summary incrementally.
type CompressionState struct {
	LastSummary      string
	CompressionCount int
}

// NewCompressionState creates a zero-value CompressionState.
func NewCompressionState() *CompressionState {
	return &CompressionState{}
}

// RecordCompression updates state after a successful compression.
func (s *CompressionState) RecordCompression(summary string) {
	s.LastSummary = summary
	s.CompressionCount++
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
