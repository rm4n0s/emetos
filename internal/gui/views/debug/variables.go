package debug

import (
	"context"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// VariablesPane is the Variables view: the scopes of the focused stack frame, each expanding to its variables, which expand to theirs.
// The children are asked of the adapter when a node is first expanded, on a goroutine of their own, and the tree shows a "Loading..."
// row until they arrive. The first scope that is not expensive is expanded when the frame is focused, as VS Code does.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/variablesView.ts (VariablesView, VariablesDataSource, VariablesRenderer,
// ScopesRenderer)
type VariablesPane struct {
	*base
	frame  *debug.StackFrame
	gen    int
	scopes []*debug.Scope
	// children caches the variables loaded for a node, by key, until the frame changes.
	children map[string][]*debug.Variable
}

func newVariablesPane(opts Opts) *VariablesPane {
	v := &VariablesPane{children: map[string][]*debug.Variable{}}
	v.base = newBase(opts, "workbench.debug.variablesView", kit.TreeModelOpts{LoadChildren: v.load})
	return v
}

func (v *VariablesPane) refresh(ev debug.Event) {
	frame := v.opts.Service.Model().Focus().Frame
	if frame == v.frame && ev.Kind != 0 && v.frame != nil {
		// a value was set or loaded, or the model changed in a way the scopes do not show
		v.notify()
		return
	}
	if frame == v.frame {
		return
	}
	v.frame = frame
	v.gen++
	v.scopes = nil
	v.children = map[string][]*debug.Variable{}
	v.rows = map[string]Row{}
	_ = v.model.SetItems(nil)
	v.message = ""
	if frame == nil {
		v.notify()
		return
	}
	v.message = "Loading..."
	v.notify()
	gen, ctx := v.gen, v.ctx
	go func() {
		scopes, err := frame.Scopes(ctx)
		if err != nil {
			return // the view is gone, or the frame was replaced
		}
		v.opts.Post(func() {
			if gen == v.gen {
				v.setScopes(scopes)
			}
		})
	}()
}

// setScopes shows the scopes of the frame and opens the first one that is not expensive.
func (v *VariablesPane) setScopes(scopes []*debug.Scope) {
	v.scopes = scopes
	v.message = ""
	var items []kit.TreeItem
	used := keys{}
	for _, s := range scopes {
		key := used.of("scope:" + s.ID())
		row := Row{Kind: RowScope, Key: key, Scope: s}
		v.rows[key] = row
		if s.Err != "" {
			row = Row{Kind: RowMessage, Key: key, Text: s.Err, Scope: s}
			v.rows[key] = row
			items = append(items, kit.TreeItem{Key: key, Data: row, Label: s.Err})
			continue
		}
		items = append(items, kit.TreeItem{Key: key, Data: row, Label: s.Name, Lazy: s.HasChildren() || s.Reference() > 0})
	}
	_ = v.model.SetItems(items)
	for _, it := range items {
		r := it.Data.(Row)
		if r.Kind == RowScope && !r.Scope.Expensive {
			v.model.Expand(it.Key)
			break
		}
	}
	v.notify()
}

// load asks the adapter for the children of a node; the model calls it on the UI thread when the node is first expanded.
func (v *VariablesPane) load(key string, data any) {
	row, ok := data.(Row)
	if !ok {
		return
	}
	node := row.Node()
	if node == nil {
		v.model.FailLoad(key)
		return
	}
	if cached, ok := v.children[key]; ok {
		v.setChildren(key, node, cached)
		return
	}
	gen, ctx := v.gen, v.ctx
	go func() {
		vars, err := node.Children(ctx)
		if err != nil {
			v.opts.Post(func() {
				if gen == v.gen {
					v.model.FailLoad(key)
				}
			})
			return
		}
		v.opts.Post(func() {
			if gen == v.gen {
				v.children[key] = vars
				v.setChildren(key, node, vars)
			}
		})
	}()
}

func (v *VariablesPane) setChildren(parent string, node debug.Node, vars []*debug.Variable) {
	items := make([]kit.TreeItem, 0, len(vars))
	used := keys{}
	for _, x := range vars {
		key := parent + "/" + used.of(x.ID())
		row := Row{Kind: RowVariable, Key: key, Variable: x, Parent: node}
		v.rows[key] = row
		items = append(items, kit.TreeItem{Key: key, Data: row, Label: x.Name, Lazy: x.HasChildren()})
	}
	_ = v.model.SetChildren(parent, items)
	v.notify()
}

// Render builds the contents of a row: a scope is its name, a variable its name and value in the colors of the debug tokens.
func (v *VariablesPane) Render(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
	row, ok := n.Data.(Row)
	if !ok {
		return kit.NewHighlightedLabel(bc, n.Label, st)
	}
	if v.edit != nil && v.edit.key == n.Key {
		return v.editBox()
	}
	switch row.Kind {
	case RowScope:
		return widgets.NewText(row.Scope.Name, widgets.TextOpts{Style: kit.TextStyleOf(bc, rowFontSize, st.Foreground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	case RowVariable:
		return LineOfNode(row.Variable, row.Variable.Name, v.showTypes()).Widget(bc, st)
	case RowMessage:
		return widgets.NewText(row.Text, widgets.TextOpts{Style: kit.TextStyleOf(bc, rowFontSize, named(kit.ThemeOf(bc), "debugTokenExpression.error", st.Foreground)), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	return kit.NewHighlightedLabel(bc, n.Label, st)
}

// Frame is the frame whose variables the view shows.
func (v *VariablesPane) Frame() *debug.StackFrame { return v.frame }

// CollapseAll folds every scope and variable.
func (v *VariablesPane) CollapseAll() { v.model.CollapseAll() }

var _ context.Context

// SetValue opens the input on a variable with its value (Set Value, F2); the new value goes to the adapter.
func (v *VariablesPane) SetValue(r Row) { v.setValue(r, v.frame) }
