package exthost

import (
	"slices"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The box (SPEC §10.15 "As built (step 11.5)"): the widget the workbench puts in the body of a view of an extension, and
// the only place the widget of the extension is built. It gives the widget the constraints of the body, clips what it
// paints to them, sets the text style of the theme, keeps the keyboard focus inside (a key the widget does not handle goes
// on to the keys of the workbench, as it does from any focused widget) and catches a panic of the extension's Build.

// viewBox is the widget of the box of a view.
type viewBox struct {
	widgets.StatefulWidget
	scope *Scope
	decl  *viewDecl
}

// newViewBox makes the box of a view.
func newViewBox(s *Scope, d *viewDecl) *viewBox {
	return &viewBox{StatefulWidget: widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: foundation.NewValueKey("exthost:view:" + d.id)}), scope: s, decl: d}
}

// CreateState implements widgets.StatefulWidget.
func (b *viewBox) CreateState() widgets.State { return &viewBoxState{} }

// viewBoxState is the state of a box on screen.
type viewBoxState struct {
	widgets.StateOf[*viewBox]
	// restored says that the content of the view was given its saved state for this box.
	restored bool
}

// InitState registers the box with its view and has the extension activated.
func (s *viewBoxState) InitState() {
	b := s.Widget()
	sc := b.scope
	sc.mu.Lock()
	b.decl.boxes = append(b.decl.boxes, s)
	sc.mu.Unlock()
	sc.ViewShown(b.decl.id)
}

// Dispose saves the state of the view, which is going off screen, and unregisters the box.
func (s *viewBoxState) Dispose() {
	b := s.Widget()
	sc := b.scope
	sc.saveView(b.decl)
	sc.viewFocus(b.decl.id, false)
	sc.mu.Lock()
	b.decl.boxes = slices.DeleteFunc(b.decl.boxes, func(o *viewBoxState) bool { return o == s })
	sc.mu.Unlock()
}

// refresh builds the box again; it is called on the UI thread.
func (s *viewBoxState) refresh() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// Build implements widgets.State: the box around what the extension built, or around the failure that took its place.
func (s *viewBoxState) Build(bc widgets.BuildContext) widgets.Widget {
	b := s.Widget()
	sc, d := b.scope, b.decl
	sc.mu.Lock()
	content, inst, failure := d.content, d.inst, d.slot.failure
	failed := d.slot.state == StateFailed
	sc.mu.Unlock()
	colors := kit.ThemeOf(bc)
	var body widgets.Widget
	switch {
	case failed && failure != nil:
		body = sc.failureBox(bc, failure.BoxText)
	case content == nil || content.Build == nil || inst == nil:
		body = widgets.NewSizedBox(nil)
	default:
		if !s.restored {
			s.restored = true
			sc.restoreView(d)
		}
		body = s.build(bc, d, inst, content.Build)
	}
	scoped := ui.NewBoxScope(d.id, func(r any) { sc.failView(d, inst, "ExtensionViewWidgetPanicked", r) }, body)
	focused := widgets.NewFocus(scoped, widgets.FocusOpts{
		CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true), DebugLabel: "ExtensionView",
		OnFocusChange: func(has bool) { sc.viewFocus(d.id, has) },
	})
	return widgets.NewClipRect(widgets.NewDefaultTextStyle(kit.TextStyleOf(bc, 13, colors.Foreground), focused))
}

// build calls the Build function of the extension. A panic fails the extension with ExtensionViewBuildPanicked, and the
// box shows the failure.
func (s *viewBoxState) build(bc widgets.BuildContext, d *viewDecl, inst *instance, f func(widgets.BuildContext) widgets.Widget) (built widgets.Widget) {
	sc := s.Widget().scope
	defer func() {
		if r := recover(); r != nil {
			sc.failView(d, inst, "ExtensionViewBuildPanicked", r)
			built = sc.failureBox(bc, d.slot.info.Manifest.Name+" failed")
		}
	}()
	if built = f(bc); built == nil {
		built = widgets.NewSizedBox(nil)
	}
	return built
}

// failView fails the extension of a view whose widget panicked, on the UI thread's next turn (the tree is being built, so
// the registries must not change now). The tag is ExtensionViewBuildPanicked for the Build function and
// ExtensionViewWidgetPanicked for a guarded builder below it.
func (s *Scope) failView(d *viewDecl, inst *instance, tag string, recovered any) {
	if inst == nil || inst.isDead() {
		return
	}
	var failure error
	switch tag {
	case "ExtensionViewBuildPanicked":
		failure = errors.NewErrf("ExtensionViewBuildPanicked", "it panicked in Build of the view %s: %v", d.id, recovered).SetMetadata("extension", inst.id, "view", d.id, "scope", s.opts.Info.Name)
	default:
		failure = errors.NewErrf("ExtensionViewWidgetPanicked", "it panicked in a widget of the view %s: %v", d.id, recovered).SetMetadata("extension", inst.id, "view", d.id, "scope", s.opts.Info.Name)
	}
	s.post(func() { s.fail(d.slot, failure, "view "+d.id) })
}

// viewFocus sets the context key that says which view has the keyboard focus.
func (s *Scope) viewFocus(viewID string, has bool) {
	keys := s.opts.Keys
	if keys == nil {
		return
	}
	if has {
		keys.Set("focusedView", viewID)
	} else if keys.Get("focusedView") == viewID {
		keys.Reset("focusedView")
	}
}
