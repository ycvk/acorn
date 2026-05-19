package contextplane

import "context"

type contextSessionContextKey struct{}

func WithContextSession(ctx context.Context, session ContextSession) context.Context {
	return context.WithValue(ctx, contextSessionContextKey{}, session)
}

func ContextSessionFromContext(ctx context.Context) ContextSession {
	if ctx == nil {
		return nil
	}
	session, ok := ctx.Value(contextSessionContextKey{}).(ContextSession)
	if !ok {
		return nil
	}
	return session
}
