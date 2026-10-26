package lsp

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestHandleRequestErrors(t *testing.T) {
	c := &Client{log: slog.New(slog.DiscardHandler), registrations: map[string]Registration{}}
	tests := []struct {
		name   string
		method string
		params string
		route  string
	}{
		{"an unknown method", "custom/unknown", `{}`, "Client.handleRequest.MethodNotFound"},
		{"configuration params of a wrong type", MethodConfiguration, `5`, "Client.handleRequest->decodeParams.InvalidParams"},
		{"applyEdit params of a wrong type", MethodApplyEdit, `5`, "Client.handleRequest->decodeParams.InvalidParams"},
		{"showMessageRequest params of a wrong type", MethodShowMessageRequest, `5`, "Client.handleRequest->decodeParams.InvalidParams"},
		{"progress create params of a wrong type", MethodProgressCreate, `5`, "Client.handleRequest->decodeParams.InvalidParams"},
		{"registration params of a wrong type", MethodRegisterCapability, `5`, "Client.handleRequest->decodeParams.InvalidParams"},
		{"unregistration params of a wrong type", MethodUnregisterCapability, `5`, "Client.handleRequest->decodeParams.InvalidParams"},
		{"no params at all", MethodConfiguration, ``, "Client.handleRequest->decodeParams.InvalidParams"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.handleRequest(context.Background(), tt.method, json.RawMessage(tt.params))
			wantRoute(t, err, tt.route)
			// The connection answers with the code of the tag.
			re := responseError(err)
			want := CodeMethodNotFound
			if tt.method != "custom/unknown" {
				want = CodeInvalidParams
			}
			if re.Code != want {
				t.Errorf("code = %d, want %d", re.Code, want)
			}
		})
	}
}

func TestDeliverDropsUnreadableNotifications(t *testing.T) {
	log := &logRecorder{}
	c := &Client{log: slog.New(log)}
	called := false
	deliverTo(c, MethodLogMessage, json.RawMessage(`[1]`), func(LogMessageParams) { called = true })
	deliverTo[LogMessageParams](c, MethodLogMessage, json.RawMessage(`{}`), nil) // no handler
	if called {
		t.Errorf("the handler got params that cannot be read")
	}
	errs := log.errors()
	if len(errs) != 1 {
		t.Fatalf("logged errors = %v", errs)
	}
	wantRoute(t, errs[0], "decodeParams.InvalidParams")
}

func TestQueue(t *testing.T) {
	q := newQueue[int]()
	for i := 1; i <= 3; i++ {
		if !q.push(i) {
			t.Fatal("push on an open queue failed")
		}
	}
	q.close()
	if q.push(4) {
		t.Errorf("push on a closed queue succeeded")
	}
	if v, ok := q.pop(true); !ok || v != 1 {
		t.Errorf("pop(drain) = %d, %v", v, ok)
	}
	if _, ok := q.pop(false); ok {
		t.Errorf("pop(no drain) on a closed queue returned a value")
	}
	for want := 2; want <= 3; want++ {
		if v, ok := q.pop(true); !ok || v != want {
			t.Errorf("pop(drain) = %d, %v, want %d", v, ok, want)
		}
	}
	if _, ok := q.pop(true); ok {
		t.Errorf("pop on an empty closed queue returned a value")
	}
}
