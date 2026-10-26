package editor

import (
	"slices"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// Diagnostics in the editor (SPEC §9.12 "Diagnostics"): the markers of a file that the Problems panel holds are squiggles in
// its editor. They are decorations of the document that this editor owns, so they follow the edits until the producer sets
// new markers, and only the squiggles of the rows in view are looked up and drawn.
//
// Ported from VS Code: src/vs/editor/common/services/markerDecorationsService.ts (ModelMarkerHandler, _createDecorationRange,
// _createDecorationOption)

// diagnosticsOwner is the slot of the decorations of the diagnostics (see ViewModel.owner).
const diagnosticsOwner = 8

// DiagnosticSeverity says how serious a diagnostic is. The values are VS Code's MarkerSeverity, so that a larger value is
// more serious.
type DiagnosticSeverity int

// The severities of a diagnostic.
const (
	// DiagnosticHint is a suggestion, drawn as three dots (no mark on the overview ruler).
	DiagnosticHint DiagnosticSeverity = 1
	// DiagnosticInfo is information.
	DiagnosticInfo DiagnosticSeverity = 2
	// DiagnosticWarning is a warning.
	DiagnosticWarning DiagnosticSeverity = 4
	// DiagnosticError is an error.
	DiagnosticError DiagnosticSeverity = 8
)

// String is the word for the severity as the hover and the tests name it.
func (s DiagnosticSeverity) String() string {
	switch s {
	case DiagnosticHint:
		return "hint"
	case DiagnosticInfo:
		return "info"
	case DiagnosticWarning:
		return "warning"
	}
	return "error"
}

// DiagnosticRelated is a place that is part of a diagnostic, in another file or in the same one. Its range counts from 1, in
// UTF-16 units, as the markers do.
type DiagnosticRelated struct {
	// Resource is the absolute path of the file.
	Resource string
	// Message says why the place is related.
	Message string
	// Line and Column are where the range starts, counted from 1.
	Line, Column int
}

// Diagnostic is a problem of the document the editor shows: a marker, with its range in byte positions of the text.
type Diagnostic struct {
	// Range is where the problem is.
	Range buffer.Range
	// Severity is how serious it is.
	Severity DiagnosticSeverity
	// Message is what is wrong; Source says who found it and Code names the problem. CodeHref, when set, is a page that
	// describes the code.
	Message, Source, Code, CodeHref string
	// Unnecessary fades the text of the range (unused code) and Deprecated strikes it through.
	Unnecessary, Deprecated bool
	// Owner is the producer of the diagnostic (the owner of the marker).
	Owner string
	// Related are the places that are part of the problem.
	Related []DiagnosticRelated
}

// diagnosticState is what the view model keeps for its diagnostics.
type diagnosticState struct {
	// ids are the decorations of the diagnostics, so that a new set takes the old one away without a search.
	ids []int
	// generation counts the sets, so that what is worked out from them (the marks of the overview ruler) is known to be
	// stale.
	generation int
	// ruler holds the marks of the overview ruler for the generation, the height of the ruler and the content it was made for.
	ruler rulerCache
}

// SetDiagnostics replaces the diagnostics of the editor. The diagnostics are decorations of the document that this editor
// owns, so another editor of the same document does not show them, and they move with the edits.
func (vm *ViewModel) SetDiagnostics(ds []Diagnostic) {
	snap := vm.doc.Snapshot()
	words := vm.wordClassifier()
	add := make([]document.NewDecoration, len(ds))
	for i, d := range ds {
		d.Range = diagnosticRange(snap, words, d)
		add[i] = document.NewDecoration{Range: d.Range, Options: document.DecorationOptions{
			Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: "diagnostic", Data: d,
		}}
	}
	st := &vm.diag
	st.ids = vm.doc.DeltaDecorations(vm.owner(diagnosticsOwner), st.ids, add)
	st.generation++
	vm.NotifyListeners()
}

// DisposeDiagnostics takes the diagnostics off the document.
func (vm *ViewModel) DisposeDiagnostics() {
	vm.doc.RemoveDecorationsOf(vm.owner(diagnosticsOwner))
	vm.diag.ids = nil
	vm.diag.generation++
}

// DiagnosticCount is the number of diagnostics the editor holds.
func (vm *ViewModel) DiagnosticCount() int { return len(vm.diag.ids) }

// Diagnostics are the diagnostics as they are now (their ranges moved with the edits), in order of position.
func (vm *ViewModel) Diagnostics() []Diagnostic {
	if len(vm.diag.ids) == 0 {
		return nil
	}
	return diagnosticsOf(vm.doc.DecorationsOf(vm.owner(diagnosticsOwner)))
}

// diagnosticsOf reads the diagnostics out of decorations, with their ranges as the decorations have them.
func diagnosticsOf(decos []document.Decoration) []Diagnostic {
	out := make([]Diagnostic, 0, len(decos))
	for _, d := range decos {
		if data, ok := d.Options.Data.(Diagnostic); ok {
			data.Range = d.Range
			out = append(out, data)
		}
	}
	return out
}

// diagnosticsShown tells whether the squiggles are drawn: there are some, and the editor is not read-only (VS Code's
// editor.renderValidationDecorations "editable", its default).
func (vm *ViewModel) diagnosticsShown() bool {
	return len(vm.diag.ids) > 0 && !vm.ReadOnly()
}

// diagnosticsIn are the diagnostics that touch the lines of the rows first to last, in order of severity (the most serious
// last, so that it is drawn over the others), then of position. Only they are read from the document, so that the cost of a
// frame does not depend on how many diagnostics the file has elsewhere.
func (vm *ViewModel) diagnosticsIn(firstRow, lastRow int) []Diagnostic {
	if !vm.diagnosticsShown() {
		return nil
	}
	proj := vm.Projection()
	firstLine, _, _ := proj.RowSpan(firstRow)
	lastLine, _, _ := proj.RowSpan(lastRow)
	span := buffer.Range{Start: buffer.Position{Line: firstLine}, End: buffer.Position{Line: lastLine + 1}}
	out := diagnosticsOf(vm.doc.DecorationsIn(span, vm.owner(diagnosticsOwner)))
	slices.SortStableFunc(out, func(a, b Diagnostic) int { return int(a.Severity) - int(b.Severity) })
	return out
}

// DiagnosticsAt are the diagnostics under a position, the most serious first: a position is under a diagnostic when it is
// inside its range, or at its start when the range is empty.
func (vm *ViewModel) DiagnosticsAt(p buffer.Position) []Diagnostic {
	if !vm.diagnosticsShown() {
		return nil
	}
	here := buffer.Range{Start: p, End: p}
	var out []Diagnostic
	for _, d := range diagnosticsOf(vm.doc.DecorationsIn(here, vm.owner(diagnosticsOwner))) {
		if rangeHolds(d.Range, p) {
			out = append(out, d)
		}
	}
	slices.SortStableFunc(out, func(a, b Diagnostic) int { return int(b.Severity) - int(a.Severity) })
	return out
}

// diagnosticRange is the range a diagnostic is drawn over: the range of the marker made valid for the text; a hint that has
// no tag covers two characters; an empty range covers the word it is in, unless it is at the end of the line.
//
// Ported from VS Code: markerDecorationsService.ts (_createDecorationRange)
func diagnosticRange(snap buffer.Snapshot, words *edit.WordClassifier, d Diagnostic) buffer.Range {
	r := snap.ValidateRange(d.Range)
	if d.Severity == DiagnosticHint && !d.Unnecessary && !d.Deprecated {
		line, _ := snap.LineContent(r.Start.Line)
		end := r.Start.Col
		for range 2 {
			end = edit.NextGrapheme(line, min(end, len(line)))
		}
		r = snap.ValidateRange(buffer.Range{Start: r.Start, End: buffer.Position{Line: r.Start.Line, Col: end}})
	}
	if r.Start == r.End {
		line, _ := snap.LineContent(r.Start.Line)
		last := len(strings.TrimRight(line, " \t"))
		if last == 0 || r.End.Col >= last {
			return r // an empty line, or behind the text: one character wide
		}
		if w, ok := words.WordAt(line, r.Start.Col); ok {
			return buffer.Range{Start: buffer.Position{Line: r.Start.Line, Col: w.Start}, End: buffer.Position{Line: r.Start.Line, Col: w.End}}
		}
	}
	return r
}

// rangeHolds tells whether a position is inside a range of a diagnostic: from its start up to, not including, its end; an
// empty range holds its own position only.
func rangeHolds(r buffer.Range, p buffer.Position) bool {
	if r.Start == r.End {
		return r.Start == p
	}
	after := r.Start.Line < p.Line || (r.Start.Line == p.Line && r.Start.Col <= p.Col)
	before := p.Line < r.End.Line || (p.Line == r.End.Line && p.Col < r.End.Col)
	return after && before
}

// wordClassifier tells the words of the editor: those of editor.wordSeparators, VS Code's when it is empty.
func (vm *ViewModel) wordClassifier() *edit.WordClassifier {
	if vm.opts.WordSeparators != "" {
		return edit.NewWordClassifier(vm.opts.WordSeparators)
	}
	return edit.DefaultOptions().WordSeparators
}
