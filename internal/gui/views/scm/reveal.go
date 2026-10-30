package scm

import (
	"math"

	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
)

// Reveal selects the row of a file in the view and scrolls to it (scm.autoReveal): the last group that has the file of the visible
// repositories wins, its repository, group and folders are unfolded. It says whether the file is in the view.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts (onDidActiveEditorChange)
func (v *View) Reveal(uri string) bool {
	if uri == "" {
		return false
	}
	// do not select again what is selected and focused already
	if k, ok := v.list.Focused(); ok {
		if it, found := v.list.Item(k); found && v.list.IsSelected(k) {
			if r := it.Data.(*row); r.kind == kindResource && r.res.URI == uri {
				return true
			}
		}
	}
	for _, src := range v.model.Visible() {
		groups := src.Groups()
		for i := len(groups) - 1; i >= 0; i-- {
			g := groups[i]
			res := findResource(g, uri)
			if res == nil {
				continue
			}
			v.unfoldTo(src, g, res)
			key := "f:" + src.Key() + ":" + g.ID() + ":" + res.URI
			if v.list.IndexOf(key) < 0 {
				return false
			}
			v.list.SetSelection([]string{key})
			v.list.SetFocus(key)
			v.scrollTo = key
			v.revealLater()
			return true
		}
	}
	return false
}

// findResource is the resource of a group with a URI.
func findResource(g *scmcore.Group, uri string) *scmcore.Resource {
	rs := g.Resources()
	for i := range rs {
		if rs[i].URI == uri {
			return &rs[i]
		}
	}
	return nil
}

// unfoldTo unfolds what holds a resource: its repository, its group and, in the tree view mode, its folders.
func (v *View) unfoldTo(src *scmcore.Source, g *scmcore.Group, res *scmcore.Resource) {
	changed := false
	del := func(k string) {
		if v.collapsed[k] {
			delete(v.collapsed, k)
			changed = true
		}
	}
	del("r\x00" + src.StorageKey())
	del("g\x00" + src.StorageKey() + "\x00" + g.ID())
	if v.model.ViewMode() == scmcore.ViewTree {
		if n := g.Tree().NodeOf(res); n != nil {
			for _, a := range n.Ancestors() {
				del("d\x00" + src.StorageKey() + "\x00" + g.ID() + "\x00" + a.Path())
			}
		}
	}
	if changed {
		v.rebuild()
	}
}

// revealLater scrolls to the row that was revealed once the list is laid out.
func (v *View) revealLater() {
	if v.scrollTo == "" {
		return
	}
	key := v.scrollTo
	v.scrollTo = ""
	do := func() { v.ctl.Reveal(key) }
	if v.opts.AfterFrame != nil {
		v.opts.AfterFrame(do)
		return
	}
	do()
}

// ---- dragging a row into the editor area ----

// dragSlop is how far the pointer moves with the button held before the drag of a row starts.
const dragSlop = 6.0

// rowDrag is the drag of a resource row that may be going on.
type rowDrag struct {
	down    bool
	pointer int
	start   ui.Offset
	uri     string
	session DragSession
}

// dragRow wraps the content of a resource row so that a press and a move past the slop start the drag of the file into the editor
// area: the window's tab drag takes it from there and opens the file in the group it is dropped on. The state of the drag is the
// view's, because the row is built again while the pointer moves.
func (v *View) dragRow(r *row, content widgets.Widget) widgets.Widget {
	if v.opts.BeginDrag == nil || r.kind != kindResource {
		return content
	}
	uri, name := r.res.URI, r.res.Name()
	finish := func(at ui.Offset, drop bool) {
		d := &v.drag
		s := d.session
		*d = rowDrag{}
		if s != nil {
			_, _ = s.End(at, drop, func(group, index int) error { return v.openIn(uri, group, index) })
		}
	}
	return widgets.NewListener(content, widgets.ListenerOpts{
		Behavior: rendering.HitTestBehaviorTranslucent,
		OnPointerDown: func(e *gestures.PointerDownEvent) {
			if e.Buttons()&gestures.PrimaryButton != 0 {
				v.drag = rowDrag{down: true, pointer: e.Pointer(), start: e.Position(), uri: uri}
			}
		},
		OnPointerMove: func(e *gestures.PointerMoveEvent) {
			d := &v.drag
			if !d.down || e.Pointer() != d.pointer || d.uri != uri {
				return
			}
			p := e.Position()
			if d.session == nil {
				if math.Hypot(p.Dx-d.start.Dx, p.Dy-d.start.Dy) < dragSlop {
					return
				}
				s, err := v.opts.BeginDrag(name)
				if err != nil {
					*d = rowDrag{}
					return
				}
				d.session = s
			}
			d.session.Move(p)
		},
		OnPointerUp: func(e *gestures.PointerUpEvent) {
			if v.drag.down && e.Pointer() == v.drag.pointer {
				finish(e.Position(), true)
			}
		},
		OnPointerCancel: func(e *gestures.PointerCancelEvent) {
			if v.drag.down && e.Pointer() == v.drag.pointer {
				finish(e.Position(), false)
			}
		},
	})
}

// openIn opens a file in a group at a place of its strip (the end of a drag).
func (v *View) openIn(uri string, group, index int) error {
	if v.opts.OpenIn == nil {
		return nil
	}
	return v.opts.OpenIn(uri, group, index)
}
