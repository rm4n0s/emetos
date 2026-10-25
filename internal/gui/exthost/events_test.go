package exthost

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The tests of the event bus (step 11.2): delivery, patterns, request and reply, ownership, copies, batching, the core
// events and the scopes.

// evExt describes an extension of a test.
type evExt struct {
	id   string
	decl extension.EventDecl
}

// harness is a host with the extensions of a test activated in one tab; the tab queues what it posts, as the UI thread
// does, so that a test sees what is delivered later.
type harness struct {
	t    *testing.T
	r    *rig
	tb   *tab
	apis map[string]extension.API
}

// newHarness activates the extensions in a tab named "one"; the tab is deferred once they are active.
func newHarness(t *testing.T, exts ...evExt) *harness {
	t.Helper()
	h := &harness{t: t, apis: map[string]extension.API{}}
	var factories []extension.Factory
	for _, e := range exts {
		m := manifest(e.id)
		m.Events = e.decl
		f, _ := factory(m, func(f *fake) {
			f.activate = func(_ context.Context, api extension.API) error { h.apis[e.id] = api; return nil }
		})
		factories = append(factories, f)
	}
	h.r = newRig(t, factories...)
	h.tb = h.r.newTab("one")
	h.tb.scope.Start()
	for _, e := range exts {
		if h.apis[e.id] == nil {
			t.Fatalf("%s was not activated: %v", e.id, h.tb.scope.Failure(e.id))
		}
	}
	h.tb.pump()
	h.tb.deferred = true
	return h
}

// events is the Events of an extension of the harness.
func (h *harness) events(id string) extension.Events { return h.apis[id].Events() }

// heard records the events a subscriber got.
type heard struct {
	mu     sync.Mutex
	events []extension.Event
}

func (g *heard) fn(ev extension.Event) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.events = append(g.events, ev)
}

func (g *heard) all() []extension.Event {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.events)
}

func (g *heard) topics() []string {
	var out []string
	for _, ev := range g.all() {
		out = append(out, ev.Topic)
	}
	return out
}

// subscribe subscribes the extension to the pattern and records what it hears.
func (h *harness) subscribe(id, pattern string) *heard {
	h.t.Helper()
	g := &heard{}
	if _, err := h.events(id).Subscribe(pattern, g.fn); err != nil {
		h.t.Fatal(err)
	}
	return g
}

// outcome is what a request ended with.
type outcome struct {
	mu    sync.Mutex
	calls int
	ev    extension.Event
	err   error
}

func newOutcome() *outcome { return &outcome{} }

func (o *outcome) fn(ev extension.Event, err error) {
	o.mu.Lock()
	o.calls++
	o.ev, o.err = ev, err
	o.mu.Unlock()
}

func (o *outcome) get() (int, extension.Event, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls, o.ev, o.err
}

// await pumps the tab, as the UI thread does, until the request is answered by a goroutine that posts its answer.
func (h *harness) await(o *outcome) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.tb.pump()
		if calls, _, _ := o.get(); calls > 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	h.t.Fatal("the request was not answered")
}

// wantRoute checks that an error is an *errors.Error whose route has the tail.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want ...%s)", appErr.Route(), route)
	}
}

func TestEventsAreDeliveredLaterInOrderToEverySubscriber(t *testing.T) {
	h := newHarness(t, evExt{id: "a"}, evExt{id: "b"})
	own := h.subscribe("a", "a.thing")
	other := h.subscribe("b", "a.thing")
	for i := range 3 {
		if err := h.events("a").Publish("a.thing", map[string]int{"n": i}); err != nil {
			t.Fatal(err)
		}
	}
	if len(own.all())+len(other.all()) != 0 {
		t.Fatal("an event was delivered inside Publish")
	}
	h.tb.pump()
	for name, g := range map[string]*heard{"the publisher": own, "another extension": other} {
		got := g.all()
		if len(got) != 3 {
			t.Fatalf("%s heard %d events, want 3", name, len(got))
		}
		for i, ev := range got {
			n, err := extension.EventValue[map[string]int](ev)
			if err != nil || n["n"] != i {
				t.Errorf("%s: event %d carries %v (%v)", name, i, n, err)
			}
			if ev.Topic != "a.thing" || ev.Publisher != "a" || ev.Scope.Name != "one" || ev.Batch != nil {
				t.Errorf("%s: event %d = %+v", name, i, ev)
			}
			if i > 0 && ev.Seq <= got[i-1].Seq {
				t.Errorf("%s: sequence numbers %d then %d", name, got[i-1].Seq, ev.Seq)
			}
		}
	}
}

func TestSubscribePatterns(t *testing.T) {
	topics := []string{"a.x", "a.x.y", "a.z", "b.x"}
	tests := []struct {
		pattern string
		want    []string
	}{
		{"a.x", []string{"a.x"}},
		{"a.*", []string{"a.x", "a.z"}},
		{"a.**", []string{"a.x", "a.x.y", "a.z"}},
		{"*.x", []string{"a.x", "b.x"}},
		{"*.x.*", []string{"a.x.y"}},
		{"**", []string{"a.x", "a.x.y", "a.z", "b.x"}},
		{"*", nil},
		{"a.x.y.z", nil},
	}
	h := newHarness(t, evExt{id: "a"}, evExt{id: "b"}, evExt{id: "c"})
	got := map[string]*heard{}
	for _, tt := range tests {
		got[tt.pattern] = h.subscribe("c", tt.pattern)
	}
	for _, topic := range topics {
		id := topic[:1]
		if err := h.events(id).Publish(topic, nil); err != nil {
			t.Fatal(err)
		}
	}
	h.tb.pump()
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			if g := got[tt.pattern].topics(); !slices.Equal(g, tt.want) {
				t.Errorf("heard %v, want %v", g, tt.want)
			}
		})
	}
}

func TestSubscribeStopsWithCancelAndWithTheExtension(t *testing.T) {
	h := newHarness(t, evExt{id: "a"}, evExt{id: "b"})
	g := &heard{}
	cancel, err := h.events("b").Subscribe("a.*", g.fn)
	if err != nil {
		t.Fatal(err)
	}
	gone := h.subscribe("a", "a.*")
	pub := func() {
		if err := h.events("a").Publish("a.t", nil); err != nil {
			t.Fatal(err)
		}
		h.tb.pump()
	}
	pub()
	cancel()
	pub()
	if len(g.all()) != 1 {
		t.Errorf("a cancelled subscription heard %d events, want 1", len(g.all()))
	}
	if len(gone.all()) != 2 {
		t.Fatalf("heard %d events, want 2", len(gone.all()))
	}
	h.tb.disabled = []string{"a"}
	h.tb.scope.Refresh()
	pub2 := h.events("a").Publish("a.t", nil)
	wantRoute(t, pub2, "Publish.ExtensionDeactivated")
	if _, err := h.events("a").Subscribe("a.*", gone.fn); err == nil {
		t.Error("a deactivated extension subscribed")
	}
	if len(gone.all()) != 2 {
		t.Errorf("a deactivated extension heard an event")
	}
}

func TestEventsAreRefusedForTopicsTheExtensionDoesNotOwn(t *testing.T) {
	a := evExt{id: "a", decl: extension.EventDecl{
		Publishes: []extension.EventSpec{{Topic: "shared.topic"}},
		Handles:   []string{"shared.req"},
	}}
	h := newHarness(t, a, evExt{id: "ab"}, evExt{id: "b"})
	ev := h.events("a")
	ok := func(context.Context, extension.Event) (any, error) { return nil, nil }
	tests := []struct {
		name  string
		call  func() error
		route string // empty: allowed
	}{
		{"publish under the own id", func() error { return ev.Publish("a.ok", nil) }, ""},
		{"publish a declared topic", func() error { return ev.Publish("shared.topic", nil) }, ""},
		{"publish under another id", func() error { return ev.Publish("b.other", nil) }, "Publish.EventTopicNotOwned"},
		{"publish under an id that starts with the own", func() error { return ev.Publish("ab.x", nil) }, "Publish.EventTopicNotOwned"},
		{"publish the id itself", func() error { return ev.Publish("a", nil) }, "Publish.EventTopicNotOwned"},
		{"publish a core event", func() error { return ev.Publish(extension.TopicDocumentOpened, nil) }, "Publish.EventTopicNotOwned"},
		{"handle under the own id", func() error { return ev.Handle("a.req", ok) }, ""},
		{"handle a declared topic", func() error { return ev.Handle("shared.req", ok) }, ""},
		{"handle a topic only declared for publishing", func() error { return ev.Handle("shared.topic", ok) }, "HandleAsync.EventTopicNotOwned"},
		{"handle under another id", func() error { return ev.Handle("b.req", ok) }, "HandleAsync.EventTopicNotOwned"},
		{"handle twice", func() error { return h.events("b").Handle("a.req", ok) }, "HandleAsync.EventTopicNotOwned"},
		{"a topic that is not well formed", func() error { return ev.Publish("a..x", nil) }, "Publish->CheckTopic.EventTopicInvalid"},
		{"a topic with a star", func() error { return ev.Publish("a.*", nil) }, "Publish->CheckTopic.EventTopicInvalid"},
		{"a pattern that is not well formed", func() error { _, err := ev.Subscribe("a.**.x", func(extension.Event) {}); return err }, "Subscribe->CheckPattern.EventPatternInvalid"},
		{"a value that is not JSON", func() error { return ev.Publish("a.bad", make(chan int)) }, "Publish.EventValueInvalid"},
		{"a request value that is not JSON", func() error {
			return ev.Request(context.Background(), "a.bad", func() {}, nil)
		}, "Request.EventValueInvalid"},
		{"a request topic that is not well formed", func() error { return ev.Request(context.Background(), "a b", nil, nil) }, "Request->CheckTopic.EventTopicInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if tt.route == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			wantRoute(t, err, tt.route)
		})
	}
	// a second handler for a topic of the scope is refused, whoever asks
	err := h.events("a").Handle("a.req", ok)
	wantRoute(t, err, "HandleAsync.EventHandlerTaken")
}

func TestValuesAreCopies(t *testing.T) {
	h := newHarness(t, evExt{id: "a"}, evExt{id: "b"}, evExt{id: "c"})
	first := &heard{}
	second := &heard{}
	if _, err := h.events("b").Subscribe("a.v", func(ev extension.Event) {
		first.fn(ev)
		// the receiver damages everything it was given
		for i := range ev.Data {
			ev.Data[i] = 'x'
		}
		for _, b := range ev.Batch {
			b[0] = 'x'
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.events("c").Subscribe("a.v", second.fn); err != nil {
		t.Fatal(err)
	}
	sent := map[string]any{"list": []any{"one", "two"}, "n": 1}
	if err := h.events("a").Publish("a.v", sent); err != nil {
		t.Fatal(err)
	}
	// the sender changes its value after Publish
	sent["list"].([]any)[0] = "changed"
	sent["n"] = 2
	h.tb.pump()
	got, err := extension.EventValue[map[string]any](second.all()[0])
	if err != nil {
		t.Fatalf("the second subscriber got damaged data: %v", err)
	}
	if want := `{"list":["one","two"],"n":1}`; mustJSON(t, got) != want {
		t.Errorf("the second subscriber got %s, want %s", mustJSON(t, got), want)
	}
	if len(first.all()) != 1 {
		t.Fatal("the first subscriber heard nothing")
	}
	// a reply is a copy too
	if err := h.events("c").HandleAsync("c.get", func(_ context.Context, _ extension.Event, reply extension.EventReply) {
		value := map[string]any{"k": []any{"v"}}
		reply(value, nil)
		value["k"].([]any)[0] = "changed" // after the reply was given
	}); err != nil {
		t.Fatal(err)
	}
	o := newOutcome()
	if err := h.events("a").Request(context.Background(), "c.get", nil, o.fn); err != nil {
		t.Fatal(err)
	}
	h.tb.pump()
	_, ev, err := o.get()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(ev.Data); got != `{"k":["v"]}` {
		t.Errorf("the reply is %s", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRequestAndReply(t *testing.T) {
	h := newHarness(t, evExt{id: "a"}, evExt{id: "b"})
	var seen extension.Event
	if err := h.events("b").Handle("b.double", func(_ context.Context, ev extension.Event) (any, error) {
		seen = ev
		n, err := extension.EventValue[int](ev)
		return n * 2, err
	}); err != nil {
		t.Fatal(err)
	}
	o := newOutcome()
	if err := h.events("a").Request(context.Background(), "b.double", 21, o.fn); err != nil {
		t.Fatal(err)
	}
	if calls, _, _ := o.get(); calls != 0 {
		t.Fatal("the reply came inside Request")
	}
	h.tb.pump()
	calls, ev, err := o.get()
	if calls != 1 || err != nil {
		t.Fatalf("calls %d, err %v", calls, err)
	}
	if n, _ := extension.EventValue[int](ev); n != 42 || ev.Publisher != "b" || ev.Topic != "b.double" {
		t.Errorf("reply %+v, want 42 from b", ev)
	}
	if seen.Publisher != "a" || seen.Topic != "b.double" || seen.Scope.Name != "one" {
		t.Errorf("the handler saw %+v", seen)
	}
	if h.tb.clock.pending() != 0 {
		t.Errorf("%d timers left after the reply", h.tb.clock.pending())
	}
}

func TestRequestWithNoHandler(t *testing.T) {
	h := newHarness(t, evExt{id: "a"})
	o := newOutcome()
	if err := h.events("a").Request(context.Background(), "nobody.home", nil, o.fn); err != nil {
		t.Fatal(err)
	}
	h.tb.pump()
	calls, _, err := o.get()
	if calls != 1 {
		t.Fatalf("calls %d", calls)
	}
	wantRoute(t, err, "start.EventNoHandler")
}

func TestRequestTimesOutAfterTenSecondsAndTheLateReplyIsIgnored(t *testing.T) {
	h := newHarness(t, evExt{id: "a"}, evExt{id: "b"})
	var late extension.EventReply
	var hctx context.Context
	if err := h.events("b").HandleAsync("b.slow", func(ctx context.Context, _ extension.Event, reply extension.EventReply) {
		hctx, late = ctx, reply
	}); err != nil {
		t.Fatal(err)
	}
	o := newOutcome()
	if err := h.events("a").Request(context.Background(), "b.slow", nil, o.fn); err != nil {
		t.Fatal(err)
	}
	h.tb.pump()
	h.tb.clock.advance(DefaultRequestTimeout - time.Second)
	h.tb.pump()
	if calls, _, _ := o.get(); calls != 0 {
		t.Fatal("answered before the timeout")
	}
	h.tb.clock.advance(time.Second)
	h.tb.pump()
	calls, _, err := o.get()
	if calls != 1 {
		t.Fatalf("calls %d after the timeout", calls)
	}
	wantRoute(t, err, "EventTimeout")
	if hctx.Err() == nil {
		t.Error("the context of the handler was not cancelled")
	}
	late("too late", nil)
	h.tb.pump()
	if calls, _, _ := o.get(); calls != 1 {
		t.Errorf("a late reply was delivered (calls %d)", calls)
	}
}

func TestRequestEndsWithTheContext(t *testing.T) {
	tests := []struct {
		name  string
		ctx   func() (context.Context, context.CancelFunc)
		route string
	}{
		{"a deadline", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 20*time.Millisecond)
		}, "EventTimeout"},
		{"a cancel", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(20*time.Millisecond, cancel)
			return ctx, cancel
		}, "EventCanceled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, evExt{id: "a"}, evExt{id: "b"})
			if err := h.events("b").HandleAsync("b.slow", func(context.Context, extension.Event, extension.EventReply) {}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := tt.ctx()
			defer cancel()
			o := newOutcome()
			if err := h.events("a").Request(ctx, "b.slow", nil, o.fn); err != nil {
				t.Fatal(err)
			}
			h.await(o)
			_, _, err := o.get()
			wantRoute(t, err, tt.route)
		})
	}
}

func TestAHandlersErrorIsTheRequestersErrorAndNoFailure(t *testing.T) {
	h := newHarness(t, evExt{id: "a"}, evExt{id: "b"})
	cause := errors.New("Boom", "the handler could not do it")
	if err := h.events("b").Handle("b.fail", func(context.Context, extension.Event) (any, error) { return nil, cause }); err != nil {
		t.Fatal(err)
	}
	o := newOutcome()
	if err := h.events("a").Request(context.Background(), "b.fail", nil, o.fn); err != nil {
		t.Fatal(err)
	}
	h.tb.pump()
	_, _, err := o.get()
	wantRoute(t, err, "EventHandlerFailed")
	if appErr, _ := errors.FromError(err); appErr.Tag != "EventHandlerFailed" {
		t.Errorf("tag %q", appErr.Tag)
	}
	if !errors.Is(err, cause) {
		t.Errorf("the handler's error is not the cause of %v", err)
	}
	for _, id := range []string{"a", "b"} {
		if st := h.tb.scope.State(id); st != StateActive {
			t.Errorf("%s is %v after a handler's error", id, st)
		}
	}
	if n := h.tb.notifications(); len(n) != 0 {
		t.Errorf("notifications %v", n)
	}
	// a reply that is not JSON is the handler's error too
	if err := h.events("b").Handle("b.bad", func(context.Context, extension.Event) (any, error) { return make(chan int), nil }); err != nil {
		t.Fatal(err)
	}
	o2 := newOutcome()
	if err := h.events("a").Request(context.Background(), "b.bad", nil, o2.fn); err != nil {
		t.Fatal(err)
	}
	h.tb.pump()
	_, _, err = o2.get()
	wantRoute(t, err, "EventReplyInvalid")
}

func TestAHandlerThatPanicsFailsItsExtensionAndAnswersTheRequester(t *testing.T) {
	h := newHarness(t, evExt{id: "a"}, evExt{id: "b"})
	if err := h.events("b").Handle("b.boom", func(context.Context, extension.Event) (any, error) { panic("oops") }); err != nil {
		t.Fatal(err)
	}
	o := newOutcome()
	if err := h.events("a").Request(context.Background(), "b.boom", nil, o.fn); err != nil {
		t.Fatal(err)
	}
	h.tb.pump()
	calls, _, err := o.get()
	if calls != 1 {
		t.Fatalf("the requester was called %d times", calls)
	}
	wantRoute(t, err, "runHandler.func3.EventHandlerPanicked")
	if st := h.tb.scope.State("b"); st != StateFailed {
		t.Fatalf("b is %v", st)
	}
	wantRoute(t, h.tb.scope.Failure("b").Err, "runHandler.func3.ExtensionEventHandlerPanicked")
	if st := h.tb.scope.State("a"); st != StateActive {
		t.Errorf("the requester is %v", st)
	}
	if n := h.tb.notifications(); len(n) != 1 || !strings.Contains(n[0], "Ext b failed") {
		t.Errorf("notifications %v", n)
	}
	// its handler is gone with it
	o2 := newOutcome()
	if err := h.events("a").Request(context.Background(), "b.boom", nil, o2.fn); err != nil {
		t.Fatal(err)
	}
	h.tb.pump()
	_, _, err = o2.get()
	wantRoute(t, err, "EventNoHandler")
}

func TestARequestToAnExtensionThatGoesAwayIsAnswered(t *testing.T) {
	h := newHarness(t, evExt{id: "a"}, evExt{id: "b"})
	if err := h.events("b").HandleAsync("b.slow", func(context.Context, extension.Event, extension.EventReply) {}); err != nil {
		t.Fatal(err)
	}
	o := newOutcome()
	if err := h.events("a").Request(context.Background(), "b.slow", nil, o.fn); err != nil {
		t.Fatal(err)
	}
	h.tb.pump()
	h.tb.disabled = []string{"b"}
	h.tb.scope.Refresh()
	h.tb.pump()
	calls, _, err := o.get()
	if calls != 1 {
		t.Fatalf("calls %d", calls)
	}
	wantRoute(t, err, "EventHandlerGone")
	if h.tb.clock.pending() != 0 {
		t.Errorf("%d timers left", h.tb.clock.pending())
	}
}

func TestARequesterThatGoesAwayIsNotCalledBack(t *testing.T) {
	h := newHarness(t, evExt{id: "a"}, evExt{id: "b"})
	var reply extension.EventReply
	if err := h.events("b").HandleAsync("b.slow", func(_ context.Context, _ extension.Event, r extension.EventReply) { reply = r }); err != nil {
		t.Fatal(err)
	}
	o := newOutcome()
	if err := h.events("a").Request(context.Background(), "b.slow", nil, o.fn); err != nil {
		t.Fatal(err)
	}
	h.tb.pump()
	h.tb.disabled = []string{"a"}
	h.tb.scope.Refresh()
	reply("answer", nil)
	h.tb.pump()
	if calls, _, _ := o.get(); calls != 0 {
		t.Errorf("a deactivated requester was called %d times", calls)
	}
	if h.tb.clock.pending() != 0 {
		t.Errorf("%d timers left", h.tb.clock.pending())
	}
}

func TestAnAsyncHandlerRepliesFromAGoroutine(t *testing.T) {
	h := newHarness(t, evExt{id: "a"}, evExt{id: "b"})
	if err := h.events("b").HandleAsync("b.later", func(_ context.Context, ev extension.Event, reply extension.EventReply) {
		go reply("done", nil)
	}); err != nil {
		t.Fatal(err)
	}
	o := newOutcome()
	if err := h.events("a").Request(context.Background(), "b.later", nil, o.fn); err != nil {
		t.Fatal(err)
	}
	h.await(o)
	_, ev, err := o.get()
	if got, _ := extension.EventValue[string](ev); err != nil || got != "done" {
		t.Errorf("reply %q (%v)", got, err)
	}
}

func TestACallbackThatPanicsFailsItsExtension(t *testing.T) {
	t.Run("a subscriber", func(t *testing.T) {
		h := newHarness(t, evExt{id: "a"}, evExt{id: "b"}, evExt{id: "c"})
		if _, err := h.events("b").Subscribe("a.*", func(extension.Event) { panic("oops") }); err != nil {
			t.Fatal(err)
		}
		healthy := h.subscribe("c", "a.*")
		for range 2 {
			if err := h.events("a").Publish("a.t", nil); err != nil {
				t.Fatal(err)
			}
		}
		h.tb.pump()
		if st := h.tb.scope.State("b"); st != StateFailed {
			t.Fatalf("b is %v", st)
		}
		wantRoute(t, h.tb.scope.Failure("b").Err, "Scope.runSubscriber.func1.ExtensionEventSubscriberPanicked")
		if len(healthy.all()) != 2 {
			t.Errorf("another subscriber heard %d events, want 2", len(healthy.all()))
		}
		if st := h.tb.scope.State("a"); st != StateActive {
			t.Errorf("the publisher is %v", st)
		}
		if n := h.tb.notifications(); len(n) != 1 {
			t.Errorf("notifications %v, want one", n)
		}
	})
	t.Run("the reply of a request", func(t *testing.T) {
		h := newHarness(t, evExt{id: "a"}, evExt{id: "b"})
		if err := h.events("b").Handle("b.ok", func(context.Context, extension.Event) (any, error) { return 1, nil }); err != nil {
			t.Fatal(err)
		}
		if err := h.events("a").Request(context.Background(), "b.ok", nil, func(extension.Event, error) { panic("oops") }); err != nil {
			t.Fatal(err)
		}
		h.tb.pump()
		if st := h.tb.scope.State("a"); st != StateFailed {
			t.Fatalf("a is %v", st)
		}
		wantRoute(t, h.tb.scope.Failure("a").Err, "Scope.callReply.func1.ExtensionRequestReplyPanicked")
		if st := h.tb.scope.State("b"); st != StateActive {
			t.Errorf("the handler is %v", st)
		}
	})
}

func TestASlowHandlerIsLogged(t *testing.T) {
	h := newHarness(t, evExt{id: "a"}, evExt{id: "b"})
	for _, tt := range []struct {
		name string
		took time.Duration
		slow bool
	}{{"under the limit", SlowHandler, false}, {"over the limit", SlowHandler + time.Millisecond, true}} {
		t.Run(tt.name, func(t *testing.T) {
			h.tb.log.Reset()
			sub, err := h.events("b").Subscribe("a.slow", func(extension.Event) { h.tb.clock.advance(tt.took) })
			if err != nil {
				t.Fatal(err)
			}
			defer sub()
			if err := h.events("a").Publish("a.slow", nil); err != nil {
				t.Fatal(err)
			}
			h.tb.pump()
			logged := strings.Contains(h.tb.log.String(), "an event subscriber is slow")
			if logged != tt.slow {
				t.Errorf("logged %v, want %v: %s", logged, tt.slow, h.tb.log.String())
			}
			if tt.slow && !strings.Contains(h.tb.log.String(), "extension=b") {
				t.Errorf("the log does not name the extension: %s", h.tb.log.String())
			}
		})
	}
}

func TestFastTopicsAreDeliveredOncePerFrameAsABatch(t *testing.T) {
	a := evExt{id: "a", decl: extension.EventDecl{Publishes: []extension.EventSpec{{Topic: "a.typing", Fast: true}}}}
	h := newHarness(t, a, evExt{id: "b"})
	g := h.subscribe("b", "a.*")
	for i := range 100 {
		if err := h.events("a").Publish("a.typing", i); err != nil {
			t.Fatal(err)
		}
	}
	h.tb.pump()
	if len(g.all()) != 0 {
		t.Fatalf("%d events before the frame ended", len(g.all()))
	}
	h.tb.frame()
	got := g.all()
	if len(got) != 1 {
		t.Fatalf("typing 100 characters gave %d events, want 1 batch", len(got))
	}
	values, err := extension.EventValues[int](got[0])
	if err != nil || len(values) != 100 {
		t.Fatalf("%d values (%v)", len(values), err)
	}
	for i, v := range values {
		if v != i {
			t.Fatalf("value %d is %d: the batch is out of order", i, v)
		}
	}
	if last, _ := extension.EventValue[int](got[0]); last != 99 {
		t.Errorf("Data is %d, want the last value", last)
	}
	// the next frame has nothing to deliver, and a later change makes a batch of its own
	h.tb.frame()
	if err := h.events("a").Publish("a.typing", 100); err != nil {
		t.Fatal(err)
	}
	h.tb.frame()
	if got = g.all(); len(got) != 2 || len(got[1].Batch) != 1 {
		t.Errorf("events %d, second batch %v", len(got), got[len(got)-1].Batch)
	}
}

func TestAnEventIsNeverDeliveredBeforeAnEarlierOne(t *testing.T) {
	a := evExt{id: "a", decl: extension.EventDecl{Publishes: []extension.EventSpec{{Topic: "a.typing", Fast: true}}}}
	h := newHarness(t, a, evExt{id: "b"})
	g := h.subscribe("b", "a.*")
	pub := func(topic string, v int) {
		if err := h.events("a").Publish(topic, v); err != nil {
			t.Fatal(err)
		}
	}
	pub("a.typing", 1)
	pub("a.typing", 2)
	pub("a.closed", 3)
	pub("a.typing", 4)
	h.tb.pump() // no frame: the normal event comes, with the fast ones before it
	if got := g.topics(); !slices.Equal(got, []string{"a.typing", "a.closed"}) {
		t.Fatalf("before the frame: %v", got)
	}
	if v, _ := extension.EventValues[int](g.all()[0]); !slices.Equal(v, []int{1, 2}) {
		t.Errorf("first batch %v", v)
	}
	h.tb.frame()
	got := g.all()
	if len(got) != 3 || got[2].Topic != "a.typing" || len(got[2].Batch) != 1 {
		t.Errorf("after the frame: %v", g.topics())
	}
	if got[0].Seq >= got[1].Seq || got[1].Seq >= got[2].Seq {
		t.Errorf("sequence numbers %d %d %d", got[0].Seq, got[1].Seq, got[2].Seq)
	}
}

func TestAnEventPublishedByASubscriberIsDeliveredAfterTheCurrentOne(t *testing.T) {
	h := newHarness(t, evExt{id: "a"})
	h.tb.deferred = false // Post runs at once, so a publish inside a subscriber would nest if the bus let it
	depth, deepest := 0, 0
	var order []string
	if _, err := h.events("a").Subscribe("a.*", func(ev extension.Event) {
		depth++
		deepest = max(deepest, depth)
		order = append(order, ev.Topic)
		if ev.Topic == "a.one" {
			if err := h.events("a").Publish("a.two", nil); err != nil {
				t.Error(err)
			}
		}
		depth--
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.events("a").Publish("a.one", nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []string{"a.one", "a.two"}) || deepest != 1 {
		t.Errorf("order %v, nesting %d", order, deepest)
	}
}

func TestPublishCore(t *testing.T) {
	h := newHarness(t, evExt{id: "a"})
	g := h.subscribe("a", "emetos.**")
	wantRoute(t, h.tb.scope.PublishCore("a.thing", nil), "PublishCore.CoreTopicUnknown")
	wantRoute(t, h.tb.scope.PublishCore(extension.TopicDocumentOpened, make(chan int)), "PublishCore.EventValueInvalid")
	for i := range 3 {
		if err := h.tb.scope.PublishCore(extension.TopicEditorSelectionChanged, extension.EditorSelection{URI: "file:///a", Selections: []extension.Selection{{Active: extension.Position{Line: i}}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.tb.scope.PublishCore(extension.TopicDocumentOpened, extension.DocumentInfo{URI: "file:///a", LanguageID: "go", Version: 1}); err != nil {
		t.Fatal(err)
	}
	h.tb.pump()
	if got := g.topics(); !slices.Equal(got, []string{extension.TopicEditorSelectionChanged, extension.TopicDocumentOpened}) {
		t.Fatalf("heard %v", got)
	}
	first := g.all()[0]
	if first.Publisher != extension.CorePublisher || len(first.Batch) != 3 {
		t.Errorf("selection events: publisher %q, batch %d", first.Publisher, len(first.Batch))
	}
	opened, err := extension.EventValue[extension.DocumentInfo](g.all()[1])
	if err != nil || opened.LanguageID != "go" || g.all()[1].Batch != nil {
		t.Errorf("opened %+v (%v), batch %v", opened, err, g.all()[1].Batch)
	}
}

func TestTheWorkspaceEventsComeAtTheStartAndBeforeTheEnd(t *testing.T) {
	var g heard
	var ctx context.Context
	f, _ := factory(manifest("a"), func(f *fake) {
		f.activate = func(c context.Context, api extension.API) error {
			ctx = c
			_, err := api.Events().Subscribe("emetos.workspace.*", func(ev extension.Event) {
				g.fn(ev)
				if ev.Topic == extension.TopicWorkspaceClosed && c.Err() != nil {
					t.Error("the extension was deactivated before it heard that the workspace closes")
				}
			})
			return err
		}
	})
	r := newRig(t, f)
	tb := r.newTab("work")
	tb.deferred = true
	tb.scope.Start()
	if len(g.all()) != 0 {
		t.Fatal("the event came inside Start")
	}
	tb.pump()
	if got := g.topics(); !slices.Equal(got, []string{extension.TopicWorkspaceOpened}) {
		t.Fatalf("after the start: %v", got)
	}
	info, err := extension.EventValue[extension.WorkspaceInfo](g.all()[0])
	if err != nil || info.Name != "work" || info.Folder != "/work/work" || info.Kind != extension.ScopeWorkspace {
		t.Errorf("payload %+v (%v)", info, err)
	}
	tb.scope.Close() // nothing is pumped: the closing delivers at once
	if got := g.topics(); !slices.Equal(got, []string{extension.TopicWorkspaceOpened, extension.TopicWorkspaceClosed}) {
		t.Errorf("after the close: %v", got)
	}
	if ctx.Err() == nil {
		t.Error("the extension was not deactivated")
	}
}

func TestNoEventCrossesToAnotherScope(t *testing.T) {
	apis := map[string]extension.API{}
	var mu sync.Mutex
	f, _ := factory(manifest("a"), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			mu.Lock()
			apis[api.Scope().Name] = api
			mu.Unlock()
			return nil
		}
	})
	r := newRig(t, f)
	notes, office := r.newTab("notes"), r.newTab("office")
	notes.scope.Start()
	office.scope.Start()
	notes.deferred, office.deferred = true, true
	heardIn := map[string]*heard{"notes": {}, "office": {}}
	for name, g := range heardIn {
		if _, err := apis[name].Events().Subscribe("**", g.fn); err != nil {
			t.Fatal(err)
		}
	}
	if err := apis["notes"].Events().Publish("a.t", nil); err != nil {
		t.Fatal(err)
	}
	if err := notes.scope.PublishCore(extension.TopicDocumentOpened, extension.DocumentInfo{URI: "file:///x"}); err != nil {
		t.Fatal(err)
	}
	notes.pump()
	office.pump()
	if got := heardIn["notes"].topics(); !slices.Equal(got, []string{"a.t", extension.TopicDocumentOpened}) {
		t.Errorf("notes heard %v", got)
	}
	if got := heardIn["office"].topics(); len(got) != 0 {
		t.Errorf("office heard %v", got)
	}
	// a handler of one scope is not a handler of the other
	if err := apis["notes"].Events().Handle("a.req", func(context.Context, extension.Event) (any, error) { return 1, nil }); err != nil {
		t.Fatal(err)
	}
	o := newOutcome()
	if err := apis["office"].Events().Request(context.Background(), "a.req", nil, o.fn); err != nil {
		t.Fatal(err)
	}
	office.pump()
	_, _, err := o.get()
	wantRoute(t, err, "start.EventNoHandler")
	// the same topic can be handled in both scopes
	if err := apis["office"].Events().Handle("a.req", func(context.Context, extension.Event) (any, error) { return 2, nil }); err != nil {
		t.Errorf("the handler of another scope blocks this one: %v", err)
	}
}

func TestDeclaredTopicsAreCheckedAgainstTheOtherExtensions(t *testing.T) {
	decl := func(topics ...string) extension.EventDecl {
		var d extension.EventDecl
		for _, topic := range topics {
			d.Publishes = append(d.Publishes, extension.EventSpec{Topic: topic})
		}
		return d
	}
	mk := func(id string, d extension.EventDecl) extension.Factory {
		m := manifest(id)
		m.Events = d
		f, _ := factory(m, nil)
		return f
	}
	tests := []struct {
		name   string
		decl   extension.EventDecl
		left   int    // extensions kept
		route  string // empty: none left out
		others string
	}{
		{"a free topic", decl("shared.topic"), 2, "", ""},
		{"a topic under the own id", decl("a.x"), 2, "", ""},
		{"a topic under another extension", decl("b.x"), 1, "checkTopicOwnership.ExtensionTopicForeign", ""},
		{"a topic of Emetos", decl("emetos.document.opened"), 1, "checkTopicOwnership.ExtensionTopicForeign", ""},
		{"a handled topic under another extension", extension.EventDecl{Handles: []string{"b.req"}}, 1, "checkTopicOwnership.ExtensionTopicForeign", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := NewHost(HostOptions{Factories: []extension.Factory{mk("a", tt.decl), mk("b", extension.EventDecl{})}})
			if got := len(host.Manifests()); got != tt.left {
				t.Fatalf("%d extensions kept, want %d", got, tt.left)
			}
			problems := host.Problems()
			if tt.route == "" {
				if len(problems) != 0 {
					t.Fatalf("problems %v", problems)
				}
				return
			}
			if len(problems) != 1 {
				t.Fatalf("problems %v", problems)
			}
			wantRoute(t, problems[0], tt.route)
		})
	}
}
