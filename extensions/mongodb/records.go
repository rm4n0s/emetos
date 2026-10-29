package mongodb

import (
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
	"github.com/rm4n0s/emetos/extensions/mongodb/playground"
)

// runRecord is a run (Run, Run All or Open Collection) as the panel shows it: the statements, their messages and their sets of
// documents. It is made and changed on the UI thread only.
type runRecord struct {
	// n numbers the runs of an editor from 1.
	n       int
	started time.Time
	elapsed time.Duration
	// connID and connection are the connection the run was for, and database the database it started on.
	connID, connection, database string
	// text is the playground the statements were cut from, for the places of errors.
	text string
	// total is how many statements the run was given; steps are those that ran.
	total int
	steps []*stepRecord
	sets  []*setRecord
	// running says that the run goes on; current is the number of the statement that runs.
	running bool
	current int
	// cancelled says that the user stopped the run, and skipped how many statements were not run.
	cancelled bool
	skipped   int
	// refused says why nothing was run (a mistake in the playground, a read-only connection).
	refused string
	// tab is the tab of the panel that shows: an index in sets, or len(sets) for the Messages.
	tab int
	// fetching says that More is reading.
	fetching bool
}

// stepRecord is one statement of a run and what it gave.
type stepRecord struct {
	res *StepResult
	// line and column place the statement or its error in the document (from 1, in UTF-16 units); line is 0 when there is no place.
	line, column       int
	endLine, endColumn int
}

// setRecord is a set of documents with the views of it that were made.
type setRecord struct {
	step  int
	index int
	set   *DocSet
	// selected is the index of the document the user selected, or -1.
	selected int

	// tree, json and table are made when the panel first shows them, and again when the documents change.
	tree  *treeView
	json  *jsonView
	table *tableView
}

// title is the text of the tab of a set: "orders.find (20+ documents)".
func (s *setRecord) title() string {
	return s.set.Title + " (" + documentsText(len(s.set.Docs), s.set.More()) + ")"
}

// invalidate drops the views, which are made again from the documents when they are next shown.
func (s *setRecord) invalidate() {
	s.tree, s.json = nil, nil
	if s.table != nil {
		s.table.grid.Dispose()
		s.table = nil
	}
}

// release lets go of the views and the cursor of the set.
func (s *setRecord) release() {
	s.invalidate()
	s.set.Release()
}

// release drops the documents of the run and closes its cursors.
func (r *runRecord) release() {
	for _, s := range r.sets {
		s.release()
	}
}

// failed tells whether a statement of the run failed or was cancelled.
func (r *runRecord) failed() bool {
	if r.refused != "" {
		return true
	}
	for _, st := range r.steps {
		if st.res.Err != nil {
			return true
		}
	}
	return false
}

// summary is the one line the toolbar says of the run.
func (r *runRecord) summary() string {
	var parts []string
	switch {
	case r.running:
		parts = append(parts, "Running statement "+strconv.Itoa(r.current)+" of "+strconv.Itoa(r.total)+"...")
	case r.cancelled:
		parts = append(parts, "Cancelled")
	case r.failed():
		parts = append(parts, "Failed")
	}
	if !r.running {
		parts = append(parts, plural(len(r.steps), "statement"))
		var docs int
		for _, s := range r.sets {
			if r.steps[s.step].res.Write == nil { // the document that says a write was acknowledged is no result of a read
				docs += len(s.set.Docs)
			}
		}
		if docs > 0 {
			parts = append(parts, documentsText(docs, false))
		}
		parts = append(parts, formatElapsed(r.elapsed))
	}
	return strings.Join(parts, " · ")
}

// plural writes a count and its noun.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return groupDigits(n) + " " + noun + "s"
}

// formatElapsed writes a duration for a person: 0.4 ms, 12 ms, 1.25 s, 2 min 5 s.
func formatElapsed(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 1, 64) + " ms"
	case d < time.Second:
		return strconv.Itoa(int(d/time.Millisecond)) + " ms"
	case d < time.Minute:
		return strconv.FormatFloat(d.Seconds(), 'f', 2, 64) + " s"
	}
	return strconv.Itoa(int(d/time.Minute)) + " min " + strconv.Itoa(int((d%time.Minute)/time.Second)) + " s"
}

// firstLine is the text of a statement on one line, cut at a length.
func firstLine(text string, limit int) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = strings.TrimSpace(text[:i]) + " ..."
	}
	if r := []rune(text); len(r) > limit {
		text = strings.TrimSpace(string(r[:limit])) + " ..."
	}
	return text
}

// lineColumn converts a byte offset in the text to the line and column the editor's markers use: from 1, the column in UTF-16 units.
func lineColumn(text string, offset int) (line, column int) {
	offset = min(max(offset, 0), len(text))
	line = 1 + strings.Count(text[:offset], "\n")
	start := strings.LastIndexByte(text[:offset], '\n') + 1
	for _, r := range text[start:offset] {
		if r >= 0x10000 {
			column += 2
		} else {
			column++
		}
	}
	return line, column + 1
}

// offsetOf converts a position of the core events (a line and a column in bytes, both counted from 0) to a byte offset in the text.
func offsetOf(text string, line, col int) int {
	off := 0
	for ; line > 0; line-- {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return len(text)
		}
		off += i + 1
	}
	end := strings.IndexByte(text[off:], '\n')
	if end < 0 {
		end = len(text) - off
	}
	return off + min(col, end)
}

// ---- the table view ----

// tableView is the grid of a set.
type tableView struct {
	grid *resultgrid.Grid
	src  *docSource
	// shown is how many documents the grid has.
	shown int
}

// docSource is a resultgrid.Source over the documents of a set: the columns are the top-level fields of the documents, in the order
// they were first seen.
type docSource struct {
	cols  []resultgrid.Column
	index map[string]int
	docs  []bson.D
}

var _ resultgrid.Source = (*docSource)(nil)

// maxColumns is the most columns the table view makes; the other fields are left out (the Tree and JSON views have them).
const maxColumns = 200

// newDocSource makes a source over the documents.
func newDocSource(docs []bson.D) *docSource {
	s := &docSource{index: map[string]int{}}
	s.update(docs)
	return s
}

// update makes the source read the documents; it reports whether it has columns it did not have.
func (s *docSource) update(docs []bson.D) (newColumns bool) {
	s.docs = docs
	for _, d := range docs {
		for _, e := range d {
			if _, ok := s.index[e.Key]; ok || len(s.cols) >= maxColumns {
				continue
			}
			s.index[e.Key] = len(s.cols)
			s.cols = append(s.cols, resultgrid.Column{Name: e.Key})
			newColumns = true
		}
	}
	for _, d := range docs {
		for _, e := range d {
			if i, ok := s.index[e.Key]; ok && s.cols[i].Type == "" {
				s.cols[i].Type = bsonTypeName(e.Value)
			}
		}
	}
	return newColumns
}

// Columns implements resultgrid.Source.
func (s *docSource) Columns() []resultgrid.Column { return s.cols }

// Len implements resultgrid.Source.
func (s *docSource) Len() int { return len(s.docs) }

// Row implements resultgrid.Source.
func (s *docSource) Row(i int, dst []resultgrid.Value) {
	for _, e := range s.docs[i] {
		if c, ok := s.index[e.Key]; ok && c < len(dst) {
			dst[c] = gridValue(e.Value)
		}
	}
}

// gridValue turns a BSON value into a value of the grid.
func gridValue(v any) resultgrid.Value {
	switch x := v.(type) {
	case nil:
		return resultgrid.Null()
	case string:
		return resultgrid.Text(x)
	case int32:
		return resultgrid.Int(int64(x))
	case int64:
		return resultgrid.Int(x)
	case int:
		return resultgrid.Int(int64(x))
	case float64:
		return resultgrid.Float(x)
	case bool:
		return resultgrid.Bool(x)
	case bson.DateTime:
		return resultgrid.Time(x.Time().UTC())
	case bson.Binary:
		if x.Subtype == bson.TypeBinaryUUID && len(x.Data) == 16 {
			return resultgrid.Text(playground.Format(x))
		}
		return resultgrid.Bytes(x.Data)
	case bson.D, bson.A:
		return resultgrid.JSON(valueText(x))
	}
	return resultgrid.Text(playground.Format(v))
}

// ---- the tree view ----

// treeView is the tree of a set: a row for each document, and below it the fields.
type treeView struct {
	model *ui.TreeModel
	ctl   *ui.TreeController
	// count is how many documents the tree was made from.
	count int
}

// treeData is what a row of the tree holds.
type treeData struct {
	// doc is the index of the document the row is in.
	doc int
	// name is the name of the field, or "[0]" for an element of an array, or "" for a document.
	name  string
	value any
	root  bool
}

// childrenOf lists the fields of a document and the elements of an array as rows of the tree.
func childrenOf(parent string, doc int, v any) []ui.TreeItem {
	var items []ui.TreeItem
	add := func(i int, name string, value any) {
		key := parent + "/" + strconv.Itoa(i)
		it := ui.TreeItem{Key: key, Data: treeData{doc: doc, name: name, value: value}, Label: name}
		if hasChildren(value) {
			it.Lazy = true
		}
		items = append(items, it)
	}
	switch x := v.(type) {
	case bson.D:
		for i, e := range x {
			add(i, e.Key, e.Value)
		}
	case bson.A:
		for i, e := range x {
			add(i, "["+strconv.Itoa(i)+"]", e)
		}
	}
	return items
}

// hasChildren tells whether a value is a document or an array with something in it.
func hasChildren(v any) bool {
	switch x := v.(type) {
	case bson.D:
		return len(x) > 0
	case bson.A:
		return len(x) > 0
	}
	return false
}

// previewOf is the one-line summary of a document for its row: its first fields.
func previewOf(d bson.D) string {
	var b strings.Builder
	for i, e := range d {
		if i == 3 {
			b.WriteString(", ...")
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(e.Key + ": " + shortValue(e.Value, 40))
	}
	return b.String()
}

// shortValue writes a value on one line as the shell does, cut at n characters.
func shortValue(v any, n int) string {
	switch x := v.(type) {
	case bson.D:
		return "{" + strconv.Itoa(len(x)) + " fields}"
	case bson.A:
		return "[" + strconv.Itoa(len(x)) + " items]"
	}
	return shorten(playground.Format(v), n)
}

// ---- the JSON view ----

// jsonView is the text of the documents of a set as Extended JSON, in lines.
type jsonView struct {
	model  *ui.ListViewModel
	ctl    *ui.ListController
	lines  []string
	tokens [][]extension.Token
	// docOf is the index of the document each line belongs to, -1 for the brackets.
	docOf []int
	// count is how many documents the text was made from.
	count int
}

// buildJSON makes the text of the documents and the map from lines to documents.
func buildJSON(docs []bson.D, reuse *jsonView) (*jsonView, error) {
	v := &jsonView{count: len(docs)}
	v.lines = append(v.lines, "[")
	v.docOf = append(v.docOf, -1)
	for i, d := range docs {
		t, err := DocumentText(d)
		if err != nil {
			return nil, err
		}
		ls := strings.Split(t, "\n")
		for j, l := range ls {
			if j == len(ls)-1 && i < len(docs)-1 {
				l += ","
			}
			v.lines = append(v.lines, "  "+l)
			v.docOf = append(v.docOf, i)
		}
	}
	v.lines = append(v.lines, "]")
	v.docOf = append(v.docOf, -1)
	if reuse != nil {
		v.model, v.ctl = reuse.model, reuse.ctl // the list keeps its place when more documents came
	} else {
		v.model, v.ctl = ui.NewListViewModel(ui.ListViewModelOpts{RowHeight: foundation.Some(18.0)}), ui.NewListController()
	}
	items := make([]ui.ListViewItem, len(v.lines))
	for i := range items {
		items[i] = ui.ListViewItem{Key: "l" + strconv.Itoa(i), Data: i}
	}
	if err := v.model.SetItems(items); err != nil {
		return nil, err
	}
	return v, nil
}

// text is the whole text of the view.
func (v *jsonView) text() string { return strings.Join(v.lines, "\n") }
