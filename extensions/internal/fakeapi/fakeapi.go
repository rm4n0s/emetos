package fakeapi

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// API is the fake. Make one with New; every method of extension.API is implemented, and the fields below are what a test
// looks at. It is safe for use from several goroutines, as the real API is.
type API struct {
	t      testing.TB
	info   extension.ScopeInfo
	logger *slog.Logger

	mu                sync.Mutex
	posted            []func()
	handlers          map[string]extension.CommandHandler
	specs             map[string]extension.SettingSpec
	values            map[string]any
	changeFn          []func()
	subs              []*subscription
	keys              map[string]any
	status            map[string]*StatusItem
	notes             []Note
	shown             []Shown
	views             map[string]*View
	openedURIs        []string
	openedTabs        []string
	fileViewRefreshes int
	kinds             map[string]extension.EditorKind
	windowKind        *extension.WindowTabKind
	windowKindID      string
	windowTab         *WindowTab
	tabs              []*Tab
	proposals         []*Proposal
	fileViews         map[string]extension.FileViewProvider
	fileViewOpens     []FileViewOpen
	tokenizer         func(languageID, text string) ([][]extension.Token, error)
	providers         []*ProviderRegistration
	lensRefreshes     int
	channels          map[string]*Channel
	markers           map[string]map[string][]extension.Marker
	files             []string
	complete          bool
	manifest          extension.Manifest
	servers           map[string]*serverState
	picks             []*Pick
	ranCommands       []RanCommand
	docs              map[string]string
	applied           []extension.WorkspaceEdit
	searches          []SearchCall
	replaceCalls      [][]extension.FileReplacement
	inputs            []*Input
	serverCalls       []ServerCall
	serverCommands    map[string][]string
	commandHandler    func(ServerCall) error
	actionHandler     func(CodeActionCall) (string, error)
	actionCalls       []CodeActionCall
	headless          []*HeadlessView
	headlessErr       error
	banners           []*FakeBanner
	chooserCalls      []ChooserCall
	chooserAnswers    []*chooserAnswer
	published         []Published
	requests          []RequestCall
	requestDoubles    map[string]RequestDouble
	handlers2         map[string]RequestDouble
	active            *extension.ActiveDocument
	clipboard         []string
	clipboardErr      error
	locationAnswers   map[string]func(uri string, pos extension.Position) ([]extension.Location, error)
	locationRequests  []LocationRequest
	updates           []SettingUpdate
}

// Note is a notification the extension showed.
type Note struct {
	Severity, Message string
	// Actions are the labels of the buttons of the notification; RunNoteAction presses one.
	Actions []string
	run     []func()
}

// Shown is a call of Documents().Show.
type Shown struct {
	URI           string
	Line, Column  int
	Preview       bool
	ReturnedError error
}

// New makes a fake API for a test. info is the scope the extension runs in; with a Folder, Workspace() reads that folder
// from disk and lists its files.
func New(t testing.TB, m extension.Manifest, info extension.ScopeInfo) *API {
	t.Helper()
	a := &API{
		t: t, info: info, manifest: m, logger: slog.Default(), complete: true,
		handlers: map[string]extension.CommandHandler{}, specs: map[string]extension.SettingSpec{}, values: map[string]any{},
		keys: map[string]any{}, status: map[string]*StatusItem{}, views: map[string]*View{}, channels: map[string]*Channel{},
		markers: map[string]map[string][]extension.Marker{}, requestDoubles: map[string]RequestDouble{}, handlers2: map[string]RequestDouble{},
	}
	for _, spec := range m.Contributes.Settings {
		a.specs[spec.Key] = spec
	}
	if info.Folder != "" {
		a.RescanFiles()
	}
	return a
}

// RescanFiles reads the list of files of the folder from disk, as the file index would: the paths relative to the folder,
// with forward slashes, in the order of a walk, leaving out the folders that start with a dot.
func (a *API) RescanFiles() {
	var files []string
	_ = filepath.WalkDir(a.info.Folder, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(a.info.Folder, path)
		if d.IsDir() {
			if rel != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	a.mu.Lock()
	a.files = files
	a.mu.Unlock()
}

// SetComplete says whether the walk of the folder has finished.
func (a *API) SetComplete(complete bool) {
	a.mu.Lock()
	a.complete = complete
	a.mu.Unlock()
}

// ---- the UI thread ----

// Post implements extension.API: the function waits for Pump.
func (a *API) Post(fn func()) {
	a.mu.Lock()
	a.posted = append(a.posted, fn)
	a.mu.Unlock()
}

// Pump runs what the extension posted, and what that posts, as the UI thread does between two frames; it reports whether
// it ran anything.
func (a *API) Pump() bool {
	ran := false
	for {
		a.mu.Lock()
		if len(a.posted) == 0 {
			a.mu.Unlock()
			return ran
		}
		fn := a.posted[0]
		a.posted = a.posted[1:]
		a.mu.Unlock()
		fn()
		ran = true
	}
}

// WaitFor pumps until cond holds, or fails the test after the timeout. cond runs on the test's goroutine, as the UI thread.
func (a *API) WaitFor(what string, cond func() bool) {
	a.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		a.Pump()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			a.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// ---- the API ----

// Scope implements extension.API.
func (a *API) Scope() extension.ScopeInfo { return a.info }

// Log implements extension.API.
func (a *API) Log() *slog.Logger { return a.logger }

// Commands implements extension.API.
func (a *API) Commands() extension.Commands { return commands{a} }

// Keybindings implements extension.API.
func (a *API) Keybindings() extension.Keybindings { return nothing{} }

// Menus implements extension.API.
func (a *API) Menus() extension.Menus { return menusNothing{} }

// Settings implements extension.API.
func (a *API) Settings() extension.Settings { return settings{a} }

// Notifications implements extension.API.
func (a *API) Notifications() extension.Notifications { return notifications{a} }

// StatusBar implements extension.API.
func (a *API) StatusBar() extension.StatusBar { return statusBar{a} }

// Documents implements extension.API.
func (a *API) Documents() extension.Documents { return documents{a} }

// Events implements extension.API.
func (a *API) Events() extension.Events { return events{a} }

// Clipboard implements extension.API: the text is recorded (ClipboardTexts).
func (a *API) Clipboard() extension.Clipboard { return clipboard{a} }

type clipboard struct{ a *API }

func (c clipboard) WriteText(text string) error {
	c.a.mu.Lock()
	defer c.a.mu.Unlock()
	if c.a.clipboardErr != nil {
		return c.a.clipboardErr
	}
	c.a.clipboard = append(c.a.clipboard, text)
	return nil
}

// ClipboardTexts are the texts the extension wrote to the clipboard, the first first.
func (a *API) ClipboardTexts() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.clipboard...)
}

// FailClipboard makes the clipboard fail with err from now on; nil makes it work again.
func (a *API) FailClipboard(err error) {
	a.mu.Lock()
	a.clipboardErr = err
	a.mu.Unlock()
}

// Views implements extension.API.
func (a *API) Views() extension.Views { return views{a} }

// Output implements extension.API.
func (a *API) Output() extension.Output { return output{a} }

// Problems implements extension.API.
func (a *API) Problems() extension.Problems { return problems{a} }

// Workspace implements extension.API.
func (a *API) Workspace() extension.Workspace { return workspace{a} }

// Context implements extension.API.
func (a *API) Context() extension.ContextKeys { return contextKeys{a} }

type nothing struct{}

func (nothing) Add(extension.Keybinding) error { return nil }

// Add of Menus is the same function with another argument; a type of its own keeps the two interfaces apart.
type menusNothing struct{}

func (menusNothing) Add(extension.MenuItem) error { return nil }

// ---- commands ----

type commands struct{ a *API }

func (c commands) Handle(id string, h extension.CommandHandler) error {
	for _, spec := range c.a.manifest.Contributes.Commands {
		if spec.ID == id {
			c.a.mu.Lock()
			c.a.handlers[id] = h
			c.a.mu.Unlock()
			return nil
		}
	}
	return errors.New("FakeCommandNotDeclared", "the manifest does not declare the command", "command", id)
}

func (c commands) Register(spec extension.CommandSpec, h extension.CommandHandler) error {
	c.a.mu.Lock()
	defer c.a.mu.Unlock()
	if _, taken := c.a.handlers[spec.ID]; taken {
		return errors.New("FakeCommandTaken", "the command has a handler already", "command", spec.ID)
	}
	c.a.handlers[spec.ID] = h
	return nil
}

// Run runs the handler a command was given, as the palette does.
func (a *API) Run(id string, args any) error {
	a.t.Helper()
	a.mu.Lock()
	h := a.handlers[id]
	a.mu.Unlock()
	if h == nil {
		a.t.Fatalf("no handler for the command %s", id)
	}
	return h(context.Background(), args)
}

// RunCtx is Run with the context of the command.
func (a *API) RunCtx(ctx context.Context, id string, args any) error {
	a.t.Helper()
	a.mu.Lock()
	h := a.handlers[id]
	a.mu.Unlock()
	if h == nil {
		a.t.Fatalf("no handler for the command %s", id)
	}
	return h(ctx, args)
}

// HasHandler tells whether a command has a handler.
func (a *API) HasHandler(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.handlers[id] != nil
}

// ---- settings ----

type settings struct{ a *API }

func (s settings) Register(spec extension.SettingSpec) error {
	if !strings.HasPrefix(spec.Key, s.a.manifest.ID+".") {
		return errors.New("FakeSettingKeyForeign", "the key is not under the id of the extension", "key", spec.Key)
	}
	s.a.mu.Lock()
	s.a.specs[spec.Key] = spec
	s.a.mu.Unlock()
	return nil
}

func (s settings) Get(key string) (any, error) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	spec, ok := s.a.specs[key]
	if !ok {
		return nil, errors.New("FakeSettingUnknown", "no setting has this key", "key", key)
	}
	if v, ok := s.a.values[key]; ok {
		return v, nil
	}
	return roundTrip(spec.Default), nil
}

func (s settings) OnChange(fn func()) func() {
	s.a.mu.Lock()
	s.a.changeFn = append(s.a.changeFn, fn)
	s.a.mu.Unlock()
	return func() {}
}

// Update implements extension.Settings: a setting the extension declares (or registered) takes the value and the listeners are
// told, as they are when the user's file changes; the call is recorded (SettingUpdates). Any other key fails with
// FakeSettingNotOwned.
func (s settings) Update(key string, value any) error {
	s.a.mu.Lock()
	_, ok := s.a.specs[key]
	s.a.mu.Unlock()
	if !ok {
		return errors.New("FakeSettingNotOwned", "an extension writes only the settings it declares or registered", "key", key)
	}
	s.a.mu.Lock()
	s.a.updates = append(s.a.updates, SettingUpdate{Key: key, Value: roundTrip(value)})
	s.a.mu.Unlock()
	s.a.SetSetting(key, value)
	return nil
}

// SettingUpdate is a call of Settings().Update.
type SettingUpdate struct {
	Key   string
	Value any
}

// SettingUpdates are the calls of Settings().Update.
func (a *API) SettingUpdates() []SettingUpdate {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]SettingUpdate(nil), a.updates...)
}

// roundTrip turns a Go value into what the JSON of the settings file reads: a []string is a []any.
func roundTrip(v any) any {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return v
	}
	return out
}

// SetSetting changes a setting as the user does and tells the listeners, on the test's goroutine.
func (a *API) SetSetting(key string, value any) {
	a.mu.Lock()
	a.values[key] = roundTrip(value)
	fns := append([]func(){}, a.changeFn...)
	a.mu.Unlock()
	for _, f := range fns {
		f()
	}
}

// ---- notifications ----

type notifications struct{ a *API }

func (n notifications) add(sev, msg string, actions []extension.NotificationAction) {
	note := Note{Severity: sev, Message: msg}
	for _, a := range actions {
		note.Actions = append(note.Actions, a.Label)
		note.run = append(note.run, a.Run)
	}
	n.a.mu.Lock()
	n.a.notes = append(n.a.notes, note)
	n.a.mu.Unlock()
}
func (n notifications) Info(m string, a ...extension.NotificationAction) { n.add("info", m, a) }
func (n notifications) Warn(m string, a ...extension.NotificationAction) { n.add("warn", m, a) }
func (n notifications) Error(m string, a ...extension.NotificationAction) {
	n.add("error", m, a)
}

// RunNoteAction presses the button with the label on the notification with the index, as the user does; it fails the test
// when there is no such button.
func (a *API) RunNoteAction(note int, label string) {
	a.t.Helper()
	a.mu.Lock()
	var run func()
	if note >= 0 && note < len(a.notes) {
		for i, l := range a.notes[note].Actions {
			if l == label {
				run = a.notes[note].run[i]
			}
		}
	}
	a.mu.Unlock()
	if run == nil {
		a.t.Fatalf("notification %d has no button %q", note, label)
	}
	run()
}

// Notes are the notifications shown so far.
func (a *API) Notes() []Note {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Note(nil), a.notes...)
}

// ---- status bar ----

// StatusItem is an item of the status bar as the extension last set it.
type StatusItem struct {
	extension.StatusItem
	Hidden, Disposed bool
}

type statusBar struct{ a *API }

func (s statusBar) Add(item extension.StatusItem) (extension.StatusHandle, error) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if _, taken := s.a.status[item.ID]; taken {
		return nil, errors.New("FakeStatusItemTaken", "the extension has a status item with this id", "item", item.ID)
	}
	it := &StatusItem{StatusItem: item}
	s.a.status[item.ID] = it
	return &statusHandle{a: s.a, it: it}, nil
}

type statusHandle struct {
	a  *API
	it *StatusItem
}

func (h *statusHandle) Update(item extension.StatusItem) {
	h.a.mu.Lock()
	h.it.StatusItem = item
	h.a.mu.Unlock()
}
func (h *statusHandle) SetVisible(v bool) { h.a.mu.Lock(); h.it.Hidden = !v; h.a.mu.Unlock() }
func (h *statusHandle) Dispose()          { h.a.mu.Lock(); h.it.Disposed = true; h.a.mu.Unlock() }

// StatusItem returns the status bar item with the id, or nil.
func (a *API) StatusItem(id string) *StatusItem {
	a.mu.Lock()
	defer a.mu.Unlock()
	if it := a.status[id]; it != nil {
		c := *it
		return &c
	}
	return nil
}

// ---- documents ----

type documents struct{ a *API }

func (d documents) Read(_ context.Context, uri string, _ ...extension.ReadOption) (string, error) {
	d.a.mu.Lock()
	text, open := d.a.docs[uri]
	d.a.mu.Unlock()
	if open {
		return text, nil
	}
	path := strings.TrimPrefix(uri, "file://")
	data, err := os.ReadFile(path)
	return string(data), err
}
func (d documents) Open() []string {
	d.a.mu.Lock()
	defer d.a.mu.Unlock()
	uris := make([]string, 0, len(d.a.docs))
	for uri := range d.a.docs {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	return uris
}

// Active implements extension.Documents: the editor a test set with SetActive.
func (d documents) Active() (extension.ActiveDocument, bool) {
	d.a.mu.Lock()
	defer d.a.mu.Unlock()
	if d.a.active == nil {
		return extension.ActiveDocument{}, false
	}
	return *d.a.active, true
}

// SetActive makes the document the active text editor with the selections, as the window reports it (Documents().Active); nil
// for none.
func (a *API) SetActive(doc *extension.ActiveDocument) {
	a.mu.Lock()
	a.active = doc
	a.mu.Unlock()
}

func (d documents) Show(uri string, line, column int, preview bool) error {
	d.a.mu.Lock()
	d.a.shown = append(d.a.shown, Shown{URI: uri, Line: line, Column: column, Preview: preview})
	d.a.mu.Unlock()
	return nil
}

// ShownDocuments are the calls of Documents().Show.
func (a *API) ShownDocuments() []Shown {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Shown(nil), a.shown...)
}

// ---- events ----

type subscription struct {
	pattern string
	fn      func(extension.Event)
	off     bool
	// others says that the subscription hears the other window tabs too (extension.FromOtherScopes).
	others bool
}

type events struct{ a *API }

// Publish implements extension.Events: the event is recorded (Published) and delivered to the subscribers at once, as Emit does,
// with the extension's own id as publisher.
func (e events) Publish(topic string, value any) error {
	if err := extension.CheckTopic(topic); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return errors.NewErr("FakeEventValueInvalid", err).SetMetadata("topic", topic)
	}
	e.a.mu.Lock()
	e.a.published = append(e.a.published, Published{Topic: topic, Data: data})
	e.a.mu.Unlock()
	e.a.emit(e.a.manifest.ID, e.a.info, topic, value)
	return nil
}

func (e events) Subscribe(pattern string, fn func(extension.Event), opts ...extension.SubscribeOption) (func(), error) {
	if err := extension.CheckPattern(pattern); err != nil {
		return nil, err
	}
	var o extension.SubscribeOptions
	for _, opt := range opts {
		opt(&o)
	}
	s := &subscription{pattern: pattern, fn: fn, others: o.From.Others}
	e.a.mu.Lock()
	e.a.subs = append(e.a.subs, s)
	e.a.mu.Unlock()
	return func() { e.a.mu.Lock(); s.off = true; e.a.mu.Unlock() }, nil
}

// Request implements extension.Events: the request is recorded (Requests) and answered, later, on Pump, by the double the test
// set for the topic with OnRequest (the core topics of SPEC §10.19 are faked this way), else by the handler the extension
// itself set with Handle or HandleAsync, else with EventNoHandler. A request with a topic that is not valid fails at once.
func (e events) Request(ctx context.Context, topic string, value any, reply func(extension.Event, error), _ ...extension.RequestOption) error {
	if err := extension.CheckTopic(topic); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return errors.NewErr("FakeEventValueInvalid", err).SetMetadata("topic", topic)
	}
	a := e.a
	a.mu.Lock()
	a.requests = append(a.requests, RequestCall{Topic: topic, Data: data})
	double, own := a.requestDoubles[topic], a.handlers2[topic]
	a.mu.Unlock()
	if reply == nil {
		reply = func(extension.Event, error) {}
	}
	ev := extension.Event{Topic: topic, Publisher: a.manifest.ID, Scope: a.info, Data: data}
	answer := func(v any, err error) {
		a.Post(func() {
			if err != nil {
				reply(extension.Event{}, err)
				return
			}
			out, merr := json.Marshal(v)
			if merr != nil {
				reply(extension.Event{}, errors.NewErr("FakeEventValueInvalid", merr).SetMetadata("topic", topic))
				return
			}
			reply(extension.Event{Topic: topic, Publisher: extension.CorePublisher, Scope: a.info, Data: out}, nil)
		})
	}
	switch {
	case double != nil:
		double(ctx, ev, answer)
	case own != nil:
		own(ctx, ev, answer)
	default:
		answer(nil, errors.New("EventNoHandler", "the fake has no double or handler for this topic", "topic", topic))
	}
	return nil
}

// Handle implements extension.Events: fn answers the requests of the topic that the extension makes itself, and the ones a test
// makes with Ask.
func (e events) Handle(topic string, fn extension.EventHandler) error {
	return e.HandleAsync(topic, func(ctx context.Context, ev extension.Event, reply extension.EventReply) {
		v, err := fn(ctx, ev)
		reply(v, err)
	})
}

// HandleAsync implements extension.Events.
func (e events) HandleAsync(topic string, fn extension.AsyncEventHandler) error {
	if err := extension.CheckTopic(topic); err != nil {
		return err
	}
	e.a.mu.Lock()
	defer e.a.mu.Unlock()
	if _, taken := e.a.handlers2[topic]; taken {
		return errors.New("EventHandlerTaken", "a handler for this topic is set already", "topic", topic)
	}
	e.a.handlers2[topic] = func(ctx context.Context, ev extension.Event, reply func(any, error)) { fn(ctx, ev, reply) }
	return nil
}

// Emit delivers an event to the subscribers whose pattern matches, at once, on the caller's goroutine (a test calls it on
// its own, which stands for the UI thread).
func (a *API) Emit(topic string, value any) { a.emit(extension.CorePublisher, a.info, topic, value) }

// EmitFrom delivers an event as if the scope published it: to the subscribers of the topic that listen to other window tabs
// (extension.FromOtherScopes), the way a link that the user allowed does.
func (a *API) EmitFrom(from extension.ScopeInfo, topic string, value any) {
	a.emit(extension.CorePublisher, from, topic, value)
}

// emit delivers an event published in a scope.
func (a *API) emit(publisher string, from extension.ScopeInfo, topic string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		a.t.Fatalf("event value: %v", err)
	}
	a.mu.Lock()
	var fns []func(extension.Event)
	own := from.ID == a.info.ID && from.Name == a.info.Name
	for _, s := range a.subs {
		if !s.off && extension.MatchTopic(s.pattern, topic) && (own || s.others) {
			fns = append(fns, s.fn)
		}
	}
	a.mu.Unlock()
	for _, fn := range fns {
		fn(extension.Event{Topic: topic, Publisher: publisher, Scope: from, Data: data})
	}
}

// ---- views ----

// View is a view the extension provided.
type View struct {
	ID      string
	Content extension.ViewContent

	mu       sync.Mutex
	Refreshs int
	Badge    int
	BadgeTip string
	// Title is the title the extension set with SetTitle.
	Title   string
	Reveals []bool
	// Hides counts the calls of Hide.
	Hides    int
	Disposed bool
}

type views struct{ a *API }

func (v views) Provide(id string, c extension.ViewContent) (extension.ViewHandle, error) {
	declared := false
	for _, s := range v.a.manifest.Contributes.Views {
		declared = declared || s.ID == id
	}
	for _, s := range v.a.manifest.Contributes.PanelViews {
		declared = declared || s.ID == id
	}
	if !declared {
		return nil, errors.New("FakeViewNotDeclared", "the manifest does not declare the view", "view", id)
	}
	if c.Build == nil {
		return nil, errors.New("FakeViewContentInvalid", "the content has no Build function", "view", id)
	}
	view := &View{ID: id, Content: c}
	v.a.mu.Lock()
	v.a.views[id] = view
	v.a.mu.Unlock()
	return viewHandle{view}, nil
}

type viewHandle struct{ v *View }

// SetTitle implements extension.ViewHandle.
func (h viewHandle) SetTitle(title string) { h.v.mu.Lock(); h.v.Title = title; h.v.mu.Unlock() }

func (h viewHandle) Refresh() { h.v.mu.Lock(); h.v.Refreshs++; h.v.mu.Unlock() }
func (h viewHandle) SetBadge(n int, tip string) {
	h.v.mu.Lock()
	h.v.Badge, h.v.BadgeTip = n, tip
	h.v.mu.Unlock()
}
func (h viewHandle) Reveal(focus bool) {
	h.v.mu.Lock()
	h.v.Reveals = append(h.v.Reveals, focus)
	h.v.mu.Unlock()
}
func (h viewHandle) Hide()    { h.v.mu.Lock(); h.v.Hides++; h.v.mu.Unlock() }
func (h viewHandle) Dispose() { h.v.mu.Lock(); h.v.Disposed = true; h.v.mu.Unlock() }

// View returns the view the extension provided with the id, or nil.
func (a *API) View(id string) *View {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.views[id]
}

// Snapshot is the numbers of a view read under its lock.
func (v *View) Snapshot() (refreshs, badge int, tip string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.Refreshs, v.Badge, v.BadgeTip
}

// ---- Output ----

// Channel is an Output channel the extension made.
type Channel struct {
	name string

	mu       sync.Mutex
	lines    []string
	disposed bool
	shown    int
}

type output struct{ a *API }

func (o output) Channel(name string) (extension.OutputChannel, error) {
	o.a.mu.Lock()
	defer o.a.mu.Unlock()
	if _, taken := o.a.channels[name]; taken {
		return nil, errors.New("FakeChannelOpen", "the extension has this channel open", "channel", name)
	}
	c := &Channel{name: name}
	o.a.channels[name] = c
	return c, nil
}

// Name implements extension.OutputChannel.
func (c *Channel) Name() string { return c.name }

// AppendLine implements extension.OutputChannel.
func (c *Channel) AppendLine(text string) {
	c.mu.Lock()
	c.lines = append(c.lines, strings.Split(text, "\n")...)
	c.mu.Unlock()
}

// Clear implements extension.OutputChannel.
func (c *Channel) Clear() { c.mu.Lock(); c.lines = nil; c.mu.Unlock() }

// Show implements extension.OutputChannel.
func (c *Channel) Show(bool) { c.mu.Lock(); c.shown++; c.mu.Unlock() }

// Dispose implements extension.OutputChannel.
func (c *Channel) Dispose() { c.mu.Lock(); c.disposed = true; c.mu.Unlock() }

// Lines are the lines written to the channel.
func (c *Channel) Lines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

// Shown is how many times Show was called.
func (c *Channel) Shown() int { c.mu.Lock(); defer c.mu.Unlock(); return c.shown }

// Channel returns the Output channel with the name, or nil.
func (a *API) Channel(name string) *Channel {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.channels[name]
}

// ---- Problems ----

type problems struct{ a *API }

func (p problems) Set(owner, uri string, markers []extension.Marker) error {
	p.a.mu.Lock()
	defer p.a.mu.Unlock()
	if p.a.markers[owner] == nil {
		p.a.markers[owner] = map[string][]extension.Marker{}
	}
	if len(markers) == 0 {
		delete(p.a.markers[owner], uri)
		return nil
	}
	p.a.markers[owner][uri] = append([]extension.Marker(nil), markers...)
	return nil
}

func (p problems) Clear(owner string) {
	p.a.mu.Lock()
	delete(p.a.markers, owner)
	p.a.mu.Unlock()
}

// Markers are the markers of an owner by URI.
func (a *API) Markers(owner string) map[string][]extension.Marker {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string][]extension.Marker{}
	for uri, ms := range a.markers[owner] {
		out[uri] = append([]extension.Marker(nil), ms...)
	}
	return out
}

// MarkerCount is the number of markers of all owners.
func (a *API) MarkerCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, byURI := range a.markers {
		for _, ms := range byURI {
			n += len(ms)
		}
	}
	return n
}

// ---- the folder ----

type workspace struct{ a *API }

// OpenURI implements extension.Workspace: the call is recorded (OpenedURIs).
func (w workspace) OpenURI(_ context.Context, uri string) error {
	w.a.mu.Lock()
	w.a.openedURIs = append(w.a.openedURIs, uri)
	w.a.mu.Unlock()
	return nil
}

// OpenWindowTab implements extension.Workspace: the call is recorded (OpenedWindowTabs).
func (w workspace) OpenWindowTab(kindID string) error {
	w.a.mu.Lock()
	w.a.openedTabs = append(w.a.openedTabs, kindID)
	w.a.mu.Unlock()
	return nil
}

// OpenedWindowTabs are the kinds the extension opened with Workspace().OpenWindowTab.
func (a *API) OpenedWindowTabs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.openedTabs...)
}

// OpenedURIs are the URIs the extension opened with Workspace().OpenURI.
func (a *API) OpenedURIs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.openedURIs...)
}

func (w workspace) Files() (extension.FileList, error) {
	w.a.mu.Lock()
	defer w.a.mu.Unlock()
	if w.a.info.Folder == "" {
		return extension.FileList{}, errors.New("FakeNoFolder", "the scope has no folder")
	}
	return extension.FileList{Files: append([]string(nil), w.a.files...), Complete: w.a.complete}, nil
}

func (w workspace) ReadFile(ctx context.Context, path string) (string, error) {
	if !fs.ValidPath(path) {
		return "", errors.New("FakePathInvalid", "the path is relative to the folder, with forward slashes and no ..", "path", path)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(w.a.info.Folder, filepath.FromSlash(path)))
	if err != nil {
		return "", err
	}
	if len(data) > extension.MaxReadFileSize {
		return "", errors.New("FakeFileTooLarge", "the file is larger than extension.MaxReadFileSize", "path", path)
	}
	return string(data), nil
}

// ---- context keys ----

type contextKeys struct{ a *API }

func (c contextKeys) Set(key string, value any) error {
	if !strings.HasPrefix(key, c.a.manifest.ID+".") && !slices.Contains(c.a.manifest.Contributes.ContextKeys, key) {
		return errors.New("FakeContextKeyForeign", "the key is not under the id of the extension", "key", key)
	}
	c.a.mu.Lock()
	c.a.keys[key] = value
	c.a.mu.Unlock()
	return nil
}
func (c contextKeys) Reset(key string) { c.a.mu.Lock(); delete(c.a.keys, key); c.a.mu.Unlock() }

// ContextKey is the value of a context key the extension set, or nil.
func (a *API) ContextKey(key string) any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.keys[key]
}

// ViewIDs lists the ids of the views the extension provided.
func (a *API) ViewIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var ids []string
	for id := range a.views {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
