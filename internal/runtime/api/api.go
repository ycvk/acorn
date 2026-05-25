package api

import (
	"context"
	"errors"
	"time"

	"github.com/cloudwego/eino/adk"

	"github.com/ycvk/acorn/internal/events"
)

// --- Errors ---

var (
	ErrRunNotActive      = errors.New("run not active")
	ErrRunNotInterrupted = errors.New("run not interrupted")
	ErrExecutionNotReady = errors.New("execution not ready")
)

// --- EventAppender port ---

type EventAppender interface {
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error)
}

// --- ExecuteRequest ---

type ExecuteRequest struct {
	RunID             string
	SessionID         string
	TurnIndex         int
	Input             string
	SkillID           string
	AllowedToolNames  []string
	Messages          []adk.Message
	OrchestrationMode events.OrchestrationMode
	ParentRunID       string
	Depth             int
}

// --- Context plumbing helpers ---

type runIDContextKey struct{}

func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDContextKey{}, runID)
}

func GetRunID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, ok := ctx.Value(runIDContextKey{}).(string)
	if !ok {
		return ""
	}
	return id
}

type sessionIDContextKey struct{}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDContextKey{}, sessionID)
}

func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, ok := ctx.Value(sessionIDContextKey{}).(string)
	if !ok {
		return ""
	}
	return id
}

type storeContextKey struct{}

func WithStore(ctx context.Context, store any) context.Context {
	return context.WithValue(ctx, storeContextKey{}, store)
}

// --- Turn index context plumbing ---

type turnIndexContextKey struct{}

func WithTurnIndex(ctx context.Context, turnIndex int) context.Context {
	return context.WithValue(ctx, turnIndexContextKey{}, turnIndex)
}

func TurnIndexFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	index, ok := ctx.Value(turnIndexContextKey{}).(int)
	if !ok {
		return 0
	}
	return index
}

// --- Session state ---

type SessionState string

const (
	SessionStateNew         SessionState = "new"
	SessionStateRunning     SessionState = "running"
	SessionStateCompleted   SessionState = "completed"
	SessionStateFailed      SessionState = "failed"
	SessionStateInterrupted SessionState = "interrupted"
	SessionStateDegraded    SessionState = "degraded"
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

// --- Evidence types ---

type EvidenceKind string

const (
	EvidenceKindTool       EvidenceKind = "tool"
	EvidenceKindCommand    EvidenceKind = "command"
	EvidenceKindDiff       EvidenceKind = "diff"
	EvidenceKindCheckpoint EvidenceKind = "checkpoint"
	EvidenceKindRollback   EvidenceKind = "rollback"
	EvidenceKindTest       EvidenceKind = "test"
	EvidenceKindSubagent   EvidenceKind = "subagent"
	EvidenceKindVerifier   EvidenceKind = "verifier"
	EvidenceKindManual     EvidenceKind = "manual"
)

type EvidenceStatus string

const (
	EvidenceStatusRecorded  EvidenceStatus = "recorded"
	EvidenceStatusPassed    EvidenceStatus = "passed"
	EvidenceStatusFailed    EvidenceStatus = "failed"
	EvidenceStatusConfirmed EvidenceStatus = "confirmed"
)

type PlanEvidence struct {
	ID            string         `json:"id"`
	StepID        string         `json:"step_id"`
	Kind          EvidenceKind   `json:"kind"`
	Status        EvidenceStatus `json:"status"`
	Summary       string         `json:"summary"`
	ToolResultRef string         `json:"tool_result_ref,omitempty"`
	ToolName      string         `json:"tool_name,omitempty"`
	Command       []string       `json:"command,omitempty"`
	Paths         []string       `json:"paths,omitempty"`
	DiffRef       string         `json:"diff_ref,omitempty"`
	ChildRunID    string         `json:"child_run_id,omitempty"`
	Error         string         `json:"error,omitempty"`
	SourceRunID   string         `json:"source_run_id"`
	RecordedAt    time.Time      `json:"recorded_at"`
}
