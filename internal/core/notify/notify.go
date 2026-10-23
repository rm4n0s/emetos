package notify

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Severity is how serious a notification is.
type Severity int

// The severities, as in VS Code.
const (
	// Info is for news the user may like to know.
	Info Severity = iota
	// Warning is for something that may not work as the user expects.
	Warning
	// Error is for a failure the user should know about.
	Error
)

// String is the name of the severity: "info", "warning" or "error".
func (s Severity) String() string {
	switch s {
	case Warning:
		return "warning"
	case Error:
		return "error"
	}
	return "info"
}

// level is the log level a severity is written at.
func (s Severity) level() slog.Level {
	switch s {
	case Warning:
		return slog.LevelWarn
	case Error:
		return slog.LevelError
	}
	return slog.LevelInfo
}

// Action is a button of a notification: it runs a command.
type Action struct {
	// Label is the text of the button.
	Label string
	// Command is the id of the command the button runs.
	Command string
	// Args are the JSON-compatible arguments of the command; may be nil.
	Args any
}

// Notification is what the app wants to tell the user.
type Notification struct {
	// Severity says how serious the notification is.
	Severity Severity
	// Message is the text shown; a notification without one is not sent.
	Message string
	// Source says which part of the app or which feature sent it, such as "Git".
	Source string
	// Actions are the buttons.
	Actions []Action
}

// Item is a notification the service holds.
type Item struct {
	Notification
	// ID identifies the item for Dismiss; IDs start at 1.
	ID uint64
	// Time is when it was sent.
	Time time.Time
}

// EventKind tells what happened to an item.
type EventKind int

// The kinds of event.
const (
	// Added means a new item is in the list.
	Added EventKind = iota
	// Dismissed means an item left the list.
	Dismissed
)

// Event is a change of the list of items.
type Event struct {
	Kind EventKind
	Item Item
}

// Service is the model of notifications (SPEC §10.7): it holds them, tells the listeners
// of changes, and writes each one to the log, so that none is lost while the UI that shows
// them (step 6.11) is not there. It is safe for concurrent use.
type Service struct {
	log *slog.Logger
	now func() time.Time

	mu        sync.Mutex
	next      uint64
	items     []Item
	listeners map[int]func(Event)
	nextL     int
}

// NewService makes a service. log gets every notification; nil means slog.Default(). now is
// the clock for Item.Time; nil means time.Now.
func NewService(log *slog.Logger, now func() time.Time) *Service {
	if log == nil {
		log = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	return &Service{log: log, now: now, listeners: map[int]func(Event){}}
}

// Notify adds a notification and returns its ID, or 0 when it has no message. The
// listeners are called before it returns, in the goroutine of the caller, with no lock held.
func (s *Service) Notify(n Notification) uint64 {
	if n.Message == "" {
		return 0
	}
	n.Actions = slices.Clone(n.Actions)
	s.mu.Lock()
	s.next++
	item := Item{Notification: n, ID: s.next, Time: s.now()}
	s.items = append(s.items, item)
	s.mu.Unlock()

	attrs := []any{"id", item.ID}
	if n.Source != "" {
		attrs = append(attrs, "source", n.Source)
	}
	s.log.Log(context.Background(), n.Severity.level(), "notification: "+n.Message, attrs...)
	s.emit(Event{Kind: Added, Item: item})
	return item.ID
}

// Info sends an information.
func (s *Service) Info(source, message string, actions ...Action) uint64 {
	return s.Notify(Notification{Severity: Info, Source: source, Message: message, Actions: actions})
}

// Warn sends a warning.
func (s *Service) Warn(source, message string, actions ...Action) uint64 {
	return s.Notify(Notification{Severity: Warning, Source: source, Message: message, Actions: actions})
}

// Error sends an error.
func (s *Service) Error(source, message string, actions ...Action) uint64 {
	return s.Notify(Notification{Severity: Error, Source: source, Message: message, Actions: actions})
}

// Dismiss removes an item and reports whether it was there.
func (s *Service) Dismiss(id uint64) bool {
	s.mu.Lock()
	i := slices.IndexFunc(s.items, func(it Item) bool { return it.ID == id })
	if i < 0 {
		s.mu.Unlock()
		return false
	}
	item := s.items[i]
	s.items = slices.Delete(s.items, i, i+1)
	s.mu.Unlock()
	s.emit(Event{Kind: Dismissed, Item: item})
	return true
}

// Items lists the notifications that were not dismissed, oldest first.
func (s *Service) Items() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.items)
}

// OnChange registers a listener of changes of the list and returns the function that
// removes it.
func (s *Service) OnChange(f func(Event)) (remove func()) {
	s.mu.Lock()
	id := s.nextL
	s.nextL++
	s.listeners[id] = f
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.listeners, id)
		s.mu.Unlock()
	}
}

func (s *Service) emit(e Event) {
	s.mu.Lock()
	ids := make([]int, 0, len(s.listeners))
	for id := range s.listeners {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	fs := make([]func(Event), len(ids))
	for i, id := range ids {
		fs[i] = s.listeners[id]
	}
	s.mu.Unlock()
	for _, f := range fs {
		f(e)
	}
}
