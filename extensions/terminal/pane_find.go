package terminal

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// The find widget of a terminal (SPEC §10.25 "Find and links"): VS Code's simple find widget, 419 px wide at the top right of
// the terminal, with the input, the switches for case, whole word and regular expression, the number of the match, the buttons
// for the previous and next match and the close button. The searches run on a goroutine of their own and hand their result to
// the UI thread; the matches are highlighted under the text.

// The measures of the widget (findWidget.css of VS Code, as the editor's find widget has them).
const (
	findWidth     = 419.0
	findFontSize  = 12.0
	findCountWide = 69.0
	findGap       = 3.0
	findCorner    = 4.0
	// findRefreshDelay is how long after the output changed the search is made again.
	findRefreshDelay = 200 * time.Millisecond
)

// findBox is the state of the find widget of one pane.
type findBox struct {
	visible bool
	// input is the controller of the input of the widget: its text and its focus.
	input *ui.InputBoxController
	query findQuery
	// inputFocused tells that the input has the keyboard focus.
	inputFocused bool
	// matches are the matches of the buffer in order of position; cur is the current one (-1 for none).
	matches   []findMatch
	cur       int
	truncated bool
	// invalid says that the pattern does not compile; searched that the matches are those of the query.
	invalid  bool
	searched bool
	// gen numbers the searches, so that the answer of an old one is dropped; cancel stops the one that runs.
	gen    uint64
	cancel context.CancelFunc
	timer  *gestures.Timer
}

// initFind makes the parts of the find state that need the pane.
func (pv *paneView) initFind() {
	pv.find.input = ui.NewInputBoxController("")
	pv.find.cur = -1
}

// showFind shows the widget (VS Code's reveal): the text of a selection of one line is searched for, the input gets the
// keyboard focus with its text selected, and the search is made.
func (pv *paneView) showFind() {
	f := &pv.find
	if sel := pv.selectedText(); sel != "" && !strings.Contains(sel, "\n") {
		f.query.text = sel
		f.input.SetValue(sel)
	}
	was := f.visible
	f.visible = true
	if f.query.text != "" {
		pv.runSearch(false)
	}
	pv.rebuild()
	pv.afterFrame(func() {
		f.input.Focus()
		f.input.SelectAll()
	})
	if !was {
		pv.findStateChanged()
	}
}

// hideFind closes the widget, drops the highlights and gives the keyboard back to the terminal.
func (pv *paneView) hideFind() {
	f := &pv.find
	if !f.visible {
		return
	}
	f.visible = false
	f.inputFocused = false
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	f.gen++
	f.matches, f.cur, f.searched, f.truncated = nil, -1, false, false
	pv.stopFindTimer()
	pv.rebuild()
	pv.markDirty()
	pv.requestFocus()
	pv.findStateChanged()
}

// findStateChanged tells the extension that the widget was shown or hidden or got or lost the focus.
func (pv *paneView) findStateChanged() {
	if pv.env.findChanged != nil {
		pv.env.findChanged(pv)
	}
}

// markDirty paints the pane again.
func (pv *paneView) markDirty() {
	if pv.render != nil {
		pv.render.MarkNeedsPaint()
	}
}

// rebuild builds the widget of the pane again (the find widget and the tooltip are part of it).
func (pv *paneView) rebuild() {
	if pv.render != nil && pv.setState != nil {
		pv.setState(func() {})
	}
}

// afterFrame calls f once the next frame is built.
func (pv *paneView) afterFrame(f func()) {
	if pv.binding == nil {
		f()
		return
	}
	scheduler := pv.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) { f() })
}

// findTextChanged is the text of the input changing: the search is made again, keeping the current match when it still matches.
func (pv *paneView) findTextChanged(text string) {
	f := &pv.find
	if f.query.text == text {
		return
	}
	f.query.text = text
	pv.runSearch(true)
}

// toggleFind flips one of the switches and searches again.
func (pv *paneView) toggleFind(which string) {
	f := &pv.find
	switch which {
	case "case":
		f.query.matchCase = !f.query.matchCase
	case "word":
		f.query.wholeWord = !f.query.wholeWord
	case "regex":
		f.query.regex = !f.query.regex
	}
	pv.runSearch(true)
	pv.rebuild()
}

// runSearch searches the buffer on a goroutine. keep says that the current match stays the current one when it still matches,
// else the first match to be current is the last one of the buffer (VS Code's incremental find goes backward from the end).
func (pv *paneView) runSearch(keep bool) {
	f := &pv.find
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	f.gen++
	gen := f.gen
	anchor, hasAnchor := pos{}, false
	if keep && f.cur >= 0 && f.cur < len(f.matches) {
		anchor, hasAnchor = f.matches[f.cur].start, true
	}
	q := f.query
	q.separators = pv.env.cfg().wordSeparators
	if q.text == "" {
		f.matches, f.cur, f.searched, f.invalid, f.truncated = nil, -1, true, false, false
		pv.markDirty()
		pv.rebuild()
		return
	}
	f.searched = false
	parent := pv.env.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	f.cancel = cancel
	em := pv.term.Emulator()
	post := pv.env.post
	go func() {
		ms, truncated, err := searchBuffer(ctx, em, q, findLimit)
		if ctx.Err() != nil {
			return
		}
		post(func() {
			if gen != pv.find.gen || !pv.find.visible {
				return
			}
			pv.find.cancel = nil
			pv.applyMatches(ms, truncated, err != nil, anchor, hasAnchor)
		})
	}()
}

// applyMatches takes the result of a search: the current match is the last one at or before the anchor, or the last of all.
func (pv *paneView) applyMatches(ms []findMatch, truncated, invalid bool, anchor pos, hasAnchor bool) {
	f := &pv.find
	f.matches, f.truncated, f.invalid, f.searched = ms, truncated, invalid, true
	f.cur = -1
	if len(ms) > 0 {
		f.cur = len(ms) - 1
		if hasAnchor {
			if i := matchAtOrBefore(ms, anchor); i >= 0 {
				f.cur = i
			}
		}
		pv.revealMatch()
	}
	pv.markDirty()
	pv.rebuild()
}

// moveFind goes to the next match (or the previous one), around the buffer. With the widget closed it opens it first.
func (pv *paneView) moveFind(previous bool) {
	f := &pv.find
	if !f.visible {
		pv.showFind()
	}
	if len(f.matches) == 0 {
		return
	}
	var at pos
	switch {
	case f.cur >= 0:
		at = f.matches[f.cur].start
	case previous:
		at = pos{line: 1 << 62}
	default:
		at = pos{line: -1}
	}
	var i int
	if previous {
		if i = matchBefore(f.matches, at); i < 0 {
			i = len(f.matches) - 1
		}
	} else if i = matchAfter(f.matches, at); i < 0 {
		i = 0
	}
	f.cur = i
	pv.revealMatch()
	pv.markDirty()
	pv.rebuild()
}

// revealMatch scrolls the view so that the current match is on it, in the middle when it was not.
func (pv *paneView) revealMatch() {
	f := &pv.find
	if f.cur < 0 || f.cur >= len(f.matches) || pv.shown == nil {
		return
	}
	m := f.matches[f.cur]
	rows := int64(pv.rows)
	if m.start.line >= pv.shown.Top && m.end.line < pv.shown.Top+rows {
		return
	}
	liveBottom := pv.frame.Top + int64(pv.frame.Rows) - 1
	want := m.start.line + rows/2 // the last row of the view that puts the match in the middle
	pv.scrollTo(int(liveBottom - want))
}

// matchCount is the text of the count: "2 of 5", "No results" or nothing for no text.
func (pv *paneView) matchCount() string {
	f := &pv.find
	switch {
	case f.query.text == "":
		return ""
	case !f.searched:
		return "..."
	case len(f.matches) == 0:
		return "No results"
	}
	n := strconv.Itoa(len(f.matches))
	if f.truncated {
		n += "+"
	}
	if f.cur < 0 {
		return "? of " + n
	}
	return strconv.Itoa(f.cur+1) + " of " + n
}

// scheduleFindRefresh searches again a moment after the output changed, so that the highlights follow a terminal that prints.
func (pv *paneView) scheduleFindRefresh() {
	f := &pv.find
	if !f.visible || f.query.text == "" || f.timer != nil || pv.binding == nil {
		return
	}
	f.timer = pv.binding.Gestures().NewTimer(findRefreshDelay, func() {
		f.timer = nil
		if f.visible {
			pv.runSearch(true)
		}
	})
}

func (pv *paneView) stopFindTimer() {
	if pv.find.timer != nil {
		pv.find.timer.Cancel()
		pv.find.timer = nil
	}
}

// findMarks adds the highlights of the matches on the rows [top, top+rows) to out: the current match in its color and the
// others in the highlight color.
func (pv *paneView) findMarks(top int64, rows, cols int, out []mark) []mark {
	f := &pv.find
	if !f.visible || len(f.matches) == 0 {
		return out
	}
	ms := f.matches
	// the first match that ends on or after the first row
	lo, hi := 0, len(ms)
	for lo < hi {
		mid := (lo + hi) / 2
		if ms[mid].end.line < top {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	for i := lo; i < len(ms) && ms[i].start.line < top+int64(rows); i++ {
		color := pv.th.findHighlight
		if i == f.cur {
			color = pv.th.findMatch
		}
		out = ms[i].segments(top, rows, cols, color, out)
	}
	return out
}

// findWidget builds the widget over the top right of the pane.
func (pv *paneView) findWidget(bc widgets.BuildContext) widgets.Widget {
	f := &pv.find
	colors := pv.colors
	style := ui.TextStyle(bc, findFontSize, colors.EditorWidgetForeground)
	nonEmpty := f.query.text != ""
	noMatches := nonEmpty && f.searched && (len(f.matches) == 0 || f.invalid)
	toggle := func(name, tip string, checked bool, which string) widgets.Widget {
		return ui.NewToggle(name, ui.ToggleOpts{Tooltip: tip, NotFocusable: true, Checked: checked, OnChanged: func(bool) { pv.toggleFind(which) }})
	}
	input := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: f.input, Placeholder: "Find", Invalid: noMatches,
		OnChanged: pv.findTextChanged,
		OnFocusChange: func(has bool) {
			if f.inputFocused != has {
				f.inputFocused = has
				pv.findStateChanged()
			}
		},
		Trailing: []widgets.Widget{
			toggle("case-sensitive", "Match Case (Alt+C)", f.query.matchCase, "case"),
			toggle("whole-word", "Match Whole Word (Alt+W)", f.query.wholeWord, "word"),
			toggle("regex", "Use Regular Expression (Alt+R)", f.query.regex, "regex"),
		},
	})
	count := widgets.NewContainer(
		widgets.NewText(pv.matchCount(), widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowClip}),
		widgets.ContainerOpts{
			Constraints: foundation.Some(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MinWidth: foundation.Some(findCountWide)})),
			Alignment:   painting.AlignmentCenter, Padding: painting.NewEdgeInsetsFromLTRB(2, 0, 0, 0), Margin: painting.NewEdgeInsetsFromLTRB(findGap, 0, 0, 0),
		})
	button := func(name, tip string, enabled bool, run func()) widgets.Widget {
		opts := ui.IconButtonOpts{Tooltip: tip, Disabled: !enabled}
		if enabled {
			opts.OnPressed = run
		}
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(findGap, 0, 0, 0), ui.NewIconButton(name, opts))
	}
	row := widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(input),
		count,
		button("arrow-up", "Previous Match (Shift+F3)", nonEmpty && len(f.matches) > 0, func() { pv.moveFind(true) }),
		button("arrow-down", "Next Match (F3)", nonEmpty && len(f.matches) > 0, func() { pv.moveFind(false) }),
		button("close", "Close (Escape)", true, pv.hideFind),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	border := painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)})
	if colors.WidgetBorder.Alpha() == 0 {
		border = painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(gui.NewColor(0)), Width: foundation.Some(0.0)})
	}
	box := widgets.NewContainer(row, widgets.ContainerOpts{
		Padding: painting.NewEdgeInsetsFromLTRB(4, findGap, 4, findGap),
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color: foundation.Some(colors.EditorWidgetBackground), Border: border, BorderRadius: painting.NewBorderRadiusCircular(findCorner),
			BoxShadow: []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), Offset: gui.Offset{Dy: 2}, BlurRadius: 8})},
		}),
	})
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		// room for the scrollbar at the right edge
		width := min(findWidth, max(c.MaxWidth-scrollbarW-8, 120))
		return widgets.NewDefaultTextStyle(style, widgets.NewGestureDetector(
			widgets.NewSizedBox(box, widgets.SizedBoxOpts{Width: foundation.Some(width)}),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque}))
	})
}
