package api

import "context"

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
