package api

import (
	"time"

	"encoding/json"

	"github.com/ycvk/acorn/internal/core"
)

type DecidePendingActionRequest struct {
	Decision         string `json:"decision"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
	Answer           string `json:"answer,omitempty"`
}

// PendingActionDecisionDTO represents the operator's decision on a pending action.
type PendingActionDecisionDTO struct {
	ActionID         string `json:"action_id"`
	RunID            string `json:"run_id"`
	Status           string `json:"status"`
	Decision         string `json:"decision"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
	Answer           string `json:"answer,omitempty"`
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

func pendingActionDecisionDTOFromDomain(record core.PendingActionRecord) PendingActionDecisionDTO {
	status := string(record.Status)
	decision, selectedOptionID, answer := parsePendingActionDecision(record)
	return PendingActionDecisionDTO{
		ActionID:         record.ActionID,
		RunID:            record.RunID,
		Status:           status,
		Decision:         decision,
		SelectedOptionID: selectedOptionID,
		Answer:           answer,
	}
}

func parsePendingActionDecision(record core.PendingActionRecord) (string, string, string) {
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

func pendingActionDetailDTOFromDomain(item PendingActionDetail) PendingActionDetailDTO {
	return PendingActionDetailDTO{
		ActionID:  item.ActionID,
		RunID:     item.RunID,
		ThreadID:  item.ThreadID,
		Kind:      item.Kind,
		Status:    item.Status,
		Title:     item.Title,
		Body:      item.Body,
		Options:   DefaultConverter.pendingActionOptionDTOsFromDomain(item.Options),
		Payload:   item.Payload,
		Reason:    item.Reason,
		Rule:      item.Rule,
		CreatedAt: item.CreatedAt,
	}
}

func pendingActionListResponseFromDomain(items []PendingActionSummary) PendingActionListResponse {
	return PendingActionListResponse{Items: DefaultConverter.pendingActionSummaryDTOsFromDomain(items)}
}

type PairDeviceRequest struct {
	PairingCode string `json:"pairing_code"`
	DeviceName  string `json:"device_name"`
	Platform    string `json:"platform"`
}

type PairDeviceResponse struct {
	Device      DeviceDTO `json:"device"`
	AccessToken string    `json:"access_token"`
}

type DeviceDTO struct {
	DeviceID   string  `json:"device_id"`
	Name       string  `json:"name"`
	Platform   string  `json:"platform"`
	CreatedAt  string  `json:"created_at"`
	LastSeenAt string  `json:"last_seen_at"`
	RevokedAt  *string `json:"revoked_at,omitempty"`
}

type DeviceListResponse struct {
	Items []DeviceDTO `json:"items"`
}

func optionalDeviceTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	return new(value.UTC().Format(time.RFC3339Nano))
}

func deviceDTOFromView(view DeviceView) DeviceDTO {
	return DeviceDTO{
		DeviceID:   view.DeviceID,
		Name:       view.Name,
		Platform:   view.Platform,
		CreatedAt:  view.CreatedAt.UTC().Format(time.RFC3339Nano),
		LastSeenAt: view.LastSeenAt.UTC().Format(time.RFC3339Nano),
		RevokedAt:  optionalDeviceTime(view.RevokedAt),
	}
}
