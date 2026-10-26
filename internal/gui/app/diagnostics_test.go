package app

import (
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// The diagnostics in the window (step 12.4): the squiggles of the editors follow the markers, and F8 goes through the problems.

// diagnosticsApp is a window with two Go files, a.go open and b.go not.
func diagnosticsApp(t *testing.T) *groupApp {
	t.Helper()
	texts := map[string]string{
		"a.go": "package a\n\nvar é = \"😀\" + y\n\nfunc a() {}\n",
		"b.go": "package b\n\nvar x = 1\nvar z = 2\n",
	}
	return newGroupAppWith(t, newTestWorkspace(t, ""), testWindowSize, []string{"a.go", "b.go"}, texts, "a.go")
}

func diagMarker(sev problems.Severity, msg string, line, from, to int) problems.Marker {
	return problems.Marker{Severity: sev, Message: msg, StartLine: line, StartColumn: from, EndLine: line, EndColumn: to}
}

func TestMarkersAreSquigglesInTheEditorOfTheirFile(t *testing.T) {
	a := diagnosticsApp(t)
	path := a.files["a.go"]
	// line 3 is `var é = "😀" + y`: the columns of a marker count UTF-16 units, from 1, and y is the 16th of them
	a.win.Markers.Set("lsp", path, []problems.Marker{
		{Severity: problems.SeverityError, Message: "undefined: y", Source: "compiler", Code: "UndeclaredName", CodeHref: "https://example.com/y",
			StartLine: 3, StartColumn: 16, EndLine: 3, EndColumn: 17, Related: []problems.RelatedInformation{{Resource: "/x.go", Message: "r", StartLine: 2, StartColumn: 3}}},
		{Severity: problems.SeverityHint, Message: "unused", Unnecessary: true, Deprecated: true, StartLine: 5, StartColumn: 6, EndLine: 5, EndColumn: 7},
	})
	a.win.Markers.Set("lsp", a.files["b.go"], []problems.Marker{diagMarker(problems.SeverityError, "in the other file", 3, 1, 4)})
	a.pump()
	got := a.vm().Diagnostics()
	if len(got) != 2 {
		t.Fatalf("%d squiggles: %+v", len(got), got)
	}
	// é is 2 bytes and the emoji 4, so y is at the byte column 18 of its line
	line, _ := a.vm().Document().Snapshot().LineContent(2)
	col := len(line) - 1
	if e := got[0]; e.Message != "undefined: y" || e.Range != (buffer.Range{Start: buffer.Position{Line: 2, Col: col}, End: buffer.Position{Line: 2, Col: col + 1}}) ||
		e.Severity != editor.DiagnosticError || e.Source != "compiler" || e.Code != "UndeclaredName" || e.CodeHref != "https://example.com/y" ||
		e.Owner != "lsp" || len(e.Related) != 1 || e.Related[0].Line != 2 || e.Related[0].Column != 3 {
		t.Errorf("the error = %+v (y is at byte column %d)", e, col)
	}
	if h := got[1]; h.Severity != editor.DiagnosticHint || !h.Unnecessary || !h.Deprecated || h.Range.Start.Line != 4 || h.Range.Start.Col != 5 {
		t.Errorf("the hint = %+v", h)
	}

	// the markers of the owner are replaced as a whole, and removing them takes the squiggles away
	a.win.Markers.Set("lsp", path, []problems.Marker{diagMarker(problems.SeverityWarning, "a warning", 1, 1, 8)})
	a.pump()
	if got := a.vm().Diagnostics(); len(got) != 1 || got[0].Message != "a warning" {
		t.Errorf("after the update: %+v", got)
	}
	a.win.Markers.Set("other owner", path, []problems.Marker{diagMarker(problems.SeverityInfo, "from another owner", 2, 1, 1)})
	a.pump()
	if got := a.vm().Diagnostics(); len(got) != 2 {
		t.Errorf("the markers of every owner are squiggles: %+v", got)
	}
	a.win.Markers.Remove("lsp")
	a.win.Markers.Remove("other owner")
	a.pump()
	if got := a.vm().Diagnostics(); len(got) != 0 {
		t.Errorf("after removing the markers: %+v", got)
	}
}

func TestSquigglesStayAlignedWithTheTextUntilTheMarkersAreSetAgain(t *testing.T) {
	a := diagnosticsApp(t)
	a.win.Markers.Set("lsp", a.files["a.go"], []problems.Marker{diagMarker(problems.SeverityError, "on func", 5, 1, 5)})
	a.pump()
	a.vm().Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{})})
	if err := a.vm().RunCommand("type", map[string]any{"text": "// new first line\n"}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if got := a.vm().Diagnostics(); len(got) != 1 || got[0].Range.Start.Line != 5 || got[0].Range.Start.Col != 0 {
		t.Errorf("the squiggle did not move with the text: %+v", got)
	}
}

func TestSquigglesGoWithTheEditorAndComeBackWithTheNextOne(t *testing.T) {
	a := diagnosticsApp(t)
	path := a.files["a.go"]
	a.win.Markers.Set("lsp", path, []problems.Marker{diagMarker(problems.SeverityError, "kept in the service", 3, 1, 4)})
	a.pump()
	if len(a.vm().Diagnostics()) != 1 {
		t.Fatal("no squiggle")
	}
	a.run("workbench.action.closeActiveEditor")
	if n := len(a.win.Markers.Read(problems.ReadFilter{Resource: path})); n != 1 {
		t.Errorf("closing the editor took the markers away (%d left): they belong to the producer", n)
	}
	a.show("a.go", false)
	a.pump()
	if got := a.vm().Diagnostics(); len(got) != 1 || got[0].Message != "kept in the service" {
		t.Errorf("a new editor of the file does not show the markers: %+v", got)
	}
}

func TestF8GoesThroughTheProblemsOfTheFilesInOrder(t *testing.T) {
	a := diagnosticsApp(t)
	a.win.Markers.Set("lsp", a.files["a.go"], []problems.Marker{
		diagMarker(problems.SeverityWarning, "a warning", 1, 1, 8),
		diagMarker(problems.SeverityError, "a error", 5, 1, 5),
	})
	a.win.Markers.Set("lsp", a.files["b.go"], []problems.Marker{
		diagMarker(problems.SeverityError, "b error", 3, 5, 6),
		diagMarker(problems.SeverityInfo, "b info", 4, 1, 4),
	})
	a.pump()
	// where the cursor is: the start of a.go. F8 goes to the first problem at or after it: the warning of line 1
	step := func(keys, wantFile string, wantLine int, wantMessage string) {
		t.Helper()
		a.press(keys)
		a.waitFor("the problem "+wantMessage, func() bool {
			return a.active() == wantFile && a.vm().Hover() != nil && a.vm().Hover().Parts[0].Diagnostic.Message == wantMessage
		})
		if got := a.line() + 1; got != wantLine {
			t.Errorf("%s: the cursor is on line %d, want %d", keys, got, wantLine)
		}
		if !a.vm().Hover().Pinned {
			t.Errorf("%s: the hover of the problem is not kept", keys)
		}
	}
	// by file, errors before warnings before infos in each, so a's error comes before a's warning
	step("f8", "a.go", 5, "a error")
	step("f8", "a.go", 1, "a warning")
	step("f8", "b.go", 3, "b error")
	step("f8", "b.go", 4, "b info")
	step("f8", "a.go", 5, "a error") // the end wraps to the start
	step("shift+f8", "b.go", 4, "b info")
	step("shift+f8", "b.go", 3, "b error")
	step("shift+f8", "a.go", 1, "a warning")

	// Alt+F8 stays in the file and wraps in it
	a.press("alt+f8")
	a.waitFor("the error of a.go", func() bool { return a.vm().Hover() != nil && a.vm().Hover().Parts[0].Diagnostic.Message == "a error" })
	a.press("alt+f8")
	a.waitFor("the warning of a.go again", func() bool { return a.vm().Hover() != nil && a.vm().Hover().Parts[0].Diagnostic.Message == "a warning" })
	if a.active() != "a.go" {
		t.Errorf("Alt+F8 left the file: %s", a.active())
	}
	// Escape closes the hover of the problem
	a.press("escape")
	if a.vm().Hover() != nil {
		t.Error("Escape did not close the hover")
	}
}

func TestF8WithNoProblemsDoesNothing(t *testing.T) {
	a := diagnosticsApp(t)
	before := a.vm().Cursors().Primary().Position()
	a.press("f8")
	if a.vm().Cursors().Primary().Position() != before || a.vm().Hover() != nil {
		t.Error("F8 with no problems moved or showed something")
	}
}

func TestViewProblemOfTheHoverShowsTheProblem(t *testing.T) {
	a := diagnosticsApp(t)
	a.win.Markers.Set("lsp", a.files["a.go"], []problems.Marker{diagMarker(problems.SeverityError, "an error", 5, 6, 7)})
	a.pump()
	a.vm().Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{})})
	if !a.vm().ShowHoverAt(buffer.Position{Line: 4, Col: 5}) {
		t.Fatal("no hover")
	}
	hover := a.vm().Hover()
	if len(hover.Parts[0].Actions) != 1 {
		t.Fatalf("actions = %+v", hover.Parts[0].Actions)
	}
	hover.Parts[0].Actions[0].Run()
	if got := a.vm().Cursors().Primary().Position(); got != (buffer.Position{Line: 4, Col: 5}) {
		t.Errorf("View Problem put the cursor at %v", got)
	}
}

func TestDiagnosticPath(t *testing.T) {
	a := diagnosticsApp(t)
	if got := diagnosticPath(a.doc("a.go")); got != filepath.Join(filepath.Dir(a.files["a.go"]), "a.go") {
		t.Errorf("path %q", got)
	}
}
