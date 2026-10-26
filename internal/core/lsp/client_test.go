package lsp_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
	"github.com/rm4n0s/errors"
)

const wait = 10 * time.Second

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	t.Cleanup(cancel)
	return ctx
}

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	_ = wantRouteError(t, err, route)
}

func wantRouteError(t *testing.T, err error, route string) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("route = %s, want it to contain %s", appErr.Route(), route)
	}
	return appErr
}

func script(t *testing.T, caps string) lsptest.Script {
	t.Helper()
	s := lsptest.Script{Capabilities: json.RawMessage(caps)}
	if !json.Valid(s.Capabilities) {
		t.Fatalf("capabilities are not JSON: %s", caps)
	}
	return s
}

func codeOf(err error) int {
	code, _ := lsp.ErrorCode(err)
	return code
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

// trafficOf returns the messages the server got from the client, without the harness's own questions.
func trafficOf(h *lsptest.Harness) []lsptest.Message {
	var out []lsptest.Message
	for _, m := range h.Traffic() {
		if !m.FromClient || strings.HasPrefix(m.Method, "$/lsptest/") {
			continue
		}
		out = append(out, m)
	}
	return out
}

func methodsOf(msgs []lsptest.Message, fromClient bool) []string {
	var out []string
	for _, m := range msgs {
		if m.FromClient == fromClient && m.Kind != "response" {
			out = append(out, m.Method)
		}
	}
	return out
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestInitializeHandshake(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{
		Script: lsptest.Script{
			Capabilities: raw(`{"positionEncoding":"utf-8","hoverProvider":true,"textDocumentSync":2}`),
			ServerInfo:   &lsp.ServerInfo{Name: "fake", Version: "1.2"},
		},
		Client: lsp.ClientOptions{
			Root: "/home/me/my project", Version: "9.9", Locale: "el",
			InitializationOptions: raw(`{"staticcheck":true}`),
		},
	})
	c := h.Client
	if enc := c.Encoding(); enc != lsp.UTF8 {
		t.Errorf("Encoding = %v, want utf-8", enc)
	}
	if !c.Capabilities().HoverProvider.Enabled || c.Capabilities().TextDocumentSync.Change != lsp.SyncIncremental {
		t.Errorf("Capabilities = %+v", c.Capabilities())
	}
	if info := c.ServerInfo(); info == nil || info.Name != "fake" || info.Version != "1.2" {
		t.Errorf("ServerInfo = %+v", info)
	}

	msgs := trafficOf(h)
	if got := methodsOf(msgs, true); len(got) != 2 || got[0] != "initialize" || got[1] != "initialized" {
		t.Fatalf("the client sent %v, want initialize then initialized", got)
	}
	var req struct {
		Params struct {
			ProcessID  int `json:"processId"`
			ClientInfo struct {
				Name, Version string
			} `json:"clientInfo"`
			Locale                string          `json:"locale"`
			RootURI               string          `json:"rootUri"`
			InitializationOptions json.RawMessage `json:"initializationOptions"`
			Trace                 string          `json:"trace"`
			WorkspaceFolders      []lsp.WorkspaceFolder
			Capabilities          map[string]json.RawMessage `json:"capabilities"`
		} `json:"params"`
	}
	if err := json.Unmarshal(msgs[0].Body, &req); err != nil {
		t.Fatal(err)
	}
	p := req.Params
	if p.ProcessID != os.Getpid() || p.ClientInfo.Name != "Emetos" || p.ClientInfo.Version != "9.9" || p.Locale != "el" || p.Trace != "off" {
		t.Errorf("params = %+v", p)
	}
	if p.RootURI != "file:///home/me/my%20project" {
		t.Errorf("rootUri = %q", p.RootURI)
	}
	if len(p.WorkspaceFolders) != 1 || p.WorkspaceFolders[0].Name != "my project" || p.WorkspaceFolders[0].URI != p.RootURI {
		t.Errorf("workspaceFolders = %+v", p.WorkspaceFolders)
	}
	if string(p.InitializationOptions) != `{"staticcheck":true}` {
		t.Errorf("initializationOptions = %s", p.InitializationOptions)
	}
	for _, key := range []string{"general", "window", "workspace", "textDocument"} {
		if _, ok := p.Capabilities[key]; !ok {
			t.Errorf("capabilities have no %q", key)
		}
	}
	if !strings.Contains(string(p.Capabilities["general"]), `"positionEncodings":["utf-8","utf-16"]`) {
		t.Errorf("general = %s", p.Capabilities["general"])
	}
	if string(msgs[1].Body) != `{"jsonrpc":"2.0","method":"initialized","params":{}}` {
		t.Errorf("initialized = %s", msgs[1].Body)
	}
}

func TestInitializeWithoutAFolder(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{})
	var req struct {
		Params map[string]json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(trafficOf(h)[0].Body, &req); err != nil {
		t.Fatal(err)
	}
	if string(req.Params["rootUri"]) != "null" || string(req.Params["workspaceFolders"]) != "null" {
		t.Errorf("rootUri = %s, workspaceFolders = %s, want null", req.Params["rootUri"], req.Params["workspaceFolders"])
	}
	if enc := h.Client.Encoding(); enc != lsp.UTF16 {
		t.Errorf("a server that names no encoding uses utf-16, got %v", enc)
	}
}

func TestPositionEncodingNegotiation(t *testing.T) {
	tests := []struct {
		caps string
		want lsp.Encoding
	}{
		{`{}`, lsp.UTF16},
		{`{"positionEncoding":"utf-16"}`, lsp.UTF16},
		{`{"positionEncoding":"utf-8"}`, lsp.UTF8},
		{`{"positionEncoding":"utf-32"}`, lsp.UTF32},
	}
	for _, tt := range tests {
		t.Run(tt.caps, func(t *testing.T) {
			h := lsptest.Start(t, lsptest.Options{Script: script(t, tt.caps)})
			if h.Client.Encoding() != tt.want {
				t.Errorf("Encoding = %v, want %v", h.Client.Encoding(), tt.want)
			}
		})
	}
}

func TestInitializeFailures(t *testing.T) {
	tests := []struct {
		name    string
		script  lsptest.Script
		timeout time.Duration
		route   string
	}{
		{
			name: "the server answers with an error",
			script: lsptest.Script{Responses: map[string]lsptest.Response{
				"initialize": {Error: &lsp.ResponseError{Code: lsp.CodeServerNotInitialized, Message: "not ready"}},
			}},
			route: "Conn.Call.ServerError",
		},
		{
			name:   "an encoding the client does not know",
			script: lsptest.Script{Capabilities: raw(`{"positionEncoding":"utf-7"}`)},
			route:  "NewClient.PositionEncodingUnsupported",
		},
		{
			name:   "the server dies",
			script: lsptest.Script{CrashOn: "initialize"},
			route:  "Conn.Call.ServerClosed",
		},
		{
			name:    "the server does not answer",
			script:  lsptest.Script{StallOn: "initialize"},
			timeout: 50 * time.Millisecond,
			route:   "Conn.Call.RequestCancelled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rwc, _ := lsptest.Connect(t, tt.script)
			timeout := tt.timeout
			if timeout == 0 {
				timeout = wait
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			c, err := lsp.NewClient(ctx, rwc, lsp.ClientOptions{})
			if err == nil {
				t.Fatalf("NewClient succeeded: %+v", c)
			}
			wantRoute(t, err, tt.route)
			if c != nil {
				t.Errorf("a failed NewClient returned a client")
			}
			// The transport was closed: reading it ends.
			done := make(chan error, 1)
			go func() { _, err := rwc.Read(make([]byte, 1)); done <- err }()
			select {
			case err := <-done:
				if err == nil {
					t.Errorf("the transport still gives data")
				}
			case <-time.After(wait):
				t.Errorf("the transport was left open")
			}
		})
	}
}

func TestShutdown(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{})
	if err := h.Client.Shutdown(ctxT(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Client.Done():
	case <-time.After(wait):
		t.Fatal("the client is not done after Shutdown")
	}
	if err := h.Client.Err(); err != nil {
		t.Errorf("Err after a Shutdown = %v, want nil", err)
	}
	eventually(t, "the exit notification", func() bool { return h.Server.Count("exit") == 1 })
	if !h.Server.Shutdown() {
		t.Errorf("the server did not get shutdown")
	}
	got := methodsOf(h.Server.Traffic(), true)
	want := []string{"initialize", "initialized", "shutdown", "exit"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the client sent %v, want %v", got, want)
	}
	// Calls after the end fail with a tag.
	wantRoute(t, h.Client.Call(ctxT(t), "test/x", nil, nil), "Conn.Call.ServerClosed")
	// Shutting down again reports that the server is gone.
	wantRoute(t, h.Client.Shutdown(ctxT(t)), "Conn.Call.ServerClosed")
}

func TestShutdownOfAServerThatDoesNotAnswer(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: lsptest.Script{StallOn: "shutdown"}})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := h.Client.Shutdown(ctx)
	wantRoute(t, err, "Conn.Call.RequestCancelled")
	select {
	case <-h.Client.Done():
	case <-time.After(wait):
		t.Fatal("the connection stays open after a failed Shutdown")
	}
}

func TestShutdownOfAServerThatDied(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{})
	if err := h.Client.Notify(lsptest.MethodCrash, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Client.Done():
	case <-time.After(wait):
		t.Fatal("the death of the server was not noticed")
	}
	wantRoute(t, h.Client.Shutdown(ctxT(t)), "Conn.Call.ServerClosed")
}

func TestRequestHelperAndErrorCodes(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: lsptest.Script{Responses: map[string]lsptest.Response{
		"textDocument/hover":      {Result: raw(`{"contents":{"kind":"markdown","value":"hi"}}`)},
		"textDocument/definition": {Result: raw(`null`)},
		"textDocument/completion": {Error: &lsp.ResponseError{Code: lsp.CodeContentModified, Message: "content modified"}},
		"textDocument/references": {Sequence: []lsptest.Response{
			{Error: &lsp.ResponseError{Code: lsp.CodeServerCancelled, Message: "busy"}},
			{Result: raw(`[{"uri":"file:///a.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}}]`)},
		}},
	}}})
	ctx := ctxT(t)
	pos := lsp.TextDocumentPositionParams{TextDocument: lsp.TextDocumentIdentifier{URI: "file:///a.go"}}

	hover, err := lsp.Request[*lsp.Hover](ctx, h.Client, lsp.MethodHover, pos)
	if err != nil || hover == nil || hover.Contents[0].Value != "hi" {
		t.Errorf("hover = %+v, %v", hover, err)
	}
	def, err := lsp.Request[lsp.LocationLinks](ctx, h.Client, lsp.MethodDefinition, pos)
	if err != nil || len(def) != 0 {
		t.Errorf("definition = %+v, %v", def, err)
	}
	_, err = lsp.Request[lsp.CompletionList](ctx, h.Client, lsp.MethodCompletion, pos)
	wantRoute(t, err, "Conn.Call.ServerError")
	if code, ok := lsp.ErrorCode(err); !ok || code != lsp.CodeContentModified {
		t.Errorf("ErrorCode = %d, %v", code, ok)
	}
	if _, ok := lsp.ErrorCode(io.EOF); ok {
		t.Errorf("ErrorCode of a foreign error")
	}
	if _, ok := lsp.ErrorCode(errors.New("Other", "other")); ok {
		t.Errorf("ErrorCode of another tag")
	}
	_, err = lsp.Request[[]lsp.Location](ctx, h.Client, lsp.MethodReferences, pos)
	if code, _ := lsp.ErrorCode(err); code != lsp.CodeServerCancelled {
		t.Errorf("first references = %v", err)
	}
	locs, err := lsp.Request[[]lsp.Location](ctx, h.Client, lsp.MethodReferences, pos)
	if err != nil || len(locs) != 1 {
		t.Errorf("second references = %+v, %v", locs, err)
	}
	// A method the script has no answer for.
	_, err = lsp.Request[*lsp.Hover](ctx, h.Client, "textDocument/unknown", pos)
	if code, _ := lsp.ErrorCode(err); code != lsp.CodeMethodNotFound {
		t.Errorf("unknown method = %v", err)
	}
	// Shapes that do not fit give the tag of the type.
	h2 := lsptest.Start(t, lsptest.Options{Script: lsptest.Script{Responses: map[string]lsptest.Response{
		"textDocument/hover": {Result: raw(`{"contents":5}`)},
	}}})
	_, err = lsp.Request[*lsp.Hover](ctx, h2.Client, lsp.MethodHover, pos)
	wantRoute(t, err, "Hover.UnmarshalJSON.HoverInvalid")
}

func TestCancellationSendsCancelRequestToTheServer(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: lsptest.Script{StallOn: "textDocument/hover"}})
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := lsp.Request[*lsp.Hover](ctx, h.Client, lsp.MethodHover, lsp.TextDocumentPositionParams{})
		errc <- err
	}()
	eventually(t, "the hover request", func() bool { return h.Server.Count("textDocument/hover") == 1 })
	cancel()
	select {
	case err := <-errc:
		wantRoute(t, err, "Conn.Call.RequestCancelled")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
	case <-time.After(wait):
		t.Fatal("the call did not return")
	}
	eventually(t, "$/cancelRequest", func() bool { return h.Server.Count("$/cancelRequest") == 1 })
	var hoverID string
	for _, m := range h.Server.Traffic() {
		if m.Method == "textDocument/hover" {
			hoverID = m.ID
		}
		if m.Method == "$/cancelRequest" {
			if want := `{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":` + hoverID + `}}`; string(m.Body) != want {
				t.Errorf("cancel = %s, want %s", m.Body, want)
			}
		}
	}
}

func TestServerDiesMidRequest(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: lsptest.Script{CrashOn: "textDocument/hover"}})
	_, err := lsp.Request[*lsp.Hover](ctxT(t), h.Client, lsp.MethodHover, lsp.TextDocumentPositionParams{})
	appErr := wantRouteError(t, err, "Conn.Call.ServerClosed")
	if appErr.Metadata["method"] != "textDocument/hover" {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	select {
	case <-h.Client.Done():
	case <-time.After(wait):
		t.Fatal("Done is not closed")
	}
	if !errors.Is(h.Client.Err(), io.EOF) {
		t.Errorf("Err = %v, want io.EOF", h.Client.Err())
	}
}

func TestMessageOverTheLimit(t *testing.T) {
	big := `"` + strings.Repeat("x", 5000) + `"`
	h := lsptest.Start(t, lsptest.Options{
		Script: lsptest.Script{Responses: map[string]lsptest.Response{"textDocument/hover": {Result: raw(big)}}},
		Client: lsp.ClientOptions{MaxMessageSize: 4096},
	})
	var res string
	err := h.Client.Call(ctxT(t), lsp.MethodHover, nil, &res)
	wantRoute(t, err, "Conn.Call.ServerClosed")
	wantRoute(t, h.Client.Err(), "ReadFrame.FrameTooLarge")
}

func TestServerRequestsReachTheHandlers(t *testing.T) {
	var mu sync.Mutex
	var gotConfig lsp.ConfigurationParams
	var gotEdit lsp.ApplyWorkspaceEditParams
	var gotMsgReq lsp.ShowMessageRequestParams
	var token lsp.ProgressToken
	var refreshed []string
	var changed int
	h := lsptest.Start(t, lsptest.Options{Client: lsp.ClientOptions{
		Root: "/work",
		Handlers: lsp.Handlers{
			Configuration: func(_ context.Context, p lsp.ConfigurationParams) ([]json.RawMessage, error) {
				mu.Lock()
				gotConfig = p
				mu.Unlock()
				return []json.RawMessage{raw(`{"a":1}`), nil}, nil
			},
			ApplyEdit: func(_ context.Context, p lsp.ApplyWorkspaceEditParams) (lsp.ApplyWorkspaceEditResult, error) {
				mu.Lock()
				gotEdit = p
				mu.Unlock()
				return lsp.ApplyWorkspaceEditResult{Applied: true}, nil
			},
			ShowMessageRequest: func(_ context.Context, p lsp.ShowMessageRequestParams) (*lsp.MessageActionItem, error) {
				mu.Lock()
				gotMsgReq = p
				mu.Unlock()
				return &lsp.MessageActionItem{Title: p.Actions[1].Title}, nil
			},
			CreateProgress: func(_ context.Context, tk lsp.ProgressToken) error {
				mu.Lock()
				token = tk
				mu.Unlock()
				return nil
			},
			Refresh: func(_ context.Context, method string) error {
				mu.Lock()
				refreshed = append(refreshed, method)
				mu.Unlock()
				return nil
			},
			RegistrationsChanged: func() {
				mu.Lock()
				changed++
				mu.Unlock()
			},
		},
	}})
	srv := h.Server
	ask := func(method, params string) json.RawMessage {
		t.Helper()
		select {
		case resp := <-srv.SendRequest(method, raw(params)):
			return resp
		case <-time.After(wait):
			t.Fatalf("%s: no response", method)
			return nil
		}
	}
	result := func(resp json.RawMessage) string {
		t.Helper()
		var m struct {
			Result json.RawMessage `json:"result"`
			Error  *lsp.ResponseError
		}
		if err := json.Unmarshal(resp, &m); err != nil {
			t.Fatal(err)
		}
		if m.Error != nil {
			return "error " + m.Error.Message + " " + string(rune('0'+(-m.Error.Code)%10))
		}
		return string(m.Result)
	}

	if got := result(ask("workspace/configuration", `{"items":[{"section":"gopls","scopeUri":"file:///work"},{"section":"x"}]}`)); got != `[{"a":1},null]` {
		t.Errorf("configuration = %s", got)
	}
	mu.Lock()
	if len(gotConfig.Items) != 2 || gotConfig.Items[0].Section != "gopls" || gotConfig.Items[0].ScopeURI != "file:///work" {
		t.Errorf("configuration params = %+v", gotConfig)
	}
	mu.Unlock()

	if got := result(ask("workspace/applyEdit", `{"label":"Rename","edit":{"changes":{"file:///a.go":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"x"}]}}}`)); got != `{"applied":true}` {
		t.Errorf("applyEdit = %s", got)
	}
	mu.Lock()
	if gotEdit.Label != "Rename" || len(gotEdit.Edit.Changes["file:///a.go"]) != 1 {
		t.Errorf("applyEdit params = %+v", gotEdit)
	}
	mu.Unlock()

	if got := result(ask("window/showMessageRequest", `{"type":2,"message":"Install?","actions":[{"title":"No"},{"title":"Yes"}]}`)); got != `{"title":"Yes"}` {
		t.Errorf("showMessageRequest = %s", got)
	}
	mu.Lock()
	if gotMsgReq.Message != "Install?" || gotMsgReq.Type != lsp.MessageTypeWarning {
		t.Errorf("showMessageRequest params = %+v", gotMsgReq)
	}
	mu.Unlock()

	if got := result(ask("window/workDoneProgress/create", `{"token":"gopls-1"}`)); got != `null` {
		t.Errorf("workDoneProgress/create = %s", got)
	}
	mu.Lock()
	if token.Key() != `"gopls-1"` {
		t.Errorf("progress token = %s", token.Key())
	}
	mu.Unlock()

	for _, m := range []string{"workspace/semanticTokens/refresh", "workspace/codeLens/refresh", "workspace/inlayHint/refresh", "workspace/diagnostic/refresh"} {
		if got := result(ask(m, ``)); got != `null` {
			t.Errorf("%s = %s", m, got)
		}
	}
	mu.Lock()
	if len(refreshed) != 4 || refreshed[0] != "workspace/semanticTokens/refresh" {
		t.Errorf("refreshed = %v", refreshed)
	}
	mu.Unlock()

	// Dynamic registration.
	if got := result(ask("client/registerCapability", `{"registrations":[{"id":"w1","method":"workspace/didChangeWatchedFiles","registerOptions":{"watchers":[{"globPattern":"**/*.go"}]}},{"id":"f1","method":"textDocument/formatting"}]}`)); got != `null` {
		t.Errorf("registerCapability = %s", got)
	}
	reg := h.Client.Registered(lsp.MethodDidChangeWatchedFiles)
	if len(reg) != 1 || reg[0].ID != "w1" {
		t.Fatalf("Registered = %+v", reg)
	}
	var opts lsp.DidChangeWatchedFilesRegistrationOptions
	if err := json.Unmarshal(reg[0].RegisterOptions, &opts); err != nil || len(opts.Watchers) != 1 || string(opts.Watchers[0].GlobPattern) != `"**/*.go"` {
		t.Errorf("options = %+v, %v", opts, err)
	}
	if n := len(h.Client.Registrations()); n != 2 {
		t.Errorf("Registrations = %d", n)
	}
	if got := result(ask("client/unregisterCapability", `{"unregisterations":[{"id":"w1","method":"workspace/didChangeWatchedFiles"}]}`)); got != `null` {
		t.Errorf("unregisterCapability = %s", got)
	}
	if n := len(h.Client.Registrations()); n != 1 || len(h.Client.Registered(lsp.MethodDidChangeWatchedFiles)) != 0 {
		t.Errorf("after unregister: %+v", h.Client.Registrations())
	}
	mu.Lock()
	if changed != 2 {
		t.Errorf("RegistrationsChanged called %d times, want 2", changed)
	}
	mu.Unlock()

	// Requests the client answers by itself.
	if got := result(ask("workspace/workspaceFolders", ``)); got != `[{"uri":"file:///work","name":"work"}]` {
		t.Errorf("workspaceFolders = %s", got)
	}
	if got := result(ask("window/showDocument", `{"uri":"file:///a.go"}`)); got != `{"success":false}` {
		t.Errorf("showDocument = %s", got)
	}

	// What the client does not handle, and params it cannot read.
	if got := result(ask("custom/unknown", `{}`)); !strings.HasPrefix(got, "error") || !strings.Contains(got, "does not handle") {
		t.Errorf("unknown request = %s", got)
	}
	resp := ask("workspace/configuration", `"not an object"`)
	var m struct{ Error *lsp.ResponseError }
	if err := json.Unmarshal(resp, &m); err != nil || m.Error == nil || m.Error.Code != lsp.CodeInvalidParams {
		t.Errorf("bad params = %s", resp)
	}
}

func TestServerRequestsWithoutHandlers(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{})
	ask := func(method, params string) string {
		t.Helper()
		select {
		case resp := <-h.Server.SendRequest(method, raw(params)):
			var m struct {
				Result json.RawMessage `json:"result"`
				Error  *lsp.ResponseError
			}
			if err := json.Unmarshal(resp, &m); err != nil {
				t.Fatal(err)
			}
			if m.Error != nil {
				t.Fatalf("%s failed: %+v", method, m.Error)
			}
			return string(m.Result)
		case <-time.After(wait):
			t.Fatalf("%s: no response", method)
			return ""
		}
	}
	tests := []struct {
		method, params, want string
	}{
		{"workspace/configuration", `{"items":[{"section":"a"},{"section":"b"}]}`, `[null,null]`},
		{"workspace/applyEdit", `{"edit":{}}`, `{"applied":false,"failureReason":"the client cannot apply edits"}`},
		{"window/showMessageRequest", `{"type":3,"message":"m","actions":[{"title":"A"}]}`, `null`},
		{"window/workDoneProgress/create", `{"token":1}`, `null`},
		{"workspace/inlayHint/refresh", ``, `null`},
		{"workspace/workspaceFolders", ``, `null`},
	}
	for _, tt := range tests {
		if got := ask(tt.method, tt.params); got != tt.want {
			t.Errorf("%s = %s, want %s", tt.method, got, tt.want)
		}
	}
}

func TestServerNotificationsReachTheHandlers(t *testing.T) {
	var mu sync.Mutex
	var order []string
	var logs []lsp.LogMessageParams
	var shows []lsp.ShowMessageParams
	var progress []lsp.ProgressParams
	var diags []lsp.PublishDiagnosticsParams
	var others []string
	h := lsptest.Start(t, lsptest.Options{Client: lsp.ClientOptions{Handlers: lsp.Handlers{
		LogMessage: func(p lsp.LogMessageParams) {
			mu.Lock()
			logs, order = append(logs, p), append(order, "log")
			mu.Unlock()
		},
		ShowMessage: func(p lsp.ShowMessageParams) {
			mu.Lock()
			shows, order = append(shows, p), append(order, "show")
			mu.Unlock()
		},
		Progress: func(p lsp.ProgressParams) {
			mu.Lock()
			progress, order = append(progress, p), append(order, "progress")
			mu.Unlock()
		},
		PublishDiagnostics: func(p lsp.PublishDiagnosticsParams) {
			mu.Lock()
			diags, order = append(diags, p), append(order, "diag")
			mu.Unlock()
		},
		Notification: func(method string, _ json.RawMessage) {
			mu.Lock()
			others, order = append(others, method), append(order, "other")
			mu.Unlock()
		},
	}}})
	s := h.Server
	s.Send("window/logMessage", raw(`{"type":3,"message":"starting"}`))
	s.Send("window/showMessage", raw(`{"type":1,"message":"broken"}`))
	s.Send("$/progress", raw(`{"token":"t","value":{"kind":"begin","title":"Indexing","percentage":5}}`))
	s.Send("$/progress", raw(`{"token":"t","value":{"kind":"end"}}`))
	s.Send("textDocument/publishDiagnostics", raw(`{"uri":"file:///a.go","version":4,"diagnostics":[{"range":{"start":{"line":1,"character":2},"end":{"line":1,"character":5}},"severity":1,"code":"E1","message":"bad"}]}`))
	s.Send("telemetry/event", raw(`{"x":1}`))
	s.Send("window/logMessage", raw(`[1,2]`)) // params that cannot be read are dropped
	s.Send("window/logMessage", raw(`{"type":4,"message":"last"}`))
	eventually(t, "the notifications", func() bool { mu.Lock(); defer mu.Unlock(); return len(order) >= 7 })
	mu.Lock()
	defer mu.Unlock()
	if want := "log,show,progress,progress,diag,other,log"; strings.Join(order, ",") != want {
		t.Errorf("order = %v, want %s", order, want)
	}
	if len(logs) != 2 || logs[0].Message != "starting" || logs[1].Message != "last" {
		t.Errorf("logs = %+v", logs)
	}
	if len(shows) != 1 || shows[0].Type != lsp.MessageTypeError {
		t.Errorf("shows = %+v", shows)
	}
	if w, ok := progress[0].WorkDone(); !ok || w.Title != "Indexing" || *w.Percentage != 5 {
		t.Errorf("progress = %+v", progress)
	}
	if len(diags) != 1 || *diags[0].Version != 4 || diags[0].Diagnostics[0].CodeText() != "E1" || diags[0].Diagnostics[0].Range.Start.Character != 2 {
		t.Errorf("diagnostics = %+v", diags)
	}
	if len(others) != 1 || others[0] != "telemetry/event" {
		t.Errorf("others = %v", others)
	}
}

func TestNotificationsWithoutHandlersAreDropped(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{})
	h.Server.Send("window/logMessage", raw(`{"type":3,"message":"x"}`))
	h.Server.Send("telemetry/event", raw(`{}`))
	// The client is alive and in order: a call after them works.
	if _, err := lsp.Request[*lsp.Hover](ctxT(t), h.Client, "textDocument/hover", nil); codeOf(err) != lsp.CodeMethodNotFound {
		t.Errorf("err = %v", err)
	}
}

func TestTraceOption(t *testing.T) {
	var mu sync.Mutex
	var got []lsp.TraceMessage
	h := lsptest.Start(t, lsptest.Options{Client: lsp.ClientOptions{Trace: func(m lsp.TraceMessage) {
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
	}}})
	_, _ = lsp.Request[*lsp.Hover](ctxT(t), h.Client, lsp.MethodHover, nil)
	mu.Lock()
	defer mu.Unlock()
	var seq []string
	for _, m := range got {
		dir := "->"
		if m.Direction == lsp.FromServer {
			dir = "<-"
		}
		seq = append(seq, dir+m.Method)
	}
	want := []string{"->initialize", "<-initialize", "->initialized", "->textDocument/hover", "<-textDocument/hover"}
	if strings.Join(seq, " ") != strings.Join(want, " ") {
		t.Errorf("trace = %v, want %v", seq, want)
	}
}

func TestClientIsSafeForConcurrentUse(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: lsptest.Script{Responses: map[string]lsptest.Response{
		"textDocument/hover": {Result: raw(`{"contents":"x"}`), DelayMS: 1},
	}}})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if _, err := lsp.Request[*lsp.Hover](ctxT(t), h.Client, lsp.MethodHover, nil); err != nil {
					t.Errorf("hover: %v", err)
					return
				}
				if err := h.Client.Notify("$/test", nil); err != nil {
					t.Errorf("notify: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if n := h.Server.Count("textDocument/hover"); n != 200 {
		t.Errorf("the server got %d requests, want 200", n)
	}
}

func TestProcessExitsAfterShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the fake server program")
	}
	h := lsptest.StartProcess(t, lsptest.Options{})
	pid := h.Pid()
	if err := h.Client.Shutdown(ctxT(t)); err != nil {
		t.Fatal(err)
	}
	if code := h.WaitExit(); code != 0 {
		t.Errorf("exit status = %d, want 0\nstderr: %s", code, h.Stderr())
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("the process %d is still there: %v", pid, err)
	}
}

func TestProcessOverPipes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the fake server program")
	}
	prog := lsptest.BuildProgram(t)
	t.Run("requests and server requests", func(t *testing.T) {
		got := make(chan lsp.LogMessageParams, 1)
		h := lsptest.StartProcess(t, lsptest.Options{
			Program: prog,
			Script: lsptest.Script{
				Capabilities: raw(`{"hoverProvider":true}`),
				Responses:    map[string]lsptest.Response{"textDocument/hover": {Result: raw(`{"contents":"from a process"}`)}},
				Send:         []lsptest.Outgoing{{After: "initialized", Method: "window/logMessage", Params: raw(`{"type":3,"message":"hello"}`)}},
			},
			Client: lsp.ClientOptions{Handlers: lsp.Handlers{LogMessage: func(p lsp.LogMessageParams) { got <- p }}},
		})
		hover, err := lsp.Request[*lsp.Hover](ctxT(t), h.Client, lsp.MethodHover, nil)
		if err != nil || hover.Contents[0].Value != "from a process" {
			t.Errorf("hover = %+v, %v", hover, err)
		}
		select {
		case p := <-got:
			if p.Message != "hello" {
				t.Errorf("log = %+v", p)
			}
		case <-time.After(wait):
			t.Fatal("the server's notification did not arrive")
		}
	})
	t.Run("the server crashes mid-request", func(t *testing.T) {
		h := lsptest.StartProcess(t, lsptest.Options{Program: prog, Script: lsptest.Script{CrashOn: "textDocument/hover"}})
		_, err := lsp.Request[*lsp.Hover](ctxT(t), h.Client, lsp.MethodHover, nil)
		wantRoute(t, err, "Conn.Call.ServerClosed")
		if code := h.WaitExit(); code != 3 {
			t.Errorf("exit status = %d, want 3", code)
		}
		select {
		case <-h.Client.Done():
		case <-time.After(wait):
			t.Fatal("Done is not closed")
		}
	})
	t.Run("the server stalls and the request is cancelled", func(t *testing.T) {
		h := lsptest.StartProcess(t, lsptest.Options{Program: prog, Script: lsptest.Script{StallOn: "textDocument/hover"}})
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := lsp.Request[*lsp.Hover](ctx, h.Client, lsp.MethodHover, nil)
		wantRoute(t, err, "Conn.Call.RequestCancelled")
		eventually(t, "$/cancelRequest at the process", func() bool {
			for _, m := range h.Traffic() {
				if m.Method == "$/cancelRequest" {
					return true
				}
			}
			return false
		})
	})
	t.Run("the client closes the stream without exit", func(t *testing.T) {
		h := lsptest.StartProcess(t, lsptest.Options{Program: prog})
		if err := h.Client.Close(); err != nil {
			t.Fatal(err)
		}
		if code := h.WaitExit(); code != 0 {
			t.Errorf("exit status = %d, want 0\nstderr: %s", code, h.Stderr())
		}
	})
}
