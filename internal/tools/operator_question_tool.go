package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/events"
	storecore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tooling"
)

type OperatorQuestionContext interface {
	CurrentRunID(context.Context) string
	CurrentSessionID(context.Context) string
	CurrentToolCallID(context.Context) string
}

type OperatorQuestionStore interface {
	CreatePendingAction(ctx context.Context, input storecore.CreatePendingActionInput) (*events.PendingActionRecord, error)
	SyncDecisionMessageForPendingAction(ctx context.Context, actionID string) error
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error)
}

type AskOperatorInput struct {
	Title         string                   `json:"title,omitempty" jsonschema:"description=Short title shown to the operator."`
	Question      string                   `json:"question" jsonschema:"description=Question that must be answered by the human operator before the run can continue."`
	Options       []AskOperatorOptionInput `json:"options,omitempty" jsonschema:"description=Optional answer choices."`
	AllowFreeform bool                     `json:"allow_freeform,omitempty" jsonschema:"description=Whether the operator may answer with freeform text instead of selecting an option."`
}

type AskOperatorOptionInput struct {
	ID          string `json:"id" jsonschema:"description=Stable option id returned as selected_option_id."`
	Label       string `json:"label" jsonschema:"description=Human-readable option label."`
	Description string `json:"description,omitempty" jsonschema:"description=Optional extra context for this option."`
}

type AskOperatorState struct {
	ActionID string
	Info     map[string]any
}

type AskOperatorOutput struct {
	ActionID         string `json:"action_id"`
	Status           string `json:"status"`
	Decision         string `json:"decision"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
	Answer           string `json:"answer,omitempty"`
}

func buildAskOperatorTool(store OperatorQuestionStore, bridge OperatorQuestionContext) (einotool.BaseTool, error) {
	if store == nil {
		return nil, errors.New("operator question store is required")
	}
	if bridge == nil {
		return nil, errors.New("operator question context bridge is required")
	}
	tool, err := inferProgressTool("ask_operator", "Ask the human operator a blocking question and resume with a structured answer.", func(ctx context.Context, input AskOperatorInput, emit tooling.ToolProgressEmitter) (AskOperatorOutput, error) {
		wasInterrupted, hasState, state := einotool.GetInterruptState[AskOperatorState](ctx)
		if wasInterrupted {
			return resumeAskOperator(ctx, state, hasState)
		}
		return interruptAskOperator(ctx, store, bridge, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build ask_operator tool: %w", err)
	}
	return tool, nil
}

func interruptAskOperator(ctx context.Context, store OperatorQuestionStore, bridge OperatorQuestionContext, input AskOperatorInput, emit tooling.ToolProgressEmitter) (AskOperatorOutput, error) {
	payload, err := normalizeAskOperatorInput(input)
	if err != nil {
		return AskOperatorOutput{}, err
	}
	runID := strings.TrimSpace(bridge.CurrentRunID(ctx))
	if runID == "" {
		return AskOperatorOutput{}, errors.New("ask_operator requires current run context")
	}
	callID := strings.TrimSpace(bridge.CurrentToolCallID(ctx))
	if callID == "" {
		return AskOperatorOutput{}, errors.New("ask_operator requires current tool call context")
	}
	actionID := "operator_question:" + runID + ":" + callID
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return AskOperatorOutput{}, fmt.Errorf("marshal operator question payload: %w", err)
	}
	record, err := store.CreatePendingAction(ctx, storecore.CreatePendingActionInput{
		ActionID:    actionID,
		RunID:       runID,
		Kind:        events.PendingActionKindOperatorQuestion,
		Subject:     strings.TrimSpace(input.Title),
		PayloadJSON: string(payloadJSON),
		Status:      events.PendingActionStatusPending,
		Mode:        events.PendingActionModeDeferred,
		Reason:      "operator_question",
		Rule:        "ask_operator",
	})
	if err != nil {
		return AskOperatorOutput{}, err
	}
	if err := store.SyncDecisionMessageForPendingAction(ctx, record.ActionID); err != nil {
		return AskOperatorOutput{}, err
	}
	eventPayload := map[string]any{
		"action_id":      record.ActionID,
		"question":       payload.Question,
		"options":        payload.Options,
		"allow_freeform": payload.AllowFreeform,
	}
	if _, err := store.AppendEventContext(ctx, runID, "operator_question.pending", eventPayload); err != nil {
		return AskOperatorOutput{}, fmt.Errorf("append operator_question.pending event: %w", err)
	}
	if err := emitToolProgress(ctx, emit, fmt.Sprintf("waiting for operator answer %s", record.ActionID)); err != nil {
		return AskOperatorOutput{}, err
	}
	info := map[string]any{
		"kind":      "operator_question",
		"action_id": record.ActionID,
		"question":  payload.Question,
		"message":   "ask_operator is waiting for a human operator answer",
	}
	state := AskOperatorState{ActionID: record.ActionID, Info: info}
	return AskOperatorOutput{}, einotool.StatefulInterrupt(ctx, info, state)
}

func resumeAskOperator(ctx context.Context, state AskOperatorState, hasState bool) (AskOperatorOutput, error) {
	if !hasState || strings.TrimSpace(state.ActionID) == "" {
		return AskOperatorOutput{}, errors.New("ask_operator resume requires saved operator question state")
	}
	isTarget, hasData, data := einotool.GetResumeContext[map[string]any](ctx)
	if !isTarget {
		return AskOperatorOutput{}, einotool.StatefulInterrupt(ctx, state.Info, state)
	}
	if !hasData {
		return AskOperatorOutput{}, errors.New("ask_operator resume requires operator decision data")
	}
	actionID := stringFromMap(data, "action_id")
	if actionID == "" {
		actionID = state.ActionID
	}
	if actionID != state.ActionID {
		return AskOperatorOutput{}, fmt.Errorf("ask_operator resume action_id %q does not match saved action_id %q", actionID, state.ActionID)
	}
	decision := stringFromMap(data, "action")
	switch decision {
	case events.OperatorQuestionDecisionAnswer:
		selectedOptionID := stringFromMap(data, "selected_option_id")
		answer := stringFromMap(data, "answer")
		if selectedOptionID == "" && answer == "" {
			return AskOperatorOutput{}, errors.New("ask_operator answer resume data requires selected_option_id or answer")
		}
		return AskOperatorOutput{
			ActionID:         actionID,
			Status:           "answered",
			Decision:         decision,
			SelectedOptionID: selectedOptionID,
			Answer:           answer,
		}, nil
	case events.OperatorQuestionDecisionDecline:
		return AskOperatorOutput{
			ActionID: actionID,
			Status:   "declined",
			Decision: decision,
		}, nil
	default:
		return AskOperatorOutput{}, fmt.Errorf("ask_operator resume data has unsupported action %q", decision)
	}
}

func normalizeAskOperatorInput(input AskOperatorInput) (events.OperatorQuestionPayload, error) {
	question := strings.TrimSpace(input.Question)
	if question == "" {
		return events.OperatorQuestionPayload{}, errors.New("question is required")
	}
	options, err := normalizeAskOperatorOptions(input.Options)
	if err != nil {
		return events.OperatorQuestionPayload{}, err
	}
	if len(options) == 0 && !input.AllowFreeform {
		return events.OperatorQuestionPayload{}, errors.New("ask_operator requires options or allow_freeform=true")
	}
	return events.OperatorQuestionPayload{
		Question:      question,
		Options:       options,
		AllowFreeform: input.AllowFreeform,
	}, nil
}

func normalizeAskOperatorOptions(items []AskOperatorOptionInput) ([]events.PendingActionOption, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]events.PendingActionOption, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		label := strings.TrimSpace(item.Label)
		if id == "" || label == "" {
			return nil, errors.New("ask_operator options require id and label")
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("ask_operator option id %q is duplicated", id)
		}
		seen[id] = struct{}{}
		out = append(out, events.PendingActionOption{
			ID:          id,
			Label:       label,
			Description: strings.TrimSpace(item.Description),
		})
	}
	return out, nil
}

func stringFromMap(data map[string]any, key string) string {
	value, ok := data[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}
