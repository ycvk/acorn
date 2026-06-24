package core

import (
	"context"
	"fmt"
	"time"
)

// NewRunID generates a unique run identifier.
func NewRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
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
