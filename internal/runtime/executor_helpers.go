package runtime

import (
	"strings"

	"github.com/ycvk/acorn/internal/events"
)

func compactArchiveText(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= 280 {
		return trimmed
	}
	return trimmed[:280] + "..."
}

func failureReasonForStatus(status events.RunStatus, output string) string {
	if status != events.RunStatusFailed {
		return ""
	}
	if strings.TrimSpace(output) == "" {
		return "run_failed"
	}
	return "run_failed:with_output"
}
