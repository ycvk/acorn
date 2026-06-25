package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

func buildElicitationDecision(record core.PendingActionRecord, input PendingActionDecisionInput) (core.PendingActionStatus, []byte, string, map[string]any, error) {
	if strings.TrimSpace(input.SelectedOptionID) != "" || strings.TrimSpace(input.Answer) != "" {
		return "", nil, "", nil, fmt.Errorf("%w: elicitation accepts decision only", ErrPendingActionDecisionInvalid)
	}
	status, err := pendingActionDecisionStatus(input.Decision)
	if err != nil {
		return "", nil, "", nil, err
	}

	decisionJSON, err := json.Marshal(map[string]any{
		"action": statusToDecisionAction(status),
	})
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("marshal pending action decision: %w", err)
	}
	return status, decisionJSON, "elicitation.decided", map[string]any{
		"action_id": record.ActionID,
		"decision":  statusToDecisionAction(status),
	}, nil
}

func buildOperatorQuestionDecision(record core.PendingActionRecord, input PendingActionDecisionInput) (core.PendingActionStatus, []byte, string, map[string]any, error) {
	payload, err := operatorQuestionPayload(record)
	if err != nil {
		return "", nil, "", nil, err
	}
	action := strings.TrimSpace(strings.ToLower(input.Decision))
	selectedOptionID := strings.TrimSpace(input.SelectedOptionID)
	answer := strings.TrimSpace(input.Answer)

	status, err := resolveOperatorDecisionStatus(action, input.Decision, payload, selectedOptionID, answer)
	if err != nil {
		return "", nil, "", nil, err
	}

	decision := core.OperatorQuestionDecision{
		Action:           action,
		SelectedOptionID: selectedOptionID,
		Answer:           answer,
	}
	decisionJSON, err := json.Marshal(decision)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("marshal operator question decision: %w", err)
	}
	eventPayload := buildOperatorQuestionEventPayload(record, payload, action, selectedOptionID, answer)
	return status, decisionJSON, "operator_question.decided", eventPayload, nil
}

func resolveOperatorDecisionStatus(action, rawDecision string, payload core.OperatorQuestionPayload, selectedOptionID string, answer string) (core.PendingActionStatus, error) {
	switch action {
	case core.OperatorQuestionDecisionAnswer:
		if err := validateOperatorAnswer(payload, selectedOptionID, answer); err != nil {
			return "", err
		}
		return core.PendingActionStatusApproved, nil
	case core.OperatorQuestionDecisionDecline:
		if selectedOptionID != "" || answer != "" {
			return "", fmt.Errorf("%w: declined operator question must not include selected_option_id or answer", ErrPendingActionDecisionInvalid)
		}
		return core.PendingActionStatusRejected, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrPendingActionDecisionInvalid, rawDecision)
	}
}

func buildOperatorQuestionEventPayload(record core.PendingActionRecord, payload core.OperatorQuestionPayload, action string, selectedOptionID string, answer string) map[string]any {
	eventPayload := map[string]any{
		"action_id": record.ActionID,
		"question":  payload.Question,
		"decision":  action,
	}
	if selectedOptionID != "" {
		eventPayload["selected_option_id"] = selectedOptionID
	}
	if answer != "" {
		eventPayload["answer"] = answer
	}
	return eventPayload
}

func validateOperatorAnswer(payload core.OperatorQuestionPayload, selectedOptionID string, answer string) error {
	if selectedOptionID == "" && answer == "" {
		return fmt.Errorf("%w: operator answer requires selected_option_id or answer", ErrPendingActionDecisionInvalid)
	}
	if answer != "" && !payload.AllowFreeform {
		return fmt.Errorf("%w: operator question does not allow freeform answer", ErrPendingActionDecisionInvalid)
	}
	if selectedOptionID == "" {
		if len(payload.Options) > 0 && !payload.AllowFreeform {
			return fmt.Errorf("%w: operator answer requires selected_option_id", ErrPendingActionDecisionInvalid)
		}
		return nil
	}
	for _, option := range payload.Options {
		if strings.TrimSpace(option.ID) == selectedOptionID {
			return nil
		}
	}
	return fmt.Errorf("%w: unknown selected_option_id %q", ErrPendingActionDecisionInvalid, selectedOptionID)
}

func pendingActionDecisionStatus(decision string) (core.PendingActionStatus, error) {
	switch strings.TrimSpace(strings.ToLower(decision)) {
	case "accept":
		return core.PendingActionStatusApproved, nil
	case "decline":
		return core.PendingActionStatusRejected, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrPendingActionDecisionInvalid, decision)
	}
}

func statusToDecisionAction(status core.PendingActionStatus) string {
	switch status {
	case core.PendingActionStatusApproved:
		return "accept"
	case core.PendingActionStatusRejected:
		return "decline"
	default:
		return "decline"
	}
}
