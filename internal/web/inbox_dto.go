package web

import (
	"time"

	"github.com/ycvk/acorn/internal/app"
)

// SystemStatusDTO represents the overall system readiness snapshot.
type SystemStatusDTO struct {
	RuntimeReadiness  RuntimeReadinessDTO     `json:"runtime_readiness"`
	ProviderReadiness []ProviderReadinessDTO  `json:"provider_readiness,omitempty"`
	Model             CapabilitiesModelDTO    `json:"model"`
	WorkspaceRoot     string                  `json:"workspace_root"`
	Summary           CapabilitiesSummaryDTO  `json:"summary"`
	Features          CapabilitiesFeaturesDTO `json:"features"`
}

// InboxResponse is the aggregate view for the mobile inbox.
type InboxResponse struct {
	PendingActions     []PendingActionSummaryDTO `json:"pending_actions"`
	ActiveRuns         []RunSummaryDTO           `json:"active_runs"`
	RecentTerminalRuns []RunSummaryDTO           `json:"recent_terminal_runs"`
	System             SystemStatusDTO           `json:"system"`
}

// RunSummaryDTO is a lightweight summary of a run for list views.
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

// ToolSummaryDTO is an alias to the capabilities tool DTO.
type ToolSummaryDTO = CapabilitiesToolDTO

// ToolListResponse is the response body for listing tools.
type ToolListResponse struct {
	Items []ToolSummaryDTO `json:"items"`
	Total int              `json:"total"`
}

func inboxDTOFromDomain(inbox app.MobileInbox, workspaceRoot string) InboxResponse {
	return InboxResponse{
		PendingActions:     pendingActionSummaryDTOsFromDomain(inbox.PendingActions),
		ActiveRuns:         runSummaryDTOsFromDomain(inbox.ActiveRuns),
		RecentTerminalRuns: runSummaryDTOsFromDomain(inbox.RecentTerminalRuns),
		System:             systemStatusDTOFromSnapshot(inbox.System, workspaceRoot),
	}
}

func runSummaryDTOsFromDomain(items []app.RunSummary) []RunSummaryDTO {
	result := make([]RunSummaryDTO, 0, len(items))
	for _, item := range items {
		result = append(result, RunSummaryDTO{
			RunID:          item.RunID,
			ThreadID:       item.ThreadID,
			ThreadTitle:    item.ThreadTitle,
			Status:         item.Status,
			Mode:           item.Mode,
			Preview:        item.Preview,
			LastEventLabel: item.LastEventLabel,
			AttentionLevel: item.AttentionLevel,
			DurationMS:     item.DurationMS,
			CreatedAt:      item.CreatedAt,
			UpdatedAt:      item.UpdatedAt,
		})
	}
	return result
}

func systemStatusDTOFromSnapshot(snapshot app.SystemCapabilities, workspaceRoot string) SystemStatusDTO {
	return SystemStatusDTO{
		RuntimeReadiness:  runtimeReadinessDTOFromSnapshot(snapshot.RuntimeReadiness),
		ProviderReadiness: providerReadinessDTOsFromSnapshot(snapshot.ProviderReadiness),
		Model:             capabilitiesModelDTOFromSnapshot(snapshot.Model),
		WorkspaceRoot:     workspaceRoot,
		Summary:           capabilitiesSummaryDTOFromSnapshot(snapshot.Summary),
		Features:          capabilitiesFeaturesDTOFromSnapshot(snapshot.Features),
	}
}
