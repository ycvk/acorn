package runtime

import (
	"fmt"
	"strings"
	"time"
)

func compactInterruptInfo(value any) any {
	data, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"kind", "message", "question", "action_id", "command", "command_name", "command_args", "cwd", "url", "tool_name", "interrupt_id", "arguments_json", "reason", "rule"} {
		if current, exists := data[key]; exists {
			out[key] = current
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactText(value string, limit int) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	runes := []rune(trimmed)
	if limit <= 0 || len(runes) <= limit {
		return trimmed, false
	}
	return string(runes[:limit]) + "...", true
}

func newRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
}

func newSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UTC().UnixNano())
}

func extractString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func interruptPayloadFromStream(interrupt *StreamInterrupt) map[string]any {
	if interrupt == nil {
		return nil
	}
	payload := map[string]any{"context_count": interrupt.ContextCount}
	contexts := make([]map[string]any, 0, len(interrupt.Contexts))
	for _, item := range interrupt.Contexts {
		contexts = append(contexts, map[string]any{
			"id":            item.ID,
			"address":       item.Address,
			"info":          item.Info,
			"is_root_cause": item.IsRootCause,
		})
	}
	payload["contexts"] = contexts
	return payload
}
