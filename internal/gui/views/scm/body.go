package scm

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Keys of the widgets of the view, which tests find them by.
var (
	// ListKey is the key of the list of the Changes view and RepositoriesListKey that of the Repositories view.
	ListKey = foundation.NewValueKey("scm:list")
	// RepositoriesListKey is the key of the list of the Repositories view.
	RepositoriesListKey = foundation.NewValueKey("scm:repositories")
	// WelcomeKey is the key of the welcome content of an empty view.
	WelcomeKey = foundation.NewValueKey("scm:welcome")
	// BusyKey is the key of the progress bar of a repository that runs a command.
	BusyKey = foundation.NewValueKey("scm:busy")
)

// InputKey is the key of the row of the input of a source control and ActionButtonKey that of its action button.
func InputKey(src *scmcore.Source) foundation.Key {
	return foundation.NewValueKey("scm:input:" + src.Key())
}

// ActionButtonKey is the key of the action button of a source control.
func ActionButtonKey(src *scmcore.Source) foundation.Key {
	return foundation.NewValueKey("scm:button:" + src.Key())
}

// BodyOpts are the options of NewBody.
type BodyOpts struct {
	// Welcome builds the content of the view while there is no repository or the list has no row (a repository that waits for the
	// trust question has no group: the welcome content tells how to trust it); nil shows the list.
	Welcome func(bc widgets.BuildContext) widgets.Widget
}

// Body is the body of the Changes view: the list of its rows, or the welcome content while there is no repository. A repository
// that runs a command has the progress bar of an infinite task on top of the list.
type Body struct {
	widgets.StatefulWidget
	view *View
	opts BodyOpts
}

// NewBody makes the body of the Changes view for a view.
func NewBody(view *View, opts ...BodyOpts) *Body {
	b := &Body{StatefulWidget: widgets.NewStatefulWidgetBase(), view: view}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	return b
}

// CreateState implements widgets.StatefulWidget.
func (b *Body) CreateState() widgets.State { return &bodyState{} }

type bodyState struct {
	widgets.StateOf[*Body]
	stop func()
}

// InitState follows the view.
func (s *bodyState) InitState() { s.follow() }

func (s *bodyState) follow() {
	s.stop = s.Widget().view.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// DidUpdateWidget follows another view.
func (s *bodyState) DidUpdateWidget(old *Body) {
	if old.view != s.Widget().view {
		s.stop()
		s.follow()
	}
}

// Dispose stops following the view.
func (s *bodyState) Dispose() { s.stop() }

// Build implements widgets.State.
func (s *bodyState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	v := w.view
	if (v.Empty() || v.RowCount() == 0) && w.opts.Welcome != nil {
		if welcome := w.opts.Welcome(bc); welcome != nil {
			return widgets.NewKeyedSubtree(welcome, widgets.WidgetOpts{Key: WelcomeKey})
		}
	}
	var list widgets.Widget = kit.NewList(v.list, kit.ListOpts{
		Key: ListKey, Controller: v.ctl, MultiSelect: true, ForceHover: v.forceHover, RowPadding: foundation.Some(0.0), TypeToFind: foundation.Some(false),
		Render: func(bc widgets.BuildContext, row kit.ListViewRow, st kit.RowState) widgets.Widget {
			return v.render(bc, row, st)
		},
		Embedded:      func(row kit.ListViewRow) bool { return embedded(row) },
		OnSelect:      v.selected,
		OnOpen:        v.opened,
		OnContextMenu: v.showContextMenu,
	})
	list = v.keys(list)
	if v.busyAbove() {
		list = widgets.NewStack([]widgets.Widget{list, widgets.NewPositioned(
			widgets.NewKeyedSubtree(widgets.NewSizedBox(kit.NewProgressBar(kit.ProgressBarOpts{Infinite: true}), widgets.SizedBoxOpts{Height: foundation.Some(2.0)}),
				widgets.WidgetOpts{Key: BusyKey}),
			widgets.PositionedOpts{Left: foundation.Some(0.0), Right: foundation.Some(0.0), Top: foundation.Some(0.0)})},
			widgets.StackOpts{Fit: rendering.StackFitExpand})
	}
	return list
}

// busyAbove tells whether a progress bar is drawn over the list: a repository runs a command and has no header of its own to show
// it in.
func (v *View) busyAbove() bool {
	if v.showRepositories(len(v.model.Visible())) {
		return false
	}
	for _, s := range v.model.Visible() {
		if s.Busy() {
			return true
		}
	}
	return false
}

// keys wraps the list in the keys of the tree that the list does not have: Left folds a row (or goes to its parent) and Right
// unfolds it.
//
// Ported from VS Code: src/vs/base/browser/ui/tree/abstractTree.ts (the tree's list.collapse and list.expand)
func (v *View) keys(list widgets.Widget) widgets.Widget {
	return widgets.NewFocus(list, widgets.FocusOpts{
		CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true), DebugLabel: "SCMKeys",
		OnKeyEvent: func(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
			if _, ok := e.(*services.KeyDownEvent); !ok {
				return widgets.KeyEventResultIgnored
			}
			switch e.LogicalKey() {
			case services.LogicalKeyboardKeyArrowLeft:
				if v.collapseFocused() {
					return widgets.KeyEventResultHandled
				}
			case services.LogicalKeyboardKeyArrowRight:
				if v.expandFocused() {
					return widgets.KeyEventResultHandled
				}
			}
			return widgets.KeyEventResultIgnored
		},
	})
}

// focusedRow is the row that has the focus, nil when there is none.
func (v *View) focusedRow() *row {
	k, ok := v.list.Focused()
	if !ok {
		return nil
	}
	it, ok := v.list.Item(k)
	if !ok {
		return nil
	}
	return it.Data.(*row)
}

// collapseFocused folds the focused row when it is open; else it moves the focus to the parent row. It says whether it did
// something.
func (v *View) collapseFocused() bool {
	r := v.focusedRow()
	if r == nil {
		return false
	}
	if r.kind != kindResource && r.expanded {
		v.setCollapsed(r, true)
		return true
	}
	return v.focusParent(r)
}

// expandFocused unfolds the focused row when it is folded; else it moves the focus to its first child.
func (v *View) expandFocused() bool {
	r := v.focusedRow()
	if r == nil || r.kind == kindResource {
		return false
	}
	if !r.expanded {
		v.setCollapsed(r, false)
		return true
	}
	i := v.list.FocusedIndex()
	if i+1 < v.RowCount() {
		if next := rowOf(v.list.Row(i + 1)); next != nil && next.depth > r.depth {
			v.list.SetFocus(v.list.Row(i + 1).Key)
			v.ctl.Reveal(v.list.Row(i + 1).Key)
			return true
		}
	}
	return false
}

// focusParent moves the focus to the closest row above with a smaller depth.
func (v *View) focusParent(r *row) bool {
	for i := v.list.FocusedIndex() - 1; i >= 0; i-- {
		if p := rowOf(v.list.Row(i)); p != nil && p.depth < r.depth {
			v.list.SetFocus(v.list.Row(i).Key)
			v.ctl.Reveal(v.list.Row(i).Key)
			return true
		}
	}
	return false
}

// Focus gives the keyboard focus to the view: to the box of the first repository when no row has the focus, else to the list
// (the focused row stays).
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts (SCMViewPane.focus)
func (v *View) Focus() {
	if _, ok := v.list.Focused(); !ok {
		for _, src := range v.model.Visible() {
			if _, has := v.list.Item("i:" + src.Key()); has && src.Input().Enabled() {
				if u, ok := v.inputs[src]; ok {
					u.ctl.Focus()
					return
				}
			}
		}
	}
	v.ctl.Focus()
}

// HasFocus tells whether the list has the keyboard focus.
func (v *View) HasFocus() bool { return v.ctl.HasFocus() }

// selected follows a click, Enter or Space on a row: a repository, group or folder folds or unfolds; a resource opens as the
// preview and keeps the focus where it is. The repository of the row becomes the focused one.
func (v *View) selected(lr kit.ListViewRow) {
	r := rowOf(lr)
	if r == nil {
		return
	}
	v.model.Focus(r.src)
	if r.kind != kindResource {
		v.toggle(r)
		return
	}
	v.open(r, OpenOptions{Preserve: true, Side: v.opts.Side != nil && v.opts.Side()})
}

// opened follows Enter and a double click: the resource opens as a kept tab and takes the focus.
func (v *View) opened(lr kit.ListViewRow) {
	r := rowOf(lr)
	if r == nil || r.kind != kindResource {
		return
	}
	v.open(r, OpenOptions{Pinned: true, Side: v.opts.Side != nil && v.opts.Side()})
}

// open opens a resource: its command when it has one (with the options in its argument when that is an object), else the file.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts (open)
func (v *View) open(r *row, o OpenOptions) {
	if c := r.res.Command; c != nil && c.ID != "" {
		args := c.Args
		if args == nil {
			// VS Code gives the command the resource it belongs to; a command with no arguments of its own is given it here
			args = scmcore.ResourceArg(r.res)
		}
		v.run(c.ID, withOpenOptions(args, o))
		return
	}
	if v.opts.OpenResource != nil {
		v.opts.OpenResource(r.res.URI, o)
	}
}

// withOpenOptions adds how the editor is opened to the argument of a command that is an object (preserveFocus, pinned,
// sideBySide), as VS Code passes the open event after the arguments; another argument is left as it is.
func withOpenOptions(args any, o OpenOptions) any {
	m, ok := args.(map[string]any)
	if args != nil && !ok {
		return args
	}
	out := map[string]any{}
	for k, val := range m {
		out[k] = val
	}
	for k, val := range map[string]bool{"preserveFocus": o.Preserve, "pinned": o.Pinned, "sideBySide": o.Side} {
		if _, has := out[k]; !has {
			out[k] = val
		}
	}
	return out
}

var _ = scmcore.ViewList
