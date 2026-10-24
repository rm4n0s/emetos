package workspace

import (
	"sort"
	"strings"
)

// EventKind says what happened to a path.
type EventKind int

const (
	// EventCreated is a new file or folder.
	EventCreated EventKind = iota + 1
	// EventChanged is a file whose content or attributes changed. A file replaced by renaming
	// another one over it (the atomic save of editors) is reported as changed.
	EventChanged
	// EventDeleted is a file or folder that is gone, or was moved out of the watched folders.
	EventDeleted
	// EventRenamed is an item moved from OldPath to Path.
	EventRenamed
	// EventRescan says that events were lost below Path (the kernel's queue overflowed): read
	// everything below it again.
	EventRescan
)

func (k EventKind) String() string {
	switch k {
	case EventCreated:
		return "created"
	case EventChanged:
		return "changed"
	case EventDeleted:
		return "deleted"
	case EventRenamed:
		return "renamed"
	case EventRescan:
		return "rescan"
	}
	return "unknown"
}

// Event is a change of a file or folder, as the watcher reports it.
type Event struct {
	Kind EventKind
	// Path is the absolute path.
	Path string
	// OldPath is where a renamed item was.
	OldPath string
	// IsDir is true when the path is known to be a folder (not known for EventChanged).
	IsDir bool
}

// rawKind is what the kernel (or the poller) saw, before coalescing.
type rawKind int

const (
	rawCreated rawKind = iota + 1
	rawChanged
	rawDeleted
	rawMovedFrom
	rawMovedTo
	rawRescan
)

// rawEvent is a single observation. A move is two of them with the same cookie.
type rawEvent struct {
	kind   rawKind
	path   string
	isDir  bool
	cookie uint32
	// skip marks a move of a path that no root reports. It is kept to pair the move with
	// the other half, which may be reported (a temporary file renamed over a watched file).
	skip bool
}

// pendingEvent is an event of a batch that can still be cancelled.
type pendingEvent struct {
	Event
	dead bool
}

// coalescer joins the raw events of a batch (about 50 ms) into the events that are reported.
// It is not safe for concurrent use.
//
// Ported from VS Code: src/vs/platform/files/common/watcher.ts (EventCoalescer), with the
// pairing of moves of inotify added:
//
//   - created then deleted in one batch cancels both;
//   - deleted then created becomes changed;
//   - created then changed stays created; any other repeat keeps the last kind;
//   - a delete of something below a deleted folder is dropped;
//   - a move from and a move to with the same cookie are one rename; a move that is not paired
//     is a delete (from) or a create (to);
//   - a rename of a path that was created in the same batch (a temporary file renamed over its
//     target by an atomic save) is a change of the new path; so is a rename onto a watched path
//     of a path that is not watched (the temporary file next to a file that is followed alone).
type coalescer struct {
	order   []*pendingEvent
	byPath  map[string]*pendingEvent
	renames []Event
	// pending are the moves from that wait for their move to, by cookie, in arrival order.
	pending    map[uint32]rawEvent
	pendingSeq []uint32
	rescans    []Event
}

func newCoalescer() *coalescer {
	return &coalescer{byPath: map[string]*pendingEvent{}, pending: map[uint32]rawEvent{}}
}

// empty tells whether nothing is waiting to be flushed.
func (c *coalescer) empty() bool {
	return len(c.order) == 0 && len(c.renames) == 0 && len(c.pending) == 0 && len(c.rescans) == 0
}

// add takes one raw event.
func (c *coalescer) add(r rawEvent) {
	switch r.kind {
	case rawCreated:
		c.process(Event{Kind: EventCreated, Path: r.path, IsDir: r.isDir})
	case rawChanged:
		c.process(Event{Kind: EventChanged, Path: r.path, IsDir: r.isDir})
	case rawDeleted:
		c.process(Event{Kind: EventDeleted, Path: r.path, IsDir: r.isDir})
	case rawMovedFrom:
		c.pending[r.cookie] = r
		c.pendingSeq = append(c.pendingSeq, r.cookie)
	case rawMovedTo:
		from, ok := c.pending[r.cookie]
		if !ok {
			if !r.skip {
				c.process(Event{Kind: EventCreated, Path: r.path, IsDir: r.isDir})
			}
			return
		}
		delete(c.pending, r.cookie)
		switch {
		case from.skip && r.skip:
		case from.skip:
			// something that is not watched was renamed onto a watched path
			c.process(Event{Kind: EventChanged, Path: r.path, IsDir: r.isDir})
		case r.skip:
			c.process(Event{Kind: EventDeleted, Path: from.path, IsDir: from.isDir})
		default:
			c.rename(from.path, r.path, r.isDir)
		}
	case rawRescan:
		c.rescans = append(c.rescans, Event{Kind: EventRescan, Path: r.path, IsDir: true})
	}
}

// process is EventCoalescer.processEvent.
func (c *coalescer) process(e Event) {
	existing := c.byPath[e.Path]
	if existing == nil {
		p := &pendingEvent{Event: e}
		c.order = append(c.order, p)
		c.byPath[e.Path] = p
		return
	}
	switch {
	case existing.Kind == EventCreated && e.Kind == EventDeleted:
		// created and deleted in one go
		existing.dead = true
		delete(c.byPath, e.Path)
	case existing.Kind == EventDeleted && e.Kind == EventCreated:
		existing.Kind = EventChanged
	case existing.Kind == EventCreated && e.Kind == EventChanged:
		// keep the created event
	default:
		existing.Kind = e.Kind
	}
}

// rename takes a paired move.
func (c *coalescer) rename(from, to string, isDir bool) {
	if p := c.byPath[from]; p != nil {
		switch p.Kind {
		case EventCreated:
			// A path born and moved within one batch: the new path has this content now, and
			// may have replaced a file of that name.
			p.dead = true
			delete(c.byPath, from)
			c.process(Event{Kind: EventChanged, Path: to, IsDir: isDir})
			return
		case EventChanged:
			p.dead = true // the old path is gone
			delete(c.byPath, from)
		}
	}
	c.renames = append(c.renames, Event{Kind: EventRenamed, Path: to, OldPath: from, IsDir: isDir})
}

// flush returns the events of the batch and empties the coalescer: the deletes (a delete below
// a deleted folder dropped, shortest paths first), the renames in the order they happened, the
// creates and changes in the order they came, and last the rescans.
func (c *coalescer) flush() []Event {
	for _, cookie := range c.pendingSeq {
		if from, ok := c.pending[cookie]; ok && !from.skip {
			c.process(Event{Kind: EventDeleted, Path: from.path, IsDir: from.isDir})
		}
	}
	var deletes, others []Event
	for _, p := range c.order {
		if p.dead {
			continue
		}
		if p.Kind == EventDeleted {
			deletes = append(deletes, p.Event)
		} else {
			others = append(others, p.Event)
		}
	}
	sort.SliceStable(deletes, func(i, j int) bool { return len(deletes[i].Path) < len(deletes[j].Path) })
	var kept []Event
	for _, d := range deletes {
		covered := false
		for _, k := range kept {
			if isParentPath(d.Path, k.Path) {
				covered = true
				break
			}
		}
		if !covered {
			kept = append(kept, d)
		}
	}
	out := append(kept, c.renames...)
	out = append(out, others...)
	out = append(out, c.rescans...)
	*c = *newCoalescer()
	return out
}

// isParentPath tells whether path is below parent.
func isParentPath(path, parent string) bool {
	return len(path) > len(parent) && strings.HasPrefix(path, parent) && path[len(parent)] == '/'
}
