package command

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/errors"
)

// Source tells what asked for a command to run.
type Source int

// The things that run commands.
const (
	// SourceAPI is code calling the registry directly.
	SourceAPI Source = iota
	// SourceKeybinding is a key press resolved by the keybinding dispatcher.
	SourceKeybinding
	// SourceMenu is a menu entry.
	SourceMenu
	// SourcePalette is the command palette.
	SourcePalette
	// SourceStatusBar is a click on an item of the status bar.
	SourceStatusBar
)

// Context is what a command handler receives: the Go context for cancellation, the context
// keys of the part of the window that was focused when the command was asked for, and
// where the request came from.
type Context struct {
	// Ctx is cancelled when the work of the command is no longer wanted.
	Ctx context.Context
	// Keys are the context keys at the time of the request; may be nil.
	Keys contextkey.Context
	// Source tells what asked for the command.
	Source Source
	// Origin is the screen the command was asked on: the window of the host (the zero value) or the view of a client of a hosted
	// workspace. A registry that forwards a command to its parent sets it (Registry.Inherit), and the extension host keeps it for the
	// questions the command's handler asks (SPEC §10.18 "As built (step 24.25)").
	Origin extension.Origin
}

// Handler runs a command. args is a JSON-compatible value (nil, bool, float64, string, a
// slice or a map) or nil when the caller passed none.
type Handler func(ctx Context, args any) error

// Meta describes a command. A command is registered with its Meta first; the package that
// implements it attaches the handler later with SetHandler (SPEC §11.2).
//
// Ported from VS Code: src/vs/platform/commands/common/commands.ts
type Meta struct {
	// ID is the command id. It is VS Code's id where the command exists in VS Code.
	ID string
	// Title is the name shown in the command palette and the keybindings UI. It is empty for
	// a command VS Code does not list (the cursor commands), which the palette skips.
	Title string
	// Category groups commands in the palette, such as "View"; may be empty.
	Category string
	// Precondition is the when-clause under which the command is enabled; nil means always.
	Precondition contextkey.Expr
	// Description says what the command does, for the keybindings UI; may be empty.
	Description string
	// Icon is the name of the Codicon the command shows as where a menu draws an icon in place of the title (the inline
	// actions and the title actions of a view: VS Code's command icon); empty for none.
	Icon string
}

// entry is a registered command: its Meta and the stack of handlers, the last wins.
type entry struct {
	meta     Meta
	handlers []*Handler
}

// Registry holds the commands of a window. It is safe for concurrent use. Make one with
// NewRegistry; there is no global instance.
type Registry struct {
	mu       sync.RWMutex
	commands map[string]*entry
	// parent, when set, is the registry whose commands this one does not have are forwarded to, with origin (Inherit).
	parent *Registry
	origin extension.Origin
}

// NewRegistry makes an empty registry.
func NewRegistry() *Registry {
	return &Registry{commands: map[string]*entry{}}
}

// Register adds the metadata of a command. The id must not be empty, start with "-" or "^"
// (keybindings read those as a removal and as bubbling), or contain white space, and must
// not be registered yet.
func (r *Registry) Register(m Meta) error {
	if m.ID == "" || strings.ContainsAny(m.ID, " \t\r\n") || strings.HasPrefix(m.ID, "-") || strings.HasPrefix(m.ID, "^") {
		return errors.New("CommandIDInvalid", "the id of a command is empty, has white space or starts with - or ^", "id", m.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.commands[m.ID]; ok {
		return errors.New("CommandDuplicate", "the command is already registered", "id", m.ID)
	}
	r.commands[m.ID] = &entry{meta: m}
	return nil
}

// MustRegister is Register for the table of built-in commands; it panics on an invalid or
// duplicate command.
func (r *Registry) MustRegister(metas ...Meta) {
	for _, m := range metas {
		if err := r.Register(m); err != nil {
			panic(err)
		}
	}
}

// SetHandler attaches a handler to a registered command. Setting a handler on a command
// that has one stacks it: the last one set runs, and when its remover is called the one
// before it runs again (an editor widget takes over a command while it has focus). The
// remover may be called more than once.
func (r *Registry) SetHandler(id string, h Handler) (remove func(), err error) {
	if h == nil {
		return nil, errors.New("CommandHandlerInvalid", "the handler of a command is nil", "id", id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.commands[id]
	if !ok {
		return nil, errors.New("CommandNotFound", "no command has this id", "id", id)
	}
	hp := &h
	e.handlers = append(e.handlers, hp)
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if i := slices.Index(e.handlers, hp); i >= 0 {
			e.handlers = slices.Delete(e.handlers, i, i+1)
		}
	}, nil
}

// Unregister removes a command and its handlers, and reports whether it was registered. It is how an extension's
// commands go when the extension is deactivated (SPEC §10.15); the commands of the editor are never removed.
func (r *Registry) Unregister(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.commands[id]
	delete(r.commands, id)
	return ok
}

// Fork makes a registry with the same commands and the handlers they have now: the stack of each command is
// copied, so that setting or removing a handler in the fork does not touch this registry, nor the other way
// round. The windows of the process each have a fork (SPEC §5.1): the commands are the process's, the handlers
// that the shell of a window sets are the window's.
func (r *Registry) Fork() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := &Registry{commands: make(map[string]*entry, len(r.commands))}
	for id, e := range r.commands {
		out.commands[id] = &entry{meta: e.meta, handlers: slices.Clone(e.handlers)}
	}
	return out
}

// Lookup returns the metadata of a command.
func (r *Registry) Lookup(id string) (Meta, bool) {
	r.mu.RLock()
	e, ok := r.commands[id]
	r.mu.RUnlock()
	if !ok {
		if r.inherits(id) {
			return r.parent.Lookup(id)
		}
		return Meta{}, false
	}
	return e.meta, true
}

// Inherit makes the commands of parent that this registry does not have its own, as forwards: Lookup, Enabled, HasHandler, All and Run see them,
// and Run runs the parent's handler with origin in the context. It is how the view of a client of a hosted workspace has the commands of the
// extensions that the scope of the workspace registered in the registry of the window (SPEC §10.18 "As built (step 24.25)", D50). A command
// that ClientRefusal refuses is not inherited (Run fails with CommandRefused). Call it once, before the registry is used.
func (r *Registry) Inherit(parent *Registry, origin extension.Origin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parent, r.origin = parent, origin
}

// inherits tells whether the command is one the parent gives: this registry has none of the id, the parent has one, and it is not refused.
func (r *Registry) inherits(id string) bool {
	if r.parent == nil {
		return false
	}
	if _, refused := ClientRefusal(id); refused {
		return false
	}
	_, ok := r.parent.Lookup(id)
	return ok
}

// HasHandler tells whether a command is registered and has a handler.
func (r *Registry) HasHandler(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.commands[id]
	if !ok {
		return r.inherits(id) && r.parent.HasHandler(id)
	}
	return len(e.handlers) > 0
}

// All lists the registered commands ordered by id.
func (r *Registry) All() []Meta {
	r.mu.RLock()
	out := make([]Meta, 0, len(r.commands))
	for _, e := range r.commands {
		out = append(out, e.meta)
	}
	r.mu.RUnlock()
	if r.parent != nil {
		for _, m := range r.parent.All() {
			r.mu.RLock()
			_, own := r.commands[m.ID]
			r.mu.RUnlock()
			if _, refused := ClientRefusal(m.ID); !own && !refused {
				out = append(out, m)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Enabled tells whether the precondition of a command holds in keys. It is false for an
// unknown command. Menus and the palette use it; Run does not check it, as in VS Code where
// the keybinding and the menu check before they ask for the command.
func (r *Registry) Enabled(id string, keys contextkey.Context) bool {
	m, ok := r.Lookup(id)
	if !ok {
		return false
	}
	return m.Precondition == nil || m.Precondition.Evaluate(keys)
}

// Run runs a command by id with optional arguments. It fails when the id is unknown or no
// handler is attached; the error of the handler is returned as it is.
func (r *Registry) Run(ctx Context, id string, args any) error {
	r.mu.RLock()
	e, ok := r.commands[id]
	var h *Handler
	if ok && len(e.handlers) > 0 {
		h = e.handlers[len(e.handlers)-1]
	}
	r.mu.RUnlock()
	if !ok {
		if r.parent != nil {
			if _, has := r.parent.Lookup(id); has {
				if refusal, refused := ClientRefusal(id); refused {
					return errors.New("CommandRefused", "a client may not run this command: "+refusal.Reason, "id", id, "class", refusal.Class)
				}
				ctx.Origin = r.origin
				return r.parent.Run(ctx, id, args)
			}
		}
		return errors.New("CommandNotFound", "no command has this id", "id", id)
	}
	if h == nil {
		return errors.New("CommandHandlerMissing", "the command has no handler", "id", id)
	}
	return (*h)(ctx, args)
}
