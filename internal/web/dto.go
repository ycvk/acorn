package web

import (
	"time"

	"github.com/ycvk/acorn/internal/runtimehistory"
)

func optionalDeviceTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	return new(value.UTC().Format(time.RFC3339Nano))
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func summaryText(summary *runtimehistory.SessionSummary) string {
	if summary == nil {
		return ""
	}
	return summary.Summary
}

func summaryStatus(summary *runtimehistory.SessionSummary) string {
	if summary == nil {
		return ""
	}
	return summary.RunStatus
}

func summarySourceRunID(summary *runtimehistory.SessionSummary) string {
	if summary == nil {
		return ""
	}
	return summary.SourceRunID
}

func summaryUpdatedAt(summary *runtimehistory.SessionSummary) *time.Time {
	if summary == nil {
		return nil
	}
	return &summary.UpdatedAt
}
