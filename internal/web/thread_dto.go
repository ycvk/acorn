package web

import (
	"time"

	"github.com/ycvk/acorn/internal/app"
)

// ThreadDTO represents a client-visible thread summary.
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

func threadDTOFromDomain(thread app.Thread) ThreadDTO {
	return ThreadDTO{
		ID:            thread.ID,
		Title:         thread.Title,
		WorkspaceRoot: thread.WorkspaceRoot,
		CreatedAt:     thread.CreatedAt,
		UpdatedAt:     thread.UpdatedAt,
		LatestRunID:   thread.LatestRunID,
		State:         thread.State,
	}
}

func threadDTOsFromDomain(items []app.Thread) []ThreadDTO {
	result := make([]ThreadDTO, 0, len(items))
	for _, item := range items {
		result = append(result, threadDTOFromDomain(item))
	}
	return result
}
