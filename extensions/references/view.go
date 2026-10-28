package references

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// maxTextSize is the largest file the labels of the view read from disk for a location outside the open documents.
const maxTextSize = 2 << 20

// viewState is the view and what it shows: the search, its model, the history and the tree. It is used on the UI thread.
type viewState struct {
	r      *references
	api    extension.API
	handle extension.ViewHandle

	tree *ui.TreeModel
	ctl  *ui.TreeController
	menu *ui.ContextMenuController

	hist history
	// in is the search that is shown, nil when the view shows the history; mod is its result.
	in  *input
	mod *model
	// gen counts the searches that were started, so that the answer of one that was replaced is dropped.
	gen    int
	cancel context.CancelFunc
	// loading says that the answer of the search has not come yet.
	loading bool
	message string
	// texts are the lines of the files the labels are made from; reading says which are being read.
	texts   map[string][]string
	reading map[string]bool
}

// newViewState makes the view of the extension.
func newViewState(r *references) *viewState {
	return &viewState{
		r: r, api: r.api, tree: ui.NewTreeModel(), ctl: ui.NewTreeController(), menu: ui.NewContextMenuController(),
		texts: map[string][]string{}, reading: map[string]bool{},
	}
}

// provide gives the view its content and sets the context keys.
func (v *viewState) provide() error {
	h, err := v.api.Views().Provide(ViewID, extension.ViewContent{Build: v.build})
	if err != nil {
		return err
	}
	v.handle = h
	v.setKey(keyActive, false)
	v.setKey(keyHasResult, false)
	v.setKey(keyHasHistory, false)
	v.setKey(keyCanNavigate, false)
	v.clearInput()
	return nil
}

// setKey sets a context key; a failure is logged (the keys are declared by this extension, so it is a bug).
func (v *viewState) setKey(key string, value any) {
	if err := v.api.Context().Set(key, value); err != nil {
		v.api.Log().Warn("a context key cannot be set", "key", key, "err", err)
	}
}

// ---- reading text ----

// pathOf is the path of a file URI.
func pathOf(uri string) (string, bool) {
	p, ok := strings.CutPrefix(uri, "file://")
	return p, ok
}

// relPath is the path of a file relative to the folder of the workspace when it is inside it, else the whole path (VS Code's
// asRelativePath).
func (v *viewState) relPath(uri string) string {
	p, ok := pathOf(uri)
	if !ok {
		return uri
	}
	if v.r.folder != "" {
		if rel, err := filepath.Rel(v.r.folder, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return p
}

// readText is the lines of a document now: the text of the open document with its unsaved changes, else the file. It reads on
// the UI thread and keeps nothing, so it is for a document the user is working with.
func (v *viewState) readText(uri string) ([]string, bool) {
	text, err := v.api.Documents().Read(v.r.ctx, uri)
	if err != nil {
		p, ok := pathOf(uri)
		if !ok {
			return nil, false
		}
		data, rerr := readCapped(p)
		if rerr != nil {
			return nil, false
		}
		text = string(data)
	}
	return splitLines(text), true
}

// textOf is the lines of the text a search was made on, kept until the next search: the places of the locations are in it, and
// it is read once, on the UI thread, for a command that needs it at once (a copy).
func (v *viewState) textOf(uri string) ([]string, bool) {
	if lines, ok := v.texts[uri]; ok {
		return lines, lines != nil
	}
	lines, ok := v.readText(uri)
	if ok {
		v.texts[uri] = lines
	}
	return lines, ok
}

// readCapped reads a file of at most maxTextSize bytes.
func readCapped(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > maxTextSize {
		return nil, errors.New("TextTooLarge", "the file is too large to read for the labels of the view", "path", p, "size", st.Size())
	}
	data := make([]byte, st.Size())
	n, err := f.Read(data)
	if err != nil && n < len(data) {
		return nil, err
	}
	return data[:n], nil
}

// ensureText makes the lines of a file available for the labels: an open document is read at once, any other file in a
// goroutine, after which the tree is built again. The labels of a file that is not read yet are empty. The lines are those of
// the time of the search.
func (v *viewState) ensureText(uri string) ([]string, bool) {
	if lines, ok := v.texts[uri]; ok {
		return lines, lines != nil
	}
	if slices.Contains(v.api.Documents().Open(), uri) {
		return v.textOf(uri)
	}
	if v.reading[uri] {
		return nil, false
	}
	p, ok := pathOf(uri)
	if !ok {
		return nil, false
	}
	v.reading[uri] = true
	gen := v.gen
	go func() {
		data, err := readCapped(p)
		v.api.Post(func() {
			delete(v.reading, uri)
			if gen != v.gen {
				return // the search that wanted it was replaced
			}
			if err != nil {
				v.texts[uri] = nil // unreadable: the labels stay empty and it is not asked again
			} else {
				v.texts[uri] = splitLines(string(data))
			}
			v.handle.Refresh()
		})
	}()
	return nil, false
}

// ---- searches ----

// findLocations starts the search of the symbol at the cursor of the active editor.
func (v *viewState) findLocations(title, kind string) {
	doc, ok := v.api.Documents().Active()
	if !ok || len(doc.Selections) == 0 {
		return
	}
	v.setInput(&input{title: title, kind: kind, uri: doc.URI, pos: doc.Selections[0].Active})
}

// isValidRequestPosition tells whether there is a word, or any text that is not white space, at the position.
func isValidRequestPosition(lines []string, pos extension.Position) bool {
	_, _, ok := wordOrToken(lineOf(lines, pos.Line), pos.Col)
	return ok
}

// setInput shows a new search: the view is revealed with the focus, the language servers are asked, and the answer fills the
// tree (VS Code's SymbolsTree.setInput).
func (v *viewState) setInput(in *input) {
	lines, ok := v.readText(in.uri)
	if !ok || !isValidRequestPosition(lines, in.pos) {
		v.clearInput()
		return
	}
	v.stopSearch()
	v.gen++
	gen := v.gen
	v.setKey(keySource, in.kind)
	v.setKey(keyActive, true)
	v.setKey(keyHasResult, true)
	v.handle.Reveal(true)
	if v.in == nil || v.in.kind != in.kind {
		v.message = ""
	}
	v.in, v.mod, v.loading = in, nil, true
	v.setCanNavigate(false)
	v.handle.SetTitle(in.title)
	v.setItems()
	ctx, cancel := context.WithCancel(v.r.ctx)
	v.cancel = cancel
	done := func(locs []extension.Location, err error) {
		if gen != v.gen {
			return
		}
		v.loading = false
		if err != nil {
			v.api.Log().Info("the search failed", "kind", in.kind, "err", err)
		}
		if len(locs) == 0 {
			v.clearInput()
			return
		}
		v.finish(in, lines, locs)
	}
	var err error
	if in.kind == kindImplementations {
		err = v.api.Languages().Implementations(ctx, in.uri, in.pos, done)
	} else {
		err = v.api.Languages().References(ctx, in.uri, in.pos, done)
	}
	if err != nil {
		v.api.Log().Warn("the search cannot be made", "kind", in.kind, "err", err)
		v.clearInput()
	}
}

// finish shows the locations a search found: they go into the history, the tree shows them and the location nearest to the
// request is selected.
func (v *viewState) finish(in *input, lines []string, locs []extension.Location) {
	v.texts = map[string][]string{in.uri: lines}
	v.mod = newModel(locs)
	v.hist.add(in, lines, v.relPath(in.uri))
	v.setKey(keyHasHistory, true)
	v.message = v.mod.message()
	v.setCanNavigate(true)
	v.setItems()
	if near := v.mod.nearest(in.uri, in.pos); near != nil {
		v.selectItem(near, true)
	}
	v.handle.Refresh()
}

// stopSearch ends the search that is being asked for.
func (v *viewState) stopSearch() {
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
}

// setCanNavigate sets the key that enables F4 and Shift+F4.
func (v *viewState) setCanNavigate(on bool) { v.setKey(keyCanNavigate, on) }

// clearInput empties the view: the title and the message go back to the start, and the tree lists the history (VS Code's
// SymbolsTree.clearInput).
func (v *viewState) clearInput() {
	v.stopSearch()
	v.gen++
	v.in, v.mod, v.loading = nil, nil, false
	v.setKey(keyHasResult, false)
	v.api.Context().Reset(keySource)
	v.setCanNavigate(false)
	if v.handle != nil {
		v.handle.SetTitle(titleReferences)
	}
	if v.hist.size() == 0 {
		v.message = "No results."
	} else {
		v.message = "No results. Try running a previous search again:"
	}
	v.setItems()
	if v.handle != nil {
		v.handle.Refresh()
	}
}

// clearHistory forgets the searches and empties the view.
func (v *viewState) clearHistory() {
	v.hist.clear()
	v.setKey(keyHasHistory, false)
	v.clearInput()
}

// refresh runs the last search again.
func (v *viewState) refresh() {
	if it := v.hist.last(); it != nil {
		v.rerun(it)
	}
}

// rerun runs a search of the history again, at the place its word is now.
func (v *viewState) rerun(it *historyItem) {
	v.hist.remove(it.key)
	in := it.in
	if lines, ok := v.readText(it.anchor.uri); ok {
		if pos := it.anchor.guessedPosition(lines); pos != in.pos {
			in = in.with(pos)
		}
	}
	v.setInput(in)
}

// pickFromHistory asks the user which of the earlier searches to run again.
func (v *viewState) pickFromHistory() error {
	items := v.hist.newestFirst()
	picks := make([]extension.QuickPickItem, len(items))
	for i, it := range items {
		picks[i] = extension.QuickPickItem{Label: it.word, Description: it.description}
	}
	if len(picks) == 0 {
		return nil
	}
	return v.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select previous reference search", Items: picks}, func(i int) {
		if i >= 0 && i < len(items) {
			v.rerun(items[i])
		}
	})
}

// showHistoryItem opens the file of a search of the history at the place of its word.
func (v *viewState) showHistoryItem(it *historyItem) {
	pos := it.in.pos
	if lines, ok := v.readText(it.anchor.uri); ok {
		pos = it.anchor.guessedPosition(lines)
	}
	v.open(it.in.uri, pos)
}

// ---- items ----

// open shows a location in the editor: the file opens as a preview with the cursor at the start of the place.
func (v *viewState) open(uri string, pos extension.Position) {
	col := pos.Col
	if lines, ok := v.texts[uri]; ok && pos.Line < len(lines) {
		col = utf16Col(lines[pos.Line], pos.Col)
	}
	if err := v.api.Documents().Show(uri, pos.Line+1, col+1, true); err != nil {
		v.api.Notifications().Error("Cannot open " + v.relPath(uri) + ": " + err.Error())
	}
}

// target is the item a command acts on: the row named by the argument {"key": ...}, else the selected row.
func (v *viewState) target(args any) item {
	if v.mod == nil {
		return nil
	}
	if m, ok := args.(map[string]any); ok {
		if key, ok := m["key"].(string); ok {
			return v.mod.find(key)
		}
	}
	if key, ok := v.tree.Selected(); ok {
		return v.mod.find(key)
	}
	return nil
}

// historyTarget is the search of the history a command acts on.
func (v *viewState) historyTarget(args any) *historyItem {
	key := ""
	if m, ok := args.(map[string]any); ok {
		key, _ = m["key"].(string)
	}
	if key == "" {
		key, _ = v.tree.Selected()
	}
	for _, it := range v.hist.items {
		if "h:"+it.key == key {
			return it
		}
	}
	return nil
}

// refind runs a search of the history again.
func (v *viewState) refind(args any) {
	if it := v.historyTarget(args); it != nil {
		v.rerun(it)
	}
}

// dismiss takes a file or a location out of the result.
func (v *viewState) dismiss(args any) {
	it := v.target(args)
	if it == nil {
		return
	}
	v.mod.remove(it)
	v.message = v.mod.message()
	v.setItems()
	v.handle.Refresh()
}

// selectItem selects and focuses the row of an item and scrolls it into view; with expand its file is opened.
func (v *viewState) selectItem(it item, expand bool) {
	key := it.key()
	if r, ok := it.(*refItem); ok && expand {
		v.tree.Expand(r.file.key())
	}
	v.tree.Reveal(key)
	v.tree.SetSelected(key)
	v.tree.SetFocus(key)
	v.ctl.Reveal(key)
}

// navigate goes to the next or the previous location (F4 and Shift+F4): the one after the selected row, or after the one
// nearest to the cursor of the active editor, is selected in the tree and shown in the editor.
func (v *viewState) navigate(delta int) {
	if v.mod == nil || len(v.mod.files) == 0 {
		return
	}
	var from item
	if key, ok := v.tree.Selected(); ok {
		from = v.mod.find(key)
	}
	if from == nil {
		doc, ok := v.api.Documents().Active()
		if !ok || len(doc.Selections) == 0 {
			return
		}
		from = v.mod.nearest(doc.URI, doc.Selections[0].Active)
	}
	if from == nil {
		return
	}
	to := v.mod.next(from)
	if delta < 0 {
		to = v.mod.previous(from)
	}
	v.selectItem(to, true)
	loc := v.mod.location(to)
	v.ensureText(loc.URI)
	v.open(loc.URI, loc.Range.Start)
}

// collapseAll folds every file.
func (v *viewState) collapseAll() {
	v.tree.Perform(ui.TreeActionCollapseAll)
	v.handle.Refresh()
}

// ---- copying ----

// copyOfRef is the text VS Code copies for a location: "12, 5: the line around it".
func (v *viewState) copyOfRef(r *refItem) string {
	lines, _ := v.textOf(r.loc.URI)
	pos := r.loc.Range.Start
	line := lineOf(lines, pos.Line)
	before, inside, after := previewChunks(line, pos.Col, rangeEnd(pos.Line, r.loc.Range, len(line)), 21, false)
	return strconv.Itoa(pos.Line+1) + ", " + strconv.Itoa(utf16Col(line, pos.Col)+1) + ": " + before + inside + after
}

// copyOfFile is the text VS Code copies for a file: its path, then its locations indented.
func (v *viewState) copyOfFile(f *fileItem) string {
	var b strings.Builder
	b.WriteString(v.relPath(f.uri) + "\n")
	for _, r := range f.refs {
		b.WriteString("  " + v.copyOfRef(r) + "\n")
	}
	return b.String()
}

// copyOfModel is the text for the whole result.
func (v *viewState) copyOfModel() string {
	var b strings.Builder
	if v.mod != nil {
		for _, f := range v.mod.files {
			b.WriteString(v.copyOfFile(f) + "\n")
		}
	}
	return b.String()
}

// write puts a text on the clipboard; an empty one is not written.
func (v *viewState) write(text string) error {
	if text == "" {
		return nil
	}
	return v.api.Clipboard().WriteText(text)
}

// copyItem copies a location or a file.
func (v *viewState) copyItem(args any) error {
	switch it := v.target(args).(type) {
	case *refItem:
		return v.write(v.copyOfRef(it))
	case *fileItem:
		return v.write(v.copyOfFile(it))
	}
	return nil
}

// copyAll copies the whole result.
func (v *viewState) copyAll(args any) error {
	if v.target(args) == nil {
		return nil
	}
	return v.write(v.copyOfModel())
}

// copyPath copies the path of a file.
func (v *viewState) copyPath(args any) error {
	if f, ok := v.target(args).(*fileItem); ok {
		if p, ok := pathOf(f.uri); ok {
			return v.write(p)
		}
		return v.write(f.uri)
	}
	return nil
}

// ---- the tree ----

// The data of the nodes of the tree.
type (
	fileNode    struct{ f *fileItem }
	refNode     struct{ r *refItem }
	historyNode struct{ h *historyItem }
)

// setItems makes the tree from what the view shows: the files and locations of the result, or the history.
func (v *viewState) setItems() {
	var roots []ui.TreeItem
	switch {
	case v.mod != nil:
		for _, f := range v.mod.files {
			node := ui.TreeItem{Key: f.key(), Data: fileNode{f}, Label: path.Base(v.relPath(f.uri))}
			for _, r := range f.refs {
				node.Children = append(node.Children, ui.TreeItem{Key: r.key(), Data: refNode{r}, Label: path.Base(v.relPath(f.uri))})
			}
			roots = append(roots, node)
		}
	case v.in == nil:
		for _, h := range v.hist.newestFirst() {
			roots = append(roots, ui.TreeItem{Key: "h:" + h.key, Data: historyNode{h}, Label: h.word})
		}
	}
	expanded := v.tree.ExpandedKeys()
	if err := v.tree.SetItems(roots); err != nil {
		v.api.Log().Warn("the tree cannot be made", "err", err)
		return
	}
	v.tree.SetExpandedKeys(expanded)
}

// onSelect opens a location, or the file of a search of the history.
func (v *viewState) onSelect(node ui.TreeNode) {
	switch d := node.Data.(type) {
	case refNode:
		v.ensureText(d.r.loc.URI)
		v.open(d.r.loc.URI, d.r.loc.Range.Start)
	case historyNode:
		v.showHistoryItem(d.h)
	}
}

// runOn runs one of the commands that act on a row of the view on the row with the key, as a button of the row or an item of its
// menu does.
func (v *viewState) runOn(id, key string) {
	args := map[string]any{"key": key}
	var err error
	switch id {
	case RemoveReferenceItem:
		v.dismiss(args)
	case Copy:
		err = v.copyItem(args)
	case CopyAll:
		err = v.copyAll(args)
	case CopyPath:
		err = v.copyPath(args)
	case Refind:
		v.refind(args)
	}
	if err != nil {
		v.api.Log().Warn("a command of the view failed", "command", id, "err", err)
		v.api.Notifications().Warn("Cannot copy to the clipboard: " + err.Error())
	}
}

// onContextMenu opens the menu of a row at the place of the click.
func (v *viewState) onContextMenu(key string, at gui.Offset) {
	if key == "" {
		return
	}
	node, ok := v.tree.Node(key)
	if !ok {
		return
	}
	v.tree.SetSelected(key)
	run := func(id string) func() { return func() { v.runOn(id, key) } }
	var items []ui.MenuItem
	switch node.Data.(type) {
	case fileNode:
		items = []ui.MenuItem{
			{Label: "Dismiss", OnRun: run(RemoveReferenceItem)}, ui.SeparatorItem(),
			{Label: "Copy", OnRun: run(Copy)}, {Label: "Copy Path", OnRun: run(CopyPath)}, {Label: "Copy All", OnRun: run(CopyAll)},
		}
	case refNode:
		items = []ui.MenuItem{
			{Label: "Dismiss", OnRun: run(RemoveReferenceItem)}, ui.SeparatorItem(),
			{Label: "Copy", OnRun: run(Copy)}, {Label: "Copy All", OnRun: run(CopyAll)},
		}
	case historyNode:
		items = []ui.MenuItem{{Label: "Rerun", OnRun: run(Refind)}}
	default:
		return
	}
	v.menu.Show(at, items)
}

// build makes the widget of the view: the message, then the tree.
func (v *viewState) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	var children []widgets.Widget
	if v.loading {
		children = append(children, ui.NewProgressBar(ui.ProgressBarOpts{Infinite: true}))
	}
	if v.message != "" {
		children = append(children, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 6, 8, 6),
			widgets.NewText(v.message, widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.Foreground)})))
	}
	children = append(children, widgets.NewExpanded(ui.NewTree(v.tree, ui.TreeOpts{
		Controller: v.ctl, Render: v.render, OnSelect: v.onSelect, OnContextMenu: v.onContextMenu,
	})))
	col := widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return ui.NewContextMenu(col, ui.ContextMenuOpts{Controller: v.menu})
}

// render builds the contents of a row.
func (v *viewState) render(bc widgets.BuildContext, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	text := func(s string, color gui.Color) widgets.Widget {
		return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 13, color), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	gap := func(w float64) widgets.Widget {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
	}
	// the button that shows on a row the pointer is over, or that is selected (VS Code's inline actions)
	button := func(icon, tooltip, id string) widgets.Widget {
		if !st.Hovered && !st.Selected {
			return gap(8)
		}
		key := node.Key
		return ui.NewIconButton(icon, ui.IconButtonOpts{Tooltip: tooltip, OnPressed: func() { v.runOn(id, key) }})
	}
	center := rendering.CrossAxisAlignmentCenter
	switch d := node.Data.(type) {
	case fileNode:
		dir, name := path.Split(v.relPath(d.f.uri))
		row := []widgets.Widget{text(name, st.Foreground)}
		if dir != "" {
			row = append(row, gap(6), widgets.NewFlexible(text(strings.TrimSuffix(dir, "/"), colors.DescriptionForeground)))
		}
		row = append(row, widgets.NewSpacer(), ui.NewBadge(len(d.f.refs)), gap(4), button("close", "Dismiss", RemoveReferenceItem))
		return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: center})
	case refNode:
		label, hl := v.labelOf(d.r)
		st.Highlights = hl
		return widgets.NewRow([]widgets.Widget{
			widgets.NewExpanded(ui.NewHighlightedLabel(bc, label, st)), gap(4), button("close", "Dismiss", RemoveReferenceItem),
		}, widgets.RowOpts{CrossAxisAlignment: center})
	case historyNode:
		return widgets.NewRow([]widgets.Widget{
			text(d.h.word, st.Foreground), gap(6), widgets.NewFlexible(text(d.h.description, colors.DescriptionForeground)),
			widgets.NewSpacer(), button("refresh", "Rerun", Refind),
		}, widgets.RowOpts{CrossAxisAlignment: center})
	}
	return text("", st.Foreground)
}

// labelOf is the text of a location: the line around it, with the symbol marked (VS Code's getPreviewChunks of the label).
func (v *viewState) labelOf(r *refItem) (string, []ui.TextRange) {
	lines, ok := v.ensureText(r.loc.URI)
	if !ok || r.loc.Range.Start.Line >= len(lines) {
		return "", nil
	}
	pos := r.loc.Range.Start
	line := lines[pos.Line]
	before, inside, after := previewChunks(line, pos.Col, rangeEnd(pos.Line, r.loc.Range, len(line)), 8, true)
	return before + inside + after, []ui.TextRange{{Start: len(before), End: len(before) + len(inside)}}
}
