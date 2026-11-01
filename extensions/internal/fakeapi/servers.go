package fakeapi

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// serverState is a language server of the manifest as the fake keeps it.
type serverState struct {
	status   extension.ServerStatus
	launcher extension.ServerLauncher
	subs     []*serverSub
	restarts int
	shown    int
	// requests are the requests of the extension in their own protocol, handler answers them, notifs are the listeners of the
	// notifications and encoding the position encoding the fake reports ("" is utf-16).
	requests []ServerRequest
	handler  func(ServerRequest) (json.RawMessage, error)
	// notifyErr is what Notify fails with, when set (FailNotifications)
	notifyErr error
	notifs    []*notifSub
	encoding  string
}

type notifSub struct {
	method string
	fn     func(json.RawMessage)
}

// ServerRequest is a request the extension sent to a server with LanguageServers().Request.
type ServerRequest struct {
	Server, Method string
	// Params are the params of the request as JSON.
	Params json.RawMessage
	// Notification is true for what the extension sent with LanguageServers().Notify.
	Notification bool
}

type serverSub struct{ fn func(extension.ServerStatus) }

type serversAPI struct{ a *API }

// LanguageServers implements extension.API. The servers are those of the manifest; each is idle until SetServerStatus.
func (a *API) LanguageServers() extension.LanguageServers { return serversAPI{a} }

// server returns the state of the server, making it (idle) the first time. The caller holds a.mu.
func (a *API) server(id string) (*serverState, error) {
	if a.servers == nil {
		a.servers = map[string]*serverState{}
	}
	if st, ok := a.servers[id]; ok {
		return st, nil
	}
	for _, spec := range a.manifest.Contributes.LanguageServers {
		if spec.ID == id {
			st := &serverState{status: extension.ServerStatus{State: extension.ServerIdle}}
			a.servers[id] = st
			return st, nil
		}
	}
	return nil, errors.New("FakeServerUnknown", "the manifest declares no language server with this id", "server", id)
}

func (s serversAPI) Status(id string) (extension.ServerStatus, error) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	st, err := s.a.server(id)
	if err != nil {
		return extension.ServerStatus{}, err
	}
	return st.status, nil
}

func (s serversAPI) OnChange(id string, fn func(extension.ServerStatus)) (func(), error) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	st, err := s.a.server(id)
	if err != nil {
		return nil, err
	}
	sub := &serverSub{fn: fn}
	st.subs = append(st.subs, sub)
	return func() {
		s.a.mu.Lock()
		defer s.a.mu.Unlock()
		st.subs = slices.DeleteFunc(st.subs, func(x *serverSub) bool { return x == sub })
	}, nil
}

func (s serversAPI) Restart(id string) error {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	st, err := s.a.server(id)
	if err != nil {
		return err
	}
	st.restarts++
	return nil
}

func (s serversAPI) ShowOutput(id string, _ bool) error {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	st, err := s.a.server(id)
	if err != nil {
		return err
	}
	st.shown++
	return nil
}

// SetLauncher implements extension.LanguageServers: the launcher is kept for the test (Launcher, Launch); the fake starts no
// server.
func (s serversAPI) SetLauncher(id string, launcher extension.ServerLauncher) error {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	st, err := s.a.server(id)
	if err != nil {
		return err
	}
	st.launcher = launcher
	return nil
}

// Launcher is the launcher the extension gave the server, or nil.
func (a *API) Launcher(id string) extension.ServerLauncher {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, err := a.server(id); err == nil {
		return st.launcher
	}
	return nil
}

// Launch runs the launcher of the server as the host does at a start: with the folder of the scope and the settings of the fake
// (the values a test set and the defaults of the settings the manifest declares), from the calling goroutine. It fails the test when
// the extension set no launcher; the answer and the error are the launcher's.
func (a *API) Launch(ctx context.Context, id string) (extension.ServerLaunch, error) {
	a.t.Helper()
	launcher := a.Launcher(id)
	if launcher == nil {
		a.t.Fatalf("the extension set no launcher for the server %s", id)
		return extension.ServerLaunch{}, nil
	}
	return launcher(ctx, extension.LaunchRequest{Folder: a.info.Folder, Setting: a.launchSetting})
}

// launchSetting is LaunchRequest.Setting of the fake.
func (a *API) launchSetting(key string) (any, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	spec, ok := a.specs[key]
	if !ok {
		return nil, false
	}
	if v, ok := a.values[key]; ok {
		return v, true
	}
	return roundTrip(spec.Default), true
}

// SetServerStatus changes the status of a server of the manifest and calls its listeners, on the test's goroutine, as the host
// does on the UI thread.
func (a *API) SetServerStatus(id string, status extension.ServerStatus) {
	a.mu.Lock()
	st, err := a.server(id)
	if err != nil {
		a.mu.Unlock()
		a.t.Fatal(err)
		return
	}
	st.status = status
	subs := slices.Clone(st.subs)
	a.mu.Unlock()
	for _, sub := range subs {
		sub.fn(status)
	}
}

// ServerRestarts is how many times the extension restarted the server.
func (a *API) ServerRestarts(id string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, err := a.server(id); err == nil {
		return st.restarts
	}
	return 0
}

// ServerOutputShown is how many times the extension showed the Output channel of the server.
func (a *API) ServerOutputShown(id string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, err := a.server(id); err == nil {
		return st.shown
	}
	return 0
}

// ---- QuickPick ----

// RanCommand is a command a quick pick item ran, with its arguments.
type RanCommand struct {
	ID   string
	Args any
}

// Pick is a quick pick the extension showed.
type Pick struct {
	a    *API
	opts extension.QuickPickOptions
	done func(int)
	mu   sync.Mutex
	over bool
}

// Options are what the extension asked to show.
func (p *Pick) Options() extension.QuickPickOptions { return p.opts }

// Labels are the labels of the items.
func (p *Pick) Labels() []string {
	var out []string
	for _, it := range p.opts.Items {
		out = append(out, it.Label)
	}
	return out
}

// Choose chooses the item: its command is recorded (see RanCommands) and the extension is told the index. The text typed in the filter
// is empty; ChooseTyped gives one.
func (p *Pick) Choose(index int) {
	p.a.t.Helper()
	p.ChooseTyped(index, "")
}

// ChooseTyped chooses the item with the text typed in the filter (the argument of QuickPickOptions.OnAccept). A separator cannot be
// chosen: it fails the test.
func (p *Pick) ChooseTyped(index int, typed string) {
	p.a.t.Helper()
	if p.opts.Items[index].Separator {
		p.a.t.Fatalf("the item %d of the quick pick is a separator", index)
		return
	}
	p.mu.Lock()
	if p.over {
		p.mu.Unlock()
		p.a.t.Fatal("the quick pick is over already")
		return
	}
	p.over = true
	p.mu.Unlock()
	it := p.opts.Items[index]
	if p.opts.OnAccept != nil {
		p.opts.OnAccept(typed)
	}
	if it.Command != "" {
		p.a.mu.Lock()
		p.a.ranCommands = append(p.a.ranCommands, RanCommand{ID: it.Command, Args: it.Args})
		p.a.mu.Unlock()
	}
	if p.done != nil {
		p.done(index)
	}
}

// Close closes the quick pick with no choice.
func (p *Pick) Close() {
	p.mu.Lock()
	over := p.over
	p.over = true
	p.mu.Unlock()
	if !over && p.done != nil {
		p.done(-1)
	}
}

type quickPick struct{ a *API }

// QuickPick implements extension.API.
func (a *API) QuickPick() extension.QuickPick { return quickPick{a} }

func (q quickPick) Show(opts extension.QuickPickOptions, done func(int)) error {
	if !slices.ContainsFunc(opts.Items, func(it extension.QuickPickItem) bool { return it.Label != "" && !it.Separator }) {
		return errors.New("FakeQuickPickInvalid", "a quick pick needs an item with a label that is not a separator")
	}
	q.a.mu.Lock()
	if opts.Origin == extension.HostOrigin {
		opts.Origin = q.a.origins.cur // the screen of the command being handled, as the host does
	}
	if done != nil {
		inner, origin := done, opts.Origin
		done = func(i int) { q.a.RunAs(origin, func() { inner(i) }) }
	}
	q.a.picks = append(q.a.picks, &Pick{a: q.a, opts: opts, done: done})
	q.a.mu.Unlock()
	return nil
}

// Picks are the quick picks the extension showed, the first first.
func (a *API) Picks() []*Pick {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.picks)
}

// LastPick is the quick pick shown last, or nil.
func (a *API) LastPick() *Pick {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.picks) == 0 {
		return nil
	}
	return a.picks[len(a.picks)-1]
}

// RanCommands are the commands that quick pick items ran.
func (a *API) RanCommands() []RanCommand {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.ranCommands)
}

// Request implements extension.LanguageServers: the handler set with OnServerRequest answers on the calling goroutine; without one the
// answer is null. A server that is not running (SetServerStatus) fails as the host does.
func (s serversAPI) Request(_ context.Context, id, method string, params any) (json.RawMessage, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return nil, errors.NewErr("FakeRequestParamsInvalid", err).SetMetadata("server", id, "method", method)
	}
	a := s.a
	a.mu.Lock()
	st, err := a.server(id)
	if err != nil {
		a.mu.Unlock()
		return nil, err
	}
	if st.status.State != extension.ServerRunning {
		a.mu.Unlock()
		return nil, errors.New("ExtensionServerNotRunning", "the language server is not running", "server", id)
	}
	req := ServerRequest{Server: id, Method: method, Params: data}
	st.requests = append(st.requests, req)
	handler := st.handler
	a.mu.Unlock()
	if handler == nil {
		return json.RawMessage("null"), nil
	}
	return handler(req)
}

// Notify implements extension.LanguageServers: the notification is recorded with the requests (Requests lists both), and the handler
// set with OnServerRequest is not asked.
func (s serversAPI) Notify(id, method string, params any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return errors.NewErr("FakeRequestParamsInvalid", err).SetMetadata("server", id, "method", method)
	}
	a := s.a
	a.mu.Lock()
	defer a.mu.Unlock()
	st, err := a.server(id)
	if err != nil {
		return err
	}
	if st.status.State != extension.ServerRunning {
		return errors.New("ExtensionServerNotRunning", "the language server is not running", "server", id)
	}
	if st.notifyErr != nil {
		return st.notifyErr
	}
	st.requests = append(st.requests, ServerRequest{Server: id, Method: method, Params: data, Notification: true})
	return nil
}

// OnNotification implements extension.LanguageServers; ServerNotify delivers.
func (s serversAPI) OnNotification(id, method string, fn func(json.RawMessage)) (func(), error) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	st, err := s.a.server(id)
	if err != nil {
		return nil, err
	}
	sub := &notifSub{method: method, fn: fn}
	st.notifs = append(st.notifs, sub)
	return func() {
		s.a.mu.Lock()
		defer s.a.mu.Unlock()
		st.notifs = slices.DeleteFunc(st.notifs, func(x *notifSub) bool { return x == sub })
	}, nil
}

// PositionEncoding implements extension.LanguageServers: utf-16 unless SetServerEncoding says otherwise.
func (s serversAPI) PositionEncoding(id string) (string, error) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	st, err := s.a.server(id)
	if err != nil {
		return "", err
	}
	if st.encoding == "" {
		return "utf-16", nil
	}
	return st.encoding, nil
}

// OnServerRequest sets what the server answers to the requests of the extension: the JSON of the result, or an error. The function runs
// on the calling goroutine, which may be one of the extension (a request is made off the UI thread).
func (a *API) OnServerRequest(id string, fn func(ServerRequest) (json.RawMessage, error)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, err := a.server(id); err == nil {
		st.handler = fn
	}
}

// FailNotifications makes the notifications the extension sends to the server with LanguageServers().Notify fail with err (nil
// stops failing).
func (a *API) FailNotifications(id string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, e := a.server(id); e == nil {
		st.notifyErr = err
	}
}

// ServerRequests are the requests the extension sent to the server, the first first.
func (a *API) ServerRequests(id string) []ServerRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, err := a.server(id); err == nil {
		return slices.Clone(st.requests)
	}
	return nil
}

// SetServerEncoding sets the position encoding PositionEncoding reports ("utf-8", "utf-16" or "utf-32").
func (a *API) SetServerEncoding(id, encoding string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, err := a.server(id); err == nil {
		st.encoding = encoding
	}
}

// ServerNotify delivers a notification of the server to the listeners of its method, on the calling goroutine, as the host does on the UI
// thread. params must encode to JSON.
func (a *API) ServerNotify(id, method string, params any) {
	a.t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		a.t.Fatal(err)
		return
	}
	a.mu.Lock()
	st, serr := a.server(id)
	if serr != nil {
		a.mu.Unlock()
		a.t.Fatal(serr)
		return
	}
	subs := slices.Clone(st.notifs)
	a.mu.Unlock()
	for _, sub := range subs {
		if sub.method == method {
			sub.fn(data)
		}
	}
}
