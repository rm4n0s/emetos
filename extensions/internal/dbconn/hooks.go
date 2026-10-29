package dbconn

import "sync"

// The optional parts of a Driver (step 21.1). A database extension whose nodes do something when they are opened, or that keeps what
// it listed, implements them; the view finds them with a type assertion on the driver of the engine, so a driver without them is
// unchanged.

// Opener is implemented by a Driver that acts when a node is opened with Enter or a double click in the view: the Redis extension
// opens a key in an editor tab. It runs on the UI thread. A connection is opened by the view itself, not by the driver.
type Opener interface {
	Open(c Connection, n Node)
}

// Refresher is implemented by a Driver that keeps what it listed (the keys it scanned): Refresh drops it, so that the next Children
// asks the database again. The Refresh command of the view calls it for each open connection of the driver, then lists the open
// rows again. It runs on the UI thread.
type Refresher interface {
	Refresh(c Connection)
}

// reloads are the subscribers of Hub.Reload.
type reloads struct {
	mu   sync.Mutex
	next int
	subs map[int]func(connID string)
}

// SubscribeReload calls fn when a driver asks for the tree of a connection to be listed again (Reload). fn runs on the goroutine
// that asked and must not block; a view hands the news to its UI thread with Post. It returns the function that stops the calls.
func (h *Hub) SubscribeReload(fn func(connID string)) (cancel func()) {
	h.reloadSubs.mu.Lock()
	defer h.reloadSubs.mu.Unlock()
	if h.reloadSubs.subs == nil {
		h.reloadSubs.subs = map[int]func(string){}
	}
	id := h.reloadSubs.next
	h.reloadSubs.next++
	h.reloadSubs.subs[id] = fn
	return func() {
		h.reloadSubs.mu.Lock()
		delete(h.reloadSubs.subs, id)
		h.reloadSubs.mu.Unlock()
	}
}

// Reload asks every view to list the open rows of a connection again, without calling Refresher.Refresh: the driver changed what it
// lists (a page of keys was loaded, a key was deleted) and answers the next Children from what it holds.
func (h *Hub) Reload(connID string) {
	h.reloadSubs.mu.Lock()
	fns := make([]func(string), 0, len(h.reloadSubs.subs))
	for _, fn := range h.reloadSubs.subs {
		fns = append(fns, fn)
	}
	h.reloadSubs.mu.Unlock()
	for _, fn := range fns {
		fn(connID)
	}
}

// Open lets the driver of the connection act on a node that was opened. It does nothing for a driver without Opener.
func (h *Hub) Open(id string, n Node) {
	c, ok := h.Store().Get(id)
	if !ok {
		return
	}
	_, drv, ok := h.Engine(c.Engine)
	if !ok {
		return
	}
	if o, ok := drv.(Opener); ok {
		expanded, err := c.Expanded(h.env)
		if err != nil {
			return
		}
		o.Open(expanded, n)
	}
}

// RefreshDrivers calls Refresher.Refresh for each connection that is open, through the driver of its engine.
func (h *Hub) RefreshDrivers() {
	h.mu.Lock()
	var open []string
	for id, st := range h.states {
		if st.Status == Connected {
			open = append(open, id)
		}
	}
	h.mu.Unlock()
	for _, id := range open {
		c, ok := h.Store().Get(id)
		if !ok {
			continue
		}
		_, drv, ok := h.Engine(c.Engine)
		if !ok {
			continue
		}
		if r, ok := drv.(Refresher); ok {
			if expanded, err := c.Expanded(h.env); err == nil {
				r.Refresh(expanded)
			}
		}
	}
}
