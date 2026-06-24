package api

import (
	"context"
	"strings"

	"github.com/ycvk/acorn/internal/domain"
)

func pendingActionOptionsFromEventOptions(items []domain.PendingActionOption) []PendingActionOption {
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

func (s *InboxService) projectRunSummary(ctx context.Context, record domain.RunRecord) (RunSummary, error) {
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
		UpdatedAt:      record.FinishedAt,
	}, nil
}

func runSummaryThreadTitle(session domain.SessionRecord, run domain.RunRecord) string {
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

func runSummaryPreview(record domain.RunRecord) string {
	switch record.Status {
	case domain.RunStatusFailed:
		if preview := previewText(record.Error); preview != "" {
			return preview
		}
		if preview := previewText(record.Output); preview != "" {
			return preview
		}
	case domain.RunStatusSucceeded:
		if preview := previewText(record.Output); preview != "" {
			return preview
		}
	case domain.RunStatusInterrupted, domain.RunStatusRunning:
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

func runSummaryLastEventLabel(status domain.RunStatus) string {
	switch status {
	case domain.RunStatusRunning:
		return "Run is running"
	case domain.RunStatusSucceeded:
		return "Run completed"
	case domain.RunStatusInterrupted:
		return "Run interrupted"
	case domain.RunStatusFailed:
		return "Run failed"
	default:
		return string(status)
	}
}

func runSummaryAttentionLevel(status domain.RunStatus) string {
	switch status {
	case domain.RunStatusRunning:
		return "running"
	case domain.RunStatusFailed:
		return "failed"
	case domain.RunStatusInterrupted:
		return "needs_action"
	default:
		return "normal"
	}
}

func runSummaryDurationMS(record domain.RunRecord) int64 {
	if record.FinishedAt.IsZero() || record.CreatedAt.IsZero() {
		return 0
	}
	duration := record.FinishedAt.Sub(record.CreatedAt)
	if duration < 0 {
		return 0
	}
	return duration.Milliseconds()
}
