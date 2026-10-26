package exthost

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// The part of the extension API that reads and controls the language servers of the manifest (SPEC §9.12 "As built (step
// 12.3)", §10.17 "As built (step 14.1)") and the list an extension may show (QuickPick).

// serverSub is a listener of the changes of a server.
type serverSub struct {
	server    string
	fn        func(extension.ServerStatus)
	cancelled atomic.Bool
}

// serversAPI is the extension.LanguageServers of one activation.
type serversAPI struct{ inst *instance }

var _ extension.LanguageServers = serversAPI{}

// LanguageServers implements extension.API.
func (a *api) LanguageServers() extension.LanguageServers { return serversAPI{a.inst} }

// declared tells whether the manifest declares a server with the id.
func (in *instance) declared(serverID string) bool {
	for _, spec := range in.slot.info.Manifest.Contributes.LanguageServers {
		if spec.ID == serverID {
			return true
		}
	}
	return false
}

// serverCtl is the controller of the server with the id, or nil while the host has not made it (the activation is running).
func (in *instance) serverCtl(serverID string) *serverCtl {
	in.mu.Lock()
	defer in.mu.Unlock()
	for _, c := range in.servers {
		if c.spec.ID == serverID {
			return c
		}
	}
	return nil
}

// Status implements extension.LanguageServers.
func (s serversAPI) Status(serverID string) (extension.ServerStatus, error) {
	in := s.inst
	if !in.declared(serverID) {
		return extension.ServerStatus{}, errors.New("ExtensionServerUnknown", "the manifest of the extension declares no language server with this id", "extension", in.id, "server", serverID)
	}
	c := in.serverCtl(serverID)
	if c == nil {
		return extension.ServerStatus{State: extension.ServerIdle}, nil
	}
	return c.extensionStatus(), nil
}

// extensionStatus is the state of the server as the extension API gives it.
func (c *serverCtl) extensionStatus() extension.ServerStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return extension.ServerStatus{
		State: extension.ServerState(c.state.String()), Starts: c.starts, Crashes: len(c.crashes), Progress: c.progressText(),
	}
}

// OnChange implements extension.LanguageServers.
func (s serversAPI) OnChange(serverID string, fn func(extension.ServerStatus)) (func(), error) {
	in := s.inst
	if !in.declared(serverID) {
		return nil, errors.New("ExtensionServerUnknown", "the manifest of the extension declares no language server with this id", "extension", in.id, "server", serverID)
	}
	if in.isDead() || fn == nil {
		return func() {}, nil
	}
	sub := &serverSub{server: serverID, fn: fn}
	in.mu.Lock()
	in.serverSubs = append(in.serverSubs, sub)
	in.mu.Unlock()
	cancel := func() {
		sub.cancelled.Store(true)
		in.mu.Lock()
		in.serverSubs = slices.DeleteFunc(in.serverSubs, func(x *serverSub) bool { return x == sub })
		in.mu.Unlock()
	}
	in.track(cancel)
	return cancel, nil
}

// Restart implements extension.LanguageServers.
func (s serversAPI) Restart(serverID string) error {
	in := s.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if !in.declared(serverID) {
		return errors.New("ExtensionServerUnknown", "the manifest of the extension declares no language server with this id", "extension", in.id, "server", serverID)
	}
	if c := in.serverCtl(serverID); c != nil {
		c.restart()
	}
	return nil
}

// ShowOutput implements extension.LanguageServers.
func (s serversAPI) ShowOutput(serverID string, preserveFocus bool) error {
	in := s.inst
	if !in.declared(serverID) {
		return errors.New("ExtensionServerUnknown", "the manifest of the extension declares no language server with this id", "extension", in.id, "server", serverID)
	}
	if c := in.serverCtl(serverID); c != nil {
		c.showOutput(preserveFocus)
	}
	return nil
}

// changed tells the listeners of the server that its state or progress changed. The call is made on the UI thread, once for a
// burst of changes, with the status as it is then.
func (c *serverCtl) changed() {
	in := c.inst
	in.mu.Lock()
	any := slices.ContainsFunc(in.serverSubs, func(x *serverSub) bool { return x.server == c.spec.ID })
	in.mu.Unlock()
	if !any || !c.changePending.CompareAndSwap(false, true) {
		return
	}
	c.s.post(func() {
		c.changePending.Store(false)
		in.mu.Lock()
		subs := slices.DeleteFunc(slices.Clone(in.serverSubs), func(x *serverSub) bool { return x.server != c.spec.ID })
		in.mu.Unlock()
		st := c.extensionStatus()
		for _, sub := range subs {
			if !sub.cancelled.Load() {
				c.s.runServerListener(in, sub.fn, st)
			}
		}
	})
}

// runServerListener calls a function that an extension gave to hear about a server. A panic fails the extension with
// ExtensionServerCallbackPanicked.
func (s *Scope) runServerListener(inst *instance, f func(extension.ServerStatus), st extension.ServerStatus) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionServerCallbackPanicked", "it panicked in a listener of a language server: %v", r).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "language server listener")
		}
	}()
	f(st)
}

// ---- declared settings ----

// settingFromSpec is the setting of a spec. Its scope is the window's, like every setting of an extension.
func settingFromSpec(spec extension.SettingSpec, category string) (settings.Setting, bool) {
	typ, ok := settingTypes[spec.Type]
	if !ok {
		return settings.Setting{}, false
	}
	if spec.Category != "" {
		category = spec.Category
	}
	return settings.Setting{
		Key: spec.Key, Types: []settings.Type{typ}, Default: spec.Default, Description: spec.Description, Enum: spec.Enum,
		Minimum: spec.Minimum, Maximum: spec.Maximum, Scope: settings.ScopeWindow, Category: category,
	}, true
}

// installSettings registers the settings the manifest declares (Contributes.Settings) and returns the functions that take them
// away. A key that the editor or another extension has is left out, with a line in the log.
func (s *Scope) installSettings(sl *slot) []func() {
	m := sl.info.Manifest
	var removers []func()
	for _, spec := range m.Contributes.Settings {
		def, ok := settingFromSpec(spec, m.Name)
		if !ok {
			continue
		}
		if err := s.h.holdSetting(m.ID, def); err != nil {
			s.log.Warn("a setting an extension declares is left out", "extension", m.ID, "key", spec.Key, "err", err)
			continue
		}
		key := spec.Key
		removers = append(removers, func() { s.h.releaseSetting(key) })
	}
	return removers
}

// ---- QuickPick ----

// QuickPickEntry is a row of a list the window shows for an extension.
type QuickPickEntry struct {
	Label, Description, Detail string
}

// QuickPickRequest is what a window shows for QuickPick().Show: the list, and what it calls when it ends. Chosen is called
// with the index in Items of the entry, after the list is hidden; Hidden is called once when the list ends without a choice
// (closed, or replaced by another quick input).
type QuickPickRequest struct {
	Placeholder        string
	Items              []QuickPickEntry
	MatchOnDescription bool
	Chosen             func(index int)
	Hidden             func()
}

// quickPickAPI is the extension.QuickPick of one activation.
type quickPickAPI struct{ inst *instance }

// QuickPick implements extension.API.
func (a *api) QuickPick() extension.QuickPick { return quickPickAPI{a.inst} }

// Show implements extension.QuickPick.
func (q quickPickAPI) Show(opts extension.QuickPickOptions, done func(index int)) error {
	in := q.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	show := in.scope.opts.QuickPick
	if show == nil {
		return errors.New("ExtensionQuickPickUnavailable", "the scope cannot show a list to choose from", "extension", in.id)
	}
	var entries []QuickPickEntry
	var index []int
	for i, it := range opts.Items {
		if it.Label == "" {
			continue
		}
		entries = append(entries, QuickPickEntry{Label: it.Label, Description: it.Description, Detail: it.Detail})
		index = append(index, i)
	}
	if len(entries) == 0 {
		return errors.New("ExtensionQuickPickInvalid", "a quick pick needs at least one item with a label", "extension", in.id)
	}
	var once sync.Once
	finish := func(i int) {
		once.Do(func() {
			if done != nil {
				in.scope.runQuickPickDone(in, done, i)
			}
		})
	}
	show(QuickPickRequest{
		Placeholder: opts.Placeholder, Items: entries, MatchOnDescription: opts.MatchOnDescription,
		Chosen: func(i int) {
			if i < 0 || i >= len(index) || in.isDead() {
				finish(-1)
				return
			}
			chosen := opts.Items[index[i]]
			if chosen.Command != "" && in.scope.opts.Commands != nil {
				ctx := command.Context{Ctx: in.ctx, Source: command.SourceAPI}
				if err := in.scope.opts.Commands.Run(ctx, chosen.Command, chosen.Args); err != nil {
					in.log.Warn("the command of a quick pick item failed", "command", chosen.Command, "err", err)
				}
			}
			finish(index[i])
		},
		Hidden: func() { finish(-1) },
	})
	return nil
}

// runQuickPickDone calls the function an extension gave to hear the end of a quick pick. A panic fails the extension with
// ExtensionQuickPickCallbackPanicked.
func (s *Scope) runQuickPickDone(inst *instance, f func(int), index int) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionQuickPickCallbackPanicked", "it panicked in the function that hears the end of a quick pick: %v", r).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "quick pick")
		}
	}()
	f(index)
}
