package exthost

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// eventsAPI is the extension.Events of one activation (SPEC §10.15 "Events").
type eventsAPI struct{ inst *instance }

var _ extension.Events = eventsAPI{}

// Events implements extension.API.
func (a *api) Events() extension.Events { return eventsAPI{a.inst} }

// owns tells whether the extension may publish or handle a topic: it is under the id of the extension and a dot, or the
// manifest lists it.
func (in *instance) owns(topic string, declared func(extension.EventDecl) []string) bool {
	return strings.HasPrefix(topic, in.id+".") || slices.Contains(declared(in.slot.info.Manifest.Events), topic)
}

// hasPrivilege tells whether the extension was granted a privilege: the host kept its manifest only when it is on the
// allowlist or declares none (validateManifest), and the manifest it reads is the one of the host, not the one the
// extension returns later.
func (in *instance) hasPrivilege(privilege string) bool {
	return in.slot.info.Manifest.HasPrivilege(privilege)
}

// Publish implements extension.Events. It fails with ExtensionDeactivated, the tags of extension.CheckTopic
// (EventTopicInvalid), EventTopicNotOwned and EventValueInvalid.
func (e eventsAPI) Publish(topic string, value any) error {
	in := e.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if err := extension.CheckTopic(topic); err != nil {
		return err
	}
	fast := false
	for _, spec := range in.slot.info.Manifest.Events.Publishes {
		if spec.Topic == topic {
			fast = spec.Fast
		}
	}
	if !in.owns(topic, func(d extension.EventDecl) []string {
		topics := make([]string, len(d.Publishes))
		for i, spec := range d.Publishes {
			topics[i] = spec.Topic
		}
		return topics
	}) {
		return errors.New("EventTopicNotOwned", "an extension publishes only the topics under its id and the ones its manifest declares", "extension", in.id, "topic", topic)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return errors.NewErr("EventValueInvalid", err).SetMetadata("extension", in.id, "topic", topic)
	}
	in.scope.bus.publish(in.id, topic, data, fast)
	return nil
}

// Subscribe implements extension.Events. It fails with ExtensionDeactivated and EventPatternInvalid.
func (e eventsAPI) Subscribe(pattern string, fn func(extension.Event), opts ...extension.SubscribeOption) (func(), error) {
	in := e.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if err := extension.CheckPattern(pattern); err != nil {
		return nil, err
	}
	var o extension.SubscribeOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.From.Any() && !extension.PatternWithin(pattern, in.slot.info.Manifest.Events.CrossTab) {
		return nil, errors.New("EventCrossTabNotDeclared", "a subscription to other window tabs must be within the cross-tab topics of the manifest", "extension", in.id, "pattern", pattern)
	}
	return in.scope.bus.subscribe(&subscription{pattern: pattern, fn: fn, inst: in, from: o.From}), nil
}

// Request implements extension.Events. It fails with ExtensionDeactivated, EventTopicInvalid and EventValueInvalid.
func (e eventsAPI) Request(ctx context.Context, topic string, value any, reply func(extension.Event, error), opts ...extension.RequestOption) error {
	in := e.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if err := extension.CheckTopic(topic); err != nil {
		return err
	}
	var o extension.RequestOptions
	for _, opt := range opts {
		opt(&o)
	}
	var to *extension.ScopeFilter
	if o.To.Any() {
		if !extension.PatternWithin(topic, in.slot.info.Manifest.Events.CrossTab) {
			return errors.New("EventRequestCrossTabNotDeclared", "a request to other window tabs must be within the cross-tab topics of the manifest", "extension", in.id, "topic", topic)
		}
		to = &o.To
	}
	data, err := json.Marshal(value)
	if err != nil {
		return errors.NewErr("EventValueInvalid", err).SetMetadata("extension", in.id, "topic", topic)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if reply == nil {
		reply = func(extension.Event, error) {}
	}
	in.scope.bus.request(in, ctx, topic, data, reply, to)
	return nil
}

// Handle implements extension.Events: a handler that answers at once is an asynchronous handler that replies before it
// returns.
func (e eventsAPI) Handle(topic string, fn extension.EventHandler) error {
	return e.HandleAsync(topic, func(ctx context.Context, ev extension.Event, reply extension.EventReply) {
		v, err := fn(ctx, ev)
		reply(v, err)
	})
}

// HandleAsync implements extension.Events. It fails with ExtensionDeactivated, EventTopicInvalid, EventTopicNotOwned and
// EventHandlerTaken.
func (e eventsAPI) HandleAsync(topic string, fn extension.AsyncEventHandler) error {
	in := e.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if err := extension.CheckTopic(topic); err != nil {
		return err
	}
	if !in.owns(topic, func(d extension.EventDecl) []string { return d.Handles }) {
		return errors.New("EventTopicNotOwned", "an extension handles only the topics under its id and the ones its manifest declares", "extension", in.id, "topic", topic)
	}
	if !in.scope.bus.addHandler(&handlerEntry{topic: topic, fn: fn, inst: in}) {
		return errors.New("EventHandlerTaken", "a handler for this topic is set already in the scope", "extension", in.id, "topic", topic)
	}
	return nil
}

// ---- the core events ----

// coreTopics are the topics Emetos publishes.
var coreTopics = []string{
	extension.TopicDocumentOpened, extension.TopicDocumentChanged, extension.TopicDocumentSaved, extension.TopicDocumentClosed,
	extension.TopicEditorActiveChanged, extension.TopicEditorSelectionChanged,
	extension.TopicWorkspaceOpened, extension.TopicWorkspaceClosed, extension.TopicWindowTabActivated, extension.TopicFilesChanged, extension.TopicSharingRevoked,
}

// PublishCore publishes one of the core events of Emetos (the Topic constants of package extension) in the scope; the
// window calls it. It may be called from any goroutine. It fails with CoreTopicUnknown for any other topic and with
// EventValueInvalid for a value that is not JSON.
func (s *Scope) PublishCore(topic string, value any) error {
	if !slices.Contains(coreTopics, topic) {
		return errors.New("CoreTopicUnknown", "Emetos publishes only its core events", "topic", topic)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return errors.NewErr("EventValueInvalid", err).SetMetadata("topic", topic)
	}
	s.bus.publish(extension.CorePublisher, topic, data, fastCoreTopics[topic])
	return nil
}

// workspaceInfo is the payload of the workspace events.
func (s *Scope) workspaceInfo() extension.WorkspaceInfo {
	return extension.WorkspaceInfo(s.opts.Info) // the same fields; the JSON tags are the payload's
}

// ---- the calls into extensions ----

// afterFrame runs f after the next frame; without a Frame function in the options, on the UI thread, soon.
func (s *Scope) afterFrame(f func()) {
	if s.opts.Frame != nil {
		s.opts.Frame(f)
		return
	}
	s.post(f)
}

// afterTimer calls f after d, from any goroutine, and returns the function that stops it.
func (s *Scope) afterTimer(d time.Duration, f func()) (stop func()) {
	if s.opts.After != nil {
		return s.opts.After(d, f)
	}
	t := time.AfterFunc(d, f)
	return func() { t.Stop() }
}

// now is the time of the scope's clock.
func (s *Scope) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now()
	}
	return time.Now()
}

// timed calls f and logs it when it took longer than SlowHandler: long work belongs to a goroutine.
func (s *Scope) timed(inst *instance, what, topic string, f func()) {
	start := s.now()
	f()
	if d := s.now().Sub(start); d > SlowHandler {
		inst.log.Warn("an event "+what+" is slow; long work belongs to a goroutine", "topic", topic, "took", d.String(), "limit", SlowHandler.String())
	}
}

// runSubscriber calls the function an extension subscribed with. A panic fails the extension with
// ExtensionEventSubscriberPanicked.
func (s *Scope) runSubscriber(inst *instance, pattern string, f func(extension.Event), ev extension.Event) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionEventSubscriberPanicked", "it panicked in the subscriber of %s on the event %s: %v", pattern, ev.Topic, r).SetMetadata("extension", inst.id, "pattern", pattern, "topic", ev.Topic, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "event subscriber "+pattern)
		}
	}()
	s.timed(inst, "subscriber", ev.Topic, func() { f(ev) })
}

// runHandler calls the handler of a request, on the UI thread, and routes its reply to the requester. A panic fails the
// extension that handles with ExtensionEventHandlerPanicked and answers the requester with EventHandlerPanicked; an error
// the handler returns is the requester's EventHandlerFailed and no failure of the extension.
func (s *Scope) runHandler(h *handlerEntry, r *request, hctx context.Context) {
	inst := h.inst
	rb := r.requester.scope.bus
	var once sync.Once
	// answer settles the request on the requester's UI thread; only the first answer counts.
	answer := func(ev extension.Event, err error) {
		once.Do(func() {
			rb.s.post(func() {
				if !rb.settle(r) {
					return
				}
				// the reply goes back along the link it came by; a link that was removed since stops it
				if r.handlerScope != nil && !rb.s.h.allows(r.handlerScope, rb.s, r.ev.Topic) {
					rb.s.callReply(r, extension.Event{}, errors.New("EventSharingRevoked", "the link that carried the request was removed before the reply came", "topic", r.ev.Topic, "from", r.handlerScope.ID()))
					return
				}
				rb.s.callReply(r, ev, err)
			})
		})
	}
	reply := func(value any, err error) {
		if err != nil {
			answer(extension.Event{}, errors.NewErr("EventHandlerFailed", err).SetMetadata("extension", inst.id, "topic", r.ev.Topic))
			return
		}
		data, merr := json.Marshal(value)
		if merr != nil {
			answer(extension.Event{}, errors.NewErr("EventReplyInvalid", merr).SetMetadata("extension", inst.id, "topic", r.ev.Topic))
			return
		}
		answer(extension.Event{Topic: r.ev.Topic, Publisher: inst.id, Scope: s.opts.Info, Seq: s.bus.nextSeq(), Data: data}, nil)
	}
	defer func() {
		if rec := recover(); rec != nil {
			answer(extension.Event{}, errors.NewErrf("EventHandlerPanicked", "the handler of the topic panicked: %v", rec).SetMetadata("extension", inst.id, "topic", r.ev.Topic))
			failure := errors.NewErrf("ExtensionEventHandlerPanicked", "it panicked in the handler of %s: %v", r.ev.Topic, rec).SetMetadata("extension", inst.id, "topic", r.ev.Topic, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "event handler "+r.ev.Topic)
		}
	}()
	ev := r.ev
	ev.Data = slices.Clone(ev.Data)
	s.timed(inst, "handler", r.ev.Topic, func() { h.fn(hctx, ev, reply) })
}

// callReply calls the function a requester gave Request, on the UI thread. A panic fails the requesting extension with
// ExtensionRequestReplyPanicked. Nothing is called when the requester was deactivated.
func (s *Scope) callReply(r *request, ev extension.Event, err error) {
	inst := r.requester
	if inst.isDead() {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			failure := errors.NewErrf("ExtensionRequestReplyPanicked", "it panicked in the reply of its request %s: %v", r.ev.Topic, rec).SetMetadata("extension", inst.id, "topic", r.ev.Topic, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "reply of request "+r.ev.Topic)
		}
	}()
	s.timed(inst, "reply callback", r.ev.Topic, func() { r.reply(ev, err) })
}
