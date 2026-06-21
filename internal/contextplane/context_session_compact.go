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

func (s *defaultContextSession) compact(ctx context.Context, req ModelCallRequest, pressure BudgetPressure, trigger CompactTrigger) (*ModelInput, error) {
	if s.pipeline == nil {
		return nil, errors.New("context session compression pipeline is required")
	}
	if s.preservePolicy.RecentTurns <= 0 {
		return nil, errors.New("context session preserve policy recent turns must be positive")
	}
	toolState := s.resolveToolState(ctx, req)
	beforeMessages := CloneContextSessionMessages(s.messages)
	result, err := s.runCompression(ctx, req, pressure, trigger, toolState, beforeMessages)
	if err != nil {
		return nil, err
	}
	if err := s.applyCompressionResult(ctx, beforeMessages, result, pressure, trigger); err != nil {
		return nil, err
	}
	afterPressure, err := s.evaluatePressure(ctx, req.ToolInfos)
	if err != nil {
		return nil, err
	}
	return s.modelInput(afterPressure), nil
}

func (s *defaultContextSession) resolveToolState(ctx context.Context, req ModelCallRequest) *ToolLifecycleState {
	toolState := req.ToolState
	if toolState == nil {
		if lifecycle := ToolLifecycleContextFromContext(ctx); lifecycle != nil {
			toolState = lifecycle.State
		}
	}
	return toolState
}

func (s *defaultContextSession) runCompression(ctx context.Context, req ModelCallRequest, pressure BudgetPressure, trigger CompactTrigger, toolState *ToolLifecycleState, beforeMessages []adk.Message) (*PipelineResult, error) {
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
	return result, nil
}

func (s *defaultContextSession) applyCompressionResult(ctx context.Context, beforeMessages []adk.Message, result *PipelineResult, pressure BudgetPressure, trigger CompactTrigger) error {
	s.messages = CloneContextSessionMessages(result.Messages)
	s.lastCompactTurn = s.turnIndex
	if result.Outcome == nil {
		return nil
	}
	outcome := *result.Outcome
	boundary, err := s.persistContextBoundary(ctx, beforeMessages, outcome, pressure, trigger)
	if err != nil {
		return fmt.Errorf("persist context boundary: %w", err)
	}
	outcome.BoundaryID = boundary.BoundaryID
	s.lastSummary = outcome.Summary
	s.lastBoundaryID = boundary.BoundaryID
	s.boundarySequence = boundary.Sequence
	if st, ok := s.state.(*CompressionState); ok && st != nil {
		st.RecordCompression(outcome.Summary)
	}
	return nil
}

func (s *defaultContextSession) persistContextBoundary(ctx context.Context, beforeMessages []adk.Message, outcome CompressionOutcome, pressure BudgetPressure, trigger CompactTrigger) (model.ContextBoundary, error) {
	if s.boundaryStore == nil {
		return model.ContextBoundary{}, errors.New("context session boundary store is required")
	}
	if strings.TrimSpace(outcome.Summary) == "" {
		return model.ContextBoundary{}, errors.New("context session compression outcome summary is required")
	}

	sequence, previousBoundaryID, err := s.resolveBoundarySequence(ctx)
	if err != nil {
		return model.ContextBoundary{}, err
	}

	boundary, err := s.buildContextBoundary(beforeMessages, outcome, pressure, trigger, sequence, previousBoundaryID)
	if err != nil {
		return model.ContextBoundary{}, err
	}
	if err := s.boundaryStore.SaveContextBoundary(ctx, boundary); err != nil {
		return model.ContextBoundary{}, err
	}
	return boundary, nil
}

func (s *defaultContextSession) resolveBoundarySequence(ctx context.Context) (int, string, error) {
	previousBoundaryID := s.lastBoundaryID
	sequence := s.boundarySequence + 1
	latest, err := s.boundaryStore.LoadLatestContextBoundary(ctx, s.id.SessionID)
	if err != nil {
		return 0, "", fmt.Errorf("load latest context boundary: %w", err)
	}
	if latest != nil && latest.Sequence >= sequence {
		sequence = latest.Sequence + 1
		previousBoundaryID = latest.BoundaryID
	}
	if sequence <= 0 {
		sequence = 1
	}
	return sequence, previousBoundaryID, nil
}

func (s *defaultContextSession) buildContextBoundary(beforeMessages []adk.Message, outcome CompressionOutcome, pressure BudgetPressure, trigger CompactTrigger, sequence int, previousBoundaryID string) (model.ContextBoundary, error) {
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
	return model.ContextBoundary{
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
	}, nil
}

func (s *defaultContextSession) Resume(ctx context.Context, req ResumeContextRequest) (*ModelInput, error) {
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
	boundary, err := s.loadResumeBoundary(ctx, req, id)
	if err != nil {
		return nil, err
	}
	s.applyResumeBoundary(id, req.ModelProfile, boundary)
	return s.currentInput(ctx, nil)
}

func (s *defaultContextSession) loadResumeBoundary(ctx context.Context, req ResumeContextRequest, id ContextSessionID) (*model.ContextBoundary, error) {
	var boundary *model.ContextBoundary
	var err error
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
	return boundary, nil
}

func (s *defaultContextSession) applyResumeBoundary(id ContextSessionID, modelProfile ModelProfile, boundary *model.ContextBoundary) {
	s.id = id
	s.turnIndex = boundary.TurnIndex
	s.modelProfile = modelProfile
	s.messages = []adk.Message{MarkCompressionSummary(SanitizeSummaryMessage(CompactionSummaryMessage(boundary.Summary)))}
	s.lastSummary = boundary.Summary
	s.lastBoundaryID = boundary.BoundaryID
	s.boundarySequence = boundary.Sequence
	s.lastCompactTurn = boundary.TurnIndex
	s.bootstrapped = true
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
	return state == PressureAutoCompact
}
