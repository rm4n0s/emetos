package debug

import (
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// newWatchKey is the key of the row in which a new watch expression is typed.
const newWatchKey = "watch:new"

// WatchPane is the Watch view: the watch expressions of the workspace, evaluated in the focused stack frame, each expanding to the
// children of its value. An expression is added, renamed and removed in the view; they are kept with the workspace.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/watchExpressionsView.ts (WatchExpressionsView,
// WatchExpressionsDataSource, WatchExpressionsRenderer)
type WatchPane struct {
	*base
	frame    *debug.StackFrame
	children map[string][]*debug.Variable
	gen      int
	// adding is set while the row of a new expression is open.
	adding bool
}

func newWatchPane(opts Opts) *WatchPane {
	w := &WatchPane{children: map[string][]*debug.Variable{}}
	w.base = newBase(opts, "workbench.debug.watchExpressionsView", kit.TreeModelOpts{LoadChildren: w.load})
	return w
}

func (w *WatchPane) refresh(ev debug.Event) {
	focus := w.opts.Service.Model().Focus()
	frameChanged := focus.Frame != w.frame
	if frameChanged || ev.Kind == debug.EventWatch || ev.Kind == 0 {
		w.frame = focus.Frame
		w.gen++
		w.children = map[string][]*debug.Variable{}
		w.rebuild()
	}
	if frameChanged || ev.Kind == debug.EventWatch {
		w.evaluate()
	}
	w.notify()
}

// evaluate evaluates the expressions in the focused frame, on a goroutine; the values are read when the views are drawn again.
func (w *WatchPane) evaluate() {
	if !w.visible() || len(w.opts.Service.Model().WatchExpressions()) == 0 {
		return
	}
	go func() {
		w.opts.Service.EvaluateWatchExpressions(w.ctx)
		w.opts.Post(func() {
			w.rebuild()
			w.notify()
		})
	}()
}

func (w *WatchPane) rebuild() {
	exprs := w.opts.Service.Model().WatchExpressions()
	w.rows = map[string]Row{}
	items := make([]kit.TreeItem, 0, len(exprs)+1)
	for _, e := range exprs {
		key := "watch:" + e.ID()
		row := Row{Kind: RowExpression, Key: key, Expression: e}
		w.rows[key] = row
		items = append(items, kit.TreeItem{Key: key, Data: row, Label: e.Name, Lazy: e.HasChildren()})
	}
	if w.adding {
		row := Row{Kind: RowExpression, Key: newWatchKey}
		w.rows[newWatchKey] = row
		items = append(items, kit.TreeItem{Key: newWatchKey, Data: row})
	}
	_ = w.model.SetItems(items)
}

func (w *WatchPane) load(key string, data any) {
	row, ok := data.(Row)
	if !ok || row.Expression == nil {
		w.model.FailLoad(key)
		return
	}
	if cached, ok := w.children[key]; ok {
		w.setChildren(key, row.Expression, cached)
		return
	}
	gen, ctx := w.gen, w.ctx
	go func() {
		vars, err := row.Expression.Children(ctx)
		w.opts.Post(func() {
			if err != nil || gen != w.gen {
				w.model.FailLoad(key)
				return
			}
			w.children[key] = vars
			w.setChildren(key, row.Expression, vars)
		})
	}()
}

func (w *WatchPane) setChildren(parent string, node debug.Node, vars []*debug.Variable) {
	items := make([]kit.TreeItem, 0, len(vars))
	used := keys{}
	for _, x := range vars {
		key := parent + "/" + used.of(x.ID())
		row := Row{Kind: RowVariable, Key: key, Variable: x, Parent: node}
		w.rows[key] = row
		items = append(items, kit.TreeItem{Key: key, Data: row, Label: x.Name, Lazy: x.HasChildren()})
	}
	_ = w.model.SetChildren(parent, items)
	w.notify()
}

// Add opens the row in which a new expression is typed (Add Expression); text is what it starts with (Add to Watch).
func (w *WatchPane) Add(text string) {
	if text != "" {
		w.opts.Service.Model().AddWatchExpression(text)
		return
	}
	w.adding = true
	w.rebuild()
	w.StartEdit(newWatchKey, "", func(text string, ok bool) {
		w.adding = false
		if ok && text != "" {
			w.opts.Service.Model().AddWatchExpression(text)
		} else {
			w.rebuild()
			w.notify()
		}
	})
}

// Rename opens the input on an expression (Edit Expression); an empty name removes it.
func (w *WatchPane) Rename(e *debug.Expression) {
	key := "watch:" + e.ID()
	w.StartEdit(key, e.Name, func(text string, ok bool) {
		if !ok {
			return
		}
		if text == "" {
			w.opts.Service.Model().RemoveWatchExpressions(e.ID())
			return
		}
		w.opts.Service.Model().RenameWatchExpression(e.ID(), text)
	})
}

// SetValue opens the input on an expression or one of its variables with its value (Set Value); the new value goes to the adapter.
func (w *WatchPane) SetValue(r Row) { w.setValue(r, w.frame) }

// Render builds the contents of a row.
func (w *WatchPane) Render(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
	row, ok := n.Data.(Row)
	if !ok {
		return kit.NewHighlightedLabel(bc, n.Label, st)
	}
	if w.edit != nil && w.edit.key == n.Key {
		return w.editBox()
	}
	switch row.Kind {
	case RowExpression:
		return LineOfNode(row.Expression, row.Expression.Name, w.showTypes()).Widget(bc, st)
	case RowVariable:
		return LineOfNode(row.Variable, row.Variable.Name, w.showTypes()).Widget(bc, st)
	}
	return kit.NewHighlightedLabel(bc, n.Label, st)
}

// Message is shown in place of an empty tree: VS Code has no message, the Watch is only its header.
func (w *WatchPane) Message() string { return "" }
