package web

import (
	"encoding/json"
	"time"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/events"
)

type DecidePendingActionRequest struct {
	Decision         string `json:"decision"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
	Answer           string `json:"answer,omitempty"`
}

// PendingActionDecisionDTO represents the operator's decision on a pending action.
type PendingActionDecisionDTO struct {
	ActionID         string     `json:"action_id"`
	RunID            string     `json:"run_id"`
	Status           string     `json:"status"`
	Decision         string     `json:"decision"`
	SelectedOptionID string     `json:"selected_option_id,omitempty"`
	Answer           string     `json:"answer,omitempty"`
	DecidedAt        *time.Time `json:"decided_at,omitempty"`
}

// PendingActionListResponse is the response body for listing pending actions.
type PendingActionListResponse struct {
	Items []PendingActionSummaryDTO `json:"items"`
}

// PendingActionDetailDTO represents the full detail of a pending action.
type PendingActionDetailDTO struct {
	ActionID  string                   `json:"action_id"`
	RunID     string                   `json:"run_id"`
	ThreadID  string                   `json:"thread_id"`
	Kind      string                   `json:"kind"`
	Status    string                   `json:"status"`
	Title     string                   `json:"title"`
	Body      string                   `json:"body,omitempty"`
	Options   []PendingActionOptionDTO `json:"options"`
	Payload   map[string]any           `json:"payload"`
	Reason    string                   `json:"reason,omitempty"`
	Rule      string                   `json:"rule,omitempty"`
	CreatedAt time.Time                `json:"created_at"`
}

// PendingActionOptionDTO represents a single option in a pending action.
type PendingActionOptionDTO struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

func pendingActionDecisionDTOFromDomain(record events.PendingActionRecord) PendingActionDecisionDTO {
	status := string(record.Status)
	decision, selectedOptionID, answer := parsePendingActionDecision(record)
	return PendingActionDecisionDTO{
		ActionID:         record.ActionID,
		RunID:            record.RunID,
		Status:           status,
		Decision:         decision,
		SelectedOptionID: selectedOptionID,
		Answer:           answer,
		DecidedAt:        record.DecidedAt,
	}
}

func parsePendingActionDecision(record events.PendingActionRecord) (string, string, string) {
	var payload struct {
		Action           string `json:"action"`
		SelectedOptionID string `json:"selected_option_id"`
		Answer           string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(record.DecisionJSON), &payload); err == nil {
		return payload.Action, payload.SelectedOptionID, payload.Answer
	}
	return "", "", ""
}

func pendingActionDetailDTOFromDomain(item app.PendingActionDetail) PendingActionDetailDTO {
	return PendingActionDetailDTO{
		ActionID:  item.ActionID,
		RunID:     item.RunID,
		ThreadID:  item.ThreadID,
		Kind:      item.Kind,
		Status:    item.Status,
		Title:     item.Title,
		Body:      item.Body,
		Options:   pendingActionOptionDTOsFromDomain(item.Options),
		Payload:   item.Payload,
		Reason:    item.Reason,
		Rule:      item.Rule,
		CreatedAt: item.CreatedAt,
	}
}

func pendingActionListResponseFromDomain(items []app.PendingActionSummary) PendingActionListResponse {
	return PendingActionListResponse{Items: pendingActionSummaryDTOsFromDomain(items)}
}

func decisionOptionDTOsFromDomain(options []app.DecisionOption) []DecisionOptionDTO {
	return DefaultConverter.decisionOptionDTOsFromDomain(options)
}

func pendingActionOptionDTOsFromDomain(items []app.PendingActionOption) []PendingActionOptionDTO {
	return DefaultConverter.pendingActionOptionDTOsFromDomain(items)
}

func pendingActionSummaryDTOsFromDomain(items []app.PendingActionSummary) []PendingActionSummaryDTO {
	return DefaultConverter.pendingActionSummaryDTOsFromDomain(items)
}
