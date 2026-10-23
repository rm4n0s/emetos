package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// twoFiles makes a folder with two small files and opens the first in a window.
func twoFiles(t *testing.T) (win *Window, doc *document.Document, first, second string) {
	t.Helper()
	win = newTestWindow(t, "")
	dir := t.TempDir()
	first, second = filepath.Join(dir, "first.go"), filepath.Join(dir, "second.txt")
	for _, p := range []string{first, second} {
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	doc, err := win.Documents.OpenFile(context.Background(), first, document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	return win, doc, first, second
}

func TestShellShowsTheBreadcrumbsAndOpensASibling(t *testing.T) {
	win, doc, first, second := twoFiles(t)
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByType[*editor.Breadcrumbs]()); err != nil {
		t.Fatal(err)
	}
	if h := ts.Size(gluttertest.ByType[*editor.Breadcrumbs]()).Height; h != editor.BreadcrumbsHeight {
		t.Errorf("the bar is %v high", h)
	}
	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText("first.go")); err != nil {
		t.Fatal("the name of the file is on the bar:", err)
	}

	ts.Tap(gluttertest.ByText("first.go"))
	waitFor(t, ts, "the picker", func() bool { return ts.Exists(gluttertest.ByText("second.txt")) })
	ts.Tap(gluttertest.ByText("second.txt"))
	waitFor(t, ts, "the second file", func() bool {
		return ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel().Document().URI() == document.FileURI(second)
	})
	waitFor(t, ts, "the new bar", func() bool {
		return ts.Exists(gluttertest.ByText("second.txt")) && !ts.Exists(gluttertest.ByText("first.go"))
	})
	if doc.URI() != document.FileURI(first) {
		t.Errorf("the first document changed: %v", doc.URI())
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

func TestBreadcrumbsFollowTheSettings(t *testing.T) {
	win, doc, _, _ := twoFiles(t)
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	if err := win.Settings.Update(context.Background(), "breadcrumbs.enabled", false, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ts, "the bar to go", func() bool { return ts.Size(gluttertest.ByType[*editor.Breadcrumbs]()).Height == 0 })
	if err := win.Settings.Update(context.Background(), "breadcrumbs.enabled", true, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ts, "the bar to come back", func() bool {
		return ts.Size(gluttertest.ByType[*editor.Breadcrumbs]()).Height == editor.BreadcrumbsHeight
	})
	if err := win.Settings.Update(context.Background(), "breadcrumbs.filePath", "last", settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Base(filepath.Dir(doc.URI().Path))
	waitFor(t, ts, "only the file", func() bool { return !ts.Exists(gluttertest.ByText(dir)) && ts.Exists(gluttertest.ByText("first.go")) })
}

func TestAFileThatCannotBeOpenedIsReported(t *testing.T) {
	win, doc, first, second := twoFiles(t)
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	ts.Tap(gluttertest.ByText("first.go"))
	waitFor(t, ts, "the picker", func() bool { return ts.Exists(gluttertest.ByText("second.txt")) })
	// the file goes away between the listing and the click
	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	ts.Tap(gluttertest.ByText("second.txt"))
	waitFor(t, ts, "the error", func() bool { return len(win.Process.Notify.Items()) > 0 })
	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
	if vm.Document().URI() != document.FileURI(first) {
		t.Errorf("the editor changed to %v", vm.Document().URI())
	}
}
