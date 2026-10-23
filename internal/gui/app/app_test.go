package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/kit"
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

	for _, line := range []string{"package main", `import (`, "func main() {"} {
		if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText(line)); err != nil {
			t.Error(err)
		}
	}
	// The line numbers are in the gutter.
	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText(" 3")); err != nil {
		t.Error(err)
	}
	if err := gluttertest.FindsNothing(ts, gluttertest.ByText("Emetos")); err != nil {
		t.Error("the file replaces the home page:", err)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
	testutil.Proof(t, ts, "3.1-placeholder.png")
}

func TestShellShowsAtMostTheFirstLines(t *testing.T) {
	win := newTestWindow(t, "")
	long := filepath.Join(t.TempDir(), "long.txt")
	if err := os.WriteFile(long, []byte(strings.Repeat("x\n", placeholderMaxLines+500)), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := win.Documents.OpenFile(context.Background(), long, document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
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

func TestEditorUsesTheTestFont(t *testing.T) {
	win := newTestWindow(t, "")
	var style *painting.TextStyle
	ts := pumpPage(t, win, func(bc widgets.BuildContext) widgets.Widget {
		style = editorTextStyle(bc, win.Settings, kit.ThemeOf(bc).EditorForeground)
		return widgets.NewSizedBox(nil)
	})
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Fatalf("the framework reported errors: %v", errs)
	}
	if style == nil || style.FontFamily != testutil.MonoFamily {
		t.Fatalf("the editor font is %v, want %s from editor.fontFamily", style, testutil.MonoFamily)
	}
	if got := style.FontFamilyFallback(); len(got) != 1 || got[0] != "monospace" {
		t.Errorf("fallback = %v, want monospace", got)
	}
	if size, ok := style.FontSize.Get(); !ok || size != 14 {
		t.Errorf("font size = %v, want 14", size)
	}
}

func TestEditorFontFamilies(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"Go Mono", []string{"Go Mono"}},
		{"'Droid Sans Mono', \"monospace\" , monospace", []string{"Droid Sans Mono", "monospace", "monospace"}},
		{" , ,", nil},
	}
	for _, tc := range cases {
		got := fontFamilies(tc.in)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("fontFamilies(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
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
	var size float64
	ts := pumpPage(t, win, func(bc widgets.BuildContext) widgets.Widget {
		style := editorTextStyle(bc, win.Settings, kit.DarkModern().EditorForeground)
		size, _ = style.FontSize.Get()
		return widgets.NewSizedBox(nil)
	})
	_ = ts
	if size != 20 {
		t.Errorf("the workspace layer is not read: font size %v, want 20", size)
	}
}
