package debug

import "context"

// sessionKey is the key of the session in the context that a debugger's Connect gets.
type sessionKey struct{}

// WithSession returns a context that carries the session whose adapter is being reached. The service gives it to Debugger.Connect, so
// that the debugger of an extension can hand the session to the extension (the descriptor factory and the trackers of step 25.3 are
// asked about a session, as in VS Code) without changing the signature of ConnectFunc.
func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// SessionFromContext returns the session that a context of Debugger.Connect carries.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(*Session)
	return s, ok && s != nil
}
