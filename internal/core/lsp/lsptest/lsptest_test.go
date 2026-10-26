package lsptest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/errors"
)

func TestLoadScript(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"capabilities":{"hoverProvider":true},"crashOn":"x","responses":{"m":{"result":1,"sequence":[{"delayMs":5}]}},"send":[{"after":"initialized","method":"a/b","request":true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"capabilities":`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadScript(good)
	if err != nil {
		t.Fatal(err)
	}
	if s.CrashOn != "x" || string(s.Capabilities) != `{"hoverProvider":true}` || len(s.Responses["m"].Sequence) != 1 || !s.Send[0].Request {
		t.Errorf("script = %+v", s)
	}
	tests := []struct {
		name, path, route string
	}{
		{"missing file", filepath.Join(dir, "missing.json"), "LoadScript.ScriptReadFailed"},
		{"not JSON", bad, "LoadScript.ScriptInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadScript(tt.path)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tt.route) {
				t.Errorf("err = %v, want the route %s", err, tt.route)
			}
		})
	}
}

func TestServerOffsetCountsLikeAServerOfTheEncoding(t *testing.T) {
	const text = "a😀é\r\n€x\n\nlast"
	tests := []struct {
		enc  string
		pos  lsp.Position
		want int
		ok   bool
	}{
		{"utf-16", lsp.Position{Line: 0, Character: 3}, 5, true},  // after the emoji (2 units)
		{"utf-16", lsp.Position{Line: 0, Character: 2}, 0, false}, // inside the surrogate pair
		{"utf-16", lsp.Position{Line: 0, Character: 4}, 7, true},  // after é, before the line end
		{"utf-16", lsp.Position{Line: 0, Character: 5}, 0, false}, // past the end of the line
		{"utf-16", lsp.Position{Line: 1, Character: 1}, 12, true},
		{"utf-8", lsp.Position{Line: 0, Character: 5}, 5, true},
		{"utf-8", lsp.Position{Line: 0, Character: 2}, 0, false},
		{"utf-8", lsp.Position{Line: 1, Character: 3}, 12, true},
		{"utf-32", lsp.Position{Line: 0, Character: 2}, 5, true},
		{"utf-32", lsp.Position{Line: 0, Character: 3}, 7, true},
		{"utf-16", lsp.Position{Line: 2, Character: 0}, 14, true}, // the empty line
		{"utf-16", lsp.Position{Line: 3, Character: 4}, 19, true}, // the end of the text
		{"utf-16", lsp.Position{Line: 4, Character: 0}, 0, false}, // no such line
		{"utf-16", lsp.Position{Line: 0, Character: -1}, 0, false},
	}
	for _, tt := range tests {
		s := NewServer(Script{Capabilities: json.RawMessage(`{"positionEncoding":"` + tt.enc + `"}`)}, nil)
		got, ok := s.offset(text, tt.pos)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("%s offset(%+v) = %d, %v, want %d, %v", tt.enc, tt.pos, got, ok, tt.want, tt.ok)
		}
	}
}

func TestServerNoticesViolations(t *testing.T) {
	h := Start(t, Options{Script: Script{Capabilities: json.RawMessage(`{"textDocumentSync":2}`)}})
	c := h.Client
	notify := func(method string, params any) {
		t.Helper()
		if err := c.Notify(method, params); err != nil {
			t.Fatal(err)
		}
	}
	item := lsp.TextDocumentItem{URI: "file:///a", LanguageID: "go", Version: 5, Text: "abc"}
	notify(lsp.MethodDidChange, lsp.DidChangeTextDocumentParams{TextDocument: lsp.VersionedTextDocumentIdentifier{URI: "file:///a", Version: 6}}) // not open
	notify(lsp.MethodDidClose, lsp.DidCloseTextDocumentParams{TextDocument: lsp.TextDocumentIdentifier{URI: "file:///a"}})                        // not open
	notify(lsp.MethodDidOpen, lsp.DidOpenTextDocumentParams{TextDocument: item})
	notify(lsp.MethodDidOpen, lsp.DidOpenTextDocumentParams{TextDocument: item}) // open twice
	r := lsp.Range{Start: lsp.Position{Character: 9}, End: lsp.Position{Character: 9}}
	notify(lsp.MethodDidChange, lsp.DidChangeTextDocumentParams{
		TextDocument:   lsp.VersionedTextDocumentIdentifier{URI: "file:///a", Version: 5}, // does not go up
		ContentChanges: []lsp.TextDocumentContentChangeEvent{{Range: &r, Text: "x"}},      // outside the text
	})
	if v := h.Violations(); len(v) != 5 {
		t.Errorf("violations = %q, want 5", v)
	}
	if doc := h.Document("file:///a"); doc.Text != "abc" || doc.Version != 5 {
		t.Errorf("a change outside the text was applied: %+v", doc)
	}
	if doc := h.Document("file:///unknown"); doc.Open || doc.URI != "file:///unknown" {
		t.Errorf("unknown document = %+v", doc)
	}
}

func TestScriptedAnswersAndMessages(t *testing.T) {
	sent := make(chan lsp.LogMessageParams, 1)
	h := Start(t, Options{
		Script: Script{
			Responses: map[string]Response{
				"a": {Result: json.RawMessage(`1`)},
				"b": {Sequence: []Response{{Result: json.RawMessage(`"first"`)}, {Result: json.RawMessage(`"second"`), DelayMS: 1}}},
				"c": {Error: &lsp.ResponseError{Code: -5, Message: "no"}},
			},
			Send: []Outgoing{
				{After: "a", Method: "window/logMessage", Params: json.RawMessage(`{"type":3,"message":"after a"}`)},
				{After: "a", Method: "workspace/configuration", Params: json.RawMessage(`{"items":[]}`), Request: true},
			},
		},
		Client: lsp.ClientOptions{Handlers: lsp.Handlers{LogMessage: func(p lsp.LogMessageParams) { sent <- p }}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	var n int
	if err := h.Client.Call(ctx, "a", nil, &n); err != nil || n != 1 {
		t.Fatalf("a = %d, %v", n, err)
	}
	select {
	case p := <-sent:
		if p.Message != "after a" {
			t.Errorf("message = %+v", p)
		}
	case <-time.After(Timeout):
		t.Fatal("the scripted notification did not come")
	}
	for _, want := range []string{"first", "second", "second"} {
		var s string
		if err := h.Client.Call(ctx, "b", nil, &s); err != nil || s != want {
			t.Errorf("b = %q, %v, want %q", s, err, want)
		}
	}
	if code, _ := lsp.ErrorCode(h.Client.Call(ctx, "c", nil, nil)); code != -5 {
		t.Errorf("c code = %d", code)
	}
	// The response of the client to the scripted request was recorded.
	deadline := time.Now().Add(Timeout)
	for {
		found := false
		for _, m := range h.Traffic() {
			if m.FromClient && m.Kind == "response" && string(m.ID) == `"s1"` {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the response to the scripted request was not recorded")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCommandsStallAndCrash(t *testing.T) {
	t.Run("stall", func(t *testing.T) {
		h := Start(t, Options{Script: Script{Responses: map[string]Response{"a": {Result: json.RawMessage(`1`)}}}})
		ctx, cancel := context.WithTimeout(context.Background(), Timeout)
		defer cancel()
		if err := h.Client.Call(ctx, MethodStall, nil, nil); err != nil {
			t.Fatal(err)
		}
		short, cancelShort := context.WithTimeout(ctx, 30*time.Millisecond)
		defer cancelShort()
		err := h.Client.Call(short, "a", nil, nil)
		if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("Conn.Call.RequestCancelled") {
			t.Errorf("a stalled server answered or failed differently: %v", err)
		}
		// It still answers the questions of the test.
		if v := h.Violations(); len(v) != 0 {
			t.Errorf("violations = %v", v)
		}
	})
	t.Run("crash", func(t *testing.T) {
		h := Start(t, Options{})
		ctx, cancel := context.WithTimeout(context.Background(), Timeout)
		defer cancel()
		err := h.Client.Call(ctx, MethodCrash, nil, nil)
		if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("Conn.Call.ServerClosed") {
			t.Errorf("err = %v", err)
		}
	})
}
