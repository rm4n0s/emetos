package app

import (
	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"
)

// WindowScope is the inherited widget that gives its subtree the Window it belongs to
// (SPEC §5.1). The core never sees it: a widget reads the window with WindowOf and passes
// plain values down to the core.
type WindowScope struct {
	widgets.InheritedWidget
	window *Window
}

// NewWindowScope makes a scope for the window.
func NewWindowScope(window *Window, child widgets.Widget) *WindowScope {
	return &WindowScope{InheritedWidget: widgets.NewInheritedWidgetBase(child), window: window}
}

// UpdateShouldNotify implements widgets.InheritedWidgetInterface.
func (s *WindowScope) UpdateShouldNotify(old widgets.Widget) bool {
	return s.window != old.(*WindowScope).window
}

// WindowOf returns the window above bc. It panics with the tag WindowMissing when there is
// none, which is a mistake in the widget tree and not a condition to handle.
func WindowOf(bc widgets.BuildContext) *Window {
	scope, ok := widgets.DependOn[*WindowScope](bc)
	if !ok {
		panic(errors.New("WindowMissing", "no window is above this widget; wrap it in WindowScope"))
	}
	return scope.window
}

// PeekWindow is WindowOf without the dependency: the caller is not rebuilt when the scope
// changes. Use it from InitState and from callbacks.
func PeekWindow(bc widgets.BuildContext) *Window {
	scope, ok := widgets.GetInherited[*WindowScope](bc)
	if !ok {
		panic(errors.New("WindowMissing", "no window is above this widget; wrap it in WindowScope"))
	}
	return scope.window
}
