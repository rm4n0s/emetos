package pdf

import (
	"strconv"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/pdf/poppler"
)

// The text of a document in the viewer (SPEC §10.37 "PDF"): the Outline panel, Find, and Copy Page Text. The outline is read the first
// time the panel is shown; the words of the pages are read once per file, page by page in the background, the first time Find is
// opened, and the matches grow as the pages are read.

// outlineState is how far the outline is read.
type outlineState int

const (
	outlineUnread outlineState = iota
	outlineReading
	outlineRead
	outlineFailed
)

// textState is the outline and the find of a tab; it starts again when the file is read again.
type textState struct {
	outlineShown bool
	outline      outlineState
	outlineItems []poppler.OutlineItem
	outlineErr   string
	tree         *ui.TreeModel
	// The find: shown, its text and match case, the words of the pages read so far (wordsRead pages, from the first), the matches in
	// their order and the current one (-1 for none).
	findShown  bool
	findText   string
	matchCase  bool
	words      []poppler.PageWords
	wordsRead  int
	wordsErr   string
	reading    bool
	matches    []match
	current    int
	findBox    *ui.InputBoxController
	wantsFocus bool
}

func newTextState() textState {
	return textState{current: -1, tree: ui.NewTreeModel(), findBox: ui.NewInputBoxController("")}
}

// resetText forgets what was read of the text of the file (it was read again); what is shown stays, and is read again.
func (v *viewer) resetText() {
	v.text.outline, v.text.outlineItems, v.text.outlineErr = outlineUnread, nil, ""
	v.text.words, v.text.wordsRead, v.text.wordsErr, v.text.reading = nil, 0, "", false
	v.text.matches, v.text.current = nil, -1
	if err := v.text.tree.SetItems(nil); err != nil {
		v.x.api.Log().Debug("the outline cannot be emptied", "err", err)
	}
	if v.text.outlineShown && len(v.pages) > 0 {
		v.readOutline()
	}
	if v.text.findShown && len(v.pages) > 0 {
		v.readWords()
	}
}

// ---- the outline ----

// toggleOutline shows or hides the Outline panel.
func (v *viewer) toggleOutline() {
	v.text.outlineShown = !v.text.outlineShown
	if v.text.outlineShown && v.text.outline == outlineUnread && len(v.pages) > 0 {
		v.readOutline()
	}
	v.refresh()
}

// readOutline reads the outline on a goroutine of its own.
func (v *viewer) readOutline() {
	v.text.outline = outlineReading
	gen, path, password, progs, outline, ctx := v.gen, v.file.Path(), v.password, v.progs, v.x.outline, v.ctx
	go func() {
		items, err := outline(ctx, progs, path, password)
		v.x.api.Post(func() { v.outlineRead(gen, items, err) })
	}()
}

// outlineRead takes the outline: every entry is expanded.
func (v *viewer) outlineRead(gen int, items []poppler.OutlineItem, err error) {
	if v.closed || gen != v.gen {
		return
	}
	if err != nil {
		v.text.outline, v.text.outlineErr = outlineFailed, messageOf(err)
		v.x.api.Log().Warn("the outline of a PDF cannot be read", "path", v.file.Path(), "err", err)
		v.refresh()
		return
	}
	v.text.outline, v.text.outlineItems = outlineRead, items
	var keys []string
	if err := v.text.tree.SetItems(treeItems(items, "", &keys)); err != nil {
		v.x.api.Log().Warn("the outline cannot be shown", "err", err)
	}
	v.text.tree.SetExpandedKeys(keys)
	v.refresh()
}

// outlineRow is the data of a row of the outline tree.
type outlineRow struct {
	page int
}

// treeItems are the rows of entries of the outline; keys gets the keys of the rows that have children.
func treeItems(items []poppler.OutlineItem, parent string, keys *[]string) []ui.TreeItem {
	out := make([]ui.TreeItem, 0, len(items))
	for i, it := range items {
		key := parent + "/" + strconv.Itoa(i)
		item := ui.TreeItem{Key: key, Data: outlineRow{page: it.Page}, Label: it.Title}
		if len(it.Children) > 0 {
			item.Children = treeItems(it.Children, key, keys)
			*keys = append(*keys, key)
		}
		out = append(out, item)
	}
	return out
}

// openOutlineEntry goes to the page of an entry.
func (v *viewer) openOutlineEntry(node ui.TreeNode) {
	if row, ok := node.Data.(outlineRow); ok && row.page > 0 {
		v.goToPage(row.page - 1)
	}
}

// ---- find ----

// openFind shows the find widget with its box focused; a text is looked for at once.
func (v *viewer) openFind(text string) {
	if len(v.pages) == 0 {
		return
	}
	v.text.findShown, v.text.wantsFocus = true, true
	if text != "" {
		v.text.findBox.SetValue(text)
		v.setFindText(text)
	}
	if v.text.wordsRead == 0 && !v.text.reading {
		v.readWords()
	}
	v.refresh()
	if v.view != nil {
		v.text.findBox.Focus()
		v.text.findBox.SelectAll()
	}
}

// closeFind hides the find widget and its highlights, and gives the focus back to the pages.
func (v *viewer) closeFind() {
	v.text.findShown, v.text.matches, v.text.current = false, nil, -1
	if v.view != nil {
		v.view.focus.RequestFocus()
	}
	v.refresh()
}

// setFindText looks for a text in the pages read so far; the current match is the first on or after the current page.
func (v *viewer) setFindText(text string) {
	v.text.findText = text
	v.research()
}

// setMatchCase turns match case on or off.
func (v *viewer) setMatchCase(on bool) {
	v.text.matchCase = on
	v.research()
}

// research finds the text again in the pages read, and goes to the first match on or after the current page.
func (v *viewer) research() {
	v.text.matches, v.text.current = nil, -1
	for i := range v.text.wordsRead {
		v.text.matches = append(v.text.matches, findInPage(i, v.text.words[i], v.text.findText, v.text.matchCase)...)
	}
	if len(v.text.matches) > 0 {
		cur := max(v.currentPage(), 0)
		v.text.current = 0
		for i, m := range v.text.matches {
			if m.page >= cur {
				v.text.current = i
				break
			}
		}
		v.revealMatch()
	}
	v.refresh()
}

// readWords reads the words of the pages one after the other on a goroutine of its own, and hands each page to the UI thread.
func (v *viewer) readWords() {
	v.text.reading = true
	v.text.words = make([]poppler.PageWords, len(v.pages))
	gen, path, password, progs, words, ctx, n := v.gen, v.file.Path(), v.password, v.progs, v.x.words, v.ctx, len(v.pages)
	go func() {
		for i := range n {
			pw, err := words(ctx, progs, path, password, i+1)
			done := v.x.api.Post
			if err != nil {
				done(func() { v.wordsFailed(gen, i, err) })
				return
			}
			done(func() { v.wordsOf(gen, i, pw) })
		}
	}()
}

// wordsOf takes the words of a page, and adds its matches.
func (v *viewer) wordsOf(gen, page int, pw poppler.PageWords) {
	if v.closed || gen != v.gen || page != v.text.wordsRead {
		return
	}
	v.text.words[page] = pw
	v.text.wordsRead++
	v.text.reading = v.text.wordsRead < len(v.text.words)
	if v.text.findText == "" {
		v.refresh()
		return
	}
	found := findInPage(page, pw, v.text.findText, v.text.matchCase)
	v.text.matches = append(v.text.matches, found...)
	if v.text.current < 0 && len(found) > 0 && page >= max(v.currentPage(), 0) {
		v.text.current = len(v.text.matches) - len(found)
		v.revealMatch()
	}
	v.refresh()
}

// wordsFailed stops the reading of the words at a page that cannot be read: the pages before it are searched.
func (v *viewer) wordsFailed(gen, page int, err error) {
	if v.closed || gen != v.gen {
		return
	}
	v.text.reading, v.text.wordsErr = false, messageOf(err)
	if v.ctx.Err() == nil {
		v.x.api.Log().Warn("the text of a page of a PDF cannot be read", "path", v.file.Path(), "page", page+1, "err", err)
	}
	v.refresh()
}

// moveMatch goes to the next (1) or the previous (-1) match, around the ends.
func (v *viewer) moveMatch(dir int) {
	n := len(v.text.matches)
	if n == 0 {
		return
	}
	if v.text.current < 0 {
		v.text.current = 0
	} else {
		v.text.current = ((v.text.current+dir)%n + n) % n
	}
	v.revealMatch()
	v.refresh()
}

// revealMatch scrolls the current match into view: a third down the view when it is not in it.
func (v *viewer) revealMatch() {
	if v.text.current < 0 || len(v.lay.rects) != len(v.pages) {
		return
	}
	m := v.text.matches[v.text.current]
	r, p := v.lay.rects[m.page], v.pages[m.page]
	k := r.h / p.Height
	top, bottom := r.y+m.boxes[0].y0*k, r.y+m.boxes[len(m.boxes)-1].y1*k
	left, right := r.x+m.boxes[0].x0*k, r.x+m.boxes[len(m.boxes)-1].x1*k
	newTop, newLeft := v.top, v.left
	if top < v.top || bottom > v.top+v.viewH {
		newTop = top - v.viewH/3
	}
	if left < v.left || right > v.left+v.viewW {
		newLeft = left - v.viewW/3
	}
	if newTop != v.top || newLeft != v.left {
		v.scrollTo(newLeft, newTop)
	}
}

// matchesLabel is the count of the find widget: "3 of 17", "No results", or "Finding..." while the pages are read and none matched.
func (v *viewer) matchesLabel() string {
	switch {
	case v.text.findText == "":
		return ""
	case len(v.text.matches) > 0:
		count := strconv.Itoa(len(v.text.matches))
		if v.text.reading {
			count += "+"
		}
		pos := "?"
		if v.text.current >= 0 {
			pos = strconv.Itoa(v.text.current + 1)
		}
		return pos + " of " + count
	case v.text.reading:
		return "Finding..."
	}
	return "No results"
}

// ---- Copy Page Text ----

// copyPageText copies the text of the current page, as it is laid out.
func (v *viewer) copyPageText() {
	page := v.currentPage()
	if page < 0 {
		return
	}
	gen, path, password, progs, pageText, ctx := v.gen, v.file.Path(), v.password, v.progs, v.x.pageText, v.ctx
	go func() {
		text, err := pageText(ctx, progs, path, password, page+1)
		v.x.api.Post(func() {
			if v.closed || gen != v.gen {
				return
			}
			if err != nil {
				v.x.api.Log().Warn("the text of a page of a PDF cannot be read", "path", path, "page", page+1, "err", err)
				v.x.api.Notifications().Error("The text of page " + strconv.Itoa(page+1) + " cannot be copied: " + messageOf(err))
				return
			}
			if err := v.x.api.Clipboard().WriteText(text); err != nil {
				v.x.api.Notifications().Error("The text of page " + strconv.Itoa(page+1) + " cannot be copied: " + messageOf(err))
				return
			}
			v.x.api.Notifications().Info("Copied the text of page " + strconv.Itoa(page+1) + ".")
		})
	}()
}

// messageOf is what an error says to the user: Poppler's own message when it has one.
func messageOf(err error) string {
	e, ok := errors.FromError(err)
	if !ok {
		return err.Error()
	}
	if m, ok := e.Metadata["message"].(string); ok && m != "" {
		return m
	}
	return e.Message
}
