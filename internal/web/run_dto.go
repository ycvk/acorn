package web

import (
	"time"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/runtime"
)

// CreateRunRequest is the body for creating a new run.
type CreateRunRequest struct {
	SkillID string `json:"skill_id,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

// ResumeRunRequest is the body for resuming an interrupted run.
type ResumeRunRequest struct{}

// RunDTO represents a client-visible run summary.
type RunDTO struct {
	ID          string     `json:"id"`
	ThreadID    string     `json:"thread_id"`
	Status      string     `json:"status"`
	Mode        string     `json:"mode"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// RunEventDTO represents a single event in a run event stream.
type RunEventDTO struct {
	EventID string    `json:"event_id"`
	RunID   string    `json:"run_id"`
	Seq     int64     `json:"seq"`
	TS      time.Time `json:"ts"`
	Type    string    `json:"type"`
	Data    any       `json:"data"`
}

// UnsupportedRunEventDTO captures events the client does not yet understand.
type UnsupportedRunEventDTO struct {
	EventID string         `json:"event_id"`
	RunID   string         `json:"run_id"`
	Seq     int64          `json:"seq"`
	TS      time.Time      `json:"ts"`
	Type    string         `json:"type"`
	Raw     map[string]any `json:"raw,omitempty"`
	Reason  string         `json:"reason"`
}

// RunDetailDTO aggregates a run, its thread, events, and workbench.
type RunDetailDTO struct {
	Run       RunDTO                `json:"run"`
	Thread    ThreadDTO             `json:"thread"`
	Events    []RunEventDTO         `json:"events"`
	Workbench *RuntimeWorkbenchDTO  `json:"workbench"`
	Trace     *runtime.TraceSummary `json:"trace"`
	Raw       *RunDetailRawDTO      `json:"raw,omitempty"`
}

// RunDetailRawDTO holds raw unsupported events for diagnostic purposes.
type RunDetailRawDTO struct {
	UnsupportedEvents []UnsupportedRunEventDTO `json:"unsupported_events"`
}

// InterruptRunResponse is returned after requesting a run interruption.
type InterruptRunResponse struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

func runDTOFromDomain(run app.Run) RunDTO {
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

func runEventDTOFromDomain(event app.RunEvent) RunEventDTO {
	return RunEventDTO{
		EventID: event.EventID,
		RunID:   event.RunID,
		Seq:     event.Seq,
		TS:      event.TS,
		Type:    event.Type,
		Data:    event.Data,
	}
}

func runEventDTOsFromDomain(items []app.RunEvent) []RunEventDTO {
	result := make([]RunEventDTO, 0, len(items))
	for _, item := range items {
		result = append(result, runEventDTOFromDomain(item))
	}
	return result
}

func unsupportedRunEventDTOFromDomain(event app.UnsupportedRunEvent) UnsupportedRunEventDTO {
	return UnsupportedRunEventDTO{
		EventID: event.EventID,
		RunID:   event.RunID,
		Seq:     event.Seq,
		TS:      event.TS,
		Type:    event.Type,
		Raw:     event.Raw,
		Reason:  event.Reason,
	}
}

func unsupportedRunEventDTOsFromDomain(items []app.UnsupportedRunEvent) []UnsupportedRunEventDTO {
	result := make([]UnsupportedRunEventDTO, 0, len(items))
	for _, item := range items {
		result = append(result, unsupportedRunEventDTOFromDomain(item))
	}
	return result
}
