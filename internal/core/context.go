package core

import (
	"context"
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
