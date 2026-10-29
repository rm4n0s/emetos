package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// The links of a terminal (SPEC §10.25 "Find and links"): the addresses and the files with a line and column that the program
// printed, and the hyperlinks it set with OSC 8. A link is looked for under the pointer, where it is underlined and a tooltip
// says how to follow it; a click with the Ctrl key (the Alt key when editor.multiCursorModifier is ctrlCmd) opens it.

const (
	// linkCacheTTL is how long the answer about the files of a line is kept (VS Code's link cache keeps them 10 seconds).
	linkCacheTTL = 10 * time.Second
	// linkCacheMax is the most lines whose files are kept.
	linkCacheMax = 128
	// tooltipDelay is how long the pointer rests on a link before the tooltip shows (workbench.hover.delay).
	tooltipDelay = 500 * time.Millisecond
)

// linkKind is what a link opens.
type linkKind int

const (
	linkWeb  linkKind = iota // an address: the desktop opens it
	linkFile                 // a file of the workspace: an editor opens it
	linkOSC8                 // a hyperlink the program set
)

// activeLink is a link under the pointer.
type activeLink struct {
	kind linkKind
	// url is the address of a web link or the target of an OSC 8 link; path the file of a file link, with the line and column.
	url      string
	path     string
	row, col int
	// segs are the cells of the link, a segment for each row.
	segs []mark
}

// lineLinks is what was found about the files of a line of text.
type lineLinks struct {
	at    time.Time
	files []fileLink
	done  bool
}

// hoverState is the link under the pointer and the tooltip that says how to follow it.
type hoverState struct {
	// cell and gen are the cell last looked at and the generation of the buffer then, so that a pointer moving inside a cell
	// does not look again.
	cell     pos
	gen      uint64
	checked  bool
	link     *activeLink
	tip      bool
	tipTimer *gestures.Timer
	// modDown says that the key of the link was down at the last pointer event (the pointer is the hand then).
	modDown bool
	// pendingClick is a click on a cell whose line was still being checked.
	pendingClick *pos
	// at is where the pointer is, in the coordinates of the pane.
	at gui.Offset
}

// linkModifier is the modifier of the clicks that follow a link: Ctrl, or Alt when the multi-cursor modifier of the editor is Ctrl.
func (pv *paneView) linkModifier() emulator.Mod {
	if pv.env.altClick != nil && pv.env.altClick() {
		return emulator.ModAlt
	}
	return emulator.ModCtrl
}

// linkClickLabel is the text of the hover that says what to press.
func (pv *paneView) linkClickLabel() string {
	if pv.linkModifier() == emulator.ModAlt {
		return "alt + click"
	}
	return "ctrl + click"
}

// folderOf is the folder links are confined to.
func (pv *paneView) folderOf() string {
	if pv.env.folder == nil {
		return ""
	}
	return pv.env.folder()
}

// linkEnvironment is what the detectors need: the working folder of the terminal and a check that a path is a file under the
// folder of the workspace (symbolic links followed). The check does file system calls; call it from a goroutine.
func (pv *paneView) linkEnvironment() linkEnv {
	folder := pv.folderOf()
	return linkEnv{cwd: pv.term.Cwd(), home: pv.env.home, stat: func(path string) bool { return isFileUnder(folder, path) }}
}

// isFileUnder tells whether path is a file that is in the folder, after the symbolic links of both are followed.
func isFileUnder(folder, path string) bool {
	if folder == "" {
		return false
	}
	root, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return false
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	info, err := os.Stat(real)
	return err == nil && info.Mode().IsRegular()
}

// ---- looking for a link ----

// linkAt finds the link at a place of the buffer. pending says that the files of the line are being checked and may give a
// link soon.
func (pv *paneView) linkAt(p pos) (link *activeLink, pending bool) {
	if pv.shown == nil {
		return nil, false
	}
	row := int(p.line - pv.shown.Top)
	if row < 0 || row >= pv.shown.Rows || p.col < 0 || p.col >= pv.shown.Cols {
		return nil, false
	}
	if l := pv.hyperlinkAt(row, p.col); l != nil {
		return l, false
	}
	line, ok := lineAround(pv.term.Emulator(), &pv.linkFrame, p.line, maxWebLinkLength)
	if !ok {
		return nil, false
	}
	idx := line.indexAt(int(p.line-line.first), p.col)
	if idx < 0 {
		return nil, false
	}
	for _, wl := range detectWebLinks(line.text) {
		if strings.HasPrefix(strings.ToLower(wl.url), "file://") {
			continue // a file address is a file link, if the file is there
		}
		if idx >= wl.start && idx < wl.end && runeCount(wl.url) <= maxWebLinkLength {
			return &activeLink{kind: linkWeb, url: wl.url, row: none, col: none, segs: line.segments(wl.start, wl.end)}, false
		}
	}
	if runeCount(line.text) > maxLinkLine {
		return nil, false
	}
	entry, pending := pv.filesOfLine(line)
	for _, fl := range entry {
		if idx >= fl.start && idx < fl.end {
			return &activeLink{kind: linkFile, path: fl.path, row: fl.row, col: fl.col, segs: line.segments(fl.start, fl.end)}, false
		}
	}
	return nil, pending
}

// runeCount is the number of characters of a text.
func runeCount(s string) int { return len([]rune(s)) }

// filesOfLine returns the files named in a line of text. The check against the file system runs on a goroutine the first time
// a line is asked about (and again when the answer is older than linkCacheTTL); until it is done the answer is pending.
func (pv *paneView) filesOfLine(line textLine) (files []fileLink, pending bool) {
	key := pv.term.Cwd() + "\x00" + line.text
	if e, ok := pv.linkCache[key]; ok {
		if e.done && time.Since(e.at) < linkCacheTTL {
			return e.files, false
		}
		if !e.done {
			return nil, true
		}
	}
	if len(pv.linkCache) >= linkCacheMax {
		clear(pv.linkCache)
	}
	e := &lineLinks{at: time.Now()}
	pv.linkCache[key] = e
	env, text, post := pv.linkEnvironment(), line.text, pv.env.post
	go func() {
		found := detectFileLinks(text, env)
		post(func() {
			e.files, e.done, e.at = found, true, time.Now()
			pv.linksReady()
		})
	}()
	return nil, true
}

// linksReady is told that the files of a line were checked: the link under the pointer is looked for again, and a click that
// waited for the answer is followed.
func (pv *paneView) linksReady() {
	if c := pv.hover.pendingClick; c != nil {
		pv.hover.pendingClick = nil
		if l, _ := pv.linkAt(*c); l != nil {
			pv.followLink(l)
		}
	}
	if pv.hover.checked {
		pv.lookAt(pv.hover.cell, true)
	}
}

// hyperlinkAt finds the OSC 8 link of a cell of the rows shown: the cells around it that have the same hyperlink.
func (pv *paneView) hyperlinkAt(row, col int) *activeLink {
	f := pv.shown
	idx := f.StyleOf(f.Cell(row, col)).Link
	if idx == 0 {
		return nil
	}
	target := f.LinkOf(f.StyleOf(f.Cell(row, col))).URL
	if target == "" {
		return nil
	}
	has := func(r, c int) bool { return f.StyleOf(f.Cell(r, c)).Link == idx }
	// the first cell: to the left, and up where the row autowrapped into this one
	r0, c0 := row, col
	for {
		for c0 > 0 && has(r0, c0-1) {
			c0--
		}
		if c0 == 0 && r0 > 0 && r0-1 < len(f.Wrapped) && f.Wrapped[r0-1] && has(r0-1, f.Cols-1) {
			r0, c0 = r0-1, f.Cols-1
			continue
		}
		break
	}
	// the last cell: to the right, and down where the row autowraps
	r1, c1 := row, col
	for {
		for c1 < f.Cols-1 && has(r1, c1+1) {
			c1++
		}
		if c1 == f.Cols-1 && r1 < f.Rows-1 && r1 < len(f.Wrapped) && f.Wrapped[r1] && has(r1+1, 0) {
			r1, c1 = r1+1, 0
			continue
		}
		break
	}
	var segs []mark
	for r := r0; r <= r1; r++ {
		x0, x1 := 0, f.Cols
		if r == r0 {
			x0 = c0
		}
		if r == r1 {
			x1 = c1 + 1
		}
		segs = append(segs, mark{line: f.Top + int64(r), x0: x0, x1: x1})
	}
	return &activeLink{kind: linkOSC8, url: target, row: none, col: none, segs: segs}
}

// ---- the pointer ----

// lookAt looks for the link at a cell and shows it under the pointer: underlined, with the tooltip after a moment. again says
// that the cell was looked at already.
func (pv *paneView) lookAt(p pos, again bool) {
	h := &pv.hover
	if !again && h.checked && h.cell == p && h.gen == pv.bufGen {
		return
	}
	h.cell, h.gen, h.checked = p, pv.bufGen, true
	link, _ := pv.linkAt(p)
	pv.setHoverLink(link)
}

// setHoverLink changes the link under the pointer.
func (pv *paneView) setHoverLink(link *activeLink) {
	h := &pv.hover
	same := (h.link == nil && link == nil) || (h.link != nil && link != nil && h.link.kind == link.kind && h.link.url == link.url &&
		h.link.path == link.path && len(h.link.segs) > 0 && len(link.segs) > 0 && h.link.segs[0] == link.segs[0])
	if same {
		if link != nil {
			h.link = link
		}
		return
	}
	h.link = link
	pv.stopTip()
	if link != nil {
		h.tipTimer = pv.binding.Gestures().NewTimer(tooltipDelay, func() {
			h.tipTimer = nil
			h.tip = true
			pv.rebuild()
		})
	}
	pv.markDirty()
	pv.rebuild()
}

// stopTip hides the tooltip and stops the timer that shows it.
func (pv *paneView) stopTip() {
	h := &pv.hover
	if h.tipTimer != nil {
		h.tipTimer.Cancel()
		h.tipTimer = nil
	}
	h.tip = false
}

// leaveHover is the pointer leaving the pane.
func (pv *paneView) leaveHover() {
	pv.hover.checked = false
	pv.hover.modDown = false
	pv.setHoverLink(nil)
}

// hoverAt follows the pointer: the link under it, with the key that follows it known.
func (pv *paneView) hoverAt(local gui.Offset) {
	if pv.binding == nil || pv.shown == nil {
		return
	}
	h := &pv.hover
	h.at = local
	mod := pv.modsOf()&pv.linkModifier() != 0
	col, row := pv.cellAt(local)
	pv.lookAt(pv.posOf(col, row), false)
	if mod != h.modDown {
		h.modDown = mod
		if h.link != nil {
			pv.rebuild()
		}
	}
}

// tryFollowLink follows the link at a cell for a click with the key of links held. It reports whether the click was for a link
// (also when the files of the line are still being checked: the click then waits for the answer).
func (pv *paneView) tryFollowLink(p pos) bool {
	link, pending := pv.linkAt(p)
	switch {
	case link != nil:
		pv.followLink(link)
		return true
	case pending:
		pv.hover.pendingClick = &p
		return true
	}
	return false
}

// followLink opens a link.
func (pv *paneView) followLink(l *activeLink) {
	switch l.kind {
	case linkFile:
		if pv.env.openFile != nil {
			pv.env.openFile(l.path, l.row, l.col)
		}
	default:
		if pv.env.openLink != nil {
			pv.env.openLink(l.url, l.kind == linkOSC8)
		}
	}
}

// underlines are the cells of the link under the pointer on the rows [top, top+rows).
func (pv *paneView) underlines(top int64, rows int, out []mark) []mark {
	l := pv.hover.link
	if l == nil {
		return out
	}
	for _, s := range l.segs {
		if s.line >= top && s.line < top+int64(rows) {
			out = append(out, s)
		}
	}
	return out
}

// tooltip is the widget that says how to follow the link under the pointer, above the first cell of the link.
func (pv *paneView) tooltip(bc widgets.BuildContext) widgets.Widget {
	h := &pv.hover
	if !h.tip || h.link == nil || len(h.link.segs) == 0 || pv.shown == nil || pv.factory == nil {
		return nil
	}
	a, _ := pv.atlases()
	seg := h.link.segs[0]
	row := float64(seg.line - pv.shown.Top)
	x := padLeft - pv.hscroll + float64(seg.x0)*a.m.w
	// above the link, as VS Code shows it, and below it for a link on the first row
	y := row*a.m.h - 26
	if y < 0 {
		y = (row + 1) * a.m.h
	}
	colors := pv.colors
	label := widgets.NewContainer(
		widgets.NewText("Follow link ("+pv.linkClickLabel()+")", widgets.TextOpts{
			Style: ui.TextStyle(bc, 12, colors.EditorHoverWidgetForeground), MaxLines: foundation.Some(1),
		}),
		widgets.ContainerOpts{
			Padding: painting.NewEdgeInsetsSymmetric(3, 8),
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color:        foundation.Some(colors.EditorHoverWidgetBackground),
				Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.EditorHoverWidgetBorder), Width: foundation.Some(1.0)}),
				BorderRadius: painting.NewBorderRadiusCircular(5),
				BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), Offset: gui.Offset{Dy: 2}, BlurRadius: 8})},
			}),
		})
	return widgets.NewPositioned(widgets.NewIgnorePointer(label), widgets.PositionedOpts{
		Left: foundation.Some(x), Top: foundation.Some(y),
	})
}
