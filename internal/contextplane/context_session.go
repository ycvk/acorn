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
	Pipeline       ContextCompressionPipeline
	PreservePolicy PreservePolicy
	State          any
	EmitCompressed func(context.Context, CompressionOutcome) error
	EmitPressure   func(context.Context, BudgetPressure) error
}

type defaultContextSession struct {
	id              ContextSessionID
	turnIndex       int
	modelProfile    ModelProfile
	messages        []adk.Message
	budgetGovernor  BudgetGovernor
	pipeline        ContextCompressionPipeline
	preservePolicy  PreservePolicy
	state           any
	emitCompressed  func(context.Context, CompressionOutcome) error
	emitPressure    func(context.Context, BudgetPressure) error
	lastSummary     string
	lastCompactTurn int
	bootstrapped    bool
}

func NewDefaultContextSession(opts ContextSessionOptions) ContextSession {
	s := &defaultContextSession{
		budgetGovernor: opts.BudgetGovernor,
		pipeline:       opts.Pipeline,
		preservePolicy: opts.PreservePolicy,
		state:          opts.State,
		emitCompressed: opts.EmitCompressed,
		emitPressure:   opts.EmitPressure,
	}
	if st, ok := opts.State.(*CompressionState); ok && st != nil {
		s.lastSummary = st.LastSummary
	}
	return s
}

func (s *defaultContextSession) ID() ContextSessionID {
	if s == nil {
		return ContextSessionID{}
	}
	return s.id
}

func (s *defaultContextSession) Bootstrap(ctx context.Context, req BootstrapRequest) (*ModelInput, error) {
	if s == nil {
		return nil, errors.New("context session is not initialized")
	}
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
				messages = append(messages, cloneContextSessionMessage(msg))
			}
		}
	}
	for _, msg := range req.InitialMessages {
		if msg == nil {
			continue
		}
		messages = append(messages, cloneContextSessionMessage(msg))
	}
	s.id = id
	s.turnIndex = req.TurnIndex
	s.modelProfile = req.ModelProfile
	s.messages = messages
	s.bootstrapped = true
	return s.currentInput(ctx, nil)
}

func (s *defaultContextSession) BeforeModelCall(ctx context.Context, req ModelCallRequest) (*ModelInput, error) {
	if s == nil {
		return nil, errors.New("context session is not initialized")
	}
	if !s.bootstrapped {
		return nil, errors.New("context session must be bootstrapped before model calls")
	}
	pressure, err := s.evaluatePressure(ctx, req.ToolInfos)
	if err != nil {
		return nil, err
	}
	if err := s.emitPressureEvent(ctx, pressure); err != nil {
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
	if s == nil {
		return nil, errors.New("context session is not initialized")
	}
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
	if err := s.emitPressureEvent(ctx, pressure); err != nil {
		return nil, err
	}
	return s.compact(ctx, req, pressure, CompactTriggerReactive)
}

func (s *defaultContextSession) compact(ctx context.Context, req ModelCallRequest, pressure BudgetPressure, trigger CompactTrigger) (*ModelInput, error) {
	if s.pipeline == nil {
		return nil, errors.New("context session compression pipeline is required")
	}
	if s.preservePolicy.RecentTurns <= 0 {
		return nil, errors.New("context session preserve policy recent turns must be positive")
	}
	toolState := req.ToolState
	if toolState == nil {
		if lifecycle := ToolLifecycleContextFromContext(ctx); lifecycle != nil {
			toolState = lifecycle.State
		}
	}
	result, err := s.pipeline.Compress(ctx, PipelineRequest{
		Messages:           cloneContextSessionMessages(s.messages),
		ToolInfos:          append([]*schema.ToolInfo(nil), req.ToolInfos...),
		ToolState:          toolState,
		Trigger:            trigger,
		TurnIndex:          s.turnIndex,
		LastCompactTurn:    s.lastCompactTurn,
		Pressure:           pressure,
		CurrentPlan:        req.CurrentPlan,
		RecentTouchedPaths: append([]string(nil), req.RecentTouchedPaths...),
		PreviousSummary:    s.lastSummary,
		PreservePolicy:     s.preservePolicy,
		ModelProfile:       s.modelProfile,
	})
	if err != nil {
		return nil, fmt.Errorf("compact context session before model call: %w", err)
	}
	if result == nil {
		return nil, errors.New("context session compression returned nil result")
	}
	if len(result.Messages) == 0 {
		return nil, errors.New("context session compression returned empty messages")
	}
	s.messages = cloneContextSessionMessages(result.Messages)
	s.lastCompactTurn = s.turnIndex
	if result.Outcome != nil {
		outcome := *result.Outcome
		outcome.LayersApplied = append([]CompactLayer(nil), result.LayersApplied...)
		s.lastSummary = outcome.Summary
		if st, ok := s.state.(*CompressionState); ok && st != nil {
			st.RecordCompression(outcome.Summary)
		}
		if s.emitCompressed != nil {
			if err := s.emitCompressed(ctx, outcome); err != nil {
				return nil, fmt.Errorf("emit context session compression event: %w", err)
			}
		}
	}
	afterPressure, err := s.evaluatePressure(ctx, req.ToolInfos)
	if err != nil {
		return nil, err
	}
	return s.modelInput(afterPressure), nil
}

func (s *defaultContextSession) RecordAssistant(_ context.Context, msg adk.Message) error {
	if s == nil {
		return errors.New("context session is not initialized")
	}
	if !s.bootstrapped {
		return errors.New("context session must be bootstrapped before recording assistant messages")
	}
	if msg == nil {
		return errors.New("context session assistant message is required")
	}
	s.messages = append(s.messages, s.annotateTurnIndex(cloneContextSessionMessage(msg)))
	return nil
}

func (s *defaultContextSession) RecordMessages(_ context.Context, messages []adk.Message) error {
	if s == nil {
		return errors.New("context session is not initialized")
	}
	if !s.bootstrapped {
		return errors.New("context session must be bootstrapped before recording messages")
	}
	for _, msg := range messages {
		if msg == nil {
			return errors.New("context session message is required")
		}
		s.messages = append(s.messages, s.annotateTurnIndex(cloneContextSessionMessage(msg)))
	}
	return nil
}

func (s *defaultContextSession) RecordToolResults(_ context.Context, results []adk.Message) error {
	if s == nil {
		return errors.New("context session is not initialized")
	}
	if !s.bootstrapped {
		return errors.New("context session must be bootstrapped before recording tool results")
	}
	for _, result := range results {
		if result == nil {
			return errors.New("context session tool result message is required")
		}
		s.messages = append(s.messages, s.annotateTurnIndex(cloneContextSessionMessage(result)))
	}
	return nil
}

func (s *defaultContextSession) annotateTurnIndex(msg adk.Message) adk.Message {
	return AnnotateMessageTurn(msg, s.turnIndex)
}

func (s *defaultContextSession) Resume(context.Context, ResumeContextRequest) (*ModelInput, error) {
	if s == nil {
		return nil, errors.New("context session is not initialized")
	}
	return nil, errors.New("context session resume requires persisted context boundary integration")
}

func shouldCompactForPressure(state BudgetPressureState) bool {
	return state == PressureAutoCompact || state == PressureBlocking
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
		Messages: cloneContextSessionMessages(s.messages),
		Tools:    append([]*schema.ToolInfo(nil), tools...),
	})
	if err != nil {
		return BudgetPressure{}, fmt.Errorf("evaluate context session pressure: %w", err)
	}
	return pressure, nil
}

func (s *defaultContextSession) emitPressureEvent(ctx context.Context, pressure BudgetPressure) error {
	if s.emitPressure == nil {
		return nil
	}
	if err := s.emitPressure(ctx, pressure); err != nil {
		return fmt.Errorf("emit context pressure event: %w", err)
	}
	return nil
}

func (s *defaultContextSession) modelInput(pressure BudgetPressure) *ModelInput {
	return &ModelInput{
		Messages: cloneContextSessionMessages(s.messages),
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

func cloneContextSessionMessages(messages []adk.Message) []adk.Message {
	result := make([]adk.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		result = append(result, cloneContextSessionMessage(msg))
	}
	return result
}

func cloneContextSessionMessage(msg adk.Message) adk.Message {
	return cloneMessage(msg)
}
