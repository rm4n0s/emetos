package httpclient

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/widgets"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/history"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
)

// historyView is the History view: the requests that were sent, newest first, grouped by day. It is used on the UI thread.
type historyView struct {
	e      *Extension
	handle extension.ViewHandle
	tree   *ui.TreeModel
	ctl    *ui.TreeController
	menu   *ui.ContextMenuController
	// entries are the entries of the rows, by sequence number.
	entries map[int]history.Entry
}

// A row of the tree is a day (dayRow) or an entry (entryRow).
type (
	dayRow   struct{ title string }
	entryRow struct{ entry history.Entry }
)

// newHistoryView makes the view.
func newHistoryView(e *Extension) *historyView {
	return &historyView{
		e: e, ctl: ui.NewTreeController(), menu: ui.NewContextMenuController(), entries: map[int]history.Entry{},
		tree: ui.NewTreeModel(ui.TreeModelOpts{FindMode: ui.FindModeFilter}),
	}
}

// provide gives the view its content.
func (v *historyView) provide() error {
	h, err := v.e.api.Views().Provide(HistoryViewID, extension.ViewContent{Build: v.build})
	if err != nil {
		return err
	}
	v.handle = h
	v.rebuild()
	return nil
}

// entryKey is the key of the row of an entry.
func entryKey(seq int) string { return "h:" + strconv.Itoa(seq) }

// dayTitle names a day: Today, Yesterday, or the date.
func dayTitle(day, today time.Time) string {
	switch {
	case sameDay(day, today):
		return "Today"
	case sameDay(day, today.AddDate(0, 0, -1)):
		return "Yesterday"
	}
	return day.Format("Mon, 2 Jan 2006")
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// rebuild makes the rows from the log.
func (v *historyView) rebuild() {
	if v.handle == nil {
		return
	}
	now := v.e.opts.Now()
	var items []ui.TreeItem
	var expand []string
	v.entries = map[int]history.Entry{}
	for _, en := range v.e.hist.List() {
		v.entries[en.Seq] = en
		at := en.Time.In(now.Location())
		dayKey := "d:" + at.Format("2006-01-02")
		if len(items) == 0 || items[len(items)-1].Key != dayKey {
			items = append(items, ui.TreeItem{Key: dayKey, Data: dayRow{dayTitle(at, now)}, Label: dayTitle(at, now), HasChildren: true})
			expand = append(expand, dayKey)
		}
		last := &items[len(items)-1]
		last.Children = append(last.Children, ui.TreeItem{Key: entryKey(en.Seq), Data: entryRow{en}, Label: en.Method + " " + en.URL})
	}
	opened := map[string]bool{}
	for _, k := range v.tree.ExpandedKeys() {
		opened[k] = true
	}
	seen := map[string]bool{}
	for _, it := range v.tree.Rows() {
		seen[it.Key] = true
	}
	if err := v.tree.SetItems(items); err != nil {
		v.e.api.Log().Warn("the tree of the history cannot be made", "err", err)
		return
	}
	// a day that is new is open, a day the user closed stays closed
	var keys []string
	for _, k := range expand {
		if opened[k] || !seen[k] {
			keys = append(keys, k)
		}
	}
	v.tree.SetExpandedKeys(keys)
	v.handle.Refresh()
}

// build makes the widget of the view.
func (v *historyView) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	var children []widgets.Widget
	if len(v.entries) == 0 {
		children = append(children, note(bc, "The requests you send are listed here, newest first. Nothing is kept in the project.", colors.DescriptionForeground))
	} else {
		children = append(children, widgets.NewExpanded(ui.NewTree(v.tree, ui.TreeOpts{
			Controller: v.ctl, Render: v.render, OnOpen: v.onOpen, OnContextMenu: v.onContextMenu,
		})))
	}
	return ui.NewContextMenu(column(children...), ui.ContextMenuOpts{Controller: v.menu})
}

// statusColor is the color of a status code, by its class.
func statusColor(code int, c ui.Colors) gui.Color {
	switch {
	case code >= 500:
		return colorDelete
	case code >= 400:
		return colorPost
	case code >= 300:
		return colorPut
	case code >= 200:
		return colorGet
	}
	return c.DescriptionForeground
}

// shortURL is a URL without its scheme, for a narrow row.
func shortURL(u string) string {
	if _, rest, ok := strings.Cut(u, "://"); ok {
		return rest
	}
	return u
}

// render builds the contents of a row.
func (v *historyView) render(bc widgets.BuildContext, tn ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	switch d := tn.Data.(type) {
	case dayRow:
		return widgets.NewRow([]widgets.Widget{rowText(bc, d.title, st.Foreground)}, widgets.RowOpts{CrossAxisAlignment: crossCenter})
	case entryRow:
		en := d.entry
		row := []widgets.Widget{methodBadge(bc, en.Method), widgets.NewFlexible(rowText(bc, rowURL(en.URL), st.Foreground)), gap(8)}
		if en.Status > 0 {
			row = append(row, rowText(bc, strconv.Itoa(en.Status), statusColor(en.Status, colors)))
		} else {
			row = append(row, ui.Icon("error", 12, colors.ErrorForeground))
		}
		row = append(row, gap(8), rowText(bc, en.Time.In(v.e.opts.Now().Location()).Format("15:04"), colors.DescriptionForeground), gap(8))
		return ui.NewHover(widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: crossCenter}), ui.HoverOpts{Text: entryTip(en), Compact: true})
	}
	return rowText(bc, "", st.Foreground)
}

// entryTip is the hover of an entry: what was sent and what came back.
func entryTip(en history.Entry) string {
	result := en.Error
	if en.Status > 0 {
		result = strconv.Itoa(en.Status) + " " + en.StatusText
	}
	if result == "" {
		result = "no response"
	}
	return fmt.Sprintf("%s %s\n%s, %d ms, %s\n%s", en.Method, en.URL, strings.TrimSpace(result), en.DurationMs, byteSize(en.Size), en.Time.Local().Format("2 Jan 2006 15:04:05"))
}

// byteSize is a size in bytes, KB or MB.
func byteSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return strconv.FormatInt(n, 10) + " B"
}

// entryOf is the entry a key of the tree stands for.
func (v *historyView) entryOf(key string) (history.Entry, bool) {
	n, ok := v.tree.Data(key)
	if !ok {
		return history.Entry{}, false
	}
	if row, isEntry := n.(entryRow); isEntry {
		return row.entry, true
	}
	return history.Entry{}, false
}

// onOpen is Enter or a double click: the entry is opened.
func (v *historyView) onOpen(tn ui.TreeNode) {
	if row, ok := tn.Data.(entryRow); ok {
		v.e.openEntry(row.entry)
	}
}

// onContextMenu opens the menu of a row at the place of the click.
func (v *historyView) onContextMenu(key string, at gui.Offset) {
	e := v.e
	clear := ui.MenuItem{Label: "Clear History", Disabled: len(v.entries) == 0, OnRun: e.clearHistory}
	en, ok := v.entryOf(key)
	if !ok {
		v.menu.Show(at, []ui.MenuItem{clear})
		return
	}
	v.tree.SetSelected(key)
	v.menu.Show(at, []ui.MenuItem{
		{Label: "Open", OnRun: func() { e.openEntry(en) }},
		{Label: "Save to Collection...", OnRun: func() { e.saveEntry(en) }},
		{Label: "Copy URL", OnRun: func() { e.copyText(en.URL) }},
		ui.SeparatorItem(), clear,
	})
}

// ---- what the commands do with the history ----

// RecordSent adds a request that was sent to the history of the workspace (the request tab calls it after each send) and builds the
// view again. The secrets in the request are masked before anything is kept.
func (e *Extension) RecordSent(s history.Sent) error {
	en, err := e.hist.Add(s)
	if err != nil {
		return err
	}
	if en.Method != "" {
		e.historyView.rebuild()
	}
	return nil
}

// withEntry runs fn for the entry the command names ({"seq": n}), or the one the user picks from the list.
func (e *Extension) withEntry(args any, placeholder string, fn func(history.Entry)) {
	if m, ok := args.(map[string]any); ok {
		if n, ok := seqOf(m["seq"]); ok {
			if en, found := e.hist.Get(n); found {
				fn(en)
			} else {
				e.api.Notifications().Warn("This entry is not in the history any more.")
			}
			return
		}
	}
	entries := e.hist.List()
	if len(entries) == 0 {
		e.api.Notifications().Info("The history is empty: the requests you send are listed there.")
		return
	}
	items := make([]extension.QuickPickItem, len(entries))
	for i, en := range entries {
		desc := en.Error
		if en.Status > 0 {
			desc = strconv.Itoa(en.Status)
		}
		items[i] = extension.QuickPickItem{Label: methodLabel(en.Method) + " " + shortURL(en.URL), Description: desc}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: placeholder, Items: items, MatchOnDescription: true}, func(i int) {
		if i >= 0 && i < len(entries) {
			fn(entries[i])
		}
	})
	if err != nil {
		e.fail("show the history", err)
	}
}

// requestOf is the request of an entry; an entry that was too large to keep its request has the method and the URL.
func requestOf(en history.Entry) format.Request {
	if en.Request != nil {
		return *en.Request
	}
	return format.Request{Method: en.Method, URL: en.URL}
}

// openEntry opens the request of an entry as a new request that is not saved anywhere. (Step 27.4 opens it in the request tab; until
// then it is an untitled document with the text of the request file.)
func (e *Extension) openEntry(en history.Entry) {
	req := requestOf(en)
	text, err := format.Marshal(req)
	if err != nil {
		e.fail("show the request", err)
		return
	}
	if en.Body != "" {
		text += "\n// The text of the body:\n// " + strings.ReplaceAll(strings.TrimRight(en.Body, "\n"), "\n", "\n// ") + "\n"
	}
	if _, err := e.api.Documents().OpenUntitled("jsonc", text); err != nil {
		e.fail("open the request", err)
	}
}

// saveEntry asks for a collection or folder and saves the request of an entry there. What the history masked stays masked.
func (e *Extension) saveEntry(en history.Entry) {
	if !e.ready() {
		return
	}
	var groups []*store.Node
	e.tree.Walk(func(n *store.Node) bool {
		if (n.Kind == store.KindCollection || n.Kind == store.KindFolder) && !n.ReadOnly && n.Err == nil {
			groups = append(groups, n)
		}
		return true
	})
	if len(groups) == 0 {
		e.api.Notifications().Info("There is no collection to save the request in. Make one with New Collection first.")
		return
	}
	items := make([]extension.QuickPickItem, len(groups))
	for i, g := range groups {
		items[i] = extension.QuickPickItem{Label: "$(" + nodeIcon(g) + ") " + g.Name, Description: e.where(g)}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Save the request in which collection or folder?", Items: items, MatchOnDescription: true}, func(i int) {
		if i < 0 || i >= len(groups) {
			return
		}
		e.saveEntryIn(en, groups[i])
	})
	if err != nil {
		e.fail("show the collections", err)
	}
}

// saveEntryIn saves the request of an entry in a collection or folder.
func (e *Extension) saveEntryIn(en history.Entry, parent *store.Node) {
	req := requestOf(en)
	if req.Body != nil { // the store names the files of the body after the new request
		b := *req.Body
		b.File, b.Variables = "", ""
		req.Body = &b
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = strings.TrimSpace(req.Method + " " + pathOf(req.URL))
	}
	masked := strings.Contains(en.URL, history.Masked) || strings.Contains(en.Body, history.Masked)
	if data, err := format.Marshal(req); err == nil && strings.Contains(data, history.Masked) {
		masked = true
	}
	var path string
	e.async(func(ctx context.Context) (err error) {
		path, err = e.store.CreateRequest(ctx, parent.Path, store.RequestFiles{Request: req, Body: en.Body, Variables: en.Variables})
		return err
	}, func(err error) {
		if err != nil {
			e.fail("save the request", err)
			return
		}
		msg := "Saved \"" + req.Name + "\" in \"" + parent.Name + "\"."
		if masked {
			msg += " The history had masked its secrets (" + history.Masked + "): fill them in, or use {{variables}}."
		}
		e.api.Notifications().Info(msg, extension.NotificationAction{Label: "Open", Run: func() { e.openFile(path) }})
		e.collections.reveal(path)
	})
}

// pathOf is the path part of a URL, for the name of a saved request.
func pathOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Path != "" {
		return u.Path
	}
	return shortURL(raw)
}

// clearHistory asks, then forgets every entry.
func (e *Extension) clearHistory() {
	if len(e.hist.List()) == 0 {
		e.api.Notifications().Info("The history is already empty.")
		return
	}
	err := e.api.Dialogs().Ask(extension.DialogSpec{
		Type: extension.DialogWarning, Message: "Clear the history of sent requests?", Detail: "The requests in your collections are not touched.",
		Buttons: []extension.DialogButton{{Label: "Clear History"}, {Label: "Cancel", Cancel: true}},
	}, func(r extension.DialogResult) {
		if r.Button != 0 {
			return
		}
		if err := e.hist.Clear(); err != nil {
			e.fail("clear the history", err)
			return
		}
		e.historyView.rebuild()
	})
	if err != nil {
		e.fail("ask", err)
	}
}

// seqOf reads the number of an entry a command was given: a JSON number or an integer.
func seqOf(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

// rowURL is the URL of a row of the History view: the path and the query when the URL has a host (the host is in the hover), as a narrow
// side bar has no room for both.
func rowURL(u string) string {
	_, rest, ok := strings.Cut(u, "://")
	if !ok {
		return u
	}
	if i := strings.IndexAny(rest, "/?"); i >= 0 {
		return rest[i:]
	}
	return rest
}
