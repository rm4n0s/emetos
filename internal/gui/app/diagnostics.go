package app

import (
	"context"
	"math"
	"strings"
	"sync/atomic"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// Diagnostics in the window (SPEC §9.12 "Diagnostics", "As built (step 12.4)"): the editors show the markers of their files
// as squiggles, and F8 and Shift+F8 go through the problems of the files.

// markerNav is the state of the navigation through the problems: the list it goes through and the editor it was shown in.
type markerNav struct {
	list *problems.MarkerList
}

// followDiagnostics keeps the squiggles of an editor those of the markers of its file: they are set when the editor is made
// and again whenever the markers of the file change (the change may come from any goroutine, so it is posted to the UI
// thread, once for any number of changes), and when the file is saved under another name. It returns the function that stops it.
func (s *shellState) followDiagnostics(binding *widgets.WidgetsBinding, e *textEditor) func() {
	win := PeekWorkspace(s.Context())
	var path atomic.Value
	path.Store(diagnosticPath(e.doc))
	var pending atomic.Bool
	sync := func() {
		pending.Store(false)
		if e.disposed {
			return
		}
		e.view.SetDiagnostics(diagnosticsFor(win.Markers, e.doc))
	}
	unsubscribe := win.Markers.Subscribe(func(c problems.Change) {
		mine, _ := path.Load().(string)
		if mine == "" {
			return
		}
		for _, r := range c.Resources {
			if r == mine {
				if pending.CompareAndSwap(false, true) {
					binding.Post(sync)
				}
				return
			}
		}
	})
	unwatchURI := e.doc.OnURIChange(func(_, _ document.URI) {
		path.Store(diagnosticPath(e.doc))
		binding.Post(sync)
	})
	e.view.SetOpeners(
		func(uri string) {
			s.Go(func(life context.Context) {
				if err := win.openURI(life, uri); err != nil {
					win.Notify.Error("Emetos", "Cannot open "+uri+": "+describe(err))
				}
			})
		},
		func(resource string, line, column int) {
			s.openMarker(binding, problems.Marker{Resource: resource, StartLine: line, StartColumn: column, EndLine: line, EndColumn: column}, true)
		})
	e.view.SetViewProblem(func(p buffer.Position) { s.showProblemAt(e, p) })
	if len(win.Markers.Read(problems.ReadFilter{Resource: diagnosticPath(e.doc)})) > 0 {
		sync()
	}
	return func() {
		unsubscribe()
		unwatchURI()
		e.view.DisposeDiagnostics()
	}
}

// diagnosticPath is the path the markers of a document are filed under: its file, else none.
func diagnosticPath(doc *document.Document) string {
	if uri := doc.URI(); uri.Scheme == document.SchemeFile {
		return uri.Path
	}
	return ""
}

// diagnosticsFor are the markers of a document as the diagnostics of its editor: the positions of a marker (counted from 1, in
// UTF-16 units) become byte positions in the text.
func diagnosticsFor(svc *problems.Service, doc *document.Document) []editor.Diagnostic {
	path := diagnosticPath(doc)
	if path == "" {
		return nil
	}
	markers := svc.Read(problems.ReadFilter{Resource: path})
	if len(markers) == 0 {
		return nil
	}
	snap := doc.Snapshot()
	out := make([]editor.Diagnostic, len(markers))
	for i, m := range markers {
		start := snapPosition(snap, m.StartLine, m.StartColumn)
		end := snapPosition(snap, m.EndLine, m.EndColumn)
		if m.StartColumn == 1 && m.EndColumn == math.MaxInt && start.Line == end.Line {
			// the whole line, from its first character that is not a blank (VS Code does the same)
			text, _ := snap.LineContent(start.Line)
			start.Col = len(text) - len(strings.TrimLeft(text, " \t"))
		}
		d := editor.Diagnostic{
			Range: buffer.Range{Start: start, End: end}, Severity: editor.DiagnosticSeverity(m.Severity),
			Message: m.Message, Source: m.Source, Code: m.Code, CodeHref: m.CodeHref,
			Unnecessary: m.Unnecessary, Deprecated: m.Deprecated, Owner: m.Owner,
		}
		for _, r := range m.Related {
			d.Related = append(d.Related, editor.DiagnosticRelated{Resource: r.Resource, Message: r.Message, Line: r.StartLine, Column: r.StartColumn})
		}
		out[i] = d
	}
	return out
}

// snapPosition is the byte position of a line and a UTF-16 column of a marker, both counted from 1, in a text.
func snapPosition(snap buffer.Snapshot, line, column int) buffer.Position {
	l := max(line-1, 0)
	text, err := snap.LineContent(l)
	if err != nil {
		return snap.ValidatePosition(buffer.Position{Line: l})
	}
	return buffer.Position{Line: l, Col: byteColumn(text, column)}
}

// utf16Position is the line and the column, both counted from 1 with the column in UTF-16 units, of a position of a document.
func utf16Position(snap buffer.Snapshot, p buffer.Position) (line, column int) {
	text, _ := snap.LineContent(p.Line)
	units := 0
	for _, r := range text[:min(p.Col, len(text))] {
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	return p.Line + 1, units + 1
}

// ---- Go to Next Problem and the like ----

// bindMarkerCommands gives editor.action.marker.next, .prev, .nextInFiles and .prevInFiles their handlers.
func (s *shellState) bindMarkerCommands() {
	win := PeekWorkspace(s.Context())
	for id, dir := range map[string]struct{ forward, multi bool }{
		"editor.action.marker.next":        {true, false},
		"editor.action.marker.prev":        {false, false},
		"editor.action.marker.nextInFiles": {true, true},
		"editor.action.marker.prevInFiles": {false, true},
	} {
		remove, err := win.Commands.SetHandler(id, func(command.Context, any) error {
			s.navigateMarkers(dir.forward, dir.multi)
			return nil
		})
		if err != nil {
			win.Process.Logger.Error("cannot bind a command of the marker navigation", "id", id, "err", err)
			continue
		}
		s.panels.removeCommands = append(s.panels.removeCommands, remove)
	}
}

// navigateMarkers goes to the next or the previous problem of the file of the active editor, or of every file, and shows it:
// the cursor goes to its start and its hover opens. A problem of another file opens that file.
//
// Ported from VS Code: src/vs/editor/contrib/gotoError/browser/gotoError.ts (MarkerController.navigate). The inline widget of
// the marker is the hover here.
func (s *shellState) navigateMarkers(forward, multi bool) {
	e := s.activeEditor()
	if e == nil || !s.Mounted() {
		return
	}
	win := PeekWorkspace(s.Context())
	path := diagnosticPath(e.doc)
	resource := path
	if multi {
		resource = ""
	}
	nav := &s.markerNav
	if nav.list == nil || !nav.list.Matches(resource) {
		nav.list = problems.NewMarkerList(win.Markers, resource)
	} else {
		nav.list.Refresh()
	}
	line, column := utf16Position(e.doc.Snapshot(), e.view.Cursors().Primary().Position())
	if sel, _, _, ok := nav.list.Selected(); ok && !sel.Holds(path, line, column) {
		nav.list.ResetIndex() // the cursor left the problem that was shown
	}
	nav.list.Move(forward, path, line, column)
	m, _, _, ok := nav.list.Selected()
	if !ok {
		return
	}
	if m.Resource != path {
		s.openMarkerThen(widgets.WidgetsBindingOf(s.Context()), m, false, func(other *textEditor) {
			s.showProblemAt(other, snapPosition(other.doc.Snapshot(), m.StartLine, m.StartColumn))
		})
		return
	}
	s.showProblemAt(e, snapPosition(e.doc.Snapshot(), m.StartLine, m.StartColumn))
}

// showProblemAt puts the cursor of an editor at the start of a problem, brings it into view and opens its hover, which stays
// until the cursor moves or Escape.
func (s *shellState) showProblemAt(e *textEditor, p buffer.Position) {
	if e.disposed {
		return
	}
	e.view.Cursors().SetSelections([]buffer.Selection{{Anchor: p, Active: p}})
	e.view.RevealLineInCenterIfOutsideViewport(p.Line)
	e.view.ShowHoverAt(p)
}
