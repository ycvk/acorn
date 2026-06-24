package api

import (
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type RunSummaryDTO struct {
	RunID          string    `json:"run_id"`
	ThreadID       string    `json:"thread_id"`
	ThreadTitle    string    `json:"thread_title"`
	Status         string    `json:"status"`
	Mode           string    `json:"mode"`
	Preview        string    `json:"preview"`
	LastEventLabel string    `json:"last_event_label"`
	AttentionLevel string    `json:"attention_level"`
	DurationMS     int64     `json:"duration_ms"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PendingActionSummaryDTO is a lightweight summary of a pending action.
type PendingActionSummaryDTO struct {
	ActionID  string                   `json:"action_id"`
	RunID     string                   `json:"run_id"`
	ThreadID  string                   `json:"thread_id"`
	Kind      string                   `json:"kind"`
	Status    string                   `json:"status"`
	Title     string                   `json:"title"`
	Body      string                   `json:"body,omitempty"`
	Options   []PendingActionOptionDTO `json:"options"`
	CreatedAt time.Time                `json:"created_at"`
}

func runSummaryDTOsFromDomain(items []RunSummary) []RunSummaryDTO {
	return DefaultConverter.runSummaryDTOsFromDomain(items)
}

type CreateRunRequest struct {
	SkillID string `json:"skill_id,omitempty"`
	// Mode is accepted for backward compatibility with old clients but no
	// longer read — the runtime always uses direct_response.
	Mode  string `json:"mode,omitempty"`
	Input string `json:"input,omitempty"`
}

// RunDTO represents a client-visible run summary.
type RunDTO struct {
	ID          string     `json:"id"`
	ThreadID    string     `json:"thread_id"`
	Status      string     `json:"status"`
	Mode        string     `json:"mode"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type ArtifactSummaryDTO struct {
	ArtifactID          string    `json:"artifact_id"`
	RunID               string    `json:"run_id"`
	SessionID           string    `json:"session_id,omitempty"`
	SourceToolResultRef string    `json:"source_tool_result_ref,omitempty"`
	Kind                string    `json:"kind"`
	Title               string    `json:"title,omitempty"`
	MIMEType            string    `json:"mime_type,omitempty"`
	SizeBytes           int64     `json:"size_bytes"`
	SHA256              string    `json:"sha256"`
	CreatedAt           time.Time `json:"created_at"`
}

func artifactSummaryDTOsFromDomain(items []ArtifactSummary) []ArtifactSummaryDTO {
	return DefaultConverter.artifactSummaryDTOsFromDomain(items)
}

type RunDetailDTO struct {
	Run       RunDTO                  `json:"run"`
	Thread    ThreadDTO               `json:"thread"`
	Events    []core.RunEvent `json:"events"`
	Artifacts []ArtifactSummaryDTO    `json:"artifacts"`
}

// InterruptRunResponse is returned after requesting a run interruption.
type InterruptRunResponse struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

func runDTOFromDomain(run Run) RunDTO {
	dto := RunDTO{
		ID:        run.ID,
		ThreadID:  run.ThreadID,
		Status:    run.Status,
		Mode:      run.Mode,
		CreatedAt: run.CreatedAt,
	}
	if !run.CompletedAt.IsZero() {
		dto.CompletedAt = &run.CompletedAt
	}
	return dto
}

type ThreadDTO struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	WorkspaceRoot string    `json:"workspace_root"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	LatestRunID   string    `json:"latest_run_id,omitempty"`
	State         string    `json:"state"`
}

// ThreadListResponse is the response body for listing threads.
type ThreadListResponse struct {
	Items []ThreadDTO `json:"items"`
}

// CreateThreadRequest is the body for creating a new thread.
type CreateThreadRequest struct {
	Title string `json:"title,omitempty"`
}

// UpdateThreadRequest is the body for updating a thread.
type UpdateThreadRequest struct {
	Title string `json:"title"`
}

func threadDTOFromDomain(thread Thread) ThreadDTO {
	return DefaultConverter.threadDTOFromDomain(thread)
}

func threadDTOsFromDomain(items []Thread) []ThreadDTO {
	return DefaultConverter.threadDTOsFromDomain(items)
}

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

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func messageDTOFromDomain(message Message) MessageDTO {
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

func messagePartDTOsFromDomain(parts []MessagePart) []MessagePartDTO {
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

func disclosureItemDTOsFromDomain(items []DisclosureItem) []DisclosureItemDTO {
	return DefaultConverter.disclosureItemDTOsFromDomain(items)
}

func messageActionDTOFromDomain(action *MessageAction) *MessageActionDTO {
	if action == nil {
		return nil
	}
	return &MessageActionDTO{
		Kind:  action.Kind,
		RunID: action.RunID,
		Label: action.Label,
	}
}

func messageDTOsFromDomain(items []Message) []MessageDTO {
	result := make([]MessageDTO, 0, len(items))
	for _, item := range items {
		result = append(result, messageDTOFromDomain(item))
	}
	return result
}
