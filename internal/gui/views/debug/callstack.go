package debug

import (
	"strconv"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The texts of the Call Stack.
const (
	// LoadMoreFramesText is the row at the end of a call stack that is not loaded to its end.
	LoadMoreFramesText = "Load More Stack Frames"
	// ShowMoreFramesPattern is the row of collapsed deemphasized frames: "Show 3 More Stack Frames".
	showMoreOne  = "Show 1 More Stack Frame"
	showMoreMany = "Show "
)

// CallStackPane is the Call Stack view: the sessions (the threads and the frames of the only session when there is one), the threads
// that are stopped with their frames, the frames of no source or marked subtle dimmed and the runs of deemphasized frames collapsed
// under "Show N More Stack Frames". The frame of the stop is focused and selected, and a click on a frame focuses it.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/callStackView.ts (CallStackView, CallStackDataSource, SessionsRenderer,
// ThreadsRenderer, StackFramesRenderer)
type CallStackPane struct {
	*base
	// shown are the frames whose collapsed run the user opened ("Show N More Stack Frames").
	shown map[string]bool
	// loading notes the threads whose next frames are asked for.
	loading map[string]bool
	// focused is the key of the frame, thread or session that is focused in the model.
	focused string
}

func newCallStackPane(opts Opts) *CallStackPane {
	c := &CallStackPane{shown: map[string]bool{}, loading: map[string]bool{}}
	c.base = newBase(opts, "workbench.debug.callStackView", kit.TreeModelOpts{})
	return c
}

func (c *CallStackPane) refresh(debug.Event) {
	c.rebuild()
	c.notify()
}

func (c *CallStackPane) rebuild() {
	svc := c.opts.Service
	sessions := svc.Model().Sessions(false)
	c.rows = map[string]Row{}
	var items []kit.TreeItem
	switch {
	case len(sessions) == 0:
	case len(sessions) > 1:
		for _, s := range sessions {
			if s.Parent() == nil || !contains(sessions, s.Parent()) {
				items = append(items, c.sessionItem(s, sessions, true))
			}
		}
	default:
		s := sessions[0]
		threads := s.Threads()
		if len(threads) == 1 {
			items = c.threadChildren(threads[0], 0)
		} else {
			for _, t := range threads {
				items = append(items, c.threadItem(t))
			}
		}
		for _, child := range sessions {
			if child.Parent() == s {
				items = append(items, c.sessionItem(child, sessions, false))
			}
		}
	}
	// the items are replaced; what is open stays open by key
	_ = c.model.SetItems(items)
	c.message = ""
	c.syncFocus()
}

func contains(list []*debug.Session, s *debug.Session) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (c *CallStackPane) sessionItem(s *debug.Session, all []*debug.Session, multi bool) kit.TreeItem {
	key := "session:" + s.ID()
	row := Row{Kind: RowSession, Key: key, Session: s}
	c.rows[key] = row
	item := kit.TreeItem{Key: key, Data: row, Label: s.Label()}
	threads := s.Threads()
	if len(threads) == 1 {
		item.Children = c.threadChildren(threads[0], 1)
	} else {
		for _, t := range threads {
			item.Children = append(item.Children, c.threadItem(t))
		}
	}
	for _, child := range all {
		if child.Parent() == s {
			item.Children = append(item.Children, c.sessionItem(child, all, multi))
		}
	}
	item.HasChildren = len(item.Children) > 0
	return item
}

func (c *CallStackPane) threadItem(t *debug.Thread) kit.TreeItem {
	key := t.Key()
	row := Row{Kind: RowThread, Key: key, Thread: t}
	c.rows[key] = row
	item := kit.TreeItem{Key: key, Data: row, Label: t.Name()}
	if t.Stopped() {
		item.Children = c.threadChildren(t, 1)
	}
	item.HasChildren = t.Stopped()
	return item
}

// threadChildren are the rows below a thread: its frames, the runs of deemphasized frames collapsed, the message of a failed stack
// trace and the row that loads more frames.
func (c *CallStackPane) threadChildren(t *debug.Thread, depth int) []kit.TreeItem {
	var out []kit.TreeItem
	if !t.Stopped() {
		return nil
	}
	frames := t.CallStack()
	if len(frames) == 1 && t.Session().Capabilities().SupportsDelayedStackTraceLoading {
		if d := t.StoppedDetails(); d != nil && d.TotalFrames > 1 {
			// to reduce flashing the stale call stack stays below the first frame until the real one is there
			frames = append(frames, t.StaleCallStack()[min(1, len(t.StaleCallStack())):]...)
		}
	}
	for i := 0; i < len(frames); i++ {
		f := frames[i]
		if f.Source.Available && f.Deemphasized() && !c.shown[f.Key()] {
			// collect the run of deemphasized frames
			j := i
			for j+1 < len(frames) && frames[j+1].Source.Available && frames[j+1].Deemphasized() && !c.shown[frames[j+1].Key()] {
				j++
			}
			if j > i {
				hidden := frames[i : j+1]
				key := "more:" + f.Key()
				text := showMoreMany + strconv.Itoa(len(hidden)) + " More Stack Frames"
				row := Row{Kind: RowShowMore, Key: key, Text: text, Hidden: hidden}
				c.rows[key] = row
				out = append(out, kit.TreeItem{Key: key, Data: row, Label: text})
				i = j
				continue
			}
		}
		key := f.Key()
		row := Row{Kind: RowFrame, Key: key, Frame: f}
		c.rows[key] = row
		out = append(out, kit.TreeItem{Key: key, Data: row, Label: f.Name})
	}
	if d := t.StoppedDetails(); d != nil && d.FramesErrorMessage != "" {
		key := "error:" + t.Key()
		row := Row{Kind: RowMessage, Key: key, Text: d.FramesErrorMessage}
		c.rows[key] = row
		out = append(out, kit.TreeItem{Key: key, Data: row, Label: d.FramesErrorMessage})
	}
	if d := t.StoppedDetails(); d != nil && !t.ReachedEndOfCallStack() && (d.TotalFrames > len(frames) || (len(frames) > 0 && d.TotalFrames == 0 && t.Session().Capabilities().SupportsDelayedStackTraceLoading)) {
		key := "load:" + t.Key()
		row := Row{Kind: RowLoadMore, Key: key, Text: LoadMoreFramesText, Thread: t}
		c.rows[key] = row
		out = append(out, kit.TreeItem{Key: key, Data: row, Label: LoadMoreFramesText})
	}
	_ = depth
	return out
}

// syncFocus selects the focused frame (else thread, else session) and opens the rows above it, as VS Code does when the focus changes.
func (c *CallStackPane) syncFocus() {
	f := c.opts.Service.Model().Focus()
	key := ""
	switch {
	case f.Frame != nil:
		key = f.Frame.Key()
	case f.Thread != nil:
		key = f.Thread.Key()
	case f.Session != nil:
		key = "session:" + f.Session.ID()
	}
	if _, ok := c.rows[key]; !ok {
		// a frame that sits in a collapsed run, or a thread shown through its session
		key = ""
	}
	if key == c.focused {
		return
	}
	c.focused = key
	if key == "" {
		return
	}
	for _, a := range c.model.Ancestors(key) {
		c.model.Expand(a)
	}
	c.model.SetSelected(key)
	c.model.SetFocus(key)
}

// Select does what a click on a row does: a frame, a thread or a session takes the focus of the debugger; "Show N More Stack Frames"
// shows the frames; "Load More Stack Frames" asks the adapter for twenty more.
func (c *CallStackPane) Select(r Row) {
	svc := c.opts.Service
	switch r.Kind {
	case RowFrame:
		svc.FocusStackFrame(r.Frame, r.Frame.Thread, r.Frame.Thread.Session())
	case RowThread:
		svc.FocusStackFrame(nil, r.Thread, r.Thread.Session())
	case RowSession:
		svc.FocusStackFrame(nil, nil, r.Session)
	case RowShowMore:
		for _, f := range r.Hidden {
			c.shown[f.Key()] = true
		}
		c.rebuild()
		c.notify()
	case RowLoadMore:
		t := r.Thread
		if c.loading[t.Key()] {
			return
		}
		c.loading[t.Key()] = true
		go func() {
			_ = t.Session().LoadMoreFrames(c.ctx, t, 20)
			c.opts.Post(func() {
				delete(c.loading, t.Key())
				c.rebuild()
				c.notify()
			})
		}()
	}
}

// Render builds the contents of a row.
func (c *CallStackPane) Render(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
	row, ok := n.Data.(Row)
	if !ok {
		return kit.NewHighlightedLabel(bc, n.Label, st)
	}
	colors := kit.ThemeOf(bc)
	base := kit.TextStyleOf(bc, rowFontSize, st.Foreground)
	dim := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(named(colors, "list.deemphasizedForeground", st.Foreground))})
	if st.Selected && st.Active {
		dim = base
	}
	line := func(spans ...painting.InlineSpan) widgets.Widget {
		return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans}),
			widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	text := func(s string, style *painting.TextStyle) painting.InlineSpan {
		return painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(s), Style: style})
	}
	switch row.Kind {
	case RowSession:
		cells := []widgets.Widget{
			widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconBug), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(st.Foreground)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}),
			widgets.NewFlexible(line(text(row.Session.Label(), base))),
		}
		if label := sessionStateLabel(row.Session); label != "" {
			cells = append(cells, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}), stateBadge(bc, label, sessionStopReason(row.Session) == dap.StopException))
		}
		return widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	case RowThread:
		cells := []widgets.Widget{widgets.NewFlexible(line(text(row.Thread.Name(), base)))}
		cells = append(cells, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
			stateBadge(bc, row.Thread.StateLabel(), threadReason(row.Thread) == dap.StopException))
		return widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	case RowFrame:
		f := row.Frame
		style := base
		if f.PresentationHint == "subtle" || f.Deemphasized() {
			style = dim
		}
		spans := []painting.InlineSpan{text(f.Name, style)}
		cells := []widgets.Widget{}
		if f.PresentationHint == "label" {
			return line(text(f.Name, style.CopyWith(painting.TextStyleOpts{FontStyle: ui.FontStyleItalic})))
		}
		if f.Source.Available && f.Source.Name != "" {
			spans = append(spans, text("  "+f.Source.Name, dim))
		}
		cells = append(cells, widgets.NewFlexible(line(spans...)))
		if f.Range.Line > 0 {
			badge := strconv.Itoa(f.Range.Line)
			if f.Range.Column > 0 {
				badge += ":" + strconv.Itoa(f.Range.Column)
			}
			cells = append(cells, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}), lineBadge(bc, badge, st))
		}
		return widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	case RowShowMore:
		return line(text(row.Text, dim.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.TextLinkForeground)})))
	case RowLoadMore:
		return line(text(row.Text, dim.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.TextLinkForeground)})))
	case RowMessage:
		return line(text(row.Text, dim))
	}
	return kit.NewHighlightedLabel(bc, n.Label, st)
}

func sessionStopReason(s *debug.Session) string {
	if d := s.StoppedDetails(); d != nil {
		return d.Reason
	}
	return ""
}

func threadReason(t *debug.Thread) string {
	if d := t.StoppedDetails(); d != nil {
		return d.Reason
	}
	return ""
}

// sessionStateLabel is the state of a session that shows next to its name: nothing while its only thread's row says it.
func sessionStateLabel(s *debug.Session) string {
	threads := s.Threads()
	if len(threads) != 1 {
		return ""
	}
	return threads[0].StateLabel()
}

// stateBadge is the label of the state of a session or thread: "Running", "Paused on breakpoint", on the exception colors when the
// stop is an exception.
func stateBadge(bc widgets.BuildContext, label string, exception bool) widgets.Widget {
	colors := kit.ThemeOf(bc)
	bg, fg := named(colors, "debugView.stateLabelBackground", ui.Color{}), named(colors, "debugView.stateLabelForeground", colors.Foreground)
	if exception {
		bg, fg = named(colors, "debugView.exceptionLabelBackground", bg), named(colors, "debugView.exceptionLabelForeground", fg)
	}
	return pill(bg, widgets.NewText(label, widgets.TextOpts{Style: kit.TextStyleOf(bc, 11, fg), MaxLines: foundation.Some(1)}))
}

// lineBadge is the badge with the line (and column) of a frame, in the badge colors, dimmed while the row is not selected.
func lineBadge(bc widgets.BuildContext, text string, st kit.RowState) widgets.Widget {
	colors := kit.ThemeOf(bc)
	bg, fg := colors.BadgeBackground, colors.BadgeForeground
	if !st.Selected {
		bg, fg = bg.WithOpacity(0.6), fg.WithOpacity(0.6)
	}
	return pill(bg, widgets.NewText(text, widgets.TextOpts{Style: kit.TextStyleOf(bc, 11, fg), MaxLines: foundation.Some(1)}))
}

// pill is a label on a rounded background: the state of a thread and the line of a frame. It is 1 pixel taller than its text on each side
// and 6 wider (the first argument of NewEdgeInsetsSymmetric is the vertical one: the pills of step 25.4 had 6 above and below, which made
// them taller than the row, and its bottom was cut off).
func pill(bg ui.Color, label widgets.Widget) widgets.Widget {
	return widgets.NewDecoratedBox(painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(bg), BorderRadius: painting.NewBorderRadiusCircular(10)}),
		widgets.NewPadding(painting.NewEdgeInsetsSymmetric(1, 6), label))
}
