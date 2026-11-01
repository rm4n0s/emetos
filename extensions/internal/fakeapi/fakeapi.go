package fakeapi

import (
	"context"
	"encoding/json"
	"fmt"
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
	untitled          []Untitled
	failUntitled      error
	views             map[string]*View
	openedURIs        []string
	trashed           []*Trashed
	trashErr, undoErr error
	openedTabs        []string
	openedFolders     []OpenedFolder
	fileViewRefreshes int
	kinds             map[string]extension.EditorKind
	windowKind        *extension.WindowTabKind
	windowKindID      string
	windowTab         *WindowTab
	tabs              []*Tab
	drags             []*Drag
	failEditorOpen    bool
	viewStates        map[string]json.RawMessage
	proposals         []*Proposal
	fileViews         map[string]extension.FileViewProvider
	fileViewOpens     []FileViewOpen
	tokenizer         func(languageID, text string) ([][]extension.Token, error)
	providers         []*ProviderRegistration
	lensRefreshes     int
	channels          map[string]*Channel
	markers           map[string]map[string][]extension.Marker
	problemsShown     int
	problemsOff       bool
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
	scm               scmState
	decorations       decorationsState
	timeline          timelineState
	content           contentState
	revealed          []Revealed
	watches           []*FakeWatch
	watchErr          error
	dialogs           []*FakeDialog
	progresses        []*FakeProgress
	annotations       map[string]*annotationSet
	// annotationsUnavailable makes EditorDecorations().Set fail like a scope with no editors.
	annotationsUnavailable bool
	// quickDiffUnavailable makes AddQuickDiffProvider and QuickDiffOriginal fail like a scope with no editors.
	quickDiffUnavailable bool
	// historyUnavailable makes SetHistoryProvider fail like a scope with no Graph view.
	historyUnavailable bool
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

// FakeProgress is a notification with a progress bar that the extension showed with Notifications().Progress.
type FakeProgress struct {
	a    *API
	opts extension.ProgressOptions
	// reports are the calls of Report, in order.
	reports []ProgressReport
	done    bool
}

// ProgressReport is a call of Report.
type ProgressReport struct {
	Message string
	Percent float64
}

// Progress implements extension.Notifications. A test reads what was reported with Progresses.
func (n notifications) Progress(opts extension.ProgressOptions) extension.NotificationProgress {
	p := &FakeProgress{a: n.a, opts: opts}
	n.a.mu.Lock()
	n.a.progresses = append(n.a.progresses, p)
	n.a.mu.Unlock()
	return p
}

// Report implements extension.NotificationProgress.
func (p *FakeProgress) Report(message string, percent float64) {
	p.a.mu.Lock()
	defer p.a.mu.Unlock()
	if !p.done {
		p.reports = append(p.reports, ProgressReport{message, percent})
	}
}

// Done implements extension.NotificationProgress.
func (p *FakeProgress) Done() {
	p.a.mu.Lock()
	p.done = true
	p.a.mu.Unlock()
}

// Title is the title the progress was shown with.
func (p *FakeProgress) Title() string { return p.opts.Title }

// Cancellable says that the progress has a Cancel button.
func (p *FakeProgress) Cancellable() bool { return p.opts.Cancellable }

// Reports are the calls of Report, in order.
func (p *FakeProgress) Reports() []ProgressReport {
	p.a.mu.Lock()
	defer p.a.mu.Unlock()
	return append([]ProgressReport(nil), p.reports...)
}

// Ended says that Done was called.
func (p *FakeProgress) Ended() bool {
	p.a.mu.Lock()
	defer p.a.mu.Unlock()
	return p.done
}

// Cancel presses the Cancel button, as the user does: OnCancel runs and the notification is gone (Ended is true afterwards). It fails the test
// when the progress is not cancellable.
func (p *FakeProgress) Cancel() {
	p.a.t.Helper()
	if !p.opts.Cancellable {
		p.a.t.Fatalf("the progress %q has no Cancel button", p.opts.Title)
	}
	p.Done()
	if p.opts.OnCancel != nil {
		p.opts.OnCancel()
	}
}

// Progresses are the notifications with a progress bar shown so far.
func (a *API) Progresses() []*FakeProgress {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*FakeProgress(nil), a.progresses...)
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

// OpenUntitled implements extension.Documents: the document is added to the open ones with the text, as untitled:N, and recorded
// (Untitled).
func (d documents) OpenUntitled(languageID, text string) (string, error) {
	d.a.mu.Lock()
	defer d.a.mu.Unlock()
	if d.a.failUntitled != nil {
		return "", d.a.failUntitled
	}
	uri := fmt.Sprintf("untitled:Untitled-%d", len(d.a.untitled)+1)
	d.a.untitled = append(d.a.untitled, Untitled{URI: uri, LanguageID: languageID, Text: text})
	if d.a.docs == nil {
		d.a.docs = map[string]string{}
	}
	d.a.docs[uri] = text
	return uri, nil
}

// Untitled is a call of Documents().OpenUntitled.
type Untitled struct {
	URI, LanguageID, Text string
}

// UntitledDocuments are the calls of Documents().OpenUntitled, in order.
func (a *API) UntitledDocuments() []Untitled {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Untitled(nil), a.untitled...)
}

// FailUntitled makes Documents().OpenUntitled fail with err; nil makes it work again.
func (a *API) FailUntitled(err error) {
	a.mu.Lock()
	a.failUntitled = err
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

// EmitAs delivers an event to the subscribers whose pattern matches, at once, as if the extension with the id publisher had published
// it in this scope (Emit says the core did). It is how a test plays the other extension that publishes the events this one listens
// to, and checks that the subscriber reads Event.Publisher.
func (a *API) EmitAs(publisher, topic string, value any) { a.emit(publisher, a.info, topic, value) }

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
	saved := v.a.viewStates[id]
	v.a.mu.Unlock()
	// the host gives the content the state it saved in the session before, as soon as it is provided
	if len(saved) > 0 && c.RestoreState != nil {
		if err := c.RestoreState(append(json.RawMessage(nil), saved...)); err != nil {
			v.a.logger.Warn("a view could not restore its state", "view", id, "err", err)
		}
	}
	return viewHandle{view}, nil
}

// SetViewState gives a view the state it saved in an earlier session: Provide hands it to RestoreState, as the host does.
func (a *API) SetViewState(viewID string, raw json.RawMessage) {
	a.mu.Lock()
	if a.viewStates == nil {
		a.viewStates = map[string]json.RawMessage{}
	}
	a.viewStates[viewID] = raw
	a.mu.Unlock()
}

// State asks the content of the view for the state it saves, as JSON (nil for none).
func (v *View) State() json.RawMessage {
	if v.Content.SaveState == nil {
		return nil
	}
	val, err := v.Content.SaveState()
	if err != nil || val == nil {
		return nil
	}
	data, _ := json.Marshal(val)
	return data
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
	if p.a.problemsOff {
		return errors.New("ExtensionProblemsUnavailable", "the scope has no Problems panel")
	}
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

// Read implements extension.Problems: the markers of every owner for the file, most serious first, then by position.
func (p problems) Read(uri string) ([]extension.Marker, error) {
	p.a.mu.Lock()
	defer p.a.mu.Unlock()
	switch {
	case p.a.scm.dead:
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	case p.a.problemsOff:
		return nil, errors.New("ExtensionProblemsUnavailable", "the scope has no Problems panel")
	case !strings.HasPrefix(uri, "file:///"):
		return nil, errors.New("ExtensionProblemsURIInvalid", "markers belong to files: the URI is file:///path", "uri", uri)
	}
	var out []extension.Marker
	for _, byURI := range p.a.markers {
		out = append(out, byURI[uri]...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity < out[j].Severity
		}
		if out[i].StartLine != out[j].StartLine {
			return out[i].StartLine < out[j].StartLine
		}
		return out[i].StartColumn < out[j].StartColumn
	})
	return out, nil
}

// Show implements extension.Problems: the call is recorded (ProblemsShown).
func (p problems) Show() error {
	p.a.mu.Lock()
	defer p.a.mu.Unlock()
	if p.a.scm.dead {
		return errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	if p.a.problemsOff {
		return errors.New("ExtensionProblemsUnavailable", "the scope has no Problems panel")
	}
	p.a.problemsShown++
	return nil
}

// SetProblemsUnavailable makes Problems().Set, Read and Show fail as a scope with no Problems panel does (a headless host).
func (a *API) SetProblemsUnavailable(off bool) {
	a.mu.Lock()
	a.problemsOff = off
	a.mu.Unlock()
}

// ProblemsShown is how many times the extension asked for the Problems panel.
func (a *API) ProblemsShown() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.problemsShown
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

// Revealed is a call of Workspace().RevealFile.
type Revealed struct {
	URI  string
	InOS bool
}

// RevealFile implements extension.Workspace: the call is recorded (RevealedFiles).
func (w workspace) RevealFile(uri string, inOS bool) error {
	if !strings.HasPrefix(uri, "file://") {
		return errors.New("ExtensionFilePathInvalid", "only a file can be revealed", "uri", uri)
	}
	w.a.mu.Lock()
	w.a.revealed = append(w.a.revealed, Revealed{URI: uri, InOS: inOS})
	w.a.mu.Unlock()
	return nil
}

// RevealedFiles are the calls of Workspace().RevealFile, in order.
func (a *API) RevealedFiles() []Revealed {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Revealed(nil), a.revealed...)
}

// Trashed is a call of Workspace().TrashFiles.
type Trashed struct {
	// URIs are the files that were moved to the trash.
	URIs   []string
	a      *API
	undone bool
}

// Undone says that the function TrashFiles returned was called.
func (t *Trashed) Undone() bool {
	t.a.mu.Lock()
	defer t.a.mu.Unlock()
	return t.undone
}

// TrashFiles implements extension.Workspace: the files are not touched, the call is recorded (TrashedFiles) and the undo function marks
// it undone. FailTrash makes the calls fail.
func (w workspace) TrashFiles(_ context.Context, uris []string) (func(context.Context) error, error) {
	for _, uri := range uris {
		if !strings.HasPrefix(uri, "file://") {
			return nil, errors.New("ExtensionFilePathInvalid", "only a file can be moved to the trash", "uri", uri)
		}
	}
	w.a.mu.Lock()
	defer w.a.mu.Unlock()
	if w.a.trashErr != nil {
		return nil, w.a.trashErr
	}
	rec := &Trashed{URIs: append([]string(nil), uris...), a: w.a}
	w.a.trashed = append(w.a.trashed, rec)
	return func(context.Context) error {
		w.a.mu.Lock()
		defer w.a.mu.Unlock()
		if w.a.undoErr != nil {
			return w.a.undoErr
		}
		rec.undone = true
		return nil
	}, nil
}

// FailTrash makes the next calls of Workspace().TrashFiles fail with err (nil makes them work again); FailUndoTrash does the same for
// the functions they returned.
func (a *API) FailTrash(err error) {
	a.mu.Lock()
	a.trashErr = err
	a.mu.Unlock()
}

// FailUndoTrash makes the undo functions of TrashFiles fail with err.
func (a *API) FailUndoTrash(err error) {
	a.mu.Lock()
	a.undoErr = err
	a.mu.Unlock()
}

// TrashedFiles are the calls of Workspace().TrashFiles, in order.
func (a *API) TrashedFiles() []*Trashed {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*Trashed(nil), a.trashed...)
}

// OpenWindowTab implements extension.Workspace: the call is recorded (OpenedWindowTabs).
func (w workspace) OpenWindowTab(kindID string) error {
	w.a.mu.Lock()
	w.a.openedTabs = append(w.a.openedTabs, kindID)
	w.a.mu.Unlock()
	return nil
}

// OpenedFolder is a call of Workspace().OpenFolder.
type OpenedFolder struct {
	// Path is the folder; NewTab says that it was to open in a new workspace tab and not in the tab of the scope.
	Path   string
	NewTab bool
}

// OpenFolder implements extension.Workspace: the folder is not opened, the call is recorded (OpenedFolders). A path that is not clean
// and absolute fails as the host's does.
func (w workspace) OpenFolder(path string, newTab bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("ExtensionFilePathInvalid", "the folder to open is a clean absolute path", "path", path)
	}
	w.a.mu.Lock()
	w.a.openedFolders = append(w.a.openedFolders, OpenedFolder{Path: path, NewTab: newTab})
	w.a.mu.Unlock()
	return nil
}

// OpenedFolders are the calls of Workspace().OpenFolder, in order.
func (a *API) OpenedFolders() []OpenedFolder {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]OpenedFolder(nil), a.openedFolders...)
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
