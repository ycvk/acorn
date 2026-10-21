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
	// ToolName and ToolSummary are set on tool approvals.
	ToolName    string `json:"tool_name,omitempty"`
	ToolSummary string `json:"tool_summary,omitempty"`
	// AllowFreeform is set on operator questions that take a typed answer.
	AllowFreeform bool `json:"allow_freeform,omitempty"`
}

type CreateRunRequest struct {
	SkillID string   `json:"skill_id,omitempty"`
	Input   string   `json:"input,omitempty"`
	FileIDs []string `json:"file_ids,omitempty"`
}

// RunDTO represents a client-visible run summary.
type RunDTO struct {
	ID          string     `json:"id"`
	ThreadID    string     `json:"thread_id"`
	Status      string     `json:"status"`
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

type RunDetailDTO struct {
	Run       RunDTO               `json:"run"`
	Thread    ThreadDTO            `json:"thread"`
	Events    []core.RunEvent      `json:"events"`
	Artifacts []ArtifactSummaryDTO `json:"artifacts"`
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
		CreatedAt: run.CreatedAt,
	}
	if !run.CompletedAt.IsZero() {
		dto.CompletedAt = &run.CompletedAt
	}
	return dto
}

type ThreadDTO struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	LatestRunID string    `json:"latest_run_id,omitempty"`
	State       string    `json:"state"`
	Preview     string    `json:"preview,omitempty"`
	PreviewRole string    `json:"preview_role,omitempty"`
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

type MessageContentDTO struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// MessageDTO represents a single message in a thread.
type MessageDTO struct {
	ID        string            `json:"id"`
	ThreadID  string            `json:"thread_id"`
	Role      string            `json:"role"`
	Content   MessageContentDTO `json:"content"`
	CreatedAt time.Time         `json:"created_at"`
	RunID     string            `json:"run_id,omitempty"`
	Steps     []ToolStepDTO     `json:"steps,omitempty"`
	Files     []ThreadFileDTO   `json:"files,omitempty"`
	Views     []core.ViewData   `json:"views,omitempty"`
}

// SendViewActionRequest is what the owner pressed or chose in a view.
type SendViewActionRequest struct {
	ComponentID string   `json:"component_id"`
	Scope       []int    `json:"scope,omitempty"`
	Selected    []string `json:"selected,omitempty"`
}

// ViewActionResponse is the run a view action started, the owner's message
// it sent, and the view with the action marked as sent.
type ViewActionResponse struct {
	Run  RunDTO        `json:"run"`
	Text string        `json:"text"`
	View core.ViewData `json:"view"`
}

// ThreadFileDTO describes a file in a thread.
type ThreadFileDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	MIMEType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	Origin    string    `json:"origin"`
	CreatedAt time.Time `json:"created_at"`
}

// ToolStepDTO is one tool call behind an assistant message.
type ToolStepDTO struct {
	ToolName string `json:"tool_name"`
	Summary  string `json:"summary,omitempty"`
	Outcome  string `json:"outcome"`
	Error    string `json:"error,omitempty"`
}

// MessageListResponse is the response body for listing messages.
type MessageListResponse struct {
	Items []MessageDTO `json:"items"`
}

// CreateMessageRequest is the body for creating a message.
type CreateMessageRequest struct {
	Content MessageContentDTO `json:"content" validate:"required"`
}
