package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/stream"
)

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

func InterruptPayloadFromStream(interrupt *stream.StreamInterrupt) map[string]any {
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

func DurableContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func CurrentRunID(ctx context.Context) string {
	return runtimeapi.GetRunID(ctx)
}
