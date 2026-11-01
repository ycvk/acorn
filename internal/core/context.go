package core

import (
	"context"
)

type runIDContextKey struct{}

func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDContextKey{}, runID)
}

// CurrentRunID returns the run ID stored in the context, or empty string.
func CurrentRunID(ctx context.Context) string {
	v, ok := ctx.Value(runIDContextKey{}).(string)
	if !ok {
		return ""
	}
	return v
}

type wakeContextKey struct{}

// WithWake records what woke the current run, e.g. "commitment #12: ...".
func WithWake(ctx context.Context, wake string) context.Context {
	return context.WithValue(ctx, wakeContextKey{}, wake)
}

// CurrentWake returns what woke the current run, or "" when not set.
func CurrentWake(ctx context.Context) string {
	v, ok := ctx.Value(wakeContextKey{}).(string)
	if !ok {
		return ""
	}
	return v
}

type sessionIDContextKey struct{}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDContextKey{}, sessionID)
}

func CurrentSessionID(ctx context.Context) string {
	v, ok := ctx.Value(sessionIDContextKey{}).(string)
	if !ok {
		return ""
	}
	return v
}
