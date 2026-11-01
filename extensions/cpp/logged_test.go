package cpp

import (
	"log/slog"
	"strings"
	"sync"
)

// logged is a writer whose lines a test can read: the log of an activation.
type logged struct {
	mu    sync.Mutex
	lines []string
}

func (l *logged) Write(p []byte) (int, error) {
	l.mu.Lock()
	l.lines = append(l.lines, string(p))
	l.mu.Unlock()
	return len(p), nil
}

func (l *logged) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "")
}

// newLogger is a logger that writes everything, debug included, to w.
func newLogger(w *logged) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
