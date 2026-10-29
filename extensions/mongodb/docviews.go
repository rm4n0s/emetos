package mongodb

import (
	"strconv"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// maxHighlighted is the most bytes of JSON that are highlighted; a longer text is shown plain.
const maxHighlighted = 256 << 10

// ---- the tree ----

// treeOf returns the tree of a set, making it when it is not there and bringing it up to date with the documents.
func (q *playgrounds) treeOf(sr *setRecord) *treeView {
	if sr.tree == nil {
		tv := &treeView{ctl: ui.NewTreeController()}
		tv.model = ui.NewTreeModel(ui.TreeModelOpts{LoadChildren: func(key string, data any) {
			td, ok := data.(treeData)
			if !ok {
				tv.model.FailLoad(key)
				return
			}
			q.api.Post(func() {
				if err := tv.model.SetChildren(key, childrenOf(key, td.doc, td.value)); err != nil {
					q.api.Log().Warn("the fields of a document cannot be listed", "err", err)
				}
			})
		}})
		sr.tree = tv
		q.fillTree(sr)
	} else if sr.tree.count != len(sr.set.Docs) {
		q.fillTree(sr)
	}
	return sr.tree
}

// fillTree gives the tree its rows from the documents of the set.
func (q *playgrounds) fillTree(sr *setRecord) {
	docs := sr.set.Docs
	items := make([]ui.TreeItem, len(docs))
	for i, d := range docs {
		items[i] = ui.TreeItem{Key: "d" + strconv.Itoa(i), Data: treeData{doc: i, value: d, root: true}, Lazy: len(d) > 0, Label: "#" + strconv.Itoa(i+1)}
	}
	if err := sr.tree.model.SetItems(items); err != nil {
		q.api.Log().Warn("the tree of documents cannot be made", "err", err)
		return
	}
	sr.tree.count = len(docs)
}

// treeWidget is the tree of a set.
func (q *playgrounds) treeWidget(sr *setRecord) widgets.Widget {
	tv := q.treeOf(sr)
	return ui.NewTree(tv.model, ui.TreeOpts{
		Controller: tv.ctl, Render: q.renderTreeRow,
		OnSelect: func(node ui.TreeNode) {
			if td, ok := node.Data.(treeData); ok {
				sr.selected = td.doc
			}
		},
		OnContextMenu: func(key string, at gui.Offset) {
			node, ok := tv.model.Node(key)
			if !ok {
				return
			}
			td, ok := node.Data.(treeData)
			if !ok {
				return
			}
			tv.model.SetSelected(key)
			sr.selected = td.doc
			q.menu.Show(at, q.documentMenu(sr, td.doc))
		},
	})
}

// renderTreeRow builds the contents of a row of the tree: the name of the field, its value, and its BSON type.
func (q *playgrounds) renderTreeRow(bc widgets.BuildContext, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	td, ok := node.Data.(treeData)
	if !ok {
		return widgets.NewText("", widgets.TextOpts{})
	}
	text := func(s string, color gui.Color, size float64) widgets.Widget {
		return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, size, color), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	gap := func(w float64) widgets.Widget {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
	}
	center := rendering.CrossAxisAlignmentCenter
	if td.root {
		doc, _ := td.value.(bson.D)
		return widgets.NewRow([]widgets.Widget{
			ui.Icon("symbol-structure", 16, st.Foreground), gap(6), text("#"+strconv.Itoa(td.doc+1), st.Foreground, 13), gap(8),
			widgets.NewFlexible(text(previewOf(doc), colors.DescriptionForeground, 13)),
		}, widgets.RowOpts{CrossAxisAlignment: center})
	}
	typ := bsonTypeName(td.value)
	return widgets.NewRow([]widgets.Widget{
		ui.Icon(typeIcon(typ), 16, st.Foreground), gap(6), text(td.name, st.Foreground, 13), gap(8),
		widgets.NewFlexible(text(shortValue(td.value, 80), colors.DescriptionForeground, 13)), gap(10), text(typ, colors.DescriptionForeground, 11), gap(8),
	}, widgets.RowOpts{CrossAxisAlignment: center})
}

// ---- the JSON view ----

// jsonOf returns the JSON view of a set, making it when it is not there and bringing it up to date with the documents.
func (q *playgrounds) jsonOf(sr *setRecord) *jsonView {
	docs := sr.set.Docs
	if sr.json != nil && sr.json.count == len(docs) {
		return sr.json
	}
	v, err := buildJSON(docs, sr.json)
	if err != nil {
		q.api.Log().Warn("the JSON of the documents cannot be written", "err", err)
		v = &jsonView{lines: []string{err.Error()}, docOf: []int{-1}, model: ui.NewListViewModel(ui.ListViewModelOpts{RowHeight: foundation.Some(18.0)}), ctl: ui.NewListController()}
		if serr := v.model.SetItems([]ui.ListViewItem{{Key: "l0", Data: 0}}); serr != nil {
			q.api.Log().Warn("the error cannot be listed", "err", serr)
		}
	}
	v.tokens = q.tokenize(v.text())
	sr.json = v
	return v
}

// tokenize splits the text into the tokens of the JSON grammar for the colors; a text that cannot be, or is too long, stays plain.
func (q *playgrounds) tokenize(text string) [][]extension.Token {
	if len(text) > maxHighlighted {
		return nil
	}
	langs := q.api.Languages()
	id, ok := langs.Resolve("json")
	if !ok {
		return nil
	}
	toks, err := langs.Tokenize(id, text)
	if err != nil {
		q.api.Log().Debug("the JSON cannot be highlighted", "err", err)
		return nil
	}
	return toks
}

// jsonWidget is the JSON view of a set.
func (q *playgrounds) jsonWidget(sr *setRecord) widgets.Widget {
	v := q.jsonOf(sr)
	return ui.NewList(v.model, ui.ListOpts{
		Controller: v.ctl,
		Render: func(bc widgets.BuildContext, row ui.ListViewRow, st ui.RowState) widgets.Widget {
			i, _ := row.Data.(int)
			return q.jsonLine(bc, sr, v, i, st)
		},
		OnSelect: func(row ui.ListViewRow) {
			if i, ok := row.Data.(int); ok && i < len(v.docOf) && v.docOf[i] >= 0 {
				sr.selected = v.docOf[i]
				q.refresh()
			}
		},
		OnContextMenu: func(row ui.ListViewRow, at gui.Offset) {
			if i, ok := row.Data.(int); ok && i < len(v.docOf) && v.docOf[i] >= 0 {
				sr.selected = v.docOf[i]
				q.menu.Show(at, q.documentMenu(sr, v.docOf[i]))
			}
		},
	})
}

// jsonLine is one line of the JSON view, in the colors of its tokens; the lines of the selected document have a band behind them.
func (q *playgrounds) jsonLine(bc widgets.BuildContext, sr *setRecord, v *jsonView, i int, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	look := ui.TokenLooker(bc)
	mono := ui.TextStyle(bc, 12, colors.EditorForeground).CopyWith(painting.TextStyleOpts{FontFamily: pageui.MonoFamily(bc)})
	if i < 0 || i >= len(v.lines) {
		return widgets.NewText("", widgets.TextOpts{})
	}
	line := v.lines[i]
	var spans []painting.InlineSpan
	if i < len(v.tokens) && len(v.tokens[i]) > 0 {
		at := 0
		for _, t := range v.tokens[i] {
			start, end := min(max(t.Start, at), len(line)), min(t.End, len(line))
			if start > at {
				spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line[at:start])}))
			}
			if end > start {
				l := look(t.Scopes, st.Foreground)
				ts := painting.TextStyleOpts{Color: foundation.Some(l.Color)}
				if l.Bold {
					ts.FontWeight = gui.FontWeightBold
				}
				if l.Italic {
					ts.FontStyle = gui.FontStyleItalic
				}
				spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line[start:end]), Style: mono.CopyWith(ts)}))
			}
			at = max(at, end)
		}
		if at < len(line) {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line[at:])}))
		}
	} else {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line)}))
	}
	rich := widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Style: mono, Children: spans}))
	if sr.selected >= 0 && i < len(v.docOf) && v.docOf[i] == sr.selected && !st.Selected {
		return widgets.NewColoredBox(colors.ListInactiveSelectionBackground, widgets.NewAlign(rich, widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft}))
	}
	return rich
}

// ---- the table ----

// tableOf returns the table view of a set, making it when it is not there and bringing it up to date with the documents.
func (q *playgrounds) tableOf(ed *editor, rec *runRecord, sr *setRecord) *tableView {
	docs := sr.set.Docs
	if sr.table == nil {
		tv := &tableView{src: newDocSource(docs)}
		tv.grid = resultgrid.New(resultgrid.Options{
			OnCopy: func(text string) {
				if err := q.api.Clipboard().WriteText(text); err != nil {
					q.api.Log().Warn("the clipboard cannot be written", "err", err)
					q.api.Notifications().Error("Cannot copy: " + err.Error())
				}
			},
			OnFetchMore: func() { q.fetchMore(ed, rec, sr) },
			MenuItems: func() []ui.MenuItem {
				return q.documentMenuAt(sr, tv.selectedDoc)
			},
		})
		tv.grid.SetSource(tv.src)
		tv.grid.SetMore(sr.set.More())
		tv.shown = len(docs)
		sr.table = tv
		return tv
	}
	tv := sr.table
	if tv.shown != len(docs) {
		if tv.src.update(docs) {
			tv.grid.SetSource(tv.src)
		} else {
			tv.grid.Appended()
		}
		tv.shown = len(docs)
	}
	tv.grid.SetMore(sr.set.More())
	return tv
}

// selectedDoc is the document of the first selected row of the grid, or -1.
func (t *tableView) selectedDoc() int {
	m := t.grid.Model()
	r0, _, _, _, ok := m.Selection().Bounds(m.Len(), len(m.Columns()))
	if !ok || r0 < 0 || r0 >= m.Len() {
		return -1
	}
	return m.SourceRow(r0)
}

// ---- views kept in step with the documents ----

// syncViews brings the views that were made up to date after documents came or went.
func (q *playgrounds) syncViews(sr *setRecord) {
	if sr.tree != nil && sr.tree.count != len(sr.set.Docs) {
		q.fillTree(sr)
	}
	if sr.table != nil {
		sr.table.grid.SetMore(sr.set.More())
	}
	if sr.selected >= len(sr.set.Docs) {
		sr.selected = -1
	}
}
