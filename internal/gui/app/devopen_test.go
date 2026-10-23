package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func TestParseOpenFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		args any
		want openFileArguments
	}{
		{"a path", "a.go", openFileArguments{Path: "a.go"}},
		{"an object", map[string]any{"path": "a.go", "line": 3.0, "column": 5.0}, openFileArguments{Path: "a.go", Line: 3, Column: 5}},
		{"a path only", map[string]any{"path": "a.go"}, openFileArguments{Path: "a.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOpenFile(tc.args)
			if err != nil || got != tc.want {
				t.Errorf("got %+v, %v, want %+v", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name string
		args any
	}{
		{"nothing", nil},
		{"no path", map[string]any{"line": 3.0}},
		{"an empty path", ""},
		{"a negative line", map[string]any{"path": "a.go", "line": -1.0}},
		{"a negative column", map[string]any{"path": "a.go", "column": -2.0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseOpenFile(tc.args)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("parseOpenFile.DevOpenFileInvalid") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

// waitFor pumps the tester until the condition holds.
func waitFor(t *testing.T, ts *gluttertest.Tester, what string, cond func() bool) {
	t.Helper()
	for range 600 {
		ts.Pump(10 * time.Millisecond)
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestDevOpenFileShowsTheFileAtAPosition(t *testing.T) {
	win := newTestWindow(t, "")
	args, err := json.Marshal([][]any{
		{"dev.openFile", map[string]any{"path": sampleFile(t), "line": 5, "column": 4}},
		{"dev.openFile", map[string]any{"path": "missing/file.go"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	steps, err := ParseDevCommands(string(args))
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, nil, steps))
	waitFor(t, ts, "the editor", func() bool { return gluttertest.FindsOneWidget(ts, gluttertest.ByType[*editor.Editor]()) == nil })
	waitFor(t, ts, "the error of the missing file", func() bool { return len(win.Process.Notify.Items()) > 0 })

	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
	if got := vm.Document().URI(); !strings.HasSuffix(got.Path, "testdata/proof/3.1/sample.go") {
		t.Errorf("the editor shows %s", got)
	}
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 4, Col: 3}) {
		t.Errorf("the cursor is at %v, want line 5 column 4", got)
	}
	if items := win.Process.Notify.Items(); !strings.Contains(items[0].Message, "dev.openFile") {
		t.Errorf("a file that cannot be opened is reported: %+v", items)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

func TestDevOpenFileAddsATab(t *testing.T) {
	win := newTestWindow(t, "")
	first, err := win.Documents.OpenFile(context.Background(), sampleFile(t), document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	other := strings.Replace(sampleFile(t), "3.1", "4.9", 1)
	args, err := json.Marshal([][]any{{"dev.openFile", other}})
	if err != nil {
		t.Fatal(err)
	}
	steps, err := ParseDevCommands(string(args))
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, first, steps))
	waitFor(t, ts, "the other file", func() bool {
		vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
		return vm.Document() != first
	})
	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
	if vm.Tokenizer() == nil || vm.Brackets() == nil || vm.FoldingModel() == nil {
		t.Errorf("the new editor has its tokens, its brackets and its folding")
	}
	// the first file keeps its tab and its editor, which is not shown
	if items := ts.Widget(gluttertest.ByType[*kit.Tabs]()).(*kit.Tabs).Items(); len(items) != 2 {
		t.Errorf("%d tabs, want 2", len(items))
	}
	if len(first.DecorationsOf(2)) == 0 {
		t.Error("the editor of the first file, which is not shown, keeps its folds")
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

func TestShellFoldsTheFile(t *testing.T) {
	win := newTestWindow(t, "")
	doc, err := win.Documents.OpenFile(context.Background(), sampleFile(t), document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
	if vm.FoldingModel() == nil {
		t.Fatal("the editor folds")
	}
	// the regions of the indentation are found in the background
	waitFor(t, ts, "the folding regions", func() bool { return vm.FoldingModel().Regions().Len() > 0 })
	rows := vm.Projection().RowCount()
	cursorAtEnd := buffer.Position{Line: doc.Snapshot().LineCount() - 1}
	vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(cursorAtEnd)})
	if err := vm.RunCommand("editor.foldAll", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Projection().RowCount(); got >= rows {
		t.Errorf("folding all left %d rows of %d", got, rows)
	}
	if err := vm.RunCommand("editor.unfoldAll", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Projection().RowCount(); got != rows {
		t.Errorf("unfolding all left %d rows of %d", got, rows)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}
