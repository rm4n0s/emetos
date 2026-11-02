package app

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// The debugger in the editors of the window (SPEC §10.31 "The editor", step 25.5). Every text editor of a file follows the debug model of
// the workspace: the breakpoints become the glyphs of its margin, the frames of the stopped threads the lines it highlights, the variables
// of the focused frame the values written after its lines, and an exception stop the exception widget. The editors of the window of the
// host and those of the headless views of its clients are all made here, from the one model of the host, so a breakpoint set in the
// editor of a client is the host's and shows in both (D46).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/breakpointEditorContribution.ts, callStackEditorContribution.ts and
// debugEditorContribution.ts

// ---- the breakpoints of the open documents

// debugHub keeps the breakpoints of the open documents of a workspace on the places they were set at, as the documents are edited
// (debug.Tracker), for the editors of every screen. A document is tracked while an editor shows it.
type debugHub struct {
	tracker *debug.Tracker
	docs    map[string]*hubDoc
}

// hubDoc is a tracked document and the number of editors that hold it.
type hubDoc struct {
	doc  *document.Document
	refs int
	stop func()
}

// debugHostOf is the workspace that has the debug host: the window of the host for it and for the views of its clients.
func (w *Workspace) debugHostOf() *Workspace {
	if w.isView() {
		return w.viewOf
	}
	return w
}

// debugHubOf is the hub of the workspace of the host; UI thread only.
func (w *Workspace) debugHubOf(svc *debug.Service) *debugHub {
	h := w.debugHostOf()
	if h.debugTracks == nil {
		h.debugTracks = &debugHub{tracker: debug.NewTracker(svc), docs: map[string]*hubDoc{}}
	}
	return h.debugTracks
}

// acquire starts following the breakpoints of a document for an editor and returns the function that lets go of it. post hands work
// to the UI thread.
func (h *debugHub) acquire(host *Workspace, path string, doc *document.Document, post func(func())) (release func()) {
	hd := h.docs[path]
	if hd != nil && hd.doc != doc {
		// another document of the same file (it was closed and opened again while an old editor lingers): the newer one is tracked
		h.release(path)
		hd = nil
	}
	if hd == nil {
		hd = &hubDoc{doc: doc}
		h.docs[path] = hd
		h.tracker.Track(path, doc)
		var pending atomic.Bool
		stopChange := doc.Subscribe(func(document.ChangeEvent) {
			if pending.CompareAndSwap(false, true) {
				post(func() {
					pending.Store(false)
					if h.docs[path] == hd {
						h.sync(host, path)
					}
				})
			}
		})
		stopSaved := doc.OnSaved(func(document.SavedEvent) {
			post(func() {
				if h.docs[path] != hd {
					return
				}
				h.sync(host, path)
				svc := host.DebugService()
				if svc == nil {
					return
				}
				go func() {
					if err := svc.DocumentSaved(host.ctx, path); err != nil && host.ctx.Err() == nil {
						host.Process.Logger.Warn("the breakpoints cannot be sent after a save", "path", path, "err", err)
					}
				}()
			})
		})
		hd.stop = func() { stopChange(); stopSaved() }
	}
	hd.refs++
	done := false
	return func() {
		if done {
			return
		}
		done = true
		if cur := h.docs[path]; cur == hd {
			hd.refs--
			if hd.refs <= 0 {
				h.release(path)
			}
		}
	}
}

// release stops following a file.
func (h *debugHub) release(path string) {
	hd := h.docs[path]
	if hd == nil {
		return
	}
	hd.stop()
	h.tracker.Untrack(path)
	delete(h.docs, path)
}

// sync brings the breakpoints of a file to the places the edits moved them to.
func (h *debugHub) sync(host *Workspace, path string) {
	if _, err := h.tracker.Sync(host.ctx, path); err != nil {
		host.Process.Logger.Warn("the breakpoints cannot follow the edits", "path", path, "err", err)
	}
}

// ---- the editor's side

// editorDebug is the debugger of one text editor.
type editorDebug struct {
	s       *shellState
	binding *widgets.WidgetsBinding
	win     *Workspace
	e       *textEditor
	svc     *debug.Service

	// path is the file of the document, empty for a document that is not a file on disk.
	path    string
	release func()
	pending atomic.Bool
	// gen numbers the requests of the values, so that only the latest one shows.
	gen int
	// ctx ends with the editor.
	ctx    context.Context
	cancel context.CancelFunc
	// candKey is what the candidates were asked for, excKey the stop the exception widget shows and closedKey the one the user closed.
	candKey, excKey, closedKey string
	stops                      []func()
	disposed                   bool
}

// followDebug makes the editor show and set the breakpoints and the stops of the debug service. It returns the function that stops it.
func (s *shellState) followDebug(binding *widgets.WidgetsBinding, e *textEditor) func() {
	win := PeekWorkspace(s.Context())
	if win == nil || win.DebugService() == nil {
		return func() {}
	}
	svc := win.DebugService()
	ctx, cancel := context.WithCancel(win.ctx)
	d := &editorDebug{s: s, binding: binding, win: win, e: e, svc: svc, ctx: ctx, cancel: cancel}
	e.dbg = d
	d.attach()
	d.stops = append(d.stops,
		svc.Subscribe(func(ev debug.Event) {
			switch ev.Kind {
			case debug.EventBreakpoints, debug.EventFocus, debug.EventStopped, debug.EventSessions, debug.EventCallStack,
				debug.EventVariables, debug.EventCapabilities, debug.EventConfiguration, debug.EventSessionEnded, debug.EventExited:
				d.schedule()
			}
		}),
		win.Settings.OnChange(func(ev settings.ChangeEvent) {
			if ev.Affects("debug", "") {
				d.schedule()
			}
		}),
		e.doc.OnURIChange(func(_, _ document.URI) {
			binding.Post(func() {
				if !d.disposed {
					d.attach()
					d.refresh()
				}
			})
		}),
	)
	e.view.SetDebugHost(&editor.DebugHost{
		Enabled:     d.canSet,
		GlyphPress:  d.glyphPress,
		InlinePress: d.inlinePress,
		HoverReady:  d.hoverReady,
		Evaluate:    d.evaluate,
	})
	d.refresh()
	return d.dispose
}

// schedule refreshes the editor on the UI thread, once for any number of events.
func (d *editorDebug) schedule() {
	if d.pending.CompareAndSwap(false, true) {
		d.binding.Post(func() {
			d.pending.Store(false)
			if !d.disposed {
				d.refresh()
			}
		})
	}
}

// attach follows the file of the document: its breakpoints are tracked while the editor shows it.
func (d *editorDebug) attach() {
	if d.release != nil {
		d.release()
		d.release = nil
	}
	d.path = ""
	uri := d.e.doc.URI()
	if uri.Scheme != document.SchemeFile || uri.Path == "" {
		return
	}
	d.path = uri.Path
	hub := d.win.debugHubOf(d.svc)
	d.release = hub.acquire(d.win.debugHostOf(), d.path, d.e.doc, d.binding.Post)
}

// dispose stops following the debugger and takes the marks and the widgets away.
func (d *editorDebug) dispose() {
	if d.disposed {
		return
	}
	d.disposed = true
	d.cancel()
	for _, stop := range d.stops {
		stop()
	}
	d.stops = nil
	if d.release != nil {
		d.release()
		d.release = nil
	}
	d.e.view.DisposeDebug()
	d.e.view.DisposeInlineValues()
	d.e.dbg = nil
}

// canSet says whether breakpoints can be set in the document: it is a file, and a debugger is there for its language (or
// debug.allowBreakpointsEverywhere says any file will do).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts (canSetBreakpointsIn)
func (d *editorDebug) canSet() bool {
	if d.path == "" || d.win.Debug == nil {
		return false
	}
	lang := d.e.doc.LanguageID()
	return len(d.win.Debug.DebuggersForLanguage(lang)) > 0 ||
		d.win.Debug.AllowsBreakpoints(lang, settings.Get[bool](d.win.Settings, "debug.allowBreakpointsEverywhere", ""))
}

// refresh puts the marks of the model on the editor: the breakpoints, the frames, the candidates, the inline values and the exception.
func (d *editorDebug) refresh() {
	view := d.e.view
	if d.path == "" {
		view.SetDebugBreakpoints(nil)
		view.SetDebugFrames(nil)
		return
	}
	// the places the edits moved the breakpoints to are read before the marks are made from the model, which a new breakpoint changes
	d.win.debugHubOf(d.svc).sync(d.win.debugHostOf(), d.path)
	view.SetDebugBreakpoints(d.breakpointMarks())
	view.SetDebugFrames(d.frameMarks())
	d.updateCandidates()
	d.updateValues()
	d.updateException()
}

// ---- breakpoints

// lineText is the text of a line of the document, or "" for a line it does not have.
func (d *editorDebug) lineText(line int) string {
	text, err := d.e.doc.Snapshot().LineContent(line)
	if err != nil {
		return ""
	}
	return text
}

// breakpointMarks are the breakpoints of the file as the editor draws them.
func (d *editorDebug) breakpointMarks() []editor.DebugBreakpointMark {
	m := d.svc.Model()
	activated := m.BreakpointsActivated()
	active := d.svc.State() != debug.Inactive
	var out []editor.DebugBreakpointMark
	for _, bp := range m.Breakpoints(debug.BreakpointFilter{OriginalPath: d.path}) {
		if bp.Line < 1 {
			continue
		}
		mark := editor.DebugBreakpointMark{
			ID: bp.ID, Line: bp.Line - 1, Inline: bp.Inline(), Kind: editor.DebugGlyphBreakpoint,
			Enabled: bp.Enabled, Verified: bp.Verified || !active, Activated: activated,
			Unsupported: !bp.Supported, Pending: bp.Pending,
		}
		if bp.Inline() {
			mark.Col = debug.ByteOfColumn(d.lineText(mark.Line), bp.Column)
		}
		switch {
		case bp.LogMessage != "":
			mark.Kind = editor.DebugGlyphLog
		case bp.Condition != "" || bp.HitCondition != "" || bp.TriggeredBy != "":
			mark.Kind = editor.DebugGlyphConditional
		}
		mark.Hover = breakpointHover(bp, d.triggerLabel(bp.TriggeredBy), activated, active)
		out = append(out, mark)
	}
	return out
}

// triggerLabel is the place of the breakpoint another waits for: "loader.go: 14".
func (d *editorDebug) triggerLabel(id string) string {
	if id == "" {
		return ""
	}
	for _, bp := range d.svc.Model().Breakpoints(debug.BreakpointFilter{}) {
		if bp.ID == id {
			return filepath.Base(bp.Path) + ": " + strconv.Itoa(bp.Line)
		}
	}
	return ""
}

// The texts of the hover of a glyph.
const (
	unsupportedBreakpointText = "Breakpoints of this type are not supported by the debugger"
)

// breakpointHover is the text of the hover of the glyph of a breakpoint: why it is not on, or what it waits for.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/breakpointsView.ts (getBreakpointMessageAndIcon)
func breakpointHover(bp debug.Breakpoint, triggeredBy string, activated, debugging bool) string {
	logpoint := bp.LogMessage != ""
	kind := "Breakpoint"
	if logpoint {
		kind = "Logpoint"
	}
	switch {
	case !activated || !bp.Enabled:
		return "Disabled " + kind
	case bp.Pending:
		return ""
	case debugging && !bp.Verified:
		if bp.Message != "" {
			return escapeMarkdown(bp.Message)
		}
		return "Unverified " + kind
	}
	var lines []string
	if !bp.Supported {
		lines = append(lines, unsupportedBreakpointText)
	}
	if logpoint {
		lines = append(lines, "Log Message: "+bp.LogMessage)
	}
	if bp.Condition != "" {
		lines = append(lines, "Condition: "+bp.Condition)
	}
	if bp.HitCondition != "" {
		lines = append(lines, "Hit Count: "+bp.HitCondition)
	}
	if triggeredBy != "" {
		lines = append(lines, "Hit after breakpoint: "+triggeredBy)
	}
	if len(lines) > 0 {
		text := strings.Join(lines, "\n")
		if bp.Message != "" {
			text += ", " + bp.Message
		}
		return escapeMarkdown(text)
	}
	if bp.Message != "" {
		return escapeMarkdown(bp.Message)
	}
	return kind
}

// escapeMarkdown makes a text show as it is in a hover, which renders Markdown; a line ends with two spaces to stay a line.
func escapeMarkdown(text string) string {
	r := strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "\\<", ">", "\\>", "#", "\\#", "|", "\\|")
	return strings.ReplaceAll(r.Replace(text), "\n", "  \n")
}

// ---- frames

// frameMarks are the frames of the file that are stopped at: the top frame of each stopped thread and the focused frame.
func (d *editorDebug) frameMarks() []editor.DebugFrameMark {
	m := d.svc.Model()
	focus := m.Focus()
	var out []editor.DebugFrameMark
	seen := map[[2]int]bool{}
	add := func(f *debug.StackFrame, top, glyph bool) {
		if f == nil || f.Source.Path == "" || filepath.Clean(f.Source.Path) != filepath.Clean(d.path) || f.Range.Line < 1 {
			return
		}
		line := f.Range.Line - 1
		col := 0
		if f.Range.Column > 1 {
			col = debug.ByteOfColumn(d.lineText(line), f.Range.Column)
		}
		out = append(out, editor.DebugFrameMark{Line: line, Col: col, Top: top, Glyph: glyph})
		seen[[2]int{line, col}] = true
	}
	for _, sess := range m.Sessions(false) {
		for _, t := range sess.Threads() {
			if !t.Stopped() {
				continue
			}
			if cs := t.CallStack(); len(cs) > 0 {
				add(cs[0], true, sess == focus.Session)
			}
		}
	}
	if f := focus.Frame; f != nil && f.Index != 0 {
		add(f, false, true)
	}
	return out
}

// hoverReady says that a session is stopped in a frame of the file, so the debug hover has something to evaluate in.
func (d *editorDebug) hoverReady() bool {
	f := d.svc.Model().Focus()
	return d.path != "" && f.Session != nil && f.Frame != nil && f.Thread != nil && f.Thread.Stopped() &&
		f.Frame.Source.Path != "" && filepath.Clean(f.Frame.Source.Path) == filepath.Clean(d.path)
}

// ---- candidates

// updateCandidates asks the adapter where inline breakpoints can be set on the lines with a breakpoint (the answer of a slow adapter is
// not waited for beyond half a second).
//
// Ported from VS Code: breakpointEditorContribution.ts (the breakpointLocations request of the breakpoint decorations)
func (d *editorDebug) updateCandidates() {
	view := d.e.view
	var sess *debug.Session
	f := d.svc.Model().Focus()
	if f.Session != nil && f.Session.Capabilities().SupportsBreakpointLocationsRequest {
		sess = f.Session
	}
	var lines []int
	if sess != nil && settings.Get[bool](d.win.Settings, "debug.showInlineBreakpointCandidates", "") {
		for _, bp := range d.svc.Model().Breakpoints(debug.BreakpointFilter{OriginalPath: d.path}) {
			if bp.Line > 0 && !slices.Contains(lines, bp.Line) {
				lines = append(lines, bp.Line)
			}
		}
		sort.Ints(lines)
	}
	key := ""
	if sess != nil && len(lines) > 0 {
		key = sess.ID() + fmtInts(lines)
	}
	if key == d.candKey {
		return
	}
	d.candKey = key
	if key == "" {
		view.SetDebugCandidates(nil)
		return
	}
	ctx, path := d.ctx, d.path
	go func() {
		var found []editor.DebugCandidate
		type loc struct{ line, col int }
		var locs []loc
		for _, line := range lines {
			c, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			list, err := sess.BreakpointLocations(c, path, line)
			cancel()
			if err != nil {
				continue
			}
			for _, l := range list {
				if l.Line == 0 {
					l.Line = line
				}
				if l.Line == line && l.Column > 0 {
					locs = append(locs, loc{l.Line, l.Column})
				}
			}
		}
		d.binding.Post(func() {
			if d.disposed || d.candKey != key {
				return
			}
			for _, l := range locs {
				found = append(found, editor.DebugCandidate{Line: l.line - 1, Col: debug.ByteOfColumn(d.lineText(l.line-1), l.col)})
			}
			d.e.view.SetDebugCandidates(found)
		})
	}()
}

// fmtInts writes numbers as ",1,2".
func fmtInts(list []int) string {
	var b strings.Builder
	for _, n := range list {
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(n))
	}
	return b.String()
}

// ---- inline values

// updateValues writes the values of the variables of the focused frame after the lines of the file that name them, when the frame is in
// this file.
//
// Ported from VS Code: debugEditorContribution.ts (updateInlineValueDecorations)
func (d *editorDebug) updateValues() {
	view := d.e.view
	d.gen++
	gen := d.gen
	f := d.svc.Model().Focus()
	mode := settings.Get[string](d.win.Settings, "debug.inlineValues", "")
	if mode == "off" || f.Frame == nil || f.Thread == nil || !f.Thread.Stopped() || f.Frame.Source.Path == "" ||
		filepath.Clean(f.Frame.Source.Path) != filepath.Clean(d.path) || f.Frame.Range.Line < 1 {
		view.SetInlineValues(nil)
		return
	}
	frame := f.Frame
	// The loads of the scopes and of their variables are shared with the views (the model keeps what the first request got), so they are
	// not cancelled when a newer refresh comes (that would leave the views with a load that was cut short): only the result is dropped.
	ctx := d.win.ctx
	go func() {
		scopes, err := frame.Scopes(ctx)
		if err != nil || ctx.Err() != nil {
			return
		}
		type ranged struct {
			scope debug.InlineScope
			start int
		}
		var all []ranged
		for _, sc := range scopes {
			if sc.Expensive || sc.Err != "" {
				continue
			}
			kids, err := sc.Children(ctx)
			if err != nil || ctx.Err() != nil {
				continue
			}
			is := debug.InlineScope{}
			for i, v := range kids {
				if i >= debug.MaxInlineValues {
					break
				}
				is.Variables = append(is.Variables, debug.InlineVariable{Name: v.Name, Value: v.Value()})
			}
			if sc.Range != nil {
				is.FirstLine = sc.Range.Line
			}
			all = append(all, ranged{is, is.FirstLine})
		}
		// the narrowest scope first: the one that starts last
		sort.SliceStable(all, func(i, j int) bool { return all[i].start > all[j].start })
		list := make([]debug.InlineScope, len(all))
		for i, r := range all {
			list[i] = r.scope
		}
		d.binding.Post(func() {
			if d.disposed || gen != d.gen {
				return
			}
			d.applyValues(list, frame.Range.Line)
		})
	}()
}

// applyValues works out the texts for the scopes of the frame and gives them to the editor.
func (d *editorDebug) applyValues(scopes []debug.InlineScope, stopped int) {
	first := max(stopped-500, 1)
	tz := d.e.tokenizer
	if tz != nil {
		tz.Ensure(stopped-1, 20*time.Millisecond)
	}
	words := func(line int) []string { return identifiers(d.lineText(line-1), tz, line-1) }
	content := func(line int) string { return d.lineText(line - 1) }
	lines := debug.InlineValues(scopes, stopped, first, stopped, words, content)
	list := make([]editor.LineAnnotation, 0, len(lines))
	for _, l := range lines {
		shown, full := debug.InlineText(l.Values, debug.MaxInlineLength)
		list = append(list, editor.LineAnnotation{
			Line: l.Line - 1, Text: shown, Color: "editor.inlineValuesForeground", Background: "editor.inlineValuesBackground",
			Tight: true, Hover: debug.InlineHover(full),
		})
	}
	d.e.view.SetInlineValues(list)
}

var identifierRE = regexp.MustCompile(`[\p{L}_$][\p{L}\p{N}_$]*`)

// identifiers are the words of a line that are code: the ones in a comment, a string or a regular expression are left out.
func identifiers(text string, tz *tokens.Tokenizer, line int) []string {
	if tz == nil {
		return identifierRE.FindAllString(text, -1)
	}
	toks := tz.Line(line)
	if len(toks) == 0 {
		return identifierRE.FindAllString(text, -1)
	}
	var out []string
	start := 0
	for _, t := range toks {
		end := min(int(t.End), len(text))
		if t.Type == textmate.TokenOther && end > start {
			out = append(out, identifierRE.FindAllString(text[start:end], -1)...)
		}
		start = max(start, end)
	}
	return out
}

// ---- the exception widget

// updateException shows the exception widget under the line the program stopped on for an exception, in the editor of that file, and
// takes it away when the thread goes on or the focus moves elsewhere.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugEditorContribution.ts (the exception widget in
// onFocusStackFrame) and exceptionWidget.ts
func (d *editorDebug) updateException() {
	view := d.e.view
	f := d.svc.Model().Focus()
	var details *debug.StoppedDetails
	if f.Thread != nil && f.Thread.Stopped() {
		details = f.Thread.StoppedDetails()
	}
	want := details != nil && details.Reason == dap.StopException && f.Frame != nil && f.Frame.Index == 0 &&
		f.Frame.Source.Path != "" && filepath.Clean(f.Frame.Source.Path) == filepath.Clean(d.path) && f.Frame.Range.Line > 0
	if !want {
		if d.excKey != "" || view.DebugExceptionOpen() {
			view.DebugCloseException()
		}
		d.excKey = ""
		if f.Thread == nil || !f.Thread.Stopped() {
			d.closedKey = ""
		}
		return
	}
	key := f.Session.ID() + ":" + strconv.Itoa(f.Thread.ID) + ":" + strconv.Itoa(f.Frame.ID)
	if key == d.excKey || key == d.closedKey {
		return
	}
	d.excKey = key
	thread, line := f.Thread, f.Frame.Range.Line-1
	ctx := d.ctx
	go func() {
		info, err := thread.ExceptionInfo(ctx)
		if err != nil || info == nil || ctx.Err() != nil {
			return
		}
		d.binding.Post(func() {
			if d.disposed || d.excKey != key {
				return
			}
			ex := editor.DebugException{ID: info.ID, Description: info.Description}
			if info.Details != nil {
				ex.StackTrace = info.Details.StackTrace
				if ex.Description == "" {
					ex.Description = info.Details.Message
				}
				if ex.ID == "" {
					ex.ID = info.Details.FullTypeName
					if ex.ID == "" {
						ex.ID = info.Details.TypeName
					}
				}
			}
			err := d.e.view.DebugShowException(line, ex, d.s.copyText, func() { d.closedKey = key; d.excKey = "" })
			if err != nil {
				d.win.Process.Logger.Warn("the exception widget cannot show", "err", err)
			}
		})
	}()
}

// positionOf is the position of the primary cursor of the editor.
func (d *editorDebug) positionOf() buffer.Position {
	return d.e.view.Cursors().Primary().Selection.Active
}
