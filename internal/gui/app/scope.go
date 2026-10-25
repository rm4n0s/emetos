package app

import (
	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"
)

// WorkspaceScope is the inherited widget that gives its subtree the Window it belongs to
// (SPEC §5.1). The core never sees it: a widget reads the window with WindowOf and passes
// plain values down to the core.
type WorkspaceScope struct {
	widgets.InheritedWidget
	window *Workspace
}

// NewWorkspaceScope makes a scope for the window.
func NewWorkspaceScope(window *Workspace, child widgets.Widget) *WorkspaceScope {
	return &WorkspaceScope{InheritedWidget: widgets.NewInheritedWidgetBase(child), window: window}
}

// UpdateShouldNotify implements widgets.InheritedWidgetInterface.
func (s *WorkspaceScope) UpdateShouldNotify(old widgets.Widget) bool {
	return s.window != old.(*WorkspaceScope).window
}

// WorkspaceOf returns the window above bc. It panics with the tag WindowMissing when there is
// none, which is a mistake in the widget tree and not a condition to handle.
func WorkspaceOf(bc widgets.BuildContext) *Workspace {
	scope, ok := widgets.DependOn[*WorkspaceScope](bc)
	if !ok {
		panic(errors.New("WorkspaceMissing", "no workspace is above this widget; wrap it in WorkspaceScope"))
	}
	return scope.window
}

// PeekWorkspace is WindowOf without the dependency: the caller is not rebuilt when the scope
// changes. Use it from InitState and from callbacks.
func PeekWorkspace(bc widgets.BuildContext) *Workspace {
	scope, ok := widgets.GetInherited[*WorkspaceScope](bc)
	if !ok {
		panic(errors.New("WorkspaceMissing", "no workspace is above this widget; wrap it in WorkspaceScope"))
	}
	return scope.window
}
