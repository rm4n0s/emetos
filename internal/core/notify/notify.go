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

// Action is a button of a notification. When the user presses it, the shell calls Run, if there
// is one, and runs Command, if there is one, and then closes the notification unless KeepOpen is
// set. The first action of a notification is shown as the primary button (VS Code's primary
// actions), the others as secondary buttons.
type Action struct {
	// Label is the text of the button.
	Label string
	// Command is the id of the command the button runs; may be empty.
	Command string
	// Args are the JSON-compatible arguments of the command; may be nil.
	Args any
	// Run is called on the UI thread when the button is pressed; may be nil.
	Run func()
	// KeepOpen keeps the notification open after the action ran (VS Code's ChoiceAction.keepOpen).
	KeepOpen bool
}

// Progress is the progress of a long running operation that a notification shows as a bar along its
// bottom (VS Code's INotificationProgress).
type Progress struct {
	// Infinite shows a bar with no end; it takes the place of Total and Worked.
	Infinite bool
	// Total is the amount of work and Worked the amount done, in any unit.
	Total, Worked float64
	// Done says the operation ended: the bar goes away and the notification can be closed.
	Done bool
}

// Active says whether the progress is running: it is not done, and it is infinite or has a total or
// some work done (VS Code's hasActiveProgress).
func (p Progress) Active() bool {
	return !p.Done && (p.Infinite || p.Total > 0 || p.Worked > 0)
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
	// Sticky keeps the toast on screen until the user closes it. Errors with actions, notifications with
	// a running progress and notifications that the user expanded are sticky too (see Item.IsSticky).
	Sticky bool
	// Silent sends the notification to the notification center without a toast. The service also
	// makes a notification silent while do not disturb is on, unless it is an error.
	Silent bool
	// Progress, when set, is the initial progress; UpdateProgress changes it.
	Progress *Progress
}

// Item is a notification the service holds.
type Item struct {
	Notification
	// ID identifies the item for Dismiss; IDs start at 1.
	ID uint64
	// Time is when it was sent.
	Time time.Time
	// Expanded says whether the item shows its details row (the source and the buttons) and the whole
	// message instead of one line. An item with actions starts expanded and cannot be collapsed.
	Expanded bool
}

// HasActions says whether the item has buttons.
func (i Item) HasActions() bool { return len(i.Actions) > 0 }

// CanCollapse says whether the user can expand and collapse the item: an item with buttons is
// always expanded.
func (i Item) CanCollapse() bool { return !i.HasActions() }

// HasActiveProgress says whether the item has a progress that is running.
func (i Item) HasActiveProgress() bool { return i.Progress != nil && i.Progress.Active() }

// IsSticky says whether its toast stays until the user closes it: the item says so, or it is an error
// with buttons, or it was expanded, or its progress is running.
//
// Ported from VS Code: src/vs/workbench/common/notifications.ts (NotificationViewItem.sticky)
func (i Item) IsSticky() bool {
	switch {
	case i.Sticky:
		return true
	case i.HasActions() && i.Severity == Error:
		return true
	case !i.HasActions() && i.Expanded:
		return true
	}
	return i.HasActiveProgress()
}

// clone copies the parts of the item that a caller could change.
func (i Item) clone() Item {
	i.Actions = slices.Clone(i.Actions)
	if i.Progress != nil {
		p := *i.Progress
		i.Progress = &p
	}
	return i
}

// equals says whether two notifications are the same message from the same source with the same
// buttons, which the service shows once. Notifications with a progress are never the same.
func (i Item) equals(n Notification) bool {
	if i.Progress != nil || n.Progress != nil {
		return false
	}
	if i.Severity != n.Severity || i.Message != n.Message || i.Source != n.Source || len(i.Actions) != len(n.Actions) {
		return false
	}
	for k := range n.Actions {
		if i.Actions[k].Label != n.Actions[k].Label {
			return false
		}
	}
	return true
}

// EventKind tells what happened to an item.
type EventKind int

// The kinds of event.
const (
	// Added means a new item is in the list.
	Added EventKind = iota
	// Dismissed means an item left the list.
	Dismissed
	// Updated means an item changed: its progress or its expansion.
	Updated
	// DoNotDisturbChanged means do not disturb was turned on or off.
	DoNotDisturbChanged
)

// Event is a change of the list of items or of the do not disturb mode.
type Event struct {
	Kind EventKind
	// Item is the item that was added, dismissed or updated; it is empty for DoNotDisturbChanged.
	Item Item
	// DoNotDisturb is the new mode for DoNotDisturbChanged.
	DoNotDisturb bool
}

// Service is the model of notifications (SPEC §10.7): it holds them, tells the listeners
// of changes, and writes each one to the log, so that none is lost while the UI that shows
// them is not there. It is safe for concurrent use.
//
// Ported from VS Code: src/vs/workbench/common/notifications.ts (NotificationsModel)
type Service struct {
	log *slog.Logger
	now func() time.Time

	mu        sync.Mutex
	next      uint64
	dnd       bool
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
// listeners are called before it returns, in the goroutine of the caller, with no lock held. A
// notification equal to one that is in the list (same severity, message, source and button labels,
// and no progress) replaces it: the old one is dismissed first. While do not disturb is on, a
// notification that is not an error is made silent.
func (s *Service) Notify(n Notification) uint64 {
	if n.Message == "" {
		return 0
	}
	n.Actions = slices.Clone(n.Actions)
	if n.Progress != nil {
		p := *n.Progress
		n.Progress = &p
	}
	s.mu.Lock()
	var replaced []Item
	for i := 0; i < len(s.items); i++ {
		if s.items[i].equals(n) {
			replaced = append(replaced, s.items[i])
			s.items = slices.Delete(s.items, i, i+1)
			i--
		}
	}
	s.next++
	if s.dnd && n.Severity != Error {
		n.Silent = true
	}
	item := Item{Notification: n, ID: s.next, Time: s.now(), Expanded: len(n.Actions) > 0}
	s.items = append(s.items, item)
	added := item.clone()
	s.mu.Unlock()

	attrs := []any{"id", item.ID}
	if n.Source != "" {
		attrs = append(attrs, "source", n.Source)
	}
	s.log.Log(context.Background(), n.Severity.level(), "notification: "+n.Message, attrs...)
	for _, old := range replaced {
		s.emit(Event{Kind: Dismissed, Item: old.clone()})
	}
	s.emit(Event{Kind: Added, Item: added})
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
	s.emit(Event{Kind: Dismissed, Item: item.clone()})
	return true
}

// ClearAll dismisses every item, the newest first, and returns how many there were.
func (s *Service) ClearAll() int {
	s.mu.Lock()
	items := s.items
	s.items = nil
	s.mu.Unlock()
	for _, item := range slices.Backward(items) {
		s.emit(Event{Kind: Dismissed, Item: item.clone()})
	}
	return len(items)
}

// Get returns an item that was not dismissed.
func (s *Service) Get(id uint64) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.IndexFunc(s.items, func(it Item) bool { return it.ID == id })
	if i < 0 {
		return Item{}, false
	}
	return s.items[i].clone(), true
}

// UpdateProgress sets the progress of an item (replacing the one it had) and reports whether the item
// was there. Progress{Done: true} ends it.
func (s *Service) UpdateProgress(id uint64, p Progress) bool {
	return s.update(id, func(it *Item) bool {
		if it.Progress != nil && *it.Progress == p {
			return false
		}
		it.Progress = &p
		return true
	})
}

// SetExpanded expands or collapses an item, and reports whether it changed. An item with buttons
// cannot be collapsed (see Item.CanCollapse), so it stays as it is.
func (s *Service) SetExpanded(id uint64, expanded bool) bool {
	changed := false
	s.update(id, func(it *Item) bool {
		if !it.CanCollapse() || it.Expanded == expanded {
			return false
		}
		it.Expanded = expanded
		changed = true
		return true
	})
	return changed
}

// update changes an item with f, which says whether it did, and tells the listeners. It reports
// whether the item was there.
func (s *Service) update(id uint64, f func(*Item) bool) bool {
	s.mu.Lock()
	i := slices.IndexFunc(s.items, func(it Item) bool { return it.ID == id })
	if i < 0 {
		s.mu.Unlock()
		return false
	}
	changed := f(&s.items[i])
	item := s.items[i].clone()
	s.mu.Unlock()
	if changed {
		s.emit(Event{Kind: Updated, Item: item})
	}
	return true
}

// SetDoNotDisturb turns do not disturb on or off. While it is on, notifications that are not errors
// are silent: they go to the notification center without a toast (VS Code's NotificationsFilter.ERROR).
// Items that are in the list are not changed.
func (s *Service) SetDoNotDisturb(on bool) {
	s.mu.Lock()
	changed := s.dnd != on
	s.dnd = on
	s.mu.Unlock()
	if changed {
		s.emit(Event{Kind: DoNotDisturbChanged, DoNotDisturb: on})
	}
}

// DoNotDisturb says whether do not disturb is on.
func (s *Service) DoNotDisturb() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dnd
}

// Items lists the notifications that were not dismissed, oldest first.
func (s *Service) Items() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Item, len(s.items))
	for i, it := range s.items {
		out[i] = it.clone()
	}
	return out
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
