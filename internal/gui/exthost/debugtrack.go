package exthost

import (
	"context"
	"sort"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// The debug adapter trackers (SPEC §10.31 "The extension contract"; after extHostDebugService.ts, DebugAdapterTracker): the extensions
// that registered a tracker factory for the debug type of a session, or for "*", see every message between Emetos and the adapter, in
// the order of the wire.

// trackerSet is the trackers of one session. Its calls are made one at a time, so that an extension sees the messages in order though
// the client reads and writes on different goroutines.
type trackerSet struct {
	h  *DebugHost
	id string

	mu   sync.Mutex
	list []liveTracker
}

type liveTracker struct {
	inst *instance
	t    *extension.DebugAdapterTracker
}

// startTrackers makes the trackers of a session about to reach its adapter and tells them it will start. A factory that fails is logged
// and leaves its tracker out.
func (h *DebugHost) startTrackers(ctx context.Context, es extension.DebugSession, typ string) *trackerSet {
	set := &trackerSet{h: h, id: es.ID()}
	h.mu.Lock()
	var factories []*trackerFactoryEntry
	for _, f := range h.trackers {
		if f.typ == typ || f.typ == "*" {
			factories = append(factories, f)
		}
	}
	h.mu.Unlock()
	sort.Slice(factories, func(i, j int) bool { return factories[i].seq < factories[j].seq })
	for _, f := range factories {
		var t *extension.DebugAdapterTracker
		err := h.guard(f.inst, "the adapter tracker factory of "+typ, func() error {
			var e error
			t, e = f.f(ctx, es)
			return e
		})
		if err != nil {
			h.log.Warn("a debug adapter tracker factory failed", "type", typ, "extension", f.inst.id, "err", err)
			continue
		}
		if t != nil {
			set.list = append(set.list, liveTracker{f.inst, t})
		}
	}
	if len(set.list) == 0 {
		return set
	}
	h.mu.Lock()
	h.live[set.id] = set
	h.mu.Unlock()
	set.each("OnWillStartSession", func(t *extension.DebugAdapterTracker) {
		if t.OnWillStartSession != nil {
			t.OnWillStartSession()
		}
	})
	return set
}

// each calls fn for every tracker, one set of calls at a time.
func (ts *trackerSet) each(where string, fn func(t *extension.DebugAdapterTracker)) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, lt := range ts.list {
		ts.h.safe(lt.inst, "a debug adapter tracker ("+where+")", func() { fn(lt.t) })
	}
}

// failed tells the trackers that the adapter could not be reached, and ends them.
func (ts *trackerSet) failed(err error) {
	ts.h.mu.Lock()
	if ts.h.live[ts.id] == ts {
		delete(ts.h.live, ts.id)
	}
	ts.h.mu.Unlock()
	ts.each("OnError", func(t *extension.DebugAdapterTracker) {
		if t.OnError != nil {
			t.OnError(err)
		}
	})
	ts.each("OnWillStopSession", func(t *extension.DebugAdapterTracker) {
		if t.OnWillStopSession != nil {
			t.OnWillStopSession()
		}
	})
}

// trace is debug.Options.Trace: a message of a session goes to its trackers.
func (h *DebugHost) trace(s *debug.Session, m dap.TraceMessage) {
	h.mu.Lock()
	set := h.live[s.ID()]
	h.mu.Unlock()
	if set == nil {
		return
	}
	if m.Direction == dap.ToAdapter {
		set.each("OnWillReceiveMessage", func(t *extension.DebugAdapterTracker) {
			if t.OnWillReceiveMessage != nil {
				t.OnWillReceiveMessage(m.Body)
			}
		})
		return
	}
	set.each("OnDidSendMessage", func(t *extension.DebugAdapterTracker) {
		if t.OnDidSendMessage != nil {
			t.OnDidSendMessage(m.Body)
		}
	})
}

// endTrackers tells the trackers of a session that it ends. cause is why an adapter died, or nil.
func (h *DebugHost) endTrackers(s *debug.Session, cause error) {
	h.mu.Lock()
	set := h.live[s.ID()]
	delete(h.live, s.ID())
	h.mu.Unlock()
	if set == nil {
		return
	}
	if cause != nil {
		var code *int
		if appErr, ok := errors.FromError(cause); ok {
			if c, isInt := appErr.Metadata["code"].(int); isInt {
				code = &c
			}
		}
		set.each("OnError", func(t *extension.DebugAdapterTracker) {
			if t.OnError != nil {
				t.OnError(cause)
			}
		})
		if code != nil {
			set.each("OnExit", func(t *extension.DebugAdapterTracker) {
				if t.OnExit != nil {
					t.OnExit(code)
				}
			})
		}
	}
	set.each("OnWillStopSession", func(t *extension.DebugAdapterTracker) {
		if t.OnWillStopSession != nil {
			t.OnWillStopSession()
		}
	})
}
