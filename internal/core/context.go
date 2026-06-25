package core

import (
	"context"
	"strings"
)

type runIDContextKey struct{}

func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDContextKey{}, runID)
}

func GetRunID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, ok := ctx.Value(runIDContextKey{}).(string)
	if !ok {
		return ""
	}
	return v
}

type sessionIDContextKey struct{}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDContextKey{}, sessionID)
}

func GetSessionID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, ok := ctx.Value(sessionIDContextKey{}).(string)
	if !ok {
		return ""
	}
	return v
}

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

// --- Call-site context plumbing ---

const (
	CallSiteRuntime    = "runtime"
	CallSiteAssistant  = "assistant"
	CallSiteCompaction = "compaction"
)

type callSiteContextKey struct{}

func WithCallSite(ctx context.Context, callSite string) context.Context {
	trimmed := strings.TrimSpace(callSite)
	if trimmed == "" {
		return ctx
	}
	return context.WithValue(ctx, callSiteContextKey{}, trimmed)
}


type streamSinkContextKey struct{}

// WithStreamSink attaches a StreamSink to the context for retrieval by StreamSinkFromContext.
func WithStreamSink(ctx context.Context, sink StreamSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, streamSinkContextKey{}, sink)
}

// StreamSinkFromContext retrieves the StreamSink previously attached via WithStreamSink.
func StreamSinkFromContext(ctx context.Context) StreamSink {
	if ctx == nil {
		return nil
	}
	sink, ok := ctx.Value(streamSinkContextKey{}).(StreamSink)
	if !ok {
		return nil
	}
	return sink
}
