package ui

import (
	"github.com/rm4n0s/glutter/widgets"
)

// BoxScope is the inherited widget the workbench puts at the top of the box of a view. It carries the function that fails
// the extension when a widget of the view panics (see NewBuilder). The host makes it; an extension only uses NewBuilder.
type BoxScope struct {
	widgets.InheritedWidget
	viewID string
	fail   func(recovered any)
}

// NewBoxScope makes the scope of the box of the view with the id: fail is called, on the UI thread, with the value a
// guarded builder recovered from.
func NewBoxScope(viewID string, fail func(recovered any), child widgets.Widget) *BoxScope {
	return &BoxScope{InheritedWidget: widgets.NewInheritedWidgetBase(child), viewID: viewID, fail: fail}
}

// UpdateShouldNotify implements widgets.InheritedWidgetInterface: nothing depends on the scope's contents.
func (s *BoxScope) UpdateShouldNotify(widgets.Widget) bool { return false }

// ViewID is the id of the view the box belongs to.
func (s *BoxScope) ViewID() string { return s.viewID }

// NewBuilder is widgets.NewBuilder with a recovery point: when build panics, the extension that owns the view fails in
// its scope (SPEC §10.15 "Lifetime": the view shows "<Name> failed") and the part of the view that failed is empty. Outside
// a box (a widget test) a panic is not caught.
//
// Glutter: a panic in the build of a widget is caught by Glutter, which replaces that widget with an error widget (the
// builder of the BuildOwner, one for the whole app) and reports it to OnError; there is no way to say that the widgets below a
// certain one report their failures to it, so the host cannot tell that an error widget in a view is the extension's. Glutter
// should provide an error boundary (a widget whose subtree reports its build errors to it, with the widget that takes the
// subtree's place), which would make this function and the guard in it unnecessary.
func NewBuilder(build func(bc widgets.BuildContext) widgets.Widget) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) (built widgets.Widget) {
		scope, ok := widgets.GetInherited[*BoxScope](bc)
		if !ok {
			return build(bc)
		}
		defer func() {
			if r := recover(); r != nil {
				scope.fail(r)
				built = widgets.NewSizedBox(nil)
			}
		}()
		return build(bc)
	})
}
