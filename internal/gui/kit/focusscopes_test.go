package kit

import (
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

func TestFocusScopesFindTheNearestScopeAboveANode(t *testing.T) {
	outer, inner, plain := widgets.NewFocusNode(), widgets.NewFocusNode(), widgets.NewFocusNode()
	ts := gluttertest.New(t, gluttertest.Opts{})
	ts.PumpWidget(widgets.NewDirectionality(0, widgets.NewFocus(
		widgets.NewFocus(widgets.NewFocus(widgets.NewSizedBox(nil), widgets.FocusOpts{FocusNode: plain}), widgets.FocusOpts{FocusNode: inner}),
		widgets.FocusOpts{FocusNode: outer})))

	root := contextkey.NewRoot()
	outerScope, innerScope := root.Child(), root.Child()
	scopes := NewFocusScopes()
	if got := scopes.ScopeOf(plain, root); got != root {
		t.Error("no node has a scope: the fallback")
	}
	unregisterOuter := scopes.Register(outer, outerScope)
	if got := scopes.ScopeOf(plain, root); got != outerScope {
		t.Error("a node without a scope has the one of its nearest ancestor")
	}
	scopes.Register(inner, innerScope)
	if got := scopes.ScopeOf(plain, root); got != innerScope {
		t.Error("the nearest ancestor wins")
	}
	if got := scopes.ScopeOf(outer, root); got != outerScope {
		t.Error("a node has its own scope")
	}
	if got := scopes.ScopeOf(nil, root); got != root {
		t.Error("no focus: the fallback")
	}
	unregisterOuter()
	unregisterOuter()
	if got := scopes.ScopeOf(outer, root); got != root {
		t.Error("an unregistered node has no scope")
	}
	// Unregistering a node that took another scope since leaves the new one.
	unregisterOld := scopes.Register(inner, outerScope)
	scopes.Register(inner, innerScope)
	unregisterOld()
	if got := scopes.ScopeOf(inner, root); got != innerScope {
		t.Error("the old registration took the new one away")
	}
}
