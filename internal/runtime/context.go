package runtime

import (
	"context"
)

func DurableContext(ctx context.Context) context.Context {
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
