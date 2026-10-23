package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

var testWindowSize = ui.Size{Width: windowWidth, Height: windowHeight}

// newTestWindow makes a window over a process with isolated folders and the editor font of
// the tests.
func newTestWindow(t *testing.T, folder string) *Window {
	t.Helper()
	paths := testutil.Isolate(t)
	// The store of the window reads the settings.json that testutil.Settings writes.
	testutil.Settings(t, paths, map[string]any{"editor.fontSize": 14})
	win, err := NewWindow(context.Background(), NewProcess(ProcessOptions{Paths: paths}), folder)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	return win
}

func sampleFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(testutil.RepoRoot(t), "testdata", "proof", "3.1", "sample.go")
}

func TestHomeShowsTheIconAndTheName(t *testing.T) {
	win := newTestWindow(t, "")
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, nil, nil))

	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText("Emetos")); err != nil {
		t.Error(err)
	}
	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCode))); err != nil {
		t.Error(err)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
	if n := ts.ExitRequests(); n != 0 {
		t.Errorf("the app asked to exit %d times without being told to", n)
	}
}

func TestShellShowsTheFile(t *testing.T) {
	win := newTestWindow(t, "")
	doc, err := win.Documents.OpenFile(context.Background(), sampleFile(t), document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))

	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByType[*editor.Editor]()); err != nil {
		t.Fatal(err)
	}
	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
	if vm.Document() != doc {
		t.Error("the editor shows the document that was opened")
	}
	if vm.Paragraphs().Builds() == 0 {
		t.Error("the editor drew no text")
	}
	if err := gluttertest.FindsNothing(ts, gluttertest.ByText("Emetos")); err != nil {
		t.Error("the file replaces the home page:", err)
	}
	if opts := vm.Options(); opts.FontFamilies[0] != testutil.MonoFamily || opts.FontSize != 14 {
		t.Errorf("the editor reads its font from the settings: %+v", opts)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
	testutil.Proof(t, ts, "3.1-shell-editor.png")
}

func TestEditorFollowsTheSettings(t *testing.T) {
	win := newTestWindow(t, "")
	doc, err := win.Documents.OpenFile(context.Background(), sampleFile(t), document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()

	if err := win.Settings.Update(context.Background(), "editor.fontSize", 20.0, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	for range 200 {
		ts.Pump(10 * time.Millisecond)
		if vm.Options().FontSize == 20 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got := vm.Options().FontSize; got != 20 {
		t.Errorf("the editor did not follow editor.fontSize: %v", got)
	}
	if got := vm.Options().LineHeight; got != 27 {
		t.Errorf("line height = %v, want round(1.35 * 20) = 27", got)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

// pumpPage shows the widget that build makes inside the window and theme of the app.
func pumpPage(t *testing.T, win *Window, build func(bc widgets.BuildContext) widgets.Widget) *gluttertest.Tester {
	t.Helper()
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newAppWithPage(win, func() widgets.Widget { return widgets.NewBuilder(build) }))
	return ts
}

func TestWindowScope(t *testing.T) {
	win := newTestWindow(t, "")
	var got *Window
	ts := pumpPage(t, win, func(bc widgets.BuildContext) widgets.Widget {
		got = WindowOf(bc)
		return widgets.NewSizedBox(nil)
	})
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Fatalf("the framework reported errors: %v", errs)
	}
	if got != win {
		t.Error("WindowOf must return the window of the scope")
	}
}

func TestCancelingTheContextExits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	win := newTestWindow(t, "")
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(ctx, win, nil, nil))

	cancel()
	// The goroutine that watches the context hands the exit to the UI thread.
	for range 400 {
		ts.Pump(10 * time.Millisecond)
		if ts.ExitRequests() > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if n := ts.ExitRequests(); n != 1 {
		t.Errorf("the app asked to exit %d times, want 1", n)
	}
}

func TestRunWithACanceledContextOpensNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, Options{}); err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
}

// TestDevCommandsRunAfterTheFirstFrame shows the dev commands of the app running: a dev
// command registered with RegisterDev is run with its argument once the page is up.
func TestDevCommandsRunAfterTheFirstFrame(t *testing.T) {
	win := newTestWindow(t, "")
	var got []any
	if err := RegisterDev(win.Process.Commands, command.Meta{ID: "dev.record"}, func(_ command.Context, args any) error {
		got = append(got, args)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	steps, err := ParseDevCommands(`[["dev.record", "first"], ["sleep", "1ms"], ["dev.record", {"n": 2}], ["dev.missing"]]`)
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, nil, steps))

	for range 400 {
		ts.Pump(10 * time.Millisecond)
		if len(got) == 2 && len(win.Process.Notify.Items()) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(got) != 2 || got[0] != "first" {
		t.Fatalf("the commands ran with %v", got)
	}
	if m, _ := got[1].(map[string]any); m["n"] != 2.0 {
		t.Errorf("second argument = %v", got[1])
	}
	items := win.Process.Notify.Items()
	if len(items) != 1 || !strings.Contains(items[0].Message, "dev.missing") {
		t.Errorf("a failing step is reported as a notification, got %+v", items)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

func TestProcessHasTheServices(t *testing.T) {
	paths := testutil.Isolate(t)
	p := NewProcess(ProcessOptions{Paths: paths})
	if p.Settings == nil || p.Commands == nil || p.Keymap == nil || p.Notify == nil || p.Logger == nil || p.Logs != nil {
		t.Fatalf("services: %+v", p)
	}
	if _, ok := p.Settings.Lookup("editor.fontFamily"); !ok {
		t.Error("the registry has the settings of Emetos")
	}
	if _, ok := p.Commands.Lookup("cursorLeft"); !ok {
		t.Error("the registry has the commands of Emetos")
	}
	if len(p.Keymap.Defaults()) == 0 {
		t.Error("the default keymap is not empty")
	}
}

func TestWindowsOwnTheirDocuments(t *testing.T) {
	paths := testutil.Isolate(t)
	p := NewProcess(ProcessOptions{Paths: paths})
	ctx := context.Background()
	a, err := NewWindow(ctx, p, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewWindow(ctx, p, "")
	if err != nil {
		t.Fatal(err)
	}
	da, err := a.Documents.OpenFile(ctx, sampleFile(t), document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	again, _ := a.Documents.OpenFile(ctx, sampleFile(t), document.NewOptions())
	db, _ := b.Documents.OpenFile(ctx, sampleFile(t), document.NewOptions())
	if again != da || db == da {
		t.Error("a window opens a file once; two windows have two documents")
	}
	if a.Keys == b.Keys {
		t.Error("each window has its own context key scope")
	}
	if got, ok := a.Documents.Get(da.URI()); !ok || got != da || len(a.Documents.All()) != 1 {
		t.Error("Get and All")
	}
	if !a.Documents.Close(da.URI()) || a.Documents.Close(da.URI()) || len(a.Documents.All()) != 0 {
		t.Error("Close")
	}
}

func TestOpenFileErrors(t *testing.T) {
	win := newTestWindow(t, "")
	_, err := win.Documents.OpenFile(context.Background(), filepath.Join(t.TempDir(), "missing.txt"), document.NewOptions())
	if err == nil {
		t.Fatal("opening a missing file must fail")
	}
}

func TestWindowWithAFolderReadsItsSettings(t *testing.T) {
	folder := t.TempDir()
	if err := os.MkdirAll(filepath.Join(folder, ".emetos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, ".emetos", "settings.json"), []byte(`{ "editor.fontSize": 20 }`), 0o644); err != nil {
		t.Fatal(err)
	}
	win := newTestWindow(t, folder)
	if win.Folder != folder {
		t.Errorf("Folder = %q", win.Folder)
	}
	size := editor.OptionsFromSettings(win.Settings, "", "", 4).FontSize
	if size != 20 {
		t.Errorf("the workspace layer is not read: font size %v, want 20", size)
	}
}

func TestEditorFollowsTheDisplaySettingsAndTheZoom(t *testing.T) {
	win := newTestWindow(t, "")
	doc, err := win.Documents.OpenFile(context.Background(), sampleFile(t), document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()

	for key, value := range map[string]any{
		"editor.fontWeight": "bold", "editor.letterSpacing": 1.5, "editor.cursorStyle": "block",
		"editor.cursorBlinking": "smooth", "editor.mouseWheelZoom": true, "editor.fontSize": 20.0,
	} {
		if err := win.Settings.Update(context.Background(), key, value, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
	}
	for range 200 {
		ts.Pump(10 * time.Millisecond)
		if vm.Options().CursorStyle == "block" && vm.Options().FontSize == 20 && vm.Options().MouseWheelZoom {
			break
		}
		time.Sleep(time.Millisecond)
	}
	o := vm.Options()
	if o.FontWeight != 700 || o.LetterSpacing != 1.5 || o.CursorStyle != "block" || o.CursorBlinking != "smooth" || !o.MouseWheelZoom || o.FontSize != 20 {
		t.Fatalf("the editor did not follow the settings: %+v", o)
	}

	// The zoom of the window is on top of the settings, and a command of the window's registry
	// zooms through the editor that has the focus.
	win.Zoom.Set(2)
	if got := vm.Options(); got.FontSize != 24 || got.LineHeight != 27*1.2 {
		t.Errorf("zoom 2 on a font of 20: font %v line %v", got.FontSize, got.LineHeight)
	}
	if err := win.Process.Commands.Run(command.Context{}, "editor.action.fontZoomReset", nil); err != nil {
		t.Fatal(err)
	}
	if win.Zoom.Level() != 0 || vm.Options().FontSize != 20 {
		t.Errorf("reset: level %v font %v", win.Zoom.Level(), vm.Options().FontSize)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

func TestShellHighlightsTheFile(t *testing.T) {
	win := newTestWindow(t, "")
	doc, err := win.Documents.OpenFile(context.Background(), sampleFile(t), document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	if doc.LanguageID() != "go" {
		t.Fatalf("the language of sample.go is %q", doc.LanguageID())
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
	tok := vm.Tokenizer()
	if tok == nil || !tok.Active() {
		t.Fatal("the editor has a tokenizer with a grammar")
	}
	if tok.Line(0) == nil {
		t.Error("the first line is tokenized before it is drawn")
	}
	// The editor has the bracket pairs of the file too: the sample is Go, which has braces.
	ix := vm.Brackets()
	if ix == nil || !ix.Enabled() {
		t.Fatal("the editor has the bracket pairs of the document")
	}
	if pairs := ix.Pairs(buffer.Range{End: buffer.Position{Line: 1000}}); len(pairs) == 0 {
		t.Error("the sample has bracket pairs")
	}
	// And the indentation: the rules of the languages, and the levels that the guides are drawn from.
	if vm.Indenter() == nil || vm.GuideLevels() == nil {
		t.Fatal("the editor has the indentation rules and the levels of the guides")
	}
	if !vm.Options().Guides.Indentation || vm.Options().AutoIndent != "full" {
		t.Errorf("the settings of the indentation: %+v", vm.Options())
	}
	last := doc.Snapshot().LineCount() - 1
	nested := false
	for line := 0; line <= last && !nested; line++ {
		nested = vm.GuideLevels().Level(line) > 0
	}
	if !nested {
		t.Error("the sample has indented lines")
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

func TestPlainTextIsNotTokenized(t *testing.T) {
	win := newTestWindow(t, "")
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("just words\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := win.Documents.OpenFile(context.Background(), path, document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	if doc.LanguageID() != "plaintext" {
		t.Fatalf("language %q", doc.LanguageID())
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
	if vm.Tokenizer().Active() || vm.Tokenizer().Line(0) != nil {
		t.Error("a language with no grammar has no tokens")
	}
	if n := len(win.Process.Notify.Items()); n != 0 {
		t.Errorf("plain text is no problem: %+v", win.Process.Notify.Items())
	}
}

func TestFileAssociationsChooseTheLanguage(t *testing.T) {
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"files.associations": map[string]any{"*.sample": "go", "*.json": "jsonc"}})
	win, err := NewWindow(context.Background(), NewProcess(ProcessOptions{Paths: paths}), "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, want := range map[string]string{"a.sample": "go", "b.json": "jsonc", "c.py": "python", "#!": "plaintext"} {
		path := filepath.Join(dir, strings.ReplaceAll(name, "#!", "script"))
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		doc, err := win.Documents.OpenFile(context.Background(), path, document.NewOptions())
		if err != nil {
			t.Fatal(err)
		}
		if doc.LanguageID() != want {
			t.Errorf("%s: language %q, want %q", name, doc.LanguageID(), want)
		}
	}
	// The first line decides when the name does not.
	path := filepath.Join(dir, "run")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env python3\nprint(1)\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc, err := win.Documents.OpenFile(context.Background(), path, document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	if doc.LanguageID() != "python" {
		t.Errorf("a python shebang: %q", doc.LanguageID())
	}
}

func TestProcessGrammars(t *testing.T) {
	p := NewProcess(ProcessOptions{Paths: testutil.Isolate(t)})
	g1, err := p.Grammar("go", 0)
	if err != nil || g1 == nil {
		t.Fatalf("go: %v %v", g1, err)
	}
	if g2, _ := p.Grammar("go", 0); g2 != g1 {
		t.Error("a grammar is made once and shared")
	}
	if g3, _ := p.Grammar("go", 5000); g3 == g1 {
		t.Error("a grammar with another line limit is another grammar")
	}
	if g, err := p.Grammar("plaintext", 0); g != nil || err != nil {
		t.Errorf("plain text has no grammar and no error: %v %v", g, err)
	}
}
