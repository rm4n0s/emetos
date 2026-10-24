package command

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"

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

// Lookup returns the metadata of a command.
func (r *Registry) Lookup(id string) (Meta, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.commands[id]
	if !ok {
		return Meta{}, false
	}
	return e.meta, true
}

// HasHandler tells whether a command is registered and has a handler.
func (r *Registry) HasHandler(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.commands[id]
	return ok && len(e.handlers) > 0
}

// All lists the registered commands ordered by id.
func (r *Registry) All() []Meta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Meta, 0, len(r.commands))
	for _, e := range r.commands {
		out = append(out, e.meta)
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
		return errors.New("CommandNotFound", "no command has this id", "id", id)
	}
	if h == nil {
		return errors.New("CommandHandlerMissing", "the command has no handler", "id", id)
	}
	return (*h)(ctx, args)
}
