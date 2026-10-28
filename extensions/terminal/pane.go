package terminal

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/rm4n0s/glutter/gestures"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// The look of the pane: the space left of the first column, and the width of the scrollbar at the right edge.
const (
	padLeft    = 12.0
	scrollbarW = 10.0
	// notch is the scroll of a wheel notch in Glutter, in logical pixels.
	notch = 53.0
)

// paneEnv is what a pane needs from the extension that owns it.
type paneEnv struct {
	cfg   func() *config
	fonts *fontCache
	// post runs a function on the UI thread, later.
	post func(func())
	// focusChanged, selectionChanged and scrolled tell the extension that the pane has or lost the focus, that its
	// selection changed and that it scrolled (the context keys follow).
	focusChanged     func(pv *paneView, has bool)
	selectionChanged func(pv *paneView)
	// menu opens the context menu at a place of the window.
	menu func(pv *paneView, at gui.Offset)
	// confirmPaste asks the user to confirm a paste of several lines and calls paste when the user agrees.
	confirmPaste func(lines int, paste func())
	// clipboardCtx is the context of the requests to the desktop's clipboard.
	clipboardCtx context.Context
	// mounted tells that the pane came on screen, and altChanged that its program went to or came back from the alternate
	// screen.
	mounted    func(pv *paneView)
	altChanged func(pv *paneView)
	// ctx is the context of the extension: the searches stop with it. A nil one is the background context.
	ctx context.Context
	// findChanged tells that the find widget of a pane was shown or hidden, or its input got or lost the focus.
	findChanged func(pv *paneView)
	// folder is the folder of the workspace, which file links are confined to ("" for none); home is the home folder.
	folder func() string
	home   string
	// altClick says that a click with the Alt key follows a link (editor.multiCursorModifier is ctrlCmd) and not one with Ctrl.
	altClick func() bool
	// openFile opens a file of the workspace at a line and column (none for no position); openLink opens an address or the
	// target of a hyperlink the program set.
	openFile func(path string, row, col int)
	openLink func(target string, fromProgram bool)
	// fixes are the quick fixes of the last command of a terminal (nil for none), runFix runs one, and commandMenu opens the
	// menu of a decoration with its items at a place of the window (shell integration, step 17d.5).
	fixes       func(pv *paneView) *fixSet
	runFix      func(pv *paneView, f quickFix)
	commandMenu func(pv *paneView, items []ui.MenuItem, at gui.Offset)
	// stickyMenu opens the menu of the sticky scroll overlay.
	stickyMenu func(pv *paneView, at gui.Offset)
	// suggester computes the suggestions of terminal IntelliSense (nil for none), suggestEnv tells it about the terminal of a pane
	// and suggestChanged that the list was shown or closed (step 17d.6).
	suggester      *suggester
	suggestEnv     func(pv *paneView) suggestEnv
	suggestChanged func(pv *paneView)
}

// paneView is the view of one terminal in a pane: what the widget draws, the keyboard, the pointer, the selection and the
// scrolling. It lives as long as the terminal does and is used on the UI thread only; the widgets that show it come and go with
// the tab.
type paneView struct {
	env  *paneEnv
	term *Terminal

	node    *widgets.FocusNode
	binding *widgets.WidgetsBinding
	render  *renderGrid
	ime     *imeClient

	// what the build context gave: the colors of the theme, the pixel ratio, the text engine and the desktop's font
	colors   ui.Colors
	th       theme
	dpr      float64
	factory  gui.ParagraphFactory
	desktop  string
	colorsOK bool

	painter *gridPainter
	frame   emulator.Frame // the screen
	view    emulator.Frame // the lines the scrolled view shows
	shown   *emulator.Frame
	haveTop bool
	lastTop int64
	cols    int
	rows    int
	// scroll is how many lines above the bottom the view is.
	scroll int
	sel    selection

	focused bool
	blinkOn bool
	blink   *gestures.Timer
	// the pointer: clicks in a row, the press that is held, the scrollbar
	lastDown  time.Duration
	lastAt    gui.Offset
	clicks    int
	pressed   bool
	reporting bool // the press is reported to the program
	dragBar   bool
	barGrab   float64
	wheelRest float64
	hoverBar  bool
	barUntil  time.Duration
	// preedit is the text an input method composes, shown at the cursor
	preedit string
	// marks and links are the ranges drawn in this frame over the backgrounds (the find matches) and underlined (the link under
	// the pointer); the slices are kept to be filled again
	marks, links []mark
	// alt is whether the alternate screen was shown when the extension was last told.
	alt bool
	// appliedTh is the theme the emulator was given, and themed whether it was given one.
	appliedTh theme
	themed    bool
	// paints counts the frames the pane painted, for the tests of "an idle terminal draws nothing".
	paints uint64

	// find is the find widget of the pane; hover the link under the pointer; linkCache what was found about the files of lines of
	// text; linkFrame is a frame for reading lines; bufGen counts the changes of the screen.
	find      findBox
	hover     hoverState
	linkCache map[string]*lineLinks
	linkFrame emulator.Frame
	bufGen    uint64
	// setState builds the widget of the pane again (the find widget and the tooltip are part of it).
	setState func(func())

	// shell integration (shell_pane.go): the commands with their lines, the decorations of the last frame, the sticky scroll
	// overlay, where the navigation by commands stands and the selection it made.
	shell  shellCache
	decos  []decorated
	sticky stickyState
	nav    navState
	selNav *selNavState
	// shellOn is whether the shell integration was active when the extension was last told.
	shellOn bool

	// sug is the suggest list (suggest_pane.go).
	sug suggestState
}

func newPaneView(env *paneEnv, term *Terminal) *paneView {
	pv := &paneView{env: env, term: term, painter: newGridPainter(), blinkOn: true, linkCache: map[string]*lineLinks{}, nav: navState{line: -1}}
	pv.initFind()
	pv.node = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Terminal"})
	pv.ime = &imeClient{pv: pv}
	term.SetOnFrame(func() { env.post(pv.frameChanged) })
	return pv
}

// frameChanged is told, on the UI thread, that the screen of the terminal changed.
func (pv *paneView) frameChanged() {
	pv.bufGen++
	pv.suggestFrame()
	pv.scheduleFindRefresh()
	alt, shellOn := pv.term.Emulator().Modes().AltScreen, pv.shellActive()
	if alt != pv.alt || shellOn != pv.shellOn {
		pv.alt, pv.shellOn = alt, shellOn
		if pv.env.altChanged != nil {
			pv.env.altChanged(pv)
		}
	}
	if pv.render != nil {
		pv.render.MarkNeedsPaint()
	}
}

// setContext reads the colors, the pixel ratio and the text engine from the build context of the widget.
func (pv *paneView) setContext(bc widgets.BuildContext) {
	pv.binding = widgets.WidgetsBindingOf(bc)
	pv.colors = ui.Theme(bc)
	pv.th = themeOf(pv.colors)
	pv.colorsOK = true
	pv.applyTheme()
	pv.dpr, _ = widgets.MediaQueryMaybeDevicePixelRatioOf(bc)
	if pv.dpr <= 0 {
		pv.dpr = 1
	}
	pv.factory = pv.binding.PlatformDispatcher().TextEngine()
	pv.desktop, _ = pv.binding.PlatformDispatcher().MonospaceFont()
	if pv.render != nil {
		pv.render.MarkNeedsLayout()
		pv.render.MarkNeedsPaint()
	}
}

// applyTheme gives the emulator the colors of the theme: the sixteen colors of the palette and the default foreground,
// background and cursor, which the cells that name no color are drawn with and the program reads with the queries of OSC 4, 10,
// 11 and 12 (a program that asks for the background to choose its own colors). The program's own changes are reset when the
// theme changes.
func (pv *paneView) applyTheme() {
	if pv.themed && pv.appliedTh == pv.th {
		return
	}
	var ansi [16]uint32
	for i, c := range pv.th.ansi {
		ansi[i] = rgb(c)
	}
	pv.term.Emulator().SetTheme(ansi, rgb(pv.th.fg), rgb(pv.th.bg), rgb(pv.th.cursor))
	pv.appliedTh, pv.themed = pv.th, true
}

// mounted and unmounted are told when the render object comes into the tree and leaves it.
func (pv *paneView) mounted(r *renderGrid) {
	pv.render = r
	pv.term.SetOnFrame(func() { pv.env.post(pv.frameChanged) })
	if pv.env.mounted != nil {
		pv.env.mounted(pv)
	}
}

func (pv *paneView) unmounted(r *renderGrid) {
	if pv.render == r {
		pv.render = nil
	}
	pv.stopBlink()
	pv.ime.detach()
	// Glutter: a focus node that leaves the tree with the focus does not tell OnFocusChange, so the pane says it itself
	// (docs/GLUTTER-ISSUES.md).
	if pv.focused {
		pv.focused = false
		if pv.env.focusChanged != nil {
			pv.env.focusChanged(pv, false)
		}
	}
}

// spec is the font the pane draws with.
func (pv *paneView) spec() fontSpec {
	cfg := pv.env.cfg()
	families := append([]string(nil), cfg.fontFamilies...)
	if len(families) == 0 && pv.desktop != "" {
		families = append(families, pv.desktop)
	}
	families = append(families, "monospace")
	return fontSpec{
		families: strings.Join(families, ","), size: cfg.fontSize, weight: cfg.weight, weightBold: cfg.weightBold,
		lineHeight: cfg.lineHeight, letterSpacing: cfg.letterSpace, dpr: pv.dpr,
	}
}

// atlases returns the glyph atlas and the decoration atlas of the font of the pane.
func (pv *paneView) atlases() (*atlas, *decor) { return pv.env.fonts.get(pv.factory, pv.spec()) }

// layout is called by the render object with its size: the terminal gets as many columns and rows as fit.
func (pv *paneView) layout(size gui.Size) {
	if pv.factory == nil {
		return
	}
	a, _ := pv.atlases()
	cols := int((size.Width - padLeft - scrollbarW) / a.m.w)
	rows := int(size.Height / a.m.h)
	cols, rows = max(cols, 1), max(rows, 1)
	if cols != pv.cols || rows != pv.rows {
		pv.cols, pv.rows = cols, rows
		pv.suggestHide()
		pv.term.Resize(cols, rows)
	}
}

// paint takes the snapshot of the terminal and draws it.
func (pv *paneView) paint(cv *gui.Canvas, off gui.Offset, size gui.Size) {
	cfg := pv.env.cfg()
	if pv.factory == nil || !pv.colorsOK {
		return
	}
	pv.paints++
	a, d := pv.atlases()
	em := pv.term.Emulator()
	pv.term.TakeChanged()
	em.Snapshot(&pv.frame)
	if pv.haveTop && pv.scroll > 0 {
		// output keeps the lines in view: the view follows them as the lines move up
		pv.scroll += int(pv.frame.Top - pv.lastTop)
	}
	pv.lastTop, pv.haveTop = pv.frame.Top, true
	pv.scroll = min(max(pv.scroll, 0), pv.frame.Scrollback)
	f := &pv.frame
	if pv.scroll > 0 {
		first := pv.frame.Scrollback - pv.scroll
		em.ReadLines(first, pv.frame.Rows, &pv.view)
		f = &pv.view
	}
	pv.shown = f
	cols := min(pv.cols, f.Cols)
	rows := min(pv.rows, f.Rows)
	pv.marks = pv.findMarks(f.Top, rows, cols, pv.marks[:0])
	pv.links = pv.underlines(f.Top, rows, pv.links[:0])
	s := shown{
		f: f, rows: rows, cols: cols, originX: off.Dx + padLeft, originY: off.Dy, th: &pv.th, cfg: cfg,
		focused: pv.focused, blinkOn: pv.blinkOn, sel: pv.sel, cursorOn: pv.scroll == 0, marks: pv.marks, links: pv.links,
	}
	pv.painter.build(a, d, s)
	pv.drawPreedit(a, d, s)
	a.flush()

	cv.Save()
	cv.ClipRect(gui.NewRectFromLTWH(off.Dx, off.Dy, size.Width, size.Height))
	bg := gui.NewPaint()
	bg.SetColor(gui.NewColor(pv.th.bg))
	cv.DrawRect(gui.NewRectFromLTWH(off.Dx, off.Dy, size.Width, size.Height), bg)
	pv.painter.draw(cv, a, d)
	pv.decos = pv.gutterDecorations(f.Top, rows, pv.decos)
	pv.drawGutter(cv, off, a, pv.decos)
	pv.drawSticky(cv, off, size, a, d)
	pv.drawScrollbar(cv, off, size)
	cv.Restore()
	pv.scheduleBlink(cfg, f)
	pv.scheduleInputGeometry(a, off)
}

// drawPreedit adds the text an input method composes, over the cells from the cursor, underlined.
func (pv *paneView) drawPreedit(a *atlas, d *decor, s shown) {
	if pv.preedit == "" || pv.scroll != 0 {
		return
	}
	f := s.f
	scale := float32(1 / a.m.dpr)
	x := f.Cursor.X
	y := s.originY + float64(f.Cursor.Y)*a.m.h
	for _, r := range pv.preedit {
		attrs := uint8(0)
		w := 1
		if r >= 0x1100 && unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			w = 2
			attrs |= attrWide
		}
		if x+w > s.cols {
			break
		}
		px := s.originX + float64(x)*a.m.w
		pv.painter.run(d, &d.block, s, x, w, y, pv.th.bg)
		if sl := a.lookup(r, attrs); sl != nil {
			pv.painter.glyphAdd(a, sl, scale, px, y, pv.th.fg, w)
		}
		pv.painter.run(d, &d.line, s, x, w, y+float64(a.m.ch-2*a.m.lineThick)/a.m.dpr, pv.th.fg)
		x += w
	}
}

// drawScrollbar draws the scrollbar at the right edge while the pointer is near it, the view scrolls or it is dragged.
func (pv *paneView) drawScrollbar(cv *gui.Canvas, off gui.Offset, size gui.Size) {
	top, h, ok := pv.thumb(size)
	if !ok || (!pv.hoverBar && !pv.dragBar && pv.scroll <= 0) {
		return
	}
	col := pv.colors.ScrollbarSliderBackground
	if pv.dragBar {
		col = pv.colors.ScrollbarSliderActiveBackground
	} else if pv.hoverBar {
		col = pv.colors.ScrollbarSliderHoverBackground
	}
	p := gui.NewPaint()
	p.SetColor(col)
	cv.DrawRect(gui.NewRectFromLTWH(off.Dx+size.Width-scrollbarW, off.Dy+top, scrollbarW, h), p)
}

// thumb is the place of the scrollbar's thumb: its top and height, and whether there is anything to scroll.
func (pv *paneView) thumb(size gui.Size) (top, h float64, ok bool) {
	sb := pv.frame.Scrollback
	if sb == 0 || pv.frame.Rows == 0 {
		return 0, 0, false
	}
	total := float64(sb + pv.frame.Rows)
	h = math.Max(size.Height*float64(pv.frame.Rows)/total, 24)
	top = (size.Height - h) * float64(sb-pv.scroll) / float64(sb)
	return top, h, true
}

// ---- scrolling ----

// scrollBy scrolls the view by lines (positive is up, toward older lines) and keeps it within the buffer.
func (pv *paneView) scrollBy(lines int) {
	pv.scrollTo(pv.scroll + lines)
}

// scrollTo sets how many lines above the bottom the view is.
func (pv *paneView) scrollTo(lines int) {
	lines = min(max(lines, 0), pv.frame.Scrollback)
	if lines == pv.scroll {
		return
	}
	if lines > 0 {
		pv.suggestHide()
	}
	pv.scroll = lines
	if pv.render != nil {
		pv.render.MarkNeedsPaint()
	}
}

// ---- the keyboard ----

// focusChanged is the pane getting or losing the keyboard focus.
func (pv *paneView) focusChanged(has bool) {
	if pv.focused == has {
		return
	}
	pv.focused = has
	pv.blinkOn = true
	if !has {
		pv.suggestHide()
	}
	if has {
		if pv.binding != nil {
			pv.ime.attach(pv.binding)
		}
	} else {
		pv.ime.detach()
		pv.stopBlink()
	}
	em := pv.term.Emulator()
	if b := em.EncodeFocus(has, nil); len(b) > 0 {
		pv.term.Send(b)
	}
	if pv.render != nil {
		pv.render.MarkNeedsPaint()
	}
	if pv.env.focusChanged != nil {
		pv.env.focusChanged(pv, has)
	}
}

// requestFocus gives the pane the keyboard focus.
func (pv *paneView) requestFocus() { pv.node.RequestFocus() }

// configChanged follows a change of the settings: the font may be another, so the grid is laid out and painted again.
func (pv *paneView) configChanged() {
	if !pv.suggestOn() {
		pv.suggestHide()
	}
	if pv.render != nil {
		pv.render.MarkNeedsLayout()
		pv.render.MarkNeedsPaint()
	}
}

// dispose lets go of the pane when its terminal is gone.
func (pv *paneView) dispose() {
	pv.suggestHide()
	pv.stopBlink()
	pv.stopTip()
	pv.stopFindTimer()
	if pv.find.cancel != nil {
		pv.find.cancel()
		pv.find.cancel = nil
	}
	pv.find.gen++
	pv.ime.detach()
	pv.term.SetOnFrame(nil)
	pv.term.SetOnMeta(nil)
}

// clear is the command Clear: the scrollback goes, and so do the lines above the cursor, so that the line the cursor is on
// becomes the first of the screen (VS Code's clear keeps the prompt line). It does nothing on the alternate screen.
func (pv *paneView) clear() {
	em := pv.term.Emulator()
	var f emulator.Frame
	em.Snapshot(&f)
	if f.Modes.AltScreen {
		return
	}
	row, col := f.Cursor.Y, f.Cursor.X
	seq := "\x1b[3J"
	if row > 0 {
		seq += "\x1b[1;1H\x1b[" + strconv.Itoa(row) + "M\x1b[1;" + strconv.Itoa(col+1) + "H"
	}
	if _, err := em.Write([]byte(seq)); err != nil {
		pv.term.log.Debug("the terminal cannot be cleared", "err", err)
	}
	pv.scrollTo(0)
	pv.term.markChanged()
}
