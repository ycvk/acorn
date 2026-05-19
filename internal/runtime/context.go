package runtime

import (
	"context"
)

func durableContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func CurrentRunID(ctx context.Context) string {
	return getRunID(ctx)
}

func CurrentStreamSink(ctx context.Context) StreamSink {
	return streamSinkFromContext(ctx)
}

// --- Store context plumbing ---

type storeContextKey struct{}

func withStore(ctx context.Context, store any) context.Context {
	return context.WithValue(ctx, storeContextKey{}, store)
}

// --- Session ID context plumbing ---

type sessionIDContextKey struct{}

func withSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDContextKey{}, sessionID)
}

func sessionIDFromContext(ctx context.Context) string {
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

func withTurnIndex(ctx context.Context, turnIndex int) context.Context {
	return context.WithValue(ctx, turnIndexContextKey{}, turnIndex)
}

func turnIndexFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	index, ok := ctx.Value(turnIndexContextKey{}).(int)
	if !ok {
		return 0
	}
	return index
}
