package runtime

import "github.com/ycvk/acorn/internal/events"

type SessionState string

const (
	// SessionStateNew indicates the session has no persisted runs yet.
	SessionStateNew SessionState = "new"
	// SessionStateRunning indicates the latest run is still in progress.
	SessionStateRunning SessionState = "running"
	// SessionStateCompleted indicates the latest run finished successfully.
	SessionStateCompleted SessionState = "completed"
	// SessionStateFailed indicates the latest run failed.
	SessionStateFailed SessionState = "failed"
	// SessionStateInterrupted indicates the latest run was interrupted and may be resumable.
	SessionStateInterrupted SessionState = "interrupted"
	// SessionStateDegraded indicates the session's run state is determinable but
	// the runtime environment has issues (e.g., MCP provider unavailable).
	SessionStateDegraded SessionState = "degraded"
)

// DeriveSessionState determines a session's state from its latest run record
// and provider health, without any event replay.
func DeriveSessionState(latestRun *events.RunRecord, hasDegradedProvider bool) SessionState {
	if latestRun == nil {
		return SessionStateNew
	}
	switch latestRun.Status {
	case events.RunStatusSucceeded:
		if hasDegradedProvider {
			return SessionStateDegraded
		}
		return SessionStateCompleted
	case events.RunStatusFailed:
		return SessionStateFailed
	case events.RunStatusRunning:
		return SessionStateRunning
	case events.RunStatusInterrupted:
		if hasDegradedProvider {
			return SessionStateDegraded
		}
		return SessionStateInterrupted
	default:
		return SessionStateDegraded
	}
}
