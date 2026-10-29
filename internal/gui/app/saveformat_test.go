package app

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// The save participants of the language features (step 12.8): the code actions of editor.codeActionsOnSave, then the formatter,
// before the file is written.

// saveFeatures is the language server of the tests: it organizes the imports (an action that turns "imports" into "organized
// imports") and formats (it puts "!" at the end of the text), and it records what it saw and in which order.
type saveFeatures struct {
	mu      sync.Mutex
	order   []string
	texts   []string
	ranges  []langfeatures.Range
	gate    chan struct{}
	slowFor time.Duration
	kinds   []string
}

func (f *saveFeatures) note(what, text string) {
	f.mu.Lock()
	f.order = append(f.order, what)
	f.texts = append(f.texts, text)
	f.mu.Unlock()
}

func (f *saveFeatures) wait(ctx context.Context) error {
	f.mu.Lock()
	gate, slow := f.gate, f.slowFor
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if slow > 0 {
		select {
		case <-time.After(slow):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (f *saveFeatures) ProvideCodeActions(ctx context.Context, doc langfeatures.Document, req langfeatures.CodeActionRequest) ([]langfeatures.CodeAction, error) {
	f.mu.Lock()
	f.kinds = append(f.kinds, strings.Join(req.Only, ","))
	f.mu.Unlock()
	f.note("code actions", doc.Text.String())
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	if !strings.Contains(doc.Text.String(), "imports") {
		return nil, nil
	}
	v := doc.Version
	line, _ := doc.Text.LineContent(0)
	col := strings.Index(line, "imports")
	edit := &langfeatures.WorkspaceEdit{Changes: []langfeatures.Change{{
		URI: doc.URI, Version: &v,
		Edits: []langfeatures.TextEdit{{Range: langfeatures.Range{Start: langfeatures.Position{Col: col}, End: langfeatures.Position{Col: col + len("imports")}}, Text: "organized imports"}},
	}}}
	return []langfeatures.CodeAction{{Title: "Organize Imports", Kind: langfeatures.KindSourceOrganize, Edit: edit}}, nil
}

func (f *saveFeatures) ProvideFormatting(ctx context.Context, doc langfeatures.Document, _ langfeatures.FormattingOptions) ([]langfeatures.TextEdit, error) {
	f.note("format", doc.Text.String())
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	return []langfeatures.TextEdit{{Range: langfeatures.FullRange(doc.Text), Text: doc.Text.String() + "!"}}, nil
}

func (f *saveFeatures) ProvideRangeFormatting(ctx context.Context, doc langfeatures.Document, r langfeatures.Range, _ langfeatures.FormattingOptions) ([]langfeatures.TextEdit, error) {
	f.mu.Lock()
	f.ranges = append(f.ranges, r)
	f.mu.Unlock()
	f.note("format range", doc.Text.String())
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	line, _ := doc.Text.LineContent(r.Start.Line)
	return []langfeatures.TextEdit{{Range: langfeatures.Range{Start: langfeatures.Position{Line: r.Start.Line}, End: langfeatures.Position{Line: r.Start.Line, Col: len(line)}}, Text: strings.ToUpper(line)}}, nil
}

func (f *saveFeatures) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.order...)
}

// saveFeaturesApp is a window with the fake server registered for Go, and a Go file open.
func saveFeaturesApp(t *testing.T, extra map[string]any, text string) (*saveApp, *saveFeatures) {
	t.Helper()
	values := map[string]any{"editor.formatOnSave": true, "editor.formatOnSaveTimeout": 400, "editor.codeActionsOnSaveTimeout": 400}
	for k, v := range extra {
		values[k] = v
	}
	// The [go] block of the Go pack gives Go files the defaults of VS Code's Go extension (SPEC §10.17: format and organize
	// imports on save), and a language block wins over plain settings, as it does in VS Code. These tests are about the save
	// participants and write the plain settings for the Go file they save, so the block says the same.
	// (the objects of the layers are merged, so the default action is switched off by name)
	block := map[string]any{"editor.formatOnSaveMode": "file", "editor.codeActionsOnSave": map[string]any{"source.organizeImports": "never"}}
	for _, key := range []string{"editor.formatOnSave", "editor.formatOnSaveMode", "editor.codeActionsOnSave"} {
		if v, ok := values[key]; ok {
			if m, isMap := v.(map[string]any); isMap {
				merged := map[string]any{"source.organizeImports": "never"}
				for k, x := range m {
					merged[k] = x
				}
				v = merged
			}
			block[key] = v
		}
	}
	values["[go]"] = block
	win := settingsWindow(t, values)
	t.Cleanup(win.release)
	a := newSaveAppWith(t, win, []string{"a.go"}, map[string]string{"a.go": text}, "a.go")
	scope := win.Extensions()
	if scope == nil {
		t.Fatal("the window has no scope of extensions")
	}
	f := &saveFeatures{}
	sel := langfeatures.Selector{Languages: []string{"go"}}
	reg := scope.Features()
	t.Cleanup(reg.RegisterCodeActions("fake", sel, f))
	t.Cleanup(reg.RegisterFormatting("fake", sel, f))
	return a, f
}

func TestOrganizeImportsRunsBeforeFormattingOnSaveAsOneUndoStep(t *testing.T) {
	a, f := saveFeaturesApp(t, map[string]any{
		"editor.codeActionsOnSave":     map[string]any{"source.organizeImports": "explicit"},
		"files.trimTrailingWhitespace": true,
	}, "imports  \nbody\n")
	a.typeText("x")
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.go").Dirty() })
	if got := f.seen(); strings.Join(got, ",") != "code actions,format" {
		t.Errorf("order %v, want the code actions and then the formatter", got)
	}
	// the formatter got the text the code action left (and the trim before it)
	f.mu.Lock()
	formatSaw := f.texts[1]
	f.mu.Unlock()
	if formatSaw != "xorganized imports\nbody\n" {
		t.Errorf("the formatter saw %q", formatSaw)
	}
	got := readFile(t, a.files["a.go"])
	if !strings.HasPrefix(got, "xorganized imports") || !strings.HasSuffix(got, "!") || strings.Contains(got, "  \n") {
		t.Errorf("file %q", got)
	}
	// the trim, the code action and the formatter are one undo step: one undo gives the text that was typed
	a.press("ctrl+z")
	if got := a.doc("a.go").Text(); got != "ximports  \nbody\n" {
		t.Errorf("after one undo %q", got)
	}
}

// TestTheGoLanguageDefaultsOrganizeImportsAndFormatOnSave saves a Go file in a window whose user settings say nothing about saving:
// the defaults of the [go] block of the Go extension (SPEC §10.17: formatting and organize imports on save) make the save ask for
// the code action and then the formatter, as one undo step, and leave a file of another language alone.
func TestTheGoLanguageDefaultsOrganizeImportsAndFormatOnSave(t *testing.T) {
	win := settingsWindow(t, map[string]any{"editor.formatOnSaveTimeout": 400, "editor.codeActionsOnSaveTimeout": 400})
	t.Cleanup(win.release)
	a := newSaveAppWith(t, win, []string{"a.go", "b.md"}, map[string]string{"a.go": "imports  \nbody\n", "b.md": "text\n"}, "a.go")
	scope := win.Extensions()
	if scope == nil {
		t.Fatal("the window has no scope of extensions")
	}
	f := &saveFeatures{}
	reg := scope.Features()
	for _, lang := range []string{"go", "markdown"} {
		sel := langfeatures.Selector{Languages: []string{lang}}
		t.Cleanup(reg.RegisterCodeActions("fake", sel, f))
		t.Cleanup(reg.RegisterFormatting("fake", sel, f))
	}
	// the defaults themselves: Go formats and organizes imports, Markdown does not
	if !settings.Get[bool](win.Settings, "editor.formatOnSave", "go") || settings.Get[string](win.Settings, "editor.formatOnSaveMode", "go") != "file" {
		t.Error("the defaults of the Go language block did not reach the settings of the window")
	}
	if got := settings.Get[map[string]any](win.Settings, "editor.codeActionsOnSave", "go"); got["source.organizeImports"] != "explicit" {
		t.Errorf("codeActionsOnSave for Go = %v", got)
	}
	if settings.Get[bool](win.Settings, "editor.formatOnSave", "markdown") || len(settings.Get[map[string]any](win.Settings, "editor.codeActionsOnSave", "markdown")) != 0 {
		t.Error("the Go defaults reached Markdown")
	}
	a.typeText("x")
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.go").Dirty() })
	if got := f.seen(); strings.Join(got, ",") != "code actions,format" {
		t.Errorf("order %v, want the code actions and then the formatter", got)
	}
	if got := readFile(t, a.files["a.go"]); got != "xorganized imports  \nbody\n!" {
		t.Errorf("file %q", got)
	}
	a.press("ctrl+z")
	if got := a.doc("a.go").Text(); got != "ximports  \nbody\n" {
		t.Errorf("after one undo %q", got)
	}
}

func TestFormatOnSaveWaitsNoLongerThanItsTimeout(t *testing.T) {
	a, f := saveFeaturesApp(t, map[string]any{"editor.formatOnSaveTimeout": 80}, "one\n")
	f.slowFor = 10 * time.Second
	a.typeText("x")
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.go").Dirty() })
	if got := readFile(t, a.files["a.go"]); got != "xone\n" {
		t.Errorf("file %q, want the text as it was (the formatter was too slow)", got)
	}
	var told bool
	for _, n := range a.notes() {
		if strings.Contains(n.Message, "Aborted format on save after 80ms") {
			told = true
		}
	}
	if !told {
		t.Errorf("notifications %v, want the abort", a.notes())
	}
}

func TestCodeActionsOnSaveTimeout(t *testing.T) {
	a, f := saveFeaturesApp(t, map[string]any{
		"editor.codeActionsOnSave": map[string]any{"source.organizeImports": "explicit"}, "editor.codeActionsOnSaveTimeout": 80,
		"editor.formatOnSave": false,
	}, "imports\n")
	f.slowFor = 10 * time.Second
	a.typeText("x")
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.go").Dirty() })
	if got := readFile(t, a.files["a.go"]); got != "ximports\n" {
		t.Errorf("file %q", got)
	}
	var told bool
	for _, n := range a.notes() {
		if strings.Contains(n.Message, "Aborted code actions on save after 80ms") {
			told = true
		}
	}
	if !told {
		t.Errorf("notifications %v, want the abort", a.notes())
	}
}

func TestAutoSaveRunsNoParticipantOfTheLanguageFeatures(t *testing.T) {
	// the light bulb asks the server for the actions at the cursor after typing, on its own clock: off, so that any request is the save's
	a, f := saveFeaturesApp(t, map[string]any{"editor.codeActionsOnSave": map[string]any{"source.organizeImports": "always"}, "editor.lightbulb.enabled": "off"}, "imports\n")
	a.typeText("x")
	done := false
	a.shell().saveDocument(a.doc("a.go"), saveRequest{reason: document.SaveAuto}, func(bool) { done = true })
	a.waitFor("the save", func() bool { return done })
	if got := f.seen(); len(got) != 0 {
		t.Errorf("an auto save asked the server: %v", got)
	}
	if got := readFile(t, a.files["a.go"]); got != "ximports\n" {
		t.Errorf("file %q", got)
	}
	// a save for a change of focus runs the actions marked always, and the formatter
	done = false
	a.typeText("y")
	a.shell().saveDocument(a.doc("a.go"), saveRequest{reason: document.SaveFocusChange}, func(bool) { done = true })
	a.waitFor("the save", func() bool { return done })
	if got := strings.Join(f.seen(), ","); got != "code actions,format" {
		t.Errorf("order %q", got)
	}
}

func TestCodeActionsOnSaveSettingForms(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   any
		reason  document.SaveReason
		kinds   []string
		exclude []string
	}{
		{"explicit on an explicit save", map[string]any{"source.organizeImports": "explicit"}, document.SaveExplicit, []string{"source.organizeImports"}, nil},
		{"explicit on a focus change", map[string]any{"source.organizeImports": "explicit"}, document.SaveFocusChange, nil, nil},
		{"always on a focus change", map[string]any{"source.organizeImports": "always"}, document.SaveFocusChange, []string{"source.organizeImports"}, nil},
		{"nothing on auto", map[string]any{"source.organizeImports": "always"}, document.SaveAuto, nil, nil},
		{"true is explicit", map[string]any{"source.fixAll": true}, document.SaveExplicit, []string{"source.fixAll"}, nil},
		{"never excludes", map[string]any{"source.fixAll": "explicit", "source.fixAll.eslint": "never"}, document.SaveExplicit, []string{"source.fixAll"}, []string{"source.fixAll.eslint"}},
		{"fix all first", map[string]any{"source.organizeImports": "explicit", "source.fixAll": "explicit"}, document.SaveExplicit, []string{"source.fixAll", "source.organizeImports"}, nil},
		{"a kind below another one is left out", map[string]any{"source": "explicit", "source.organizeImports": "explicit"}, document.SaveExplicit, []string{"source"}, nil},
		{"the array form runs on explicit saves", []any{"source.organizeImports"}, document.SaveExplicit, []string{"source.organizeImports"}, nil},
		{"the array form does not run on focus changes", []any{"source.organizeImports"}, document.SaveFocusChange, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kinds, excludes := codeActionsOnSaveKinds(tc.value, tc.reason)
			if strings.Join(kinds, ",") != strings.Join(tc.kinds, ",") || strings.Join(excludes, ",") != strings.Join(tc.exclude, ",") {
				t.Errorf("kinds %v excludes %v, want %v and %v", kinds, excludes, tc.kinds, tc.exclude)
			}
		})
	}
}

func TestSaveWithoutFormattingSkipsTheLanguageFeatures(t *testing.T) {
	// the light bulb asks the server for the actions at the cursor after typing, on its own clock: off, so that any request is the save's
	a, f := saveFeaturesApp(t, map[string]any{"editor.codeActionsOnSave": map[string]any{"source.organizeImports": "explicit"}, "editor.lightbulb.enabled": "off"}, "imports\n")
	a.typeText("x")
	a.press("ctrl+k s")
	a.waitFor("the save", func() bool { return !a.doc("a.go").Dirty() })
	if got := f.seen(); len(got) != 0 {
		t.Errorf("Save without Formatting asked the server: %v", got)
	}
	if got := readFile(t, a.files["a.go"]); got != "ximports\n" {
		t.Errorf("file %q", got)
	}
}

func TestSaveWithoutParticipantsAsksNoServer(t *testing.T) {
	// the light bulb asks the server for the actions at the cursor after typing, on its own clock: off, so that any request is the save's
	a, f := saveFeaturesApp(t, map[string]any{"editor.formatOnSave": false, "editor.lightbulb.enabled": "off"}, "one\n")
	a.typeText("x")
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.go").Dirty() })
	if got := f.seen(); len(got) != 0 {
		t.Errorf("the server was asked: %v", got)
	}
}

func TestAnAnswerForTextThatChangedMeanwhileIsDropped(t *testing.T) {
	a, f := saveFeaturesApp(t, nil, "one\n")
	f.gate = make(chan struct{})
	a.typeText("x")
	done := false
	a.press("ctrl+s")
	a.shell().saveDocument(a.doc("a.go"), saveRequest{}, func(bool) { done = true }) // a second request waits for the first
	a.waitFor("the formatter to be asked", func() bool { return len(f.seen()) > 0 })
	a.typeText("y") // the text changes while the formatter works
	close(f.gate)
	a.waitFor("the save", func() bool { return !a.doc("a.go").Dirty() || done })
	a.waitFor("the save of all", func() bool { return !a.doc("a.go").Dirty() })
	if got := readFile(t, a.files["a.go"]); strings.HasSuffix(got, "!") {
		t.Errorf("file %q: the formatting of the old text was made", got)
	}
}

func TestFormatOnSaveOfTheModificationsFormatsTheChangedLines(t *testing.T) {
	a, f := saveFeaturesApp(t, map[string]any{"editor.formatOnSaveMode": "modifications"}, "one\ntwo\nthree\nfour\n")
	// two lines are changed: the second and the last
	if _, err := a.doc("a.go").Edit([]document.Edit{
		{Range: buffer.Range{Start: buffer.Position{Line: 1, Col: 3}, End: buffer.Position{Line: 1, Col: 3}}, Text: "!"},
		{Range: buffer.Range{Start: buffer.Position{Line: 3, Col: 0}, End: buffer.Position{Line: 3, Col: 0}}, Text: ">"},
	}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.go").Dirty() })
	f.mu.Lock()
	ranges := f.ranges
	f.mu.Unlock()
	if len(ranges) != 2 || ranges[0].Start.Line != 1 || ranges[1].Start.Line != 3 {
		t.Errorf("ranges %v, want the second and the fourth line", ranges)
	}
	if got := readFile(t, a.files["a.go"]); got != "one\nTWO!\nthree\n>FOUR\n" {
		t.Errorf("file %q", got)
	}
}

func TestFormatOnSaveOfAnUnchangedFileInModificationsModeFormatsNothing(t *testing.T) {
	a, f := saveFeaturesApp(t, map[string]any{"editor.formatOnSaveMode": "modifications"}, "one\n")
	a.press("ctrl+s")
	a.run("workbench.action.files.save")
	a.pump()
	if got := f.seen(); len(got) != 0 {
		t.Errorf("the server was asked: %v", got)
	}
}

func TestTheWindowAppliesTheEditOfAServerToOpenAndClosedFilesAndKeepsTheCursor(t *testing.T) {
	a, _ := saveFeaturesApp(t, nil, "one two\n")
	a.vm().Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 0, Col: 6})})
	h := a.win.suggestHost()
	if h.ApplyEdit == nil || h.KeyLabel == nil {
		t.Fatal("the host has no means to apply an edit")
	}
	closed := a.files["a.go"] + ".other"
	if err := writeTestFile(closed, "closed\n"); err != nil {
		t.Fatal(err)
	}
	we := &langfeatures.WorkspaceEdit{Changes: []langfeatures.Change{
		{URI: document.FileURI(a.files["a.go"]), Edits: []langfeatures.TextEdit{{Range: langfeatures.Range{End: langfeatures.Position{Col: 3}}, Text: "ONE!!!"}}},
		{URI: document.FileURI(closed), Edits: []langfeatures.TextEdit{{Range: langfeatures.Range{End: langfeatures.Position{Col: 6}}, Text: "opened"}}},
	}}
	if err := h.ApplyEdit(context.Background(), we); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if got := a.doc("a.go").Text(); got != "ONE!!! two\n" {
		t.Errorf("text %q", got)
	}
	if p := a.vm().Cursors().Primary().Position(); p != (buffer.Position{Line: 0, Col: 9}) {
		t.Errorf("the cursor is at %v, want it to follow the text to column 9", p)
	}
	other, ok := a.win.Documents.Get(document.FileURI(closed))
	if !ok || other.Text() != "opened\n" || !other.Dirty() {
		t.Errorf("the closed file is open as an unsaved document with the edit: %v %v", ok, other)
	}
	if got := readFile(t, closed); got != "closed\n" {
		t.Errorf("the closed file was written: %q", got)
	}
}

func writeTestFile(path, text string) error { return os.WriteFile(path, []byte(text), 0o644) }
