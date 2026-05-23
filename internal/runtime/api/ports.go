package api

import (
	"context"

	"github.com/ycvk/acorn/internal/events"
)

type EventAppender interface {
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error)
}
