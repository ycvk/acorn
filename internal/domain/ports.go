package domain

import "context"

// EventAppender appends a runtime event to the persisted store.
type EventAppender interface {
	AppendEvent(ctx context.Context, runID, kind string, payload any) (EventRecord, error)
}

// ToolCallContextBridge provides access to the current run, session, and
// tool-call identifiers. Used by toolset as the context port for attributing
// artifacts and evidence to specific runs/sessions/tool-calls.
type ToolCallContextBridge interface {
	CurrentRunID(ctx context.Context) string
	CurrentSessionID(ctx context.Context) string
	CurrentToolCallID(ctx context.Context) string
}
