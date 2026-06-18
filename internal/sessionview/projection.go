package sessionview

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/events"
)

// AssistantMessageForRun projects a finished run into the assistant message
// content string and renderable parts. The summary is only consulted for
// succeeded runs.
func AssistantMessageForRun(run *events.RunRecord, summary ResultSummary) (string, []MessagePart, error) {
	if run == nil {
		return "", nil, nil
	}
	switch run.Status {
	case events.RunStatusSucceeded:
		text := strings.TrimSpace(run.Output)
		if text == "" {
			text = "Task completed."
		}
		parts := []MessagePart{
			{Kind: "text", Text: text},
		}
		if strings.TrimSpace(summary.Reasoning) != "" {
			parts = append(parts, MessagePart{
				Kind:      "reasoning",
				Reasoning: summary.Reasoning,
			})
		}
		if len(summary.Disclosures) > 0 {
			parts = append(parts, MessagePart{
				Kind:  "disclosure",
				Items: summary.Disclosures,
			})
		}
		parts = append(parts, MessagePart{
			Kind:        "result",
			Title:       "Task completed",
			Changed:     summary.Changed,
			Verified:    summary.Verified,
			Risks:       summary.Risks,
			DetailRunID: run.RunID,
		})
		if strings.TrimSpace(run.RunID) != "" {
			parts = append(parts, MessagePart{
				Kind:        "technical_detail_link",
				RunID:       run.RunID,
				DetailRunID: run.RunID,
				Label:       "View technical details",
			})
		}
		return text, parts, nil
	case events.RunStatusFailed:
		content := "Acorn could not finish this turn."
		parts := []MessagePart{{
			Kind:        "work_status",
			Status:      "failed",
			Title:       "Acorn could not finish",
			Summary:     failureSummary(run),
			DetailRunID: run.RunID,
		}, {
			Kind:        "technical_detail_link",
			RunID:       run.RunID,
			DetailRunID: run.RunID,
			Label:       "View technical details",
		}}
		return content, parts, nil
	case events.RunStatusInterrupted:
		content := "Acorn paused before continuing."
		parts := []MessagePart{{
			Kind:        "work_status",
			Status:      "interrupted",
			Title:       "Paused before continuing",
			Summary:     interruptedSummary(run),
			DetailRunID: run.RunID,
			Action: &MessageAction{
				Kind:  "resume_run",
				RunID: run.RunID,
				Label: "Resume",
			},
		}, {
			Kind:        "technical_detail_link",
			RunID:       run.RunID,
			DetailRunID: run.RunID,
			Label:       "View technical details",
		}}
		return content, parts, nil
	default:
		return "", nil, nil
	}
}

func failureSummary(run *events.RunRecord) string {
	if run == nil {
		return "The run failed before producing a final answer."
	}
	if errText := compactContinuationText(run.Error, 220); errText != "" {
		return errText
	}
	if output := compactContinuationText(run.Output, 220); output != "" {
		return output
	}
	return "The run failed before producing a final answer."
}

func interruptedSummary(run *events.RunRecord) string {
	if run == nil {
		return "Acorn paused at a real interrupt."
	}
	if output := compactContinuationText(run.Output, 220); output != "" {
		return output
	}
	return "Acorn paused at a real interrupt. Resume this run only when you want to continue the same execution."
}

// DecisionMessageForPendingAction projects a pending action into its decision
// message content string and renderable parts.
func DecisionMessageForPendingAction(action *events.PendingActionRecord) (string, []MessagePart, error) {
	if action == nil {
		return "", nil, errors.New("pending action is nil")
	}
	switch action.Kind {
	case events.PendingActionKindElicitation:
		return elicitationDecisionMessage(action)
	case events.PendingActionKindOperatorQuestion:
		return operatorQuestionDecisionMessage(action)
	default:
		return "", nil, fmt.Errorf("pending action %s has unsupported kind %q", action.ActionID, action.Kind)
	}
}

// DecisionMessageHasActionID reports whether the encoded content parts contain
// a decision part bound to the given action ID.
func DecisionMessageHasActionID(contentParts string, actionID string) (bool, error) {
	if strings.TrimSpace(contentParts) == "" {
		return false, nil
	}
	var parts []MessagePart
	if err := json.Unmarshal([]byte(contentParts), &parts); err != nil {
		return false, err
	}
	for _, part := range parts {
		if part.Kind == "decision" && strings.TrimSpace(part.DecisionID) == strings.TrimSpace(actionID) {
			return true, nil
		}
	}
	return false, nil
}

func elicitationDecisionMessage(action *events.PendingActionRecord) (string, []MessagePart, error) {
	message, err := decodeElicitationPayload(action.PayloadJSON)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(message) == "" {
		return "", nil, fmt.Errorf("pending action %s has empty elicitation message", action.ActionID)
	}
	parts := []MessagePart{{
		Kind:             "decision",
		DecisionID:       action.ActionID,
		Question:         message,
		Status:           string(action.Status),
		SelectedOptionID: decisionSelectedOptionID(action),
		Options: []DecisionOption{
			{ID: "accept", Label: "Accept"},
			{ID: "decline", Label: "Decline"},
		},
	}}
	if strings.TrimSpace(action.RunID) != "" {
		parts = append(parts, MessagePart{
			Kind:        "technical_detail_link",
			RunID:       action.RunID,
			DetailRunID: action.RunID,
			Label:       "View technical details",
		})
	}
	return message, parts, nil
}

func operatorQuestionDecisionMessage(action *events.PendingActionRecord) (string, []MessagePart, error) {
	payload, err := decodeOperatorQuestionPayload(action.PayloadJSON)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(payload.Question) == "" {
		return "", nil, fmt.Errorf("pending action %s has empty operator question", action.ActionID)
	}
	decision := decodeOperatorQuestionDecision(action.DecisionJSON)
	parts := []MessagePart{{
		Kind:             "decision",
		DecisionID:       action.ActionID,
		Question:         payload.Question,
		Status:           string(action.Status),
		SelectedOptionID: decision.SelectedOptionID,
		Answer:           decision.Answer,
		Options:          decisionOptionsFromPendingActionOptions(payload.Options),
	}}
	if strings.TrimSpace(action.RunID) != "" {
		parts = append(parts, MessagePart{
			Kind:        "technical_detail_link",
			RunID:       action.RunID,
			DetailRunID: action.RunID,
			Label:       "View technical details",
		})
	}
	return payload.Question, parts, nil
}

func decisionSelectedOptionID(action *events.PendingActionRecord) string {
	if action != nil && action.Kind == events.PendingActionKindOperatorQuestion {
		return decodeOperatorQuestionDecision(action.DecisionJSON).SelectedOptionID
	}
	switch action.Status {
	case events.PendingActionStatusApproved:
		return "accept"
	case events.PendingActionStatusRejected:
		return "decline"
	case events.PendingActionStatusResolved:
		var payload map[string]any
		if err := json.Unmarshal([]byte(action.DecisionJSON), &payload); err == nil {
			if value, ok := payload["action"].(string); ok {
				switch strings.TrimSpace(strings.ToLower(value)) {
				case "accept":
					return "accept"
				case "decline":
					return "decline"
				}
			}
		}
	}
	return ""
}

func decodeOperatorQuestionPayload(payloadJSON string) (events.OperatorQuestionPayload, error) {
	if strings.TrimSpace(payloadJSON) == "" {
		return events.OperatorQuestionPayload{}, errors.New("operator question payload is empty")
	}
	var payload events.OperatorQuestionPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return events.OperatorQuestionPayload{}, fmt.Errorf("decode operator question payload: %w", err)
	}
	return payload, nil
}

func decodeOperatorQuestionDecision(decisionJSON string) events.OperatorQuestionDecision {
	if strings.TrimSpace(decisionJSON) == "" {
		return events.OperatorQuestionDecision{}
	}
	var decision events.OperatorQuestionDecision
	if err := json.Unmarshal([]byte(decisionJSON), &decision); err != nil {
		return events.OperatorQuestionDecision{}
	}
	return decision
}

func decisionOptionsFromPendingActionOptions(items []events.PendingActionOption) []DecisionOption {
	if len(items) == 0 {
		return nil
	}
	out := make([]DecisionOption, 0, len(items))
	for _, item := range items {
		out = append(out, DecisionOption{
			ID:          strings.TrimSpace(item.ID),
			Label:       strings.TrimSpace(item.Label),
			Description: strings.TrimSpace(item.Description),
		})
	}
	return out
}

func decodeElicitationPayload(payloadJSON string) (string, error) {
	if strings.TrimSpace(payloadJSON) == "" {
		return "", errors.New("elicitation payload is empty")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return "", fmt.Errorf("decode elicitation payload: %w", err)
	}
	message := stringValue(payload, "message")
	if message == "" {
		message = stringValue(payload, "Message")
	}
	if message == "" {
		message = stringValue(payload, "content")
	}
	return message, nil
}

func stringValue(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
