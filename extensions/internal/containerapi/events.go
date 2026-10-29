package containerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/rm4n0s/errors"
)

// EventsOptions are the options of Events.
type EventsOptions struct {
	// Since replays the events from this time on (the zero time starts at now); Until ends the stream at this time (the zero
	// time never).
	Since, Until time.Time
	// Filters are the engine's filters ({"type": {"container"}}).
	Filters Filters
	// Ready, when it is not nil, is called once the engine has accepted the subscription, before the first event is read: an
	// event that happens after Ready is not missed.
	Ready func()
}

// Events reads the engine's event stream and calls fn for each event until ctx is cancelled (then it returns nil), fn
// returns an error (which Events returns), or the stream ends. A stream that ends by itself while there is no Until is the
// engine going away: that is the error ContainerAPIStreamEnded, so that the caller can connect again.
func (c *Client) Events(ctx context.Context, opts EventsOptions, fn func(Event) error) error {
	q := url.Values{}
	if !opts.Since.IsZero() {
		q.Set("since", strconv.FormatInt(opts.Since.Unix(), 10))
	}
	if !opts.Until.IsZero() {
		q.Set("until", strconv.FormatInt(opts.Until.Unix(), 10))
	}
	opts.Filters.addTo(q)
	if c.kind == KindPodman {
		q.Set("stream", "true")
	}
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/events", query: q})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() // a body that was read has nothing to report on closing
	if opts.Ready != nil {
		opts.Ready()
	}
	err = scanLines(ctx, resp.Body, func(line []byte) error {
		var w struct {
			Type   string `json:"Type"`
			Action string `json:"Action"`
			Status string `json:"status"`
			ID     string `json:"id"`
			Actor  struct {
				ID         string            `json:"ID"`
				Attributes map[string]string `json:"Attributes"`
			} `json:"Actor"`
			Scope    string `json:"scope"`
			Time     int64  `json:"time"`
			TimeNano int64  `json:"timeNano"`
		}
		if err := json.Unmarshal(line, &w); err != nil {
			return errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "GET", "path", "/events")
		}
		ev := Event{Type: w.Type, Action: w.Action, ID: w.Actor.ID, Attributes: w.Actor.Attributes, Scope: w.Scope}
		if ev.Action == "" {
			ev.Action = w.Status
		}
		if ev.ID == "" {
			ev.ID = w.ID
		}
		if w.TimeNano > 0 {
			ev.Time = time.Unix(0, w.TimeNano)
		} else if w.Time > 0 {
			ev.Time = time.Unix(w.Time, 0)
		}
		return fn(ev)
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil || !opts.Until.IsZero() {
		return nil
	}
	return errors.New("ContainerAPIStreamEnded", "the engine closed the event stream", "endpoint", c.ep.String())
}
