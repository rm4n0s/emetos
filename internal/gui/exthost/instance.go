package exthost

import (
	"context"
	"log/slog"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/notify"
)

// instance is an extension activated in a scope: the object its factory made, the context that ends with the
// activation, and what it contributed, which is taken away when the activation ends.
type instance struct {
	scope *Scope
	slot  *slot
	id    string
	name  string
	ext   extension.Extension
	ctx   context.Context
	log   *slog.Logger

	mu        sync.Mutex
	cancel    context.CancelFunc
	dead      bool
	disposers []func()
	// settingKeys and statusIDs are what the instance registered, to refuse a second registration of the same.
	settingKeys map[string]bool
	statusIDs   map[string]bool
}

// isDead tells whether the activation ended. It is safe for concurrent use.
func (in *instance) isDead() bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.dead
}

// track keeps a function that takes a contribution away when the activation ends. It reports false, after running the
// function at once, when the activation has ended already.
func (in *instance) track(dispose func()) bool {
	in.mu.Lock()
	if in.dead {
		in.mu.Unlock()
		dispose()
		return false
	}
	in.disposers = append(in.disposers, dispose)
	in.mu.Unlock()
	return true
}

// kill ends the activation: the context is cancelled and the contributions are taken away, the last first. A second
// call does nothing.
func (in *instance) kill() {
	in.mu.Lock()
	if in.dead {
		in.mu.Unlock()
		return
	}
	in.dead = true
	disposers := in.disposers
	in.disposers = nil
	in.mu.Unlock()
	in.cancel()
	for i := len(disposers) - 1; i >= 0; i-- {
		disposers[i]()
	}
}

// activate activates an extension in the scope, once: nothing happens unless it is inactive. Every call into the
// extension is caught; a failure deactivates the extension in this scope (fail).
func (s *Scope) activate(sl *slot) {
	s.mu.Lock()
	if s.closed || sl.state != StateInactive {
		s.mu.Unlock()
		return
	}
	sl.state = StateActive
	s.mu.Unlock()

	m := sl.info.Manifest
	ext, err := s.instantiate(sl.info)
	if err != nil {
		s.fail(sl, err, "factory")
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	inst := &instance{
		scope: s, slot: sl, id: m.ID, name: m.Name, ext: ext, ctx: ctx, cancel: cancel,
		log:         s.log.With("extension", m.ID, "scope", s.opts.Info.Name),
		settingKeys: map[string]bool{}, statusIDs: map[string]bool{},
	}
	s.mu.Lock()
	sl.inst = inst
	s.mu.Unlock()
	if err := s.callActivate(inst); err != nil {
		s.fail(sl, err, "activate")
		return
	}
	inst.log.Debug("extension activated")
	s.h.changed()
}

// instantiate makes the extension of a scope with its factory. A panic fails with ExtensionFactoryPanicked, a factory
// that makes nothing with ExtensionFactoryNil.
func (s *Scope) instantiate(info *Info) (ext extension.Extension, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.NewErrf("ExtensionFactoryPanicked", "its factory panicked: %v", r).SetMetadata("extension", info.Manifest.ID)
		}
	}()
	ext = info.factory()
	if ext == nil {
		return nil, errors.New("ExtensionFactoryNil", "the factory of an extension made no extension", "extension", info.Manifest.ID)
	}
	return ext, nil
}

// callActivate calls Activate. An error fails with ExtensionActivateFailed and a panic with ExtensionActivatePanicked.
func (s *Scope) callActivate(inst *instance) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.NewErrf("ExtensionActivatePanicked", "it panicked in Activate: %v", r).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
		}
	}()
	if e := inst.ext.Activate(inst.ctx, &api{inst: inst}); e != nil {
		return errors.NewErr("ExtensionActivateFailed", e).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
	}
	return nil
}

// runCommand calls the handler of a command of an extension. An error of the handler is the error of the command
// (ExtensionCommandFailed) and is returned. A panic fails the extension (ExtensionCommandPanicked), which tells the user
// once, so the command returns nil in that case: the caller must not tell them again.
func (s *Scope) runCommand(inst *instance, id string, h extension.CommandHandler, cc command.Context, args any) (err error) {
	if inst.isDead() {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionCommandPanicked", "it panicked in the command %s: %v", id, r).SetMetadata("extension", inst.id, "command", id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "command "+id)
			err = nil
		}
	}()
	ctx := cc.Ctx
	if ctx == nil {
		ctx = inst.ctx
	}
	if e := h(ctx, args); e != nil {
		return errors.NewErr("ExtensionCommandFailed", e).SetMetadata("extension", inst.id, "command", id, "scope", s.opts.Info.Name)
	}
	return nil
}

// runStatusClick calls the click handler that an extension gave a status bar item. A panic fails the
// extension with ExtensionStatusCallbackPanicked; the where says which item.
func (s *Scope) runStatusClick(inst *instance, item string, f func()) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionStatusCallbackPanicked", "it panicked in the click handler of the status bar item %s: %v", item, r).SetMetadata("extension", inst.id, "item", item, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "status bar item "+item)
		}
	}()
	f()
}

// runNotificationAction calls the action of a notification an extension showed. A panic fails the extension with
// ExtensionNotificationActionPanicked.
func (s *Scope) runNotificationAction(inst *instance, label string, f func()) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionNotificationActionPanicked", "it panicked in the action %q of a notification: %v", label, r).SetMetadata("extension", inst.id, "action", label, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "notification action "+label)
		}
	}()
	f()
}

// runSettingsListener calls a function that an extension gave to hear about settings. A panic fails the extension with
// ExtensionSettingsCallbackPanicked.
func (s *Scope) runSettingsListener(inst *instance, f func()) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionSettingsCallbackPanicked", "it panicked in a settings listener: %v", r).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "settings listener")
		}
	}()
	f()
}

// runPosted calls a function that an extension handed to the UI thread with Post. A panic fails the extension with
// ExtensionPostedPanicked.
func (s *Scope) runPosted(inst *instance, f func()) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionPostedPanicked", "it panicked in a function it posted to the UI thread: %v", r).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "posted function")
		}
	}()
	f()
}

// fail is what happens to an extension whose call failed (SPEC §10.15 "Lifetime"): it is deactivated in this scope for
// the rest of the session, a notification says so once, and the log gets the error with its route. The other scopes
// and the other extensions are not touched. Only the first failure of an activation counts.
func (s *Scope) fail(sl *slot, err error, where string) {
	s.mu.Lock()
	if sl.state != StateActive || s.closed {
		s.mu.Unlock()
		return
	}
	sl.state = StateFailed
	m := sl.info.Manifest
	sl.failure = &Failure{Err: err, Where: where, BoxText: m.Name + " failed"}
	inst := sl.inst
	sl.inst = nil
	s.mu.Unlock()
	if inst != nil {
		inst.kill()
	}
	s.uninstall(sl)
	s.log.Error("an extension failed and is off in this scope", "extension", m.ID, "scope", s.opts.Info.Name, "where", where, "err", err)
	message := m.Name + " failed: " + describe(err)
	s.opts.Notify.Notify(notify.Notification{
		Severity: notify.Error, Source: m.Name, Message: message,
		Actions: []notify.Action{{Label: "Show Log", Command: s.opts.ShowLog}},
	})
	s.h.changed()
}

// describe is the message of an error.
func describe(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}

// deactivate ends the activation of an extension, if there is one, takes the contributions of its manifest away and sets
// the state: the scope is closing (StateInactive) or the user turned the extension off (StateDisabled).
func (s *Scope) deactivate(sl *slot, next State) {
	s.mu.Lock()
	inst := sl.inst
	sl.inst = nil
	sl.state = next
	sl.failure = nil
	s.mu.Unlock()
	if inst != nil {
		inst.kill()
	}
	s.uninstall(sl)
	s.h.changed()
}
