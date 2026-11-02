package testing

import (
	"log/slog"
	"sync"
	"time"
)

// Pos is a place in a file, zero-based, with the column in runes as the buffer counts.
type Pos struct{ Line, Col int }

// Range is the text from Start up to End.
type Range struct{ Start, End Pos }

// Location is a place in a file.
type Location struct {
	// URI is file:///path/to/file.
	URI   string
	Range Range
}

// Item is a test, or a group of tests, in the tree of a controller: a copy of what the controller said.
type Item struct {
	// ID is the full id (JoinID) and ControllerID the id of the controller; ParentID is the id of the parent, which for a top item is the
	// controller's id.
	ID           string
	ControllerID string
	ParentID     string
	// URI is the file of the item, empty for none, and Range the place in it.
	URI   string
	Range *Range
	// Label, Description and SortText are the texts of the item.
	Label, Description, SortText string
	// Tags are the tags of the item.
	Tags []string
	// Error is the item's error row, Busy says that the controller is working on the item and CanResolveChildren that its children are
	// loaded when it is expanded.
	Error              string
	Busy               bool
	CanResolveChildren bool
}

// HasTag reports whether the item carries the tag.
func (it Item) HasTag(tag string) bool {
	for _, t := range it.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// EventKind says what an Event is about.
type EventKind int

const (
	// EventControllers is a controller added or removed (Event.ControllerID).
	EventControllers EventKind = iota + 1
	// EventTree is a change of the tree of a controller: Event.IDs are the items added, changed or removed (Event.Removed).
	EventTree
	// EventProfiles is a change of the run profiles or of their defaults.
	EventProfiles
	// EventResult is a change of the list of results (Event.Change says what, Event.Result which).
	EventResult
	// EventTest is a change of a test in a result (Event.Test).
	EventTest
	// EventRunning is a change of whether a run is going on (Service.IsRunning).
	EventRunning
	// EventContinuous is a continuous run started or stopped, or a change of the profile of one (Event.IDs has its test, empty for all).
	EventContinuous
	// EventCoverage is coverage added to the result's task (Event.Result).
	EventCoverage
)

// ResultChange says what happened to the list of results.
type ResultChange int

const (
	// ResultStarted is a live result added.
	ResultStarted ResultChange = iota + 1
	// ResultCompleted is a live result that ended.
	ResultCompleted
	// ResultInserted is a result that came from the saved ones.
	ResultInserted
	// ResultRemoved is the results taken away: cleared, or pushed out by newer ones.
	ResultRemoved
)

// Event is a change of the model.
type Event struct {
	Kind         EventKind
	ControllerID string
	IDs          []string
	Removed      bool
	Result       *Result
	Change       ResultChange
	Removals     []*Result
	Test         *ItemChange
}

// ChangeReason says why a test of a result changed.
type ChangeReason int

const (
	// ChangeComputedState is a change of the state or duration the test shows because of its children.
	ChangeComputedState ChangeReason = iota + 1
	// ChangeOwnState is a change of the test's own state or duration.
	ChangeOwnState
	// ChangeNewMessage is a message added to the test.
	ChangeNewMessage
)

// ItemChange is a change of a test in a result (VS Code's TestResultItemChange).
type ItemChange struct {
	ResultID string
	TestID   string
	Reason   ChangeReason
	// PreviousState and PreviousOwnDuration are those before an own change.
	PreviousState       State
	PreviousOwnDuration time.Duration
	// Message is the new message of ChangeNewMessage.
	Message *Message
}

// Options are the options of a Service.
type Options struct {
	// Logger receives the problems that do not stop a run; nil is slog.Default.
	Logger *slog.Logger
	// Now gives the time; nil is time.Now.
	Now func() time.Time
	// CancelDeadline is how long a cancelled run is waited for before it is ended by force (VS Code's RUN_CANCEL_DEADLINE); zero is
	// 10 seconds.
	CancelDeadline time.Duration
	// Marshal encodes the saved state; nil is json.Marshal. It is a seam for the tests of the failure.
	Marshal func(v any) ([]byte, error)
	// NewID makes the ids of results and tasks; nil makes random ones.
	NewID func() string
}

// Limits of the model.
const (
	// MaxResults is how many results are kept (VS Code's RETAIN_MAX_RESULTS); SPEC §10.32 fixes it.
	MaxResults = 16
	// MaxSavedOutput is how much of the output of a result is saved: the last bytes of it, in all its tasks.
	MaxSavedOutput = 1 << 20
	// MaxLiveOutput is how much output a task keeps while it runs: when more arrives the oldest is dropped.
	MaxLiveOutput = 16 << 20
	// MaxSavedTests is how many tests of a result a saved state may hold, so that a damaged file cannot make the model huge.
	MaxSavedTests = 100000
)

// Service is the test model of one window tab: the controllers, the results and the continuous runs.
type Service struct {
	log *slog.Logger
	now func() time.Time
	opt Options

	mu          sync.Mutex
	controllers map[string]*controller
	ctrlOrder   []string
	results     []*Result
	insertSeq   int
	userDefault map[profileKey]bool
	continuous  []*contRef
	idSeq       int

	subs       []*subscriber
	queue      []Event
	delivering bool
	running    bool
	subSeq     int
}

type subscriber struct {
	id int
	fn func(Event)
}

// NewService makes an empty model.
func NewService(opts Options) *Service {
	s := &Service{opt: opts, log: opts.Logger, now: opts.Now, controllers: map[string]*controller{}, userDefault: map[profileKey]bool{}}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.opt.CancelDeadline <= 0 {
		s.opt.CancelDeadline = 10 * time.Second
	}
	return s
}

// Subscribe registers a listener of the changes of the model; the returned function removes it. See the package comment for the thread
// the listener is called on.
func (s *Service) Subscribe(fn func(Event)) (cancel func()) {
	s.mu.Lock()
	s.subSeq++
	id := s.subSeq
	s.subs = append(s.subs, &subscriber{id: id, fn: fn})
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, sub := range s.subs {
			if sub.id == id {
				s.subs = append(s.subs[:i:i], s.subs[i+1:]...)
				return
			}
		}
	}
}

// emitLocked queues an event. The caller holds the lock and calls unlock afterwards.
func (s *Service) emitLocked(ev Event) { s.queue = append(s.queue, ev) }

// unlock releases the lock and delivers the queued events, one at a time and in order, unless another goroutine is delivering them.
func (s *Service) unlock() {
	if s.delivering || len(s.queue) == 0 {
		s.mu.Unlock()
		return
	}
	s.delivering = true
	for len(s.queue) > 0 {
		ev := s.queue[0]
		s.queue = s.queue[1:]
		subs := make([]*subscriber, len(s.subs))
		copy(subs, s.subs)
		s.mu.Unlock()
		for _, sub := range subs {
			sub.fn(ev)
		}
		s.mu.Lock()
	}
	s.delivering = false
	s.mu.Unlock()
}

// newIDLocked makes the id of a result or a task.
func (s *Service) newIDLocked() string {
	if s.opt.NewID != nil {
		return s.opt.NewID()
	}
	s.idSeq++
	return newID(s.idSeq)
}

// IsRunning reports whether a result is still being made (the context key testing.isRunning).
func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isRunningLocked()
}

func (s *Service) isRunningLocked() bool {
	return len(s.results) > 0 && s.results[0].completedAt.IsZero()
}

// refreshRunningLocked queues EventRunning when whether a run goes on changed.
func (s *Service) refreshRunningLocked() {
	if now := s.isRunningLocked(); now != s.running {
		s.running = now
		s.emitLocked(Event{Kind: EventRunning})
	}
}
