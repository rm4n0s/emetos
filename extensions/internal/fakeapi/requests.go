package fakeapi

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Published is an event the extension published with Events().Publish.
type Published struct {
	Topic string
	Data  json.RawMessage
}

// RequestCall is a request the extension made with Events().Request.
type RequestCall struct {
	Topic string
	Data  json.RawMessage
}

// RequestDouble answers a request: it is called when the extension makes the request, with the event of the request, and calls
// answer once with the value of the reply or an error, from any goroutine. The extension's reply function is called later, on
// Pump, as the UI thread does.
type RequestDouble func(ctx context.Context, ev extension.Event, answer func(value any, err error))

// OnRequest makes fn answer the requests of the extension for the topic. It is how a test fakes the core request topics of Emetos
// (extension.TopicFilesList, extension.TopicDocumentEdit, extension.TopicCommandsExecute and the others, SPEC §10.19 "Core
// request topics") and the handled topics of other extensions: the fake has no core and no other extension. A second call
// for the topic replaces the first.
func (a *API) OnRequest(topic string, fn RequestDouble) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requestDoubles[topic] = fn
}

// OnRequestValue is OnRequest for a double that answers at once, with a function of the payload decoded into a P. A payload
// that does not decode into a P is answered with the error of the decoding.
func OnRequestValue[P any](a *API, topic string, fn func(ctx context.Context, p P) (any, error)) {
	a.OnRequest(topic, func(ctx context.Context, ev extension.Event, answer func(any, error)) {
		var p P
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			answer(nil, err)
			return
		}
		v, err := fn(ctx, p)
		answer(v, err)
	})
}

// Requests are the requests the extension made, the first first.
func (a *API) Requests() []RequestCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.requests)
}

// PublishedEvents are the events the extension published, the first first.
func (a *API) PublishedEvents() []Published {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.published)
}

// Ask makes a request of the extension's own handler of the topic (set with Events().Handle) as another extension would, and
// returns the reply or the error. The handler runs on the caller's goroutine, which stands for the UI thread. It fails with
// EventNoHandler when the extension handles no such topic.
func (a *API) Ask(ctx context.Context, topic string, value any) (extension.Event, error) {
	return a.AskAs(ctx, "test", topic, value)
}

// AskAs is Ask for a request made by the extension with the id publisher, which the handler reads in Event.Publisher (the terminal
// reads who asked from it: Relay's word about its client counts, any other extension is what the bus says it is).
func (a *API) AskAs(ctx context.Context, publisher, topic string, value any) (extension.Event, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return extension.Event{}, err
	}
	a.mu.Lock()
	h := a.handlers2[topic]
	a.mu.Unlock()
	if h == nil {
		return extension.Event{}, errors.New("EventNoHandler", "the extension handles no such topic", "topic", topic)
	}
	type result struct {
		v   any
		err error
	}
	ch := make(chan result, 1)
	h(ctx, extension.Event{Topic: topic, Publisher: publisher, Scope: a.info, Data: data}, func(v any, err error) { ch <- result{v, err} })
	for {
		select {
		case r := <-ch:
			if r.err != nil {
				return extension.Event{}, r.err
			}
			out, merr := json.Marshal(r.v)
			if merr != nil {
				return extension.Event{}, merr
			}
			return extension.Event{Topic: topic, Publisher: a.manifest.ID, Scope: a.info, Data: out}, nil
		default:
			a.Pump()
			time.Sleep(time.Millisecond)
		}
	}
}
