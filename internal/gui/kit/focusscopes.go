package kit

import (
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// FocusScopes tells which context key scope belongs to which focus node, so that the key
// dispatcher of a window can find the scope of the widget that has the focus (SPEC §5.4): a
// widget that sets context keys (the editor: editorTextFocus, editorHasSelection, …) registers
// its focus node with its scope. A window owns one; it is used on the UI thread only.
type FocusScopes struct {
	scopes map[*widgets.FocusNode]*contextkey.Scope
}

// NewFocusScopes makes an empty registry.
func NewFocusScopes() *FocusScopes {
	return &FocusScopes{scopes: map[*widgets.FocusNode]*contextkey.Scope{}}
}

// Register gives a focus node a scope and returns the function that takes it back.
func (f *FocusScopes) Register(node *widgets.FocusNode, scope *contextkey.Scope) (unregister func()) {
	f.scopes[node] = scope
	return func() {
		if f.scopes[node] == scope {
			delete(f.scopes, node)
		}
	}
}

// ScopeOf is the scope of the nearest node, from the given one up through its ancestors, that
// has one; fallback when none has (or the node is nil).
func (f *FocusScopes) ScopeOf(node *widgets.FocusNode, fallback *contextkey.Scope) *contextkey.Scope {
	for n := node; n != nil; n = n.Parent() {
		if s, ok := f.scopes[n]; ok {
			return s
		}
	}
	return fallback
}
