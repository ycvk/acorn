package contextplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/model"
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
	EmitCompressed func(context.Context, CompressionOutcome) error
	EmitPressure   func(context.Context, BudgetPressure) error
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
	emitCompressed   func(context.Context, CompressionOutcome) error
	emitPressure     func(context.Context, BudgetPressure) error
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
	beforeMessages := CloneContextSessionMessages(s.messages)
	result, err := s.pipeline.Compress(ctx, PipelineRequest{
		Messages:           beforeMessages,
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
	s.messages = CloneContextSessionMessages(result.Messages)
	s.lastCompactTurn = s.turnIndex
	if result.Outcome != nil {
		outcome := *result.Outcome
		outcome.LayersApplied = append([]CompactLayer(nil), result.LayersApplied...)
		boundary, err := s.persistContextBoundary(ctx, beforeMessages, outcome, pressure, trigger)
		if err != nil {
			return nil, fmt.Errorf("persist context boundary: %w", err)
		}
		outcome.BoundaryID = boundary.BoundaryID
		s.lastSummary = outcome.Summary
		s.lastBoundaryID = boundary.BoundaryID
		s.boundarySequence = boundary.Sequence
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

func (s *defaultContextSession) persistContextBoundary(ctx context.Context, beforeMessages []adk.Message, outcome CompressionOutcome, pressure BudgetPressure, trigger CompactTrigger) (model.ContextBoundary, error) {
	if s.boundaryStore == nil {
		return model.ContextBoundary{}, errors.New("context session boundary store is required")
	}
	if strings.TrimSpace(outcome.Summary) == "" {
		return model.ContextBoundary{}, errors.New("context session compression outcome summary is required")
	}

	previousBoundaryID := s.lastBoundaryID
	sequence := s.boundarySequence + 1
	latest, err := s.boundaryStore.LoadLatestContextBoundary(ctx, s.id.SessionID)
	if err != nil {
		return model.ContextBoundary{}, fmt.Errorf("load latest context boundary: %w", err)
	}
	if latest != nil && latest.Sequence >= sequence {
		sequence = latest.Sequence + 1
		previousBoundaryID = latest.BoundaryID
	}
	if sequence <= 0 {
		sequence = 1
	}

	firstIndex, lastIndex := normalizeBoundaryCoveredRange(outcome.FirstIndex, outcome.LastIndex, len(beforeMessages))
	preservedFrom, preservedTo := preservedRangeAfterRewrite(lastIndex, len(beforeMessages))
	effectiveWindow := pressure.EffectiveWindowTokens
	if effectiveWindow <= 0 {
		effectiveWindow = s.modelProfile.ContextWindowTokens
	}
	boundaryID := contextBoundaryID(s.id.RunID, sequence)
	summarySnippet := strings.TrimSpace(outcome.SummarySnippet)
	if summarySnippet == "" {
		summarySnippet = Snippet(outcome.Summary, 200)
	}

	boundary := model.ContextBoundary{
		BoundaryID:               boundaryID,
		SessionID:                s.id.SessionID,
		RunID:                    s.id.RunID,
		Sequence:                 sequence,
		TurnIndex:                s.turnIndex,
		Mode:                     s.id.Mode,
		Trigger:                  string(trigger),
		FirstIndex:               firstIndex,
		LastIndex:                lastIndex,
		CoveredFirstMessageID:    contextBoundaryMessageID(s.id.RunID, firstIndex),
		CoveredLastMessageID:     contextBoundaryMessageID(s.id.RunID, lastIndex),
		PreviousBoundaryID:       previousBoundaryID,
		SummaryMessageID:         boundaryID + ":summary",
		TranscriptRef:            fmt.Sprintf("%s:messages:%d-%d", s.id.RunID, firstIndex, lastIndex),
		PreservedFromIndex:       preservedFrom,
		PreservedToIndex:         preservedTo,
		PreservedHeadMessageID:   contextBoundaryMessageID(s.id.RunID, preservedFrom),
		PreservedAnchorMessageID: contextBoundaryMessageID(s.id.RunID, preservedFrom),
		PreservedTailMessageID:   contextBoundaryMessageID(s.id.RunID, preservedTo),
		TokensBefore:             outcome.TokensBefore,
		TokensAfter:              outcome.TokensAfter,
		EffectiveWindowTokens:    effectiveWindow,
		Summary:                  strings.TrimSpace(outcome.Summary),
		SummarySnippet:           summarySnippet,
		CreatedAt:                time.Now().UTC(),
	}
	if err := s.boundaryStore.SaveContextBoundary(ctx, boundary); err != nil {
		return model.ContextBoundary{}, err
	}
	return boundary, nil
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
	s.messages = append(s.messages, s.annotateTurnIndex(CloneContextSessionMessage(msg)))
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
		s.messages = append(s.messages, s.annotateTurnIndex(CloneContextSessionMessage(msg)))
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
		s.messages = append(s.messages, s.annotateTurnIndex(CloneContextSessionMessage(result)))
	}
	return nil
}

func (s *defaultContextSession) annotateTurnIndex(msg adk.Message) adk.Message {
	return AnnotateMessageTurn(msg, s.turnIndex)
}

func (s *defaultContextSession) Resume(ctx context.Context, req ResumeContextRequest) (*ModelInput, error) {
	if s == nil {
		return nil, errors.New("context session is not initialized")
	}
	if s.budgetGovernor == nil {
		return nil, errors.New("context session budget governor is required")
	}
	if s.boundaryStore == nil {
		return nil, errors.New("context session boundary store is required")
	}
	id, err := validateContextSessionIdentity(req.SessionID, req.RunID, req.Mode)
	if err != nil {
		return nil, err
	}
	var boundary *model.ContextBoundary
	if strings.TrimSpace(req.BoundaryID) != "" {
		boundary, err = s.boundaryStore.LoadContextBoundary(ctx, req.BoundaryID)
	} else {
		boundary, err = s.boundaryStore.LoadLatestContextBoundary(ctx, req.SessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("load context boundary for resume: %w", err)
	}
	if boundary == nil {
		return nil, errors.New("context boundary not found")
	}
	if boundary.SessionID != id.SessionID {
		return nil, fmt.Errorf("context boundary session mismatch: got %q want %q", boundary.SessionID, id.SessionID)
	}
	if boundary.RunID != id.RunID {
		return nil, fmt.Errorf("context boundary run mismatch: got %q want %q", boundary.RunID, id.RunID)
	}
	if boundary.Mode != id.Mode {
		return nil, fmt.Errorf("context boundary mode mismatch: got %q want %q", boundary.Mode, id.Mode)
	}
	if strings.TrimSpace(boundary.Summary) == "" {
		return nil, errors.New("context boundary summary is required for resume")
	}

	s.id = id
	s.turnIndex = boundary.TurnIndex
	s.modelProfile = req.ModelProfile
	s.messages = []adk.Message{MarkCompressionSummary(SanitizeSummaryMessage(CompactionSummaryMessage(boundary.Summary)))}
	s.lastSummary = boundary.Summary
	s.lastBoundaryID = boundary.BoundaryID
	s.boundarySequence = boundary.Sequence
	s.lastCompactTurn = boundary.TurnIndex
	s.bootstrapped = true
	return s.currentInput(ctx, nil)
}

func normalizeBoundaryCoveredRange(firstIndex, lastIndex, messageCount int) (int, int) {
	if firstIndex < 0 {
		firstIndex = 0
	}
	if lastIndex < firstIndex {
		lastIndex = firstIndex
	}
	if messageCount <= 0 {
		return firstIndex, lastIndex
	}
	if firstIndex >= messageCount {
		firstIndex = messageCount - 1
	}
	if lastIndex >= messageCount {
		lastIndex = messageCount - 1
	}
	if lastIndex < firstIndex {
		lastIndex = firstIndex
	}
	return firstIndex, lastIndex
}

func preservedRangeAfterRewrite(lastIndex, messageCount int) (int, int) {
	if messageCount <= 0 {
		return 0, 0
	}
	from := lastIndex + 1
	to := messageCount - 1
	if from < 0 || from >= messageCount || to < from {
		idx := lastIndex
		if idx < 0 {
			idx = 0
		}
		if idx >= messageCount {
			idx = messageCount - 1
		}
		return idx, idx
	}
	return from, to
}

func contextBoundaryID(runID string, sequence int) string {
	part := sanitizeBoundaryIDPart(runID)
	if part == "" {
		part = "run"
	}
	return fmt.Sprintf("ctxb_%s_%04d", part, sequence)
}

func sanitizeBoundaryIDPart(value string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func contextBoundaryMessageID(runID string, index int) string {
	if index < 0 {
		index = 0
	}
	return fmt.Sprintf("%s:message:%04d", runID, index)
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
		Messages: CloneContextSessionMessages(s.messages),
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
