package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// compactText trims and optionally truncates a string to a rune limit,
// appending "..." when truncated. Returns the result and whether truncation
// occurred.
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

// NewRunID generates a unique run identifier.
func NewRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
}

// newSessionID generates a unique session identifier.
func newSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UTC().UnixNano())
}

// DurableContext returns a copy of ctx that is not cancelled when the parent
// is cancelled. If ctx is nil, it returns context.Background().
func DurableContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

// CurrentRunID returns the run ID stored in the context, or empty string.
func CurrentRunID(ctx context.Context) string {
	return GetRunID(ctx)
}

// InterruptPayloadFromStream converts a StreamInterrupt into a serializable
// map suitable for event payloads. Returns nil if interrupt is nil.
func InterruptPayloadFromStream(interrupt *StreamInterrupt) map[string]any {
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
