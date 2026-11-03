package httpclient

import (
	"context"
	"time"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/history"
)

// CommandDevSeedHistory is the dev command that fills the history with a few requests of today and yesterday, for the proof of the side
// bar (SPEC §15.2): the history is kept in Emetos's state, so a fixture folder cannot carry it. It exists in debug builds only.
const CommandDevSeedHistory = "dev.httpclient.seedHistory"

// registerDev registers the dev commands in the builds that run dev commands.
func (e *Extension) registerDev() {
	if !foundation.DebugMode {
		return
	}
	spec := extension.CommandSpec{ID: CommandDevSeedHistory, Description: "Fill the history of the HTTP client with sample requests of today and yesterday."}
	echo := extension.CommandSpec{ID: CommandDevEcho, Description: "Start a local echo server for the HTTP client and point the variable baseUrl of the active environment at it."}
	if err := e.api.Commands().Register(echo, e.cmdDevEcho); err != nil {
		e.api.Log().Debug("the dev command of the HTTP client is not registered", "err", err)
	}
	if err := e.api.Commands().Register(spec, e.cmdDevSeedHistory); err != nil {
		e.api.Log().Debug("the dev command of the HTTP client is not registered", "err", err)
	}
}

// cmdDevSeedHistory adds the sample requests.
func (e *Extension) cmdDevSeedHistory(context.Context, any) error {
	now := e.opts.Now()
	pair := func(n, v string) format.Pair { return format.Pair{Name: n, Value: v, Enabled: true} }
	samples := []struct {
		ago    time.Duration
		method string
		url    string
		status int
		text   string
		ms     int64
		size   int64
		auth   bool
	}{
		{26 * time.Hour, "GET", "https://api.example.com/v1/users?page=1", 200, "OK", 84, 2210, true},
		{25 * time.Hour, "DELETE", "https://api.example.com/v1/users/17", 404, "Not Found", 41, 62, true},
		{3 * time.Hour, "POST", "https://api.example.com/v1/users", 201, "Created", 132, 540, true},
		{2 * time.Hour, "PUT", "https://api.example.com/v1/users/17", 200, "OK", 97, 540, true},
		{50 * time.Minute, "PATCH", "https://api.example.com/v1/users/17?token=abc123", 500, "Internal Server Error", 310, 180, false},
		{10 * time.Minute, "GET", "{{baseUrl}}/v1/health", 200, "OK", 12, 17, false},
	}
	for _, s := range samples {
		req := format.Request{Name: s.method + " " + pathOf(s.url), Method: s.method, URL: s.url}
		if s.auth {
			req.Headers = []format.Pair{pair("Authorization", "Bearer sample-token-value"), pair("Accept", "application/json")}
		}
		if _, err := e.hist.Add(history.Sent{
			Time: now.Add(-s.ago), Request: req, Status: s.status, StatusText: s.text, Duration: time.Duration(s.ms) * time.Millisecond, Size: s.size,
		}); err != nil {
			return err
		}
	}
	e.historyView.rebuild()
	return nil
}

// CommandDevEcho is the dev command that starts a small HTTP server on 127.0.0.1 that answers every request with a description of it
// (its method, path, query, headers and body as JSON), for the proofs of the request tab and the response pane (SPEC §15.2). It
// sets the current value of the variable baseUrl of the active environment to the address of the server, so that a request with
// {{baseUrl}} reaches it. The server stops with the window. It exists in debug builds only.
const CommandDevEcho = "dev.httpclient.echo"
