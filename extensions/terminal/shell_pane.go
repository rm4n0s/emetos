package terminal

import (
	"html"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// What shell integration shows in a pane (SPEC §10.25 "Shell integration"): a decoration in the gutter for each command, the
// menu of a decoration, the sticky scroll overlay, and the commands that move between commands.

// shellCache is the commands of the terminal with the lines their markers are on, kept while nothing changes them: the commands
// (their generation), the size (a resize reflows the lines). The pane draws with it, so a frame does not ask the emulator for
// thousands of markers.
type shellCache struct {
	valid      bool
	gen        uint64
	cols, rows int
	cmds       []cmdView
	cur        *cmdView
}

// stickyState is the sticky scroll overlay of a pane as the last frame drew it.
type stickyState struct {
	// shown says the overlay was drawn; rect is where, in the coordinates of the pane; cmd the command it shows.
	shown bool
	rect  gui.Rect
	cmd   cmdView
	hover bool
	frame emulator.Frame
	paint *gridPainter
}

// navState follows the commands the user moved to with the keys: line is the command line the view went to (-1 for none), and
// scroll the scroll of the view then, so that a view the user moved some other way starts the navigation again from the view.
type navState struct {
	line   int64
	scroll int
	top    bool
}

// shellActive says that the decorations and the sticky scroll can be shown: the setting is on and the shell sent sequences.
func (pv *paneView) shellActive() bool {
	return pv.term.si != nil && pv.term.si.Active()
}

// shellState returns the commands, from the cache when it is valid.
func (pv *paneView) shellState() (cmds []cmdView, cur *cmdView) {
	si := pv.term.si
	if si == nil {
		return nil, nil
	}
	gen := si.Gen()
	c := &pv.shell
	if !c.valid || c.gen != gen || c.cols != pv.cols || c.rows != pv.rows {
		c.cmds, c.cur = si.Commands(), si.Current()
		c.gen, c.cols, c.rows, c.valid = gen, pv.cols, pv.rows, true
	}
	return c.cmds, c.cur
}

// fixSet is the quick fixes of the last command of the terminal, nil for none.
func (pv *paneView) fixSet() *fixSet {
	if pv.env.fixes == nil {
		return nil
	}
	return pv.env.fixes(pv)
}

// decorationKind is how a command is marked in the gutter.
type decorationKind int

const (
	decoDefault decorationKind = iota // running, or the exit status is not known
	decoSuccess
	decoError
	decoFix // the last command failed and a quick fix exists
)

// decoOf is the decoration of a command.
func decoOf(c *command, finished bool) decorationKind {
	switch {
	case !finished || !c.hasExit:
		return decoDefault
	case c.exit != 0:
		return decoError
	}
	return decoSuccess
}

// decorated is a command that has a decoration on a row of the view.
type decorated struct {
	row  int
	kind decorationKind
	cmd  int // index in the cache, -1 for the running command
}

// gutterDecorations lists the decorations of the rows of the view, with the quick fix bulb on the last command when it has fixes.
func (pv *paneView) gutterDecorations(top int64, rows int, out []decorated) []decorated {
	out = out[:0]
	if pv.env.cfg().decorations == "never" || pv.env.cfg().decorations == "overviewRuler" || !pv.shellActive() || pv.shown == nil || pv.shown.Modes.AltScreen {
		return out
	}
	cmds, cur := pv.shellState()
	fixes := pv.fixSet()
	for i := len(cmds) - 1; i >= 0; i-- {
		c := &cmds[i]
		if c.startLine < top {
			break
		}
		if c.startLine >= top+int64(rows) {
			continue
		}
		kind := decoOf(&c.command, true)
		if fixes != nil && fixes.start == c.start && len(fixes.list) > 0 && pv.env.cfg().quickFix {
			kind = decoFix
		}
		out = append(out, decorated{row: int(c.startLine - top), kind: kind, cmd: i})
	}
	// the command that is running now
	if cur != nil && cur.executed != 0 && cur.execLine >= 0 && cur.startLine >= top && cur.startLine < top+int64(rows) {
		out = append(out, decorated{row: int(cur.startLine - top), kind: decoDefault, cmd: -1})
	}
	return out
}

// drawGutter draws the decorations: a circle in the gutter at the left of each command line, blue for a command that worked, red
// for one that failed and gray for one that runs, and a light bulb where a quick fix is.
func (pv *paneView) drawGutter(cv *gui.Canvas, off gui.Offset, a *atlas, decos []decorated) {
	if len(decos) == 0 {
		return
	}
	colors := pv.colors
	r := min(4.0, a.m.h/4)
	cx := off.Dx + padLeft/2 - 1
	for _, d := range decos {
		cy := off.Dy + float64(d.row)*a.m.h + a.m.h/2
		p := gui.NewPaint()
		switch d.kind {
		case decoSuccess:
			p.SetColor(colors.TerminalDecorationSuccess)
		case decoError:
			p.SetColor(colors.TerminalDecorationError)
		case decoFix:
			p.SetColor(colors.EditorLightBulbForeground)
		default:
			p.SetColor(colors.TerminalDecorationDefault)
		}
		if d.kind == decoFix {
			// a light bulb: the glass, and the cap under it
			cv.DrawCircle(gui.Offset{Dx: cx, Dy: cy - 1}, r, p)
			cv.DrawRect(gui.NewRectFromLTWH(cx-r/2, cy+r-1, r, 2), p)
			continue
		}
		cv.DrawCircle(gui.Offset{Dx: cx, Dy: cy}, r, p)
	}
}

// decorationAt is the decoration under a point of the pane (the gutter), nil when there is none.
func (pv *paneView) decorationAt(local gui.Offset) *decorated {
	if local.Dx >= padLeft || local.Dx < 0 || pv.shown == nil || pv.factory == nil {
		return nil
	}
	a, _ := pv.atlases()
	row := int(local.Dy / a.m.h)
	for _, d := range pv.gutterDecorations(pv.shown.Top, pv.rows, nil) {
		if d.row == row {
			d := d
			return &d
		}
	}
	return nil
}

// ---- the menu of a decoration ----

// commandMenu is the items of the menu of a command: the quick fixes first, then Rerun Command, Copy Command, Copy Output and
// Copy Output as HTML (VS Code's decoration menu).
func (pv *paneView) commandMenu(v cmdView) []ui.MenuItem {
	var items []ui.MenuItem
	if fx := pv.fixSet(); fx != nil && fx.start == v.start && pv.env.cfg().quickFix {
		for _, f := range fx.list {
			items = append(items, ui.MenuItem{ID: "fix:" + f.id, Label: f.label, OnRun: func() { pv.env.runFix(pv, f) }})
		}
		if len(items) > 0 {
			items = append(items, ui.SeparatorItem())
		}
	}
	hasOutput := v.hasOutput()
	items = append(items,
		ui.MenuItem{ID: "rerun", Label: "Rerun Command", Disabled: v.text == "", OnRun: func() { pv.rerun(v) }},
		ui.MenuItem{ID: "copyCommand", Label: "Copy Command", Disabled: v.text == "", OnRun: func() { pv.copyText(v.text) }},
		ui.MenuItem{ID: "copyOutput", Label: "Copy Output", Disabled: !hasOutput, OnRun: func() { pv.copyText(pv.term.si.output(v)) }},
		ui.MenuItem{ID: "copyHtml", Label: "Copy Output as HTML", Disabled: !hasOutput, OnRun: func() { pv.copyText(pv.outputHTML(v)) }},
	)
	return items
}

// rerun runs the command line of a command again.
func (pv *paneView) rerun(v cmdView) {
	if v.text == "" {
		return
	}
	pv.sendText(v.text, true, strings.Contains(v.text, "\n") && pv.term.Emulator().Modes().BracketedPaste)
}

// copyText puts text on the clipboard.
func (pv *paneView) copyText(text string) {
	if text == "" || pv.binding == nil {
		return
	}
	pv.binding.Clipboard().SetData(pv.env.clipboardCtx, services.NewClipboardData(text), func(error) {})
}

// openCommandMenu opens the menu of a decoration at a point of the window.
func (pv *paneView) openCommandMenu(d decorated, at gui.Offset) {
	cmds, cur := pv.shellState()
	var v cmdView
	switch {
	case d.cmd >= 0 && d.cmd < len(cmds):
		v = cmds[d.cmd]
	case cur != nil:
		v = *cur
	default:
		return
	}
	if pv.env.commandMenu != nil {
		pv.env.commandMenu(pv, pv.commandMenu(v), at)
	}
}

// outputHTML is the output of a command as HTML: a <pre> with the text, in the foreground and background colors of the terminal,
// with the cells that have a color or a style in spans. It is what Copy Output as HTML puts on the clipboard.
//
// Glutter: the clipboard carries plain text only, so the markup is copied as text and not offered as text/html; Glutter should
// provide a clipboard with more than one representation (docs/GLUTTER-ISSUES.md).
func (pv *paneView) outputHTML(v cmdView) string {
	if !v.hasOutput() {
		return ""
	}
	em := pv.term.Emulator()
	first := em.FirstLine()
	var f emulator.Frame
	em.ReadLines(int(v.execLine-first), int(v.endLine-v.execLine), &f)
	var b strings.Builder
	b.WriteString(`<pre style="color:` + cssColor(pv.th.fg) + `;background-color:` + cssColor(pv.th.bg) + `;font-family:monospace">`)
	for y := 0; y < f.Rows; y++ {
		var runStyle string
		open := false
		for x := 0; x < f.Cols; x++ {
			c := f.Cell(y, x)
			if c.Flags&(emulator.CellTail|emulator.CellWideFill) != 0 {
				continue
			}
			st := f.StyleOf(c)
			css := pv.cellCSS(&f, st)
			if css != runStyle || !open {
				if open {
					b.WriteString("</span>")
				}
				open = false
				runStyle = css
				if css != "" {
					b.WriteString(`<span style="` + css + `">`)
					open = true
				}
			}
			t := f.Text(y, x)
			if t == "" {
				t = " "
			}
			b.WriteString(html.EscapeString(t))
		}
		if open {
			b.WriteString("</span>")
		}
		if y < f.Rows-1 || true {
			b.WriteString("\n")
		}
	}
	b.WriteString("</pre>")
	return b.String()
}

// cellCSS is the style attribute of a style: colors, bold, italic, underline and strike-through.
func (pv *paneView) cellCSS(f *emulator.Frame, st emulator.Style) string {
	var css []string
	fg := colorOf(st.Fg, &f.Palette, 0)
	bg := colorOf(st.Bg, &f.Palette, 0)
	if st.Attrs&emulator.AttrInverse != 0 {
		def := func(c, d uint32) uint32 {
			if c == 0 {
				return d
			}
			return c
		}
		fg, bg = def(bg, pv.th.bg), def(fg, pv.th.fg)
	}
	if fg != 0 {
		css = append(css, "color:"+cssColor(fg))
	}
	if bg != 0 {
		css = append(css, "background-color:"+cssColor(bg))
	}
	if st.Attrs&emulator.AttrBold != 0 {
		css = append(css, "font-weight:bold")
	}
	if st.Attrs&emulator.AttrItalic != 0 {
		css = append(css, "font-style:italic")
	}
	var deco []string
	if st.Underline != emulator.UnderlineNone {
		deco = append(deco, "underline")
	}
	if st.Attrs&emulator.AttrStrike != 0 {
		deco = append(deco, "line-through")
	}
	if len(deco) > 0 {
		css = append(css, "text-decoration:"+strings.Join(deco, " "))
	}
	return strings.Join(css, ";")
}

// cssColor is #rrggbb of an 0xAARRGGBB or 0xRRGGBB color.
func cssColor(c uint32) string {
	s := strconv.FormatUint(uint64(c&0xFFFFFF), 16)
	return "#" + strings.Repeat("0", 6-len(s)) + s
}

// ---- moving between commands ----

// markLines are the lines the navigation moves between, in order: the command line of each command, the marks the user set
// and the prompt that waits for a command.
func (pv *paneView) markLines() []int64 {
	if pv.term.si == nil {
		return nil
	}
	cmds, cur := pv.shellState()
	var lines []int64
	for _, c := range cmds {
		lines = append(lines, c.top())
	}
	lines = append(lines, pv.term.si.Marks()...)
	if cur != nil {
		lines = append(lines, cur.top())
	}
	slices.Sort(lines)
	return slices.Compact(lines)
}

// viewTop is the absolute line at the top of the view.
func (pv *paneView) viewTop() int64 {
	if pv.shown != nil {
		return pv.shown.Top
	}
	return 0
}

// navRef is the line the navigation starts from: the one it went to last, when the view is where it left it; else the first
// line of the view (a view at the bottom starts from the prompt that waits).
func (pv *paneView) navRef(marks []int64) int64 {
	if pv.nav.line >= 0 && pv.nav.scroll == pv.scroll {
		return pv.nav.line
	}
	if pv.scroll == 0 && len(marks) > 0 {
		return marks[len(marks)-1] // the prompt at the bottom is the line the first Ctrl+Up leaves
	}
	return pv.viewTop()
}

// scrollToLine puts an absolute line at the top of the view.
func (pv *paneView) scrollToLine(line int64) {
	first := pv.term.Emulator().FirstLine()
	idx := int(line - first)
	pv.scrollTo(pv.frame.Scrollback - idx)
	pv.nav.line, pv.nav.scroll, pv.nav.top = line, pv.scroll, false
}

// scrollToCommand scrolls to the previous (up) or next command: the command line of a command goes to the top of the view. Past
// the first command the view goes to the top of the buffer, past the last to the bottom.
func (pv *paneView) scrollToCommand(up bool) {
	marks := pv.markLines()
	ref := pv.navRef(marks)
	if up {
		for i := len(marks) - 1; i >= 0; i-- {
			if marks[i] < ref {
				pv.scrollToLine(marks[i])
				return
			}
		}
		pv.scrollTo(pv.frame.Scrollback)
		pv.nav = navState{line: -1, scroll: pv.scroll, top: true}
		return
	}
	for _, m := range marks {
		if m > ref {
			pv.scrollToLine(m)
			return
		}
	}
	pv.scrollTo(0)
	pv.nav = navState{line: -1}
}

// selNavState is the selection that selectToCommand made: it covers the lines from target up to the one above anchor.
type selNavState struct{ anchor, target int64 }

// selectToCommand extends the selection to the previous (up) or next command: whole lines, from the line the selection began
// (the line the navigation started from, a prompt) to the command line of the command it reaches. Down shrinks it back.
func (pv *paneView) selectToCommand(up bool) {
	marks := pv.markLines()
	if len(marks) == 0 {
		return
	}
	cols := max(pv.cols, 1)
	n := pv.selNav
	if n == nil || !pv.sel.active || pv.sel.head != (pos{line: n.target}) || pv.sel.anchor != (pos{line: n.anchor - 1, col: cols - 1}) {
		ref := pv.navRef(marks)
		n = &selNavState{anchor: ref, target: ref}
		pv.selNav = n
	}
	target := int64(-1)
	if up {
		for i := len(marks) - 1; i >= 0; i-- {
			if marks[i] < n.target {
				target = marks[i]
				break
			}
		}
		if first := pv.term.Emulator().FirstLine(); target < 0 && n.target > first {
			target = first
		}
		if target < 0 {
			return
		}
	} else {
		for _, m := range marks {
			if m > n.target {
				target = m
				break
			}
		}
		if target < 0 {
			return
		}
	}
	n.target = target
	if target >= n.anchor {
		// back where it began: nothing is selected
		pv.selNav = nil
		pv.setSelection(selection{})
		pv.scrollToLine(target)
		return
	}
	pv.setSelection(selection{active: true, anchor: pos{line: n.anchor - 1, col: cols - 1}, head: pos{line: target}})
	pv.scrollToLine(target)
}

// selectLine extends the selection by a line up or down (VS Code's selectToPreviousLine and selectToNextLine).
func (pv *paneView) selectLine(up bool) {
	cols := max(pv.cols, 1)
	s := pv.sel
	if !s.active {
		line := pv.viewTop() + int64(pv.rows) - 1
		if f := pv.shown; f != nil {
			line = f.Top + int64(f.Cursor.Y)
		}
		s = selection{active: true, anchor: pos{line: line, col: 0}, head: pos{line: line, col: cols - 1}}
		if !up {
			pv.setSelection(s)
			return
		}
	}
	if up {
		s.anchor.line = max(s.anchor.line-1, pv.term.Emulator().FirstLine())
	} else {
		s.head.line++
	}
	pv.setSelection(s)
}

// ---- sticky scroll ----

// stickyLayout is what the overlay shows for the view that starts at a line: the first line it shows, how many lines, how many
// pixels it is pushed up so that it does not cover the prompt of the next command, and the command. ok is false when there is
// nothing to show.
//
// Ported from VS Code: src/vs/workbench/contrib/terminalContrib/stickyScroll/browser/terminalStickyScrollOverlay.ts
// (_refreshNow and _updateContent)
func (pv *paneView) stickyLayout(viewTop int64, cellH float64) (first int64, count int, shift float64, cmd cmdView, ok bool) {
	cfg := pv.env.cfg()
	if !cfg.stickyOn || !pv.shellActive() || pv.shown == nil || pv.shown.Modes.AltScreen || pv.rows < 1 {
		return 0, 0, 0, cmdView{}, false
	}
	cmds, cur := pv.shellState()
	// the command that owns the line at the top: the last one whose prompt starts at or above it, the running command when
	// it is below all of them
	var v *cmdView
	partial := false
	if cur != nil && cur.top() >= 0 && viewTop >= cur.top() {
		v, partial = cur, true
	} else {
		for i := len(cmds) - 1; i >= 0; i-- {
			if cmds[i].top() <= viewTop {
				v = &cmds[i]
				break
			}
		}
	}
	if v == nil || v.startLine < 0 {
		return 0, 0, 0, cmdView{}, false
	}
	if text := strings.ToLower(strings.TrimSpace(v.text)); text != "" {
		for _, ig := range cfg.stickyIgnored {
			if strings.ToLower(ig) == text {
				return 0, 0, 0, cmdView{}, false
			}
		}
	}
	if partial && (v.executed == 0 || v.execLine < 0) {
		return 0, 0, 0, cmdView{}, false
	}
	promptRows := pv.term.si.promptRows(*v)
	cmdRows := v.commandRows()
	start := v.startLine - int64(promptRows-1)
	if viewTop <= start {
		return 0, 0, 0, cmdView{}, false
	}
	rowOffset := 0
	if !partial && v.endLine >= 0 {
		rowOffset = int(max(viewTop-v.endLine+1, 0))
	}
	maxLines := min(cfg.stickyMax, pv.rows*2/5)
	count = min(promptRows+cmdRows-1, maxLines) - rowOffset
	if count <= 0 {
		return 0, 0, 0, cmdView{}, false
	}
	// a pager (less, git log) at the bottom of the screen has a ":" or "(END)" where the cursor is: nothing to pin
	if partial && pv.scroll == 0 && pv.shown.Cursor.Y == pv.rows-1 {
		last := pv.shown.RowText(pv.rows - 1)
		if pv.shown.Cursor.X == 1 && strings.HasPrefix(last, ":") || pv.shown.Cursor.X == 5 && strings.HasPrefix(last, "(END)") {
			return 0, 0, 0, cmdView{}, false
		}
	}
	// it never covers the prompt of the next command: pushed up by the rows it would overlap
	if !partial && v.endLine >= 0 {
		lastLine := min(v.endLine, pv.frame.Top+int64(pv.frame.Cursor.Y))
		if over := viewTop + int64(count) - lastLine; over > 0 {
			shift = float64(over) * cellH
		}
	}
	return start + int64(rowOffset), count, shift, *v, true
}

// drawSticky draws the sticky scroll overlay over the top of the view, and remembers where for the pointer.
func (pv *paneView) drawSticky(cv *gui.Canvas, off gui.Offset, size gui.Size, a *atlas, d *decor) {
	pv.sticky.shown = false
	if pv.shown == nil {
		return
	}
	first, count, shift, cmd, ok := pv.stickyLayout(pv.shown.Top, a.m.h)
	if !ok {
		return
	}
	em := pv.term.Emulator()
	idx := int(first - em.FirstLine())
	if idx < 0 {
		return
	}
	st := &pv.sticky
	em.ReadLines(idx, count, &st.frame)
	if st.frame.Rows == 0 {
		return
	}
	if st.paint == nil {
		st.paint = newGridPainter()
	}
	cols := min(pv.cols, st.frame.Cols)
	s := shown{
		f: &st.frame, rows: min(count, st.frame.Rows), cols: cols, originX: off.Dx + padLeft, originY: off.Dy - shift, th: &pv.th, cfg: pv.env.cfg(),
		cursorOn: false,
	}
	st.paint.build(a, d, s)
	a.flush()
	h := float64(s.rows)*a.m.h - shift
	if h <= 0 {
		return
	}
	rect := gui.NewRectFromLTWH(off.Dx, off.Dy, size.Width-scrollbarW, h)
	bg := pv.colors.TerminalStickyScrollBackground
	if bg.Alpha() == 0 {
		bg = gui.NewColor(pv.th.bg)
	}
	if st.hover {
		if hv := pv.colors.TerminalStickyScrollHoverBackground; hv.Alpha() != 0 {
			bg = hv
		}
	}
	cv.Save()
	cv.ClipRect(rect)
	p := gui.NewPaint()
	p.SetColor(bg)
	cv.DrawRect(rect, p)
	st.paint.draw(cv, a, d)
	cv.Restore()
	// the edge: the border of the theme, else a shadow like the editor's
	if b := pv.colors.TerminalStickyScrollBorder; b.Alpha() != 0 {
		bp := gui.NewPaint()
		bp.SetColor(b)
		cv.DrawRect(gui.NewRectFromLTWH(rect.Left, rect.Bottom-1, rect.Width(), 1), bp)
	} else if sh := pv.colors.ScrollbarShadow; sh.Alpha() != 0 {
		for i := 0; i < 3; i++ {
			sp := gui.NewPaint()
			sp.SetColor(sh.WithOpacity(0.35 / float64(i+1)))
			cv.DrawRect(gui.NewRectFromLTWH(rect.Left, rect.Bottom+float64(i), rect.Width(), 1), sp)
		}
	}
	st.shown, st.rect, st.cmd = true, rect, cmd
}

// overSticky tells whether a point of the pane is on the overlay.
func (pv *paneView) overSticky(local gui.Offset) bool {
	return pv.sticky.shown && pv.sticky.rect.Contains(local)
}

// stickyHover follows the pointer over the overlay (its background changes).
func (pv *paneView) stickyHover(local gui.Offset) {
	over := pv.overSticky(local)
	if over != pv.sticky.hover {
		pv.sticky.hover = over
		if pv.render != nil {
			pv.render.MarkNeedsPaint()
		}
		pv.rebuild()
	}
}

// clickSticky scrolls to the command the overlay shows.
func (pv *paneView) clickSticky() {
	pv.scrollToLine(pv.sticky.cmd.top())
}

// shellPointerDown handles a press on a decoration or on the sticky overlay; it reports whether it was theirs.
func (pv *paneView) shellPointerDown(e *gestures.PointerDownEvent, btn emulator.MouseButton, local gui.Offset) bool {
	if pv.overSticky(local) {
		switch btn {
		case emulator.MouseLeft:
			pv.clickSticky()
		case emulator.MouseRight:
			if pv.env.stickyMenu != nil {
				pv.env.stickyMenu(pv, e.Position())
			}
		}
		return true
	}
	if btn == emulator.MouseLeft {
		if d := pv.decorationAt(local); d != nil {
			pv.openCommandMenu(*d, e.Position())
			return true
		}
	}
	return false
}
