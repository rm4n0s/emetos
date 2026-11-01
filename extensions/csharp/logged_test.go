package csharp

import "log/slog"

// newLogger is a logger that writes everything, debug included, to w.
func newLogger(w *logged) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
