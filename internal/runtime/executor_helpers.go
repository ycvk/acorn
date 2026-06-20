package runtime

import (
	"sort"
	"strings"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/store"
)

func archiveSignalsFromToolResults(records []store.ToolResultRecord) []string {
	pathSet := make(map[string]struct{})
	for _, record := range records {
		for _, effect := range record.SideEffects {
			if path := strings.TrimSpace(effect.Path); path != "" {
				pathSet[path] = struct{}{}
			}
		}
	}
	return sortedKeys(pathSet)
}

func toolNamesFromToolResults(records []store.ToolResultRecord) []string {
	toolSet := make(map[string]struct{})
	for _, record := range records {
		if toolName := strings.TrimSpace(record.ToolName); toolName != "" {
			toolSet[toolName] = struct{}{}
		}
	}
	return sortedKeys(toolSet)
}

func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

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
