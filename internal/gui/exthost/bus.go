package exthost

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The numbers of the event bus (SPEC §10.15 "As built (step 11.2)").
const (
	// SlowHandler is how long a subscriber or a handler may take before the log says so.
	SlowHandler = 50 * time.Millisecond
	// DefaultRequestTimeout is how long a request waits for its reply when its context has no deadline.
	DefaultRequestTimeout = 10 * time.Second
)

// fastCoreTopics are the topics of Emetos that are delivered once per frame.
var fastCoreTopics = map[string]bool{
	extension.TopicDocumentChanged:        true,
	extension.TopicEditorSelectionChanged: true,
}

// envelope is a published event, as it waits in the queue: the JSON of the value is made at the Publish call, so the
// publisher keeps nothing shared with the receivers.
type envelope struct {
	topic, publisher string
	seq              uint64
	data             json.RawMessage
	fast             bool
}

// subscription is a Subscribe of an extension.
type subscription struct {
	pattern string
	fn      func(extension.Event)
	inst    *instance
}

// handlerEntry is the handler of a topic.
type handlerEntry struct {
	topic string
	fn    extension.AsyncEventHandler
	inst  *instance
}

// request is a Request that has not been answered.
type request struct {
	requester *instance
	handler   *instance // set once the request reached its handler
	reply     func(extension.Event, error)
	ev        extension.Event
	cancel    context.CancelFunc // ends the context of the handler
	stops     []func()           // the timer and the watch on the context of the requester
	settled   bool               // UI thread only
}

// bus is the event bus of one scope (SPEC §10.15 "Events"). Publish and Request may be called from any goroutine; the
// delivery to subscribers and handlers happens on the UI thread, through the Post and Frame functions of the scope.
type bus struct {
	s *Scope

	mu       sync.Mutex
	seq      uint64
	queue    []*envelope
	subs     []*subscription
	handlers map[string]*handlerEntry
	pending  map[*request]bool
	closed   bool

	// postScheduled and frameScheduled say that a drain is waiting for the UI thread or the end of the frame.
	postScheduled, frameScheduled bool
	// draining is true while the UI thread delivers; a drain that is asked for meanwhile is noted (wantPost,
	// wantFrame) and done by the loop that is running, so that the subscribers are never called inside each other.
	draining, wantPost, wantFrame bool
}

func newBus(s *Scope) *bus {
	return &bus{s: s, handlers: map[string]*handlerEntry{}, pending: map[*request]bool{}}
}

// publish queues an event for delivery. fast events wait for the end of the frame, the others for the UI thread's next
// turn. It does nothing once the scope is closed.
func (b *bus) publish(publisher, topic string, data json.RawMessage, fast bool) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.seq++
	b.queue = append(b.queue, &envelope{topic: topic, publisher: publisher, seq: b.seq, data: data, fast: fast})
	schedulePost := !fast && !b.postScheduled
	scheduleFrame := fast && !b.frameScheduled
	if schedulePost {
		b.postScheduled = true
	}
	if scheduleFrame {
		b.frameScheduled = true
	}
	b.mu.Unlock()
	switch {
	case schedulePost:
		b.s.post(func() { b.drain(false) })
	case scheduleFrame:
		b.s.afterFrame(func() { b.drain(true) })
	}
}

// drain delivers what is queued, on the UI thread. After a frame it delivers everything, the fast events merged into
// one batch per publisher and topic; otherwise it delivers up to the last event that is not fast, with the fast events
// before it, so that an event never overtakes an earlier one.
func (b *bus) drain(frame bool) {
	b.mu.Lock()
	if frame {
		b.frameScheduled = false
	} else {
		b.postScheduled = false
	}
	if b.draining {
		if frame {
			b.wantFrame = true
		} else {
			b.wantPost = true
		}
		b.mu.Unlock()
		return
	}
	b.draining = true
	b.mu.Unlock()
	for {
		b.pass(frame)
		b.mu.Lock()
		switch {
		case b.wantPost:
			b.wantPost, frame = false, false
		case b.wantFrame:
			b.wantFrame, frame = false, true
		default:
			b.draining = false
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()
	}
}

// flush delivers everything that is queued at once, the fast events too: the scope is closing and there will be no next
// frame.
func (b *bus) flush() { b.drain(true) }

// batch is the fast events of a publisher and topic that one pass merges.
type batch struct {
	topic, publisher string
	last             uint64
	items            []json.RawMessage
}

// pass takes the events that are due out of the queue and delivers them.
func (b *bus) pass(frame bool) {
	b.mu.Lock()
	upto := len(b.queue)
	if !frame {
		upto = 0
		for i, e := range b.queue {
			if !e.fast {
				upto = i + 1
			}
		}
	}
	take := slices.Clone(b.queue[:upto])
	b.queue = slices.Clone(b.queue[upto:])
	b.mu.Unlock()

	var batches []*batch
	flush := func() {
		for _, bt := range batches {
			b.deliver(bt.publisher, bt.topic, bt.last, bt.items[len(bt.items)-1], bt.items)
		}
		batches = batches[:0]
	}
	for _, e := range take {
		if !e.fast {
			flush()
			b.deliver(e.publisher, e.topic, e.seq, e.data, nil)
			continue
		}
		i := slices.IndexFunc(batches, func(bt *batch) bool { return bt.topic == e.topic && bt.publisher == e.publisher })
		if i < 0 {
			batches = append(batches, &batch{topic: e.topic, publisher: e.publisher})
			i = len(batches) - 1
		}
		batches[i].items = append(batches[i].items, e.data)
		batches[i].last = e.seq
	}
	flush()
}

// deliver gives an event to the subscribers that are to get it. Inside one scope that is every subscriber of the scope;
// a later step adds the links to other scopes here, and checks them at this moment (SPEC §10.15 "Event sharing").
func (b *bus) deliver(publisher, topic string, seq uint64, data json.RawMessage, items []json.RawMessage) {
	for _, target := range b.s.h.targets(b.s, publisher, topic) {
		target.bus.dispatch(b.s.opts.Info, publisher, topic, seq, data, items)
	}
}

// dispatch calls the subscribers of this scope whose pattern matches. Each gets a copy of the bytes.
func (b *bus) dispatch(src extension.ScopeInfo, publisher, topic string, seq uint64, data json.RawMessage, items []json.RawMessage) {
	b.mu.Lock()
	var subs []*subscription
	for _, sub := range b.subs {
		if extension.MatchTopic(sub.pattern, topic) {
			subs = append(subs, sub)
		}
	}
	b.mu.Unlock()
	for _, sub := range subs {
		if sub.inst.isDead() || !b.subscribed(sub) {
			continue
		}
		ev := extension.Event{Topic: topic, Publisher: publisher, Scope: src, Seq: seq, Data: slices.Clone(data)}
		if items != nil {
			ev.Batch = make([]json.RawMessage, len(items))
			for i, it := range items {
				ev.Batch[i] = slices.Clone(it)
			}
		}
		b.s.runSubscriber(sub.inst, sub.pattern, sub.fn, ev)
	}
}

// subscribed tells whether a subscription is still in the bus.
func (b *bus) subscribed(sub *subscription) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Contains(b.subs, sub)
}

// subscribe adds a subscription and returns the function that removes it.
func (b *bus) subscribe(sub *subscription) (cancel func()) {
	b.mu.Lock()
	b.subs = append(b.subs, sub)
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.subs = slices.DeleteFunc(b.subs, func(x *subscription) bool { return x == sub })
	}
}

// addHandler makes an extension the handler of a topic; it reports false when the scope has a handler for it already.
func (b *bus) addHandler(h *handlerEntry) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, taken := b.handlers[h.topic]; taken {
		return false
	}
	b.handlers[h.topic] = h
	return true
}

// handlerOf is the handler of a topic in this scope, or nil.
func (b *bus) handlerOf(topic string) *handlerEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.handlers[topic]
}

// nextSeq gives the sequence number of a request.
func (b *bus) nextSeq() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	return b.seq
}

// instanceGone takes the subscriptions and the handlers of an extension that was deactivated away, ends the requests
// it made without calling it back, and answers the requests it was handling with EventHandlerGone.
func (b *bus) instanceGone(inst *instance) {
	b.mu.Lock()
	b.subs = slices.DeleteFunc(b.subs, func(x *subscription) bool { return x.inst == inst })
	for topic, h := range b.handlers {
		if h.inst == inst {
			delete(b.handlers, topic)
		}
	}
	var mine, handled []*request
	for r := range b.pending {
		switch {
		case r.requester == inst:
			mine = append(mine, r)
		case r.handler == inst:
			handled = append(handled, r)
		}
	}
	b.mu.Unlock()
	for _, r := range mine {
		b.s.post(func() { b.settle(r) })
	}
	for _, r := range handled {
		b.s.post(func() {
			if b.settle(r) {
				b.s.callReply(r, extension.Event{}, errors.New("EventHandlerGone", "the extension that handled the request was deactivated before it answered", "topic", r.ev.Topic, "handler", inst.id))
			}
		})
	}
}

// close ends the bus with its scope: nothing more is queued.
func (b *bus) close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
}

// settle ends a request on the UI thread: its timer and its watch of the context stop, and the context of the handler
// is cancelled. It reports false when the request was settled already, so that only the first outcome counts.
func (b *bus) settle(r *request) bool {
	if r.settled {
		return false
	}
	r.settled = true
	for _, stop := range r.stops {
		stop()
	}
	if r.cancel != nil {
		r.cancel()
	}
	b.mu.Lock()
	delete(b.pending, r)
	b.mu.Unlock()
	return true
}

// ---- the requests ----

// request sends a request from an extension; the delivery to the handler happens on the UI thread, soon.
func (b *bus) request(inst *instance, ctx context.Context, topic string, data json.RawMessage, reply func(extension.Event, error)) {
	r := &request{requester: inst, reply: reply}
	b.mu.Lock()
	b.pending[r] = true
	b.mu.Unlock()
	b.s.post(func() { b.start(r, ctx, topic, data) })
}

// start finds the handler of a request and calls it (UI thread).
func (b *bus) start(r *request, ctx context.Context, topic string, data json.RawMessage) {
	if r.settled || r.requester.isDead() {
		b.settle(r)
		return
	}
	r.ev = extension.Event{Topic: topic, Publisher: r.requester.id, Scope: b.s.opts.Info, Seq: b.nextSeq(), Data: data}
	h := b.s.h.handlerFor(b.s, topic)
	if h == nil || h.inst.isDead() {
		if b.settle(r) {
			b.s.callReply(r, extension.Event{}, errors.New("EventNoHandler", "no extension handles this topic", "topic", topic))
		}
		return
	}
	r.handler = h.inst
	hctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.stops = append(r.stops, b.watch(r, ctx))
	h.inst.scope.runHandler(h, r, hctx)
}

// watch ends the request when its context ends (EventTimeout at a deadline, EventCanceled otherwise), or after the
// default timeout when the context has no deadline. It returns the function that stops the watch.
func (b *bus) watch(r *request, ctx context.Context) (stop func()) {
	expire := func(timedOut bool) {
		b.s.post(func() {
			if !b.settle(r) {
				return
			}
			if timedOut {
				b.s.callReply(r, extension.Event{}, errors.New("EventTimeout", "the handler did not answer in time", "topic", r.ev.Topic))
				return
			}
			b.s.callReply(r, extension.Event{}, errors.New("EventCanceled", "the request was cancelled", "topic", r.ev.Topic))
		})
	}
	stopTimer := func() {}
	if _, ok := ctx.Deadline(); !ok {
		stopTimer = b.s.afterTimer(DefaultRequestTimeout, func() { expire(true) })
	}
	stopWatch := context.AfterFunc(ctx, func() { expire(ctx.Err() == context.DeadlineExceeded) })
	return func() { stopTimer(); stopWatch() }
}
