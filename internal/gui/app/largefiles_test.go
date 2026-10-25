package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func writeBinaryFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(path, []byte("ab\x00\x01\x02\x00cd\x00\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestABinaryFileShowsANoticeAndOpensAnyway(t *testing.T) {
	win := newTestWorkspace(t, "")
	path := writeBinaryFile(t)
	_, err := win.Documents.OpenFile(context.Background(), path, document.NewOptions())
	if !isBinaryError(err) {
		t.Fatalf("expected the binary failure, got %v", err)
	}
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("FileBinary") {
		t.Fatalf("unexpected failure path: %v", err)
	}

	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newAppBlocked(context.Background(), win, nil, nil, path))
	if !ts.Exists(gluttertest.ByType[*editor.Notice]()) || !ts.Exists(gluttertest.ByText(BinaryFileNotice)) {
		t.Fatal("no notice for the binary file")
	}
	if ts.Exists(gluttertest.ByType[*editor.Editor]()) {
		t.Fatal("an editor shows a binary file")
	}
	ts.Tap(gluttertest.ByText("Open Anyway"))
	waitFor(t, ts, "the editor", func() bool { return ts.Exists(gluttertest.ByType[*editor.Editor]()) })
	if ts.Exists(gluttertest.ByType[*editor.Notice]()) {
		t.Error("the notice stays")
	}
	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
	if vm.Document().URI() != document.FileURI(path) {
		t.Errorf("the editor shows %v", vm.Document().URI())
	}
}

func TestOpenAnywayFailureIsReported(t *testing.T) {
	win := newTestWorkspace(t, "")
	path := writeBinaryFile(t)
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newAppBlocked(context.Background(), win, nil, nil, path))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	ts.Tap(gluttertest.ByText("Open Anyway"))
	waitFor(t, ts, "the error", func() bool { return len(win.Notify.Items()) > 0 })
	if !ts.Exists(gluttertest.ByType[*editor.Notice]()) {
		t.Error("the notice went away although the file did not open")
	}
}

func TestLargeFileNotificationAndSetting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "many.txt")
	// more lines than the limit of large file mode
	if err := os.WriteFile(path, []byte(strings.Repeat("x\n", fileio.DefaultLargeFileLines+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Run("large", func(t *testing.T) {
		win := newTestWorkspace(t, "")
		doc, err := win.Documents.OpenFile(context.Background(), path, document.NewOptions())
		if err != nil {
			t.Fatal(err)
		}
		if !doc.LargeFile() {
			t.Fatal("not in large file mode")
		}
		ts := testutil.New(t, testWindowSize)
		ts.PumpWidget(newApp(context.Background(), win, doc, nil))
		var told int
		for _, it := range win.Notify.Items() {
			if it.Message == LargeFileNotice {
				told++
			}
		}
		if told != 1 {
			t.Errorf("the large file notification was shown %d times", told)
		}
		if win.Documents.AnnounceLarge(doc) {
			t.Error("a document is announced once")
		}
	})
	t.Run("small", func(t *testing.T) {
		win := newTestWorkspace(t, "")
		doc := document.New(document.FileURI("/small.txt"), "x", document.NewOptions())
		if win.Documents.AnnounceLarge(doc) {
			t.Error("a small document is announced")
		}
	})
	t.Run("optimizations off", func(t *testing.T) {
		paths := testutil.Isolate(t)
		testutil.Settings(t, paths, map[string]any{"editor.largeFileOptimizations": false})
		win, err := NewWorkspace(context.Background(), newTestProcess(ProcessOptions{Paths: paths}), "")
		if err != nil {
			t.Fatal(err)
		}
		doc, err := win.Documents.OpenFile(context.Background(), path, document.NewOptions())
		if err != nil {
			t.Fatal(err)
		}
		if doc.LargeFile() {
			t.Error("large file mode with editor.largeFileOptimizations off")
		}
	})
}

func TestAFileOverTheLimitFailsWithTheTaggedError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 1000)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := fileio.Read(context.Background(), path, fileio.ReadOptions{Limits: fileio.Limits{MaxFileSize: 100}})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("FileTooLarge") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
