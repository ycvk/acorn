package stream

import "context"

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
