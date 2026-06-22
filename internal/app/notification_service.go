package app

import (
	"context"
	"strings"

	"github.com/ycvk/acorn/internal/events"
)

func pendingActionOptionsFromEventOptions(items []events.PendingActionOption) []PendingActionOption {
	if len(items) == 0 {
		return nil
	}
	out := make([]PendingActionOption, 0, len(items))
	for _, item := range items {
		out = append(out, PendingActionOption{
			ID:          strings.TrimSpace(item.ID),
			Label:       strings.TrimSpace(item.Label),
			Description: strings.TrimSpace(item.Description),
		})
	}
	return out
}

func (s *InboxService) projectRunSummary(ctx context.Context, record events.RunRecord) (RunSummary, error) {
	status, err := projectRunStatus(record.Status)
	if err != nil {
		return RunSummary{}, err
	}
	if err != nil {
		return RunSummary{}, err
	}
	session, err := s.store.LoadSession(ctx, record.SessionID)
	if err != nil {
		return RunSummary{}, err
	}
	return RunSummary{
		RunID:          record.RunID,
		ThreadID:       record.SessionID,
		ThreadTitle:    runSummaryThreadTitle(*session, record),
		Status:         status,
		Preview:        runSummaryPreview(record),
		LastEventLabel: runSummaryLastEventLabel(record.Status),
		AttentionLevel: runSummaryAttentionLevel(record.Status),
		DurationMS:     runSummaryDurationMS(record),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}, nil
}

func runSummaryThreadTitle(session events.SessionRecord, run events.RunRecord) string {
	title := strings.TrimSpace(session.Title)
	if title != "" {
		return truncateRunes(title, runSummaryTitleMaxRunes)
	}
	title = strings.TrimSpace(run.Input)
	if title != "" {
		return truncateRunes(compactWhitespace(title), runSummaryTitleMaxRunes)
	}
	return "Untitled thread"
}

func runSummaryPreview(record events.RunRecord) string {
	switch record.Status {
	case events.RunStatusFailed:
		if preview := previewText(record.Error); preview != "" {
			return preview
		}
		if preview := previewText(record.Output); preview != "" {
			return preview
		}
	case events.RunStatusSucceeded:
		if preview := previewText(record.Output); preview != "" {
			return preview
		}
	case events.RunStatusInterrupted, events.RunStatusRunning:
		if preview := previewText(record.Input); preview != "" {
			return preview
		}
	}
	if preview := previewText(record.Input); preview != "" {
		return preview
	}
	if preview := previewText(record.Output); preview != "" {
		return preview
	}
	return ""
}

func previewText(value string) string {
	return truncateRunes(compactWhitespace(value), runSummaryPreviewMaxRunes)
}

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func runSummaryLastEventLabel(status events.RunStatus) string {
	switch status {
	case events.RunStatusRunning:
		return "Run is running"
	case events.RunStatusSucceeded:
		return "Run completed"
	case events.RunStatusInterrupted:
		return "Run interrupted"
	case events.RunStatusFailed:
		return "Run failed"
	default:
		return string(status)
	}
}

func runSummaryAttentionLevel(status events.RunStatus) string {
	switch status {
	case events.RunStatusRunning:
		return "running"
	case events.RunStatusFailed:
		return "failed"
	case events.RunStatusInterrupted:
		return "needs_action"
	default:
		return "normal"
	}
}

func runSummaryDurationMS(record events.RunRecord) int64 {
	if record.UpdatedAt.IsZero() || record.CreatedAt.IsZero() {
		return 0
	}
	duration := record.UpdatedAt.Sub(record.CreatedAt)
	if duration < 0 {
		return 0
	}
	return duration.Milliseconds()
}
