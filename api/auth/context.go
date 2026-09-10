package auth

import "context"

type contextKey struct{}

// ContextWithSession returns a copy of ctx carrying sess, retrievable via
// SessionFromContext.
func ContextWithSession(ctx context.Context, sess Session) context.Context {
	return context.WithValue(ctx, contextKey{}, sess)
}

// SessionFromContext returns the Session stashed by RequireSession (or a
// consuming service's own equivalent middleware), if any.
func SessionFromContext(ctx context.Context) (Session, bool) {
	sess, ok := ctx.Value(contextKey{}).(Session)
	return sess, ok
}
