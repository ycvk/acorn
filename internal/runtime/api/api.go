package api

import (
	"context"
	"errors"

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

// --- RunContextBridge port ---

// RunContextBridge provides access to the current run and session identifiers
// from a context. It is the single canonical interface for tools and skills
// that need run/session context. Consumers should embed this interface rather
// than re-declaring CurrentRunID/CurrentSessionID.
type RunContextBridge interface {
	CurrentRunID(ctx context.Context) string
	CurrentSessionID(ctx context.Context) string
}

// ToolCallContextBridge extends RunContextBridge with tool-call-scoped identity.
// Tools that need to attribute side-effects to a specific tool call embed this.
type ToolCallContextBridge interface {
	RunContextBridge
	CurrentToolCallID(ctx context.Context) string
}

// --- ExecuteRequest ---

type ExecuteRequest struct {
	RunID            string
	SessionID        string
	TurnIndex        int
	Input            string
	BoundMessageID   int64
	SkillID          string
	AllowedToolNames []string
	Messages         []adk.Message
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
