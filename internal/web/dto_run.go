package web

import (
	"time"

	"github.com/ycvk/acorn/internal/app"
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

func runSummaryDTOsFromDomain(items []app.RunSummary) []RunSummaryDTO {
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
