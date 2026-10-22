package logs

import "sync"

// MainChannel is the channel that receives Emetos's own log (SPEC §13).
const MainChannel = "Emetos"

// DefaultMaxLines is how many lines a channel keeps when NewStore is given none.
const DefaultMaxLines = 10000

// EventKind says what happened in a Store.
type EventKind int

// The kinds of Event.
const (
	// EventChannelAdded: a channel was created by its first line.
	EventChannelAdded EventKind = iota + 1
	// EventLine: a line was appended to a channel.
	EventLine
)

// Event is what subscribers of a Store are told.
type Event struct {
	Kind    EventKind
	Channel string
	// Line is the appended line, for EventLine.
	Line string
}

// Store keeps the lines of the log channels that the Output panel shows. A
// channel exists from its first line, keeps the last maxLines lines, and tells
// subscribers about every change. It is safe for use from any goroutine;
// subscribers are called on the goroutine that appended, so a UI subscriber hands
// the event over to the UI goroutine itself.
type Store struct {
	mu       sync.Mutex
	maxLines int
	order    []string
	lines    map[string][]string
	subs     map[int]func(Event)
	nextSub  int
}

// NewStore makes an empty Store whose channels keep the last maxLines lines
// (DefaultMaxLines when maxLines is not positive).
func NewStore(maxLines int) *Store {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	return &Store{maxLines: maxLines, lines: map[string][]string{}, subs: map[int]func(Event){}}
}

// Append adds a line to a channel, creating the channel when it is new.
func (s *Store) Append(channel, line string) {
	s.mu.Lock()
	var events []Event
	ls, exists := s.lines[channel]
	if !exists {
		s.order = append(s.order, channel)
		events = append(events, Event{Kind: EventChannelAdded, Channel: channel})
	}
	ls = append(ls, line)
	// Trim in bulk, so appending stays cheap: a channel holds up to twice
	// maxLines before the oldest half is dropped, and Lines returns the last
	// maxLines.
	if len(ls) >= 2*s.maxLines {
		ls = append([]string(nil), ls[len(ls)-s.maxLines:]...)
	}
	s.lines[channel] = ls
	events = append(events, Event{Kind: EventLine, Channel: channel, Line: line})
	subs := s.subscribers()
	s.mu.Unlock()

	for _, ev := range events {
		for _, fn := range subs {
			fn(ev)
		}
	}
}

// Lines returns a copy of the last lines of a channel, oldest first. An unknown
// channel has none.
func (s *Store) Lines(channel string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ls := s.lines[channel]
	if len(ls) > s.maxLines {
		ls = ls[len(ls)-s.maxLines:]
	}
	return append([]string(nil), ls...)
}

// Names returns the channel names in the order the channels were created.
func (s *Store) Names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.order...)
}

// Subscribe calls fn for every later change, and returns the function that
// stops it. fn must not call Subscribe or the returned function from inside the
// call, and must not log through the Logs whose MainChannel it watches (that
// would append a line, and call fn again).
func (s *Store) Subscribe(fn func(Event)) (unsubscribe func()) {
	s.mu.Lock()
	id := s.nextSub
	s.nextSub++
	s.subs[id] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}
}

// subscribers returns the current subscribers in subscription order; s.mu is held.
func (s *Store) subscribers() []func(Event) {
	fns := make([]func(Event), 0, len(s.subs))
	for id := 0; id < s.nextSub; id++ {
		if fn, ok := s.subs[id]; ok {
			fns = append(fns, fn)
		}
	}
	return fns
}
