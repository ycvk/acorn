package web

import (
	"time"

	"github.com/ycvk/acorn/internal/app"
)

type MessageContentDTO struct {
	Type  string           `json:"type"`
	Text  string           `json:"text"`
	Parts []MessagePartDTO `json:"parts,omitempty"`
}

// MessageDTO represents a single message in a thread.
type MessageDTO struct {
	ID        string            `json:"id"`
	ThreadID  string            `json:"thread_id"`
	Role      string            `json:"role"`
	Content   MessageContentDTO `json:"content"`
	CreatedAt time.Time         `json:"created_at"`
	RunID     string            `json:"run_id,omitempty"`
}

// MessageListResponse is the response body for listing messages.
type MessageListResponse struct {
	Items []MessageDTO `json:"items"`
}

// CreateMessageRequest is the body for creating a message.
type CreateMessageRequest struct {
	Content MessageContentDTO `json:"content" validate:"required"`
}

// MessagePartDTO represents a rich-content part inside a message.
type MessagePartDTO struct {
	Kind             string              `json:"kind"`
	Text             string              `json:"text,omitempty"`
	Reasoning        string              `json:"reasoning,omitempty"`
	Status           string              `json:"status,omitempty"`
	Title            string              `json:"title,omitempty"`
	Summary          string              `json:"summary,omitempty"`
	Changed          []string            `json:"changed,omitempty"`
	Verified         []string            `json:"verified,omitempty"`
	Risks            []string            `json:"risks,omitempty"`
	Items            []DisclosureItemDTO `json:"items,omitempty"`
	DetailRunID      string              `json:"detail_run_id,omitempty"`
	RunID            string              `json:"run_id,omitempty"`
	Label            string              `json:"label,omitempty"`
	DecisionID       string              `json:"decision_id,omitempty"`
	Question         string              `json:"question,omitempty"`
	SelectedOptionID string              `json:"selected_option_id,omitempty"`
	Answer           string              `json:"answer,omitempty"`
	Options          []DecisionOptionDTO `json:"options,omitempty"`
	Action           *MessageActionDTO   `json:"action,omitempty"`
}

// DisclosureItemDTO represents a single disclosure item inside a result part.
type DisclosureItemDTO struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Tone    string `json:"tone"`
	SkillID string `json:"skill_id,omitempty"`
}

// DecisionOptionDTO represents a single option in a decision part.
type DecisionOptionDTO struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// MessageActionDTO represents an action attached to a message part.
type MessageActionDTO struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
	Label string `json:"label"`
}

func messageDTOFromDomain(message app.Message) MessageDTO {
	return MessageDTO{
		ID:       message.ID,
		ThreadID: message.ThreadID,
		Role:     message.Role,
		Content: MessageContentDTO{
			Type:  message.Content.Type,
			Text:  message.Content.Text,
			Parts: messagePartDTOsFromDomain(message.Content.Parts),
		},
		CreatedAt: message.CreatedAt,
		RunID:     message.RunID,
	}
}

func messagePartDTOsFromDomain(parts []app.MessagePart) []MessagePartDTO {
	if len(parts) == 0 {
		return nil
	}
	items := make([]MessagePartDTO, 0, len(parts))
	for _, part := range parts {
		item := MessagePartDTO{
			Kind:             part.Kind,
			Text:             part.Text,
			Reasoning:        part.Reasoning,
			Status:           part.Status,
			Title:            part.Title,
			Summary:          part.Summary,
			Changed:          part.Changed,
			Verified:         part.Verified,
			Risks:            part.Risks,
			Items:            disclosureItemDTOsFromDomain(part.Items),
			DetailRunID:      part.DetailRunID,
			RunID:            part.RunID,
			Label:            part.Label,
			DecisionID:       part.DecisionID,
			Question:         part.Question,
			SelectedOptionID: part.SelectedOptionID,
			Answer:           part.Answer,
			Options:          decisionOptionDTOsFromDomain(part.Options),
			Action:           messageActionDTOFromDomain(part.Action),
		}
		if part.Kind == "result" {
			item.Changed = nonNilStrings(item.Changed)
			item.Verified = nonNilStrings(item.Verified)
			item.Risks = nonNilStrings(item.Risks)
		}
		items = append(items, item)
	}
	return items
}

func disclosureItemDTOsFromDomain(items []app.DisclosureItem) []DisclosureItemDTO {
	return DefaultConverter.disclosureItemDTOsFromDomain(items)
}

func messageActionDTOFromDomain(action *app.MessageAction) *MessageActionDTO {
	if action == nil {
		return nil
	}
	return &MessageActionDTO{
		Kind:  action.Kind,
		RunID: action.RunID,
		Label: action.Label,
	}
}

func messageDTOsFromDomain(items []app.Message) []MessageDTO {
	result := make([]MessageDTO, 0, len(items))
	for _, item := range items {
		result = append(result, messageDTOFromDomain(item))
	}
	return result
}
