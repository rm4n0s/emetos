package exthost

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/glob"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// State is what an extension is in one scope.
type State int

// The states of an extension in a scope.
const (
	// StateInactive: enabled, and waiting for one of its activation events.
	StateInactive State = iota
	// StateActive: activated, and running.
	StateActive
	// StateFailed: it failed, and stays off in this scope for the rest of the session. Turning it off and on again in
	// the Extensions page tries once more.
	StateFailed
	// StateDisabled: the user turned it off (extensions.disabled).
	StateDisabled
)

// String is the word the Extensions page shows.
func (s State) String() string {
	switch s {
	case StateInactive:
		return "inactive"
	case StateActive:
		return "active"
	case StateFailed:
		return "failed"
	case StateDisabled:
		return "disabled"
	}
	return "unknown"
}

// Failure is why an extension is off in a scope (SPEC §10.15): the error that was caught at the call into it, kept for
// the page, the tests and the box that would have shown the extension.
type Failure struct {
	// Err is the error, an *errors.Error: its tag says which call failed (ExtensionActivateFailed,
	// ExtensionActivatePanicked, ExtensionCommandPanicked, ExtensionStatusCallbackPanicked, ...) and its route how the
	// call was reached.
	Err error
	// Where says in words what was called: "activate", "command hello.greet", "status bar item hello.count".
	Where string
	// BoxText is what a box of the extension shows in its place: "<Name> failed" (with the Show Log action).
	BoxText string
}

// KeybindingSink takes the default keybindings that the extensions of a scope add; the window turns them into its
// keymap. The function that Add returns takes them out again.
type KeybindingSink interface {
	Add(items ...keybinding.Item) (remove func())
}

// DocumentSource is what the window offers of its documents: the text of one, and the list of the open ones.
type DocumentSource interface {
	// Read returns the text of the document with the URI. A failure is returned as it is.
	Read(ctx context.Context, uri string) (string, error)
	// Open lists the URIs of the open documents in the order they were opened.
	Open() []string
}

// ScopeOptions are the services of the window tab that a Scope binds its extensions to.
type ScopeOptions struct {
	// Info describes the scope for the extensions and the Extensions page.
	Info extension.ScopeInfo
	// Ctx is the context of the scope: the contexts of the extensions end with it.
	Ctx context.Context
	// Commands and Menus are the registries of the scope (the fork of the window), Keybindings takes the keys, Settings
	// reads the settings of the workspace, Notify shows notifications and StatusBar is the status bar.
	Commands    *command.Registry
	Menus       *command.MenuRegistry
	Keybindings KeybindingSink
	Settings    *settings.Store
	Notify      *notify.Service
	StatusBar   *workbench.StatusBar
	// Documents reads the documents; nil gives extensions an empty set.
	Documents DocumentSource
	// Disabled gives the ids in extensions.disabled; nil is none.
	Disabled func() []string
	// Files gives the paths, relative to the folder and with forward slashes, of the files of the folder, for the
	// activation event onFile; nil when the scope has no folder.
	Files func() []string
	// Post runs a function on the UI thread; nil calls it at once. Callbacks that come from other goroutines (a settings
	// change) are handed over with it.
	Post func(func())
	// Frame runs a function after the next frame, on the UI thread; fast events (typing) are delivered then, once per
	// frame. Nil is Post: every fast event is delivered by itself.
	Frame func(func())
	// After calls a function after a duration, from any goroutine, and returns the function that stops it; the timeouts
	// of requests use it, so tests give it a fake clock. Nil is a timer of the standard library.
	After func(d time.Duration, f func()) (stop func())
	// Now is the clock that measures how long a subscriber or a handler takes; nil is time.Now.
	Now func() time.Time
	// Logger is the log of the process; slog.Default when nil.
	Logger *slog.Logger
	// AskSharing asks the user, in this window tab, whether the events of another tab may reach it (SPEC §10.15 "Event
	// sharing"): it shows the question and calls answer once, on the UI thread, with what the user chose. Nil means the
	// scope cannot ask, so no link is ever made for it.
	AskSharing func(q SharingAsk, answer func(SharingAnswer))
	// Views is the views model of the window tab: the extensions register their view containers and views in its registry
	// (the fork of the window's), and it tells the workbench when a badge or a context key changed. Nil leaves the views
	// of the extensions out.
	Views *workbench.Views
	// Keys is the context keys of the window tab: the extensions set theirs there, and the focused view is noted there. Nil
	// leaves them out.
	Keys *contextkey.Scope
	// Markers is the Problems panel's service and Channels the store of the Output panel; nil leaves the contributions out.
	Markers  *problems.Service
	Channels *logs.Store
	// ShowChannel selects an Output channel in the panel and shows the panel; FocusView gives the keyboard focus to a view.
	// Both may be nil.
	ShowChannel func(name string, preserveFocus bool)
	FocusView   func(viewID string)
	// FilesComplete tells whether the walk of the folder has finished (Files lists what it found so far); nil is always.
	FilesComplete func() bool
	// Editors shows the tabs and file views of the extensions in the editor area, and Languages gives the grammars; both
	// may be nil (the scope then has no editor area, or no grammars). EditorsChanged is called, on the UI thread, when the
	// title actions, the file views or the tabs of the extensions changed (an extension came, went or failed).
	// OpenURI opens a URI with the desktop; nil leaves it unavailable.
	OpenURI        func(ctx context.Context, uri string) error
	Editors        EditorHost
	Languages      LanguageSource
	EditorsChanged func()
	// NewHeadlessView makes a second, off-screen view of the workspace of the scope for Workspace().NewHeadlessView, on the UI
	// thread; nil leaves it unavailable (the scope has no workspace to show). The size was checked already.
	NewHeadlessView func(size extension.Size) (extension.HeadlessView, error)
	// ShowLog is the command the "Show Log" button of the failure notification runs; the Output panel by default.
	ShowLog string
	// Only, when it is not empty, is the id of the only extension the scope has: the scope of a window tab of an extension
	// holds the extension that owns the kind and no other (SPEC §10.15 "As built (step 11.7)"). The others are not
	// installed, never activated and not listed in the Extensions page for the scope.
	Only string
	// WindowTab is the strip's view of the tab of a scope whose Info.Kind is ScopeWindowTab; nil leaves the title, the
	// hint, the state notice and the close to nobody.
	WindowTab WindowTabHost
	// LSP is what the tab offers the language servers of its extensions (servers.go); nil leaves the servers out (the
	// providers written in Go still work).
	LSP LanguageWorkspace
	// QuickPick shows a list to choose from for an extension (QuickPick().Show); nil leaves it unavailable.
	QuickPick func(QuickPickRequest)
	// InputBox shows an input box for an extension (InputBox().Show); nil leaves it unavailable.
	InputBox func(InputBoxRequest)
}

// slot is an extension in a scope.
type slot struct {
	info    *Info
	state   State
	failure *Failure
	inst    *instance
	// installed take away what the manifest declared (commands, menu items, keys) when the extension goes.
	installed []func()
	// handlers are the handlers that Activate set for the commands the manifest declares.
	handlers map[string]extension.CommandHandler
	// viewRemovers take the views of the manifest out of the registry. They are not part of installed: the views of an
	// extension that failed stay, to show that it did, and go when it is turned off or the scope closes (removeViews).
	viewRemovers []func()
	// editors are the editor kinds, title actions and file views the manifest declares, while the extension is on.
	editors *editorDecls
}

// Scope is the extension host of one window tab (SPEC §10.15 "Lifetime"): it activates each extension at most once, with
// an extension.API bound to the tab, and deactivates them when the tab closes. Make one with Host.NewScope. Its methods
// are for the UI thread, except where the contrary is said.
type Scope struct {
	h      *Host
	opts   ScopeOptions
	log    *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc

	// bus is the event bus of the scope (events.go, bus.go).
	bus *bus

	// denied are the questions the user answered with Deny since the extension was activated (sharing.go).
	denyMu sync.Mutex
	denied map[denyKey]bool

	mu     sync.Mutex
	slots  []*slot
	fired  map[string]bool
	closed bool

	// viewDecls are the views the extensions of the scope declare, by id, and viewStates the state they saved (views.go).
	viewDecls  map[string]*viewDecl
	viewStates map[string]json.RawMessage
	// kindDecls are the editor kinds by id, tabs the editor tabs by instance id and fileViewInstances the file views that
	// are open in editor tabs (editors.go).
	kindDecls         map[string]*kindDecl
	tabs              map[string]*editorTab
	fileViewInstances []*fileViewInstance
	// wtab is the window tab of a scope whose Info.Kind is ScopeWindowTab (windowtabs.go).
	wtab *windowTab
	// features is the registry of the language feature providers of the scope (servers.go, providers.go).
	features *langfeatures.Registry
}

// NewScope makes the scope of a window tab. The manifests of the extensions that are enabled are installed (their
// commands are in the palette, their menu items and keys are there) but nothing is activated before Start.
func (h *Host) NewScope(opts ScopeOptions) *Scope {
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	if opts.Logger == nil {
		opts.Logger = h.log
	}
	if opts.ShowLog == "" {
		opts.ShowLog = "workbench.panel.output.focus"
	}
	s := &Scope{h: h, opts: opts, log: opts.Logger, fired: map[string]bool{}, features: langfeatures.NewRegistry()}
	s.ctx, s.cancel = context.WithCancel(opts.Ctx)
	s.bus = newBus(s)
	disabled := s.disabledSet()
	h.SyncLanguages(s.disabledIDs())
	for _, in := range h.infos {
		if opts.Only != "" && in.Manifest.ID != opts.Only {
			continue
		}
		sl := &slot{info: in, handlers: map[string]extension.CommandHandler{}}
		if disabled[in.Manifest.ID] {
			sl.state = StateDisabled
		} else {
			s.install(sl)
		}
		s.slots = append(s.slots, sl)
	}
	if opts.Info.Kind == extension.ScopeWindowTab {
		s.initWindowTab()
	}
	h.addScope(s)
	return s
}

// Info describes the scope.
func (s *Scope) Info() extension.ScopeInfo { return s.opts.Info }

// Ctx is the context of the scope; it ends when the scope is closed.
func (s *Scope) Ctx() context.Context { return s.ctx }

// Start activates the extensions that want onStartup, and those whose onFile glob matches a file of the folder, then
// publishes emetos.workspace.opened, which they hear when it is delivered, right after.
func (s *Scope) Start() {
	s.Fire(extension.OnStartup)
	if s.wtab != nil {
		s.Fire(extension.OnWindowTab(s.wtab.spec.ID))
	}
	s.CheckFiles()
	if s.opts.Info.Kind == extension.ScopeWorkspace {
		if err := s.PublishCore(extension.TopicWorkspaceOpened, s.workspaceInfo()); err != nil {
			s.log.Error("the workspace event cannot be published", "err", err)
		}
	}
}

// Fire says that an activation event happened in the scope and activates the enabled extensions that list it:
// Fire(extension.OnLanguage("go")) when a Go document opens. An event other than onCommand is remembered, so that an
// extension that the user turns on later is activated for what has happened already.
func (s *Scope) Fire(event string) {
	name, _ := extension.SplitEvent(event)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if name != "onCommand" {
		s.fired[event] = true
	}
	var wanted []*slot
	for _, sl := range s.slots {
		if sl.state == StateInactive && (slices.Contains(sl.info.Manifest.ActivationEvents, event) || declaresView(sl.info.Manifest, event)) {
			wanted = append(wanted, sl)
		}
	}
	s.mu.Unlock()
	for _, sl := range wanted {
		s.activate(sl)
	}
}

// declaresView tells whether the activation event is onView of a view that the manifest declares: showing a declared view
// activates its extension without the manifest listing the event. The same holds for onEditor and the editor kinds, and
// for onWindowTab and the window tab kinds.
func declaresView(m extension.Manifest, event string) bool {
	name, id := extension.SplitEvent(event)
	if name == "onWindowTab" {
		for _, k := range m.Contributes.WindowTabKinds {
			if k.ID == id {
				return true
			}
		}
		return false
	}
	if name == "onEditor" {
		for _, k := range m.Contributes.EditorKinds {
			if k.ID == id {
				return true
			}
		}
		return false
	}
	if name != "onView" {
		return false
	}
	for _, v := range m.Contributes.Views {
		if v.ID == id {
			return true
		}
	}
	for _, v := range m.Contributes.PanelViews {
		if v.ID == id {
			return true
		}
	}
	for _, v := range m.Contributes.FileViews {
		if v.ID == id {
			return true
		}
	}
	return false
}

// CheckFiles looks at the files of the folder (ScopeOptions.Files) and activates the extensions whose onFile glob
// matches one. The window calls it when the list of files changes.
func (s *Scope) CheckFiles() {
	if s.opts.Files == nil {
		return
	}
	s.mu.Lock()
	var waiting []*slot
	for _, sl := range s.slots {
		if sl.state == StateInactive && hasFileEvent(sl.info.Manifest) {
			waiting = append(waiting, sl)
		}
	}
	s.mu.Unlock()
	if len(waiting) == 0 {
		return
	}
	files := s.opts.Files()
	for _, sl := range waiting {
		if matchesFiles(sl.info.Manifest, files) {
			s.activate(sl)
		}
	}
}

// hasFileEvent tells whether a manifest lists an onFile event.
func hasFileEvent(m extension.Manifest) bool {
	for _, e := range m.ActivationEvents {
		if name, _ := extension.SplitEvent(e); name == "onFile" {
			return true
		}
	}
	return false
}

// matchesFiles tells whether one of the onFile globs of a manifest matches one of the files.
func matchesFiles(m extension.Manifest, files []string) bool {
	for _, e := range m.ActivationEvents {
		name, pattern := extension.SplitEvent(e)
		if name != "onFile" {
			continue
		}
		matcher := glob.Parse(pattern, glob.Options{})
		for _, f := range files {
			if matcher.Match(f) {
				return true
			}
		}
	}
	return false
}

// Refresh follows extensions.disabled: an extension that was turned off is deactivated and takes what it contributed
// away, one that was turned on is installed again and activated for the events that have happened in the scope
// (a failure of the past is forgotten). The window calls it when the setting changes.
func (s *Scope) Refresh() {
	disabled := s.disabledSet()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	var off, on []*slot
	for _, sl := range s.slots {
		switch {
		case disabled[sl.info.Manifest.ID] && sl.state != StateDisabled:
			off = append(off, sl)
		case !disabled[sl.info.Manifest.ID] && sl.state == StateDisabled:
			on = append(on, sl)
		}
	}
	s.mu.Unlock()
	for _, sl := range off {
		s.deactivate(sl, StateDisabled)
	}
	for _, sl := range on {
		s.mu.Lock()
		sl.state, sl.failure = StateInactive, nil
		s.mu.Unlock()
		s.removeViews(sl) // the views a failure left
		s.install(sl)
		s.replay(sl)
	}
	if len(off)+len(on) > 0 {
		s.h.changed()
		s.windowTabChanged()
	}
}

// replay activates an extension that was just turned on for the events that have already happened.
func (s *Scope) replay(sl *slot) {
	s.mu.Lock()
	wanted := false
	for _, e := range sl.info.Manifest.ActivationEvents {
		if s.fired[e] {
			wanted = true
		}
	}
	for e := range s.fired {
		// showing a declared view, editor kind or window tab activates the extension without the manifest listing the event
		if declaresView(sl.info.Manifest, e) {
			wanted = true
		}
	}
	s.mu.Unlock()
	if wanted {
		s.activate(sl)
		return
	}
	if hasFileEvent(sl.info.Manifest) {
		s.CheckFiles()
	}
}

// Close deactivates every extension of the scope: their contexts are cancelled and what they contributed is removed.
// The window calls it when the tab closes. A second call does nothing.
func (s *Scope) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	slots := slices.Clone(s.slots)
	s.mu.Unlock()
	// the extensions hear that the workspace closes before they go: everything queued is delivered now
	if s.opts.Info.Kind == extension.ScopeWorkspace {
		if err := s.PublishCore(extension.TopicWorkspaceClosed, s.workspaceInfo()); err != nil {
			s.log.Error("the workspace event cannot be published", "err", err)
		}
	}
	s.bus.flush()
	s.bus.close()
	for _, sl := range slots {
		s.deactivate(sl, StateInactive)
	}
	s.cancel()
	s.h.removeScope(s)
}

// State is the state of an extension in the scope; an unknown id is inactive.
func (s *Scope) State(id string) State {
	st, _, _ := s.status(id)
	return st
}

// Failure is why the extension is off in the scope, or nil.
func (s *Scope) Failure(id string) *Failure {
	_, f, _ := s.status(id)
	return f
}

// status is the state and the failure of an extension in the scope; found is false for an extension the scope does not
// have (the scope of a window tab holds its owner only).
func (s *Scope) status(id string) (st State, f *Failure, found bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sl := range s.slots {
		if sl.info.Manifest.ID == id {
			return sl.state, sl.failure, true
		}
	}
	return StateInactive, nil, false
}

// disabledIDs reads extensions.disabled.
func (s *Scope) disabledIDs() []string {
	if s.opts.Disabled == nil {
		return nil
	}
	return s.opts.Disabled()
}

// disabledSet reads extensions.disabled.
func (s *Scope) disabledSet() map[string]bool {
	out := map[string]bool{}
	for _, id := range s.disabledIDs() {
		out[id] = true
	}
	return out
}

// post runs f on the UI thread.
func (s *Scope) post(f func()) {
	if s.opts.Post != nil {
		s.opts.Post(f)
		return
	}
	f()
}

// ---- what the manifest declares ----

// install adds the commands, the menu items and the keys that the manifest of an extension declares, so that they are
// there before it is activated. A contribution that cannot be added (a command of the editor with the same id, a
// when-clause that does not parse) is logged and left out; the others stay.
func (s *Scope) install(sl *slot) {
	m := sl.info.Manifest
	var removers []func()
	if m.Contributes.Languages != nil {
		// the scope holds the languages of the pack; the last to let go takes them out of the registry
		s.h.holdLanguages(m.ID)
		removers = append(removers, func() { s.h.releaseLanguages(m.ID) })
	}
	commands, menus := m.Contributes.Commands, m.Contributes.Menus
	if s.opts.Commands == nil { // a scope with no registry of commands (a test of the languages) installs no command or menu item
		commands, menus = nil, nil
	}
	for _, spec := range commands {
		meta, err := commandMeta(spec)
		if err == nil {
			err = s.opts.Commands.Register(meta)
		}
		if err != nil {
			s.log.Warn("a command of an extension is left out", "extension", m.ID, "command", spec.ID, "err", err)
			continue
		}
		removeHandler, err := s.opts.Commands.SetHandler(spec.ID, s.declaredHandler(sl, spec.ID))
		if err != nil {
			s.log.Warn("a command of an extension has no handler", "extension", m.ID, "command", spec.ID, "err", err)
			continue
		}
		id := spec.ID
		removers = append(removers, func() { removeHandler(); s.opts.Commands.Unregister(id) })
	}
	for _, item := range menus {
		remove, err := s.addMenu(item)
		if err != nil {
			s.log.Warn("a menu item of an extension is left out", "extension", m.ID, "menu", item.Menu, "command", item.Command, "err", err)
			continue
		}
		removers = append(removers, remove)
	}
	for _, kb := range m.Contributes.Keybindings {
		remove, err := s.addKeybinding(kb)
		if err != nil {
			s.log.Warn("a keybinding of an extension is left out", "extension", m.ID, "command", kb.Command, "key", kb.Key, "err", err)
			continue
		}
		removers = append(removers, remove)
	}
	removers = append(removers, s.installSettings(sl)...)
	removers = append(removers, s.installServers(sl)...)
	registry, others := s.installViews(sl)
	removers = append(removers, others...)
	removers = append(removers, s.installEditors(sl))
	s.mu.Lock()
	sl.installed = removers
	sl.viewRemovers = registry
	s.mu.Unlock()
}

// declaredHandler is the handler of a command that the manifest declares: it activates the extension when it is not
// active, and then runs the handler that Activate set. An activation that fails has been reported, so the command does
// nothing; an active extension that set no handler fails with ExtensionCommandUnhandled.
func (s *Scope) declaredHandler(sl *slot, id string) command.Handler {
	return func(cc command.Context, args any) error {
		if s.State(sl.info.Manifest.ID) == StateInactive {
			s.activate(sl)
		}
		s.mu.Lock()
		inst, h := sl.inst, sl.handlers[id]
		s.mu.Unlock()
		if inst == nil {
			return nil
		}
		if h == nil {
			return errors.New("ExtensionCommandUnhandled", "the extension declares the command but set no handler for it", "extension", sl.info.Manifest.ID, "command", id)
		}
		return s.runCommand(inst, id, h, cc, args)
	}
}

// commandMeta turns the declaration of a command into the registry's.
func commandMeta(spec extension.CommandSpec) (command.Meta, error) {
	when, err := parseWhen(spec.When)
	if err != nil {
		return command.Meta{}, err
	}
	return command.Meta{ID: spec.ID, Title: spec.Title, Category: spec.Category, Description: spec.Description, Precondition: when}, nil
}

// parseWhen parses a when-clause; empty is none. The registry's error is returned as it is.
func parseWhen(text string) (contextkey.Expr, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	return contextkey.Parse(text)
}

// addMenu adds a menu item and returns the function that removes it.
func (s *Scope) addMenu(item extension.MenuItem) (remove func(), err error) {
	when, err := parseWhen(item.When)
	if err != nil {
		return nil, err
	}
	return s.opts.Menus.AddRemovable(command.MenuItem{
		Menu: item.Menu, Command: item.Command, Title: item.Title, Group: item.Group, Order: item.Order, When: when, Args: item.Args,
	})
}

// addKeybinding adds a default keybinding and returns the function that removes it. It fails with
// ExtensionKeybindingInvalid for a key that is empty; the parser's errors are returned as they are.
func (s *Scope) addKeybinding(b extension.Keybinding) (remove func(), err error) {
	kb, err := keybinding.ParseKeybinding(b.Key)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return nil, errors.New("ExtensionKeybindingInvalid", "the key of a keybinding is empty", "command", b.Command)
	}
	when, err := parseWhen(b.When)
	if err != nil {
		return nil, err
	}
	if s.opts.Keybindings == nil {
		return func() {}, nil
	}
	return s.opts.Keybindings.Add(keybinding.NewItem(kb, b.Command, b.Args, when, true)), nil
}

// uninstall takes away what the manifest declared.
func (s *Scope) uninstall(sl *slot) {
	s.mu.Lock()
	removers := sl.installed
	sl.installed = nil
	s.mu.Unlock()
	for i := len(removers) - 1; i >= 0; i-- {
		removers[i]()
	}
}
