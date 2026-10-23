package editor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The benchmarks of SPEC §14. They are run on a release build,
//
//	go test -tags glutter_release ./internal/gui/editor -run xxx -bench . -benchmem
//
// since the debug build of Glutter checks the thread of every call and is many times slower
// (docs/PERFORMANCE.md). Each measures the UI thread: the frame that shows the change, with the
// fake platform (no renderer), which is what the targets are about.

// goLines makes a text of n lines that look like Go code: indented statements of about 50
// columns.
func goLines(n int) string {
	var b strings.Builder
	b.Grow(n * 52)
	for i := range n {
		switch i % 4 {
		case 0:
			fmt.Fprintf(&b, "func handler%d(ctx context.Context, request *Request) error {\n", i)
		case 1:
			fmt.Fprintf(&b, "\tresult, err := service.Process(ctx, request.Items[%d])\n", i%64)
		case 2:
			b.WriteString("\tif err != nil { return fmt.Errorf(\"processing: %w\", err) }\n")
		default:
			b.WriteString("}\n")
		}
	}
	return b.String()
}

// benchEditor shows an editor over a text in a window the size of a laptop screen.
func benchEditor(b *testing.B, text string) (*ViewModel, *gluttertest.Tester) {
	b.Helper()
	opts := goOptions()
	vm := NewViewModel(document.New(document.FileURI("/bench.go"), text, document.NewOptions()), opts, nil)
	ts := testutil.New(b, ui.Size{Width: 1280, Height: 800})
	pumpEditorWith(b, ts, vm, EditorOpts{Autofocus: true})
	ts.Pump(time.Millisecond)
	return vm, ts
}

// benchHighlighted is benchEditor with the tokens and the colors of Dark Modern, as the app has
// them: the visible lines are tokenized before they are drawn.
func benchHighlighted(b *testing.B, text string) (*ViewModel, *gluttertest.Tester) {
	b.Helper()
	vm, _ := highlighted(b, text)
	ts := testutil.New(b, ui.Size{Width: 1280, Height: 800})
	pumpHighlighted(b, ts, vm, kit.DarkModern(), darkModernTokens(b))
	ts.Pump(time.Millisecond)
	return vm, ts
}

// BenchmarkTypeCharacter10kLinesHighlighted is BenchmarkTypeCharacter10kLines with syntax
// highlighting: the typed character changes the tokens of its line, which is tokenized again
// and drawn in colors (SPEC §14: under 4 ms).
func BenchmarkTypeCharacter10kLinesHighlighted(b *testing.B) {
	vm, ts := benchHighlighted(b, goLines(10_000))
	for range 51 {
		if err := vm.RunCommand("cursorDown", nil); err != nil {
			b.Fatal(err)
		}
	}
	ts.Pump(time.Millisecond)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := vm.RunCommand("type", "x"); err != nil {
			b.Fatal(err)
		}
		ts.Pump(time.Millisecond)
		if i%64 == 63 { // keep the line from growing without end
			b.StopTimer()
			_ = vm.RunCommand("deleteLeft", nil)
			for range 63 {
				_ = vm.RunCommand("deleteLeft", nil)
			}
			ts.Pump(time.Millisecond)
			b.StartTimer()
		}
	}
}

// BenchmarkScrollPage100kLinesHighlighted is BenchmarkScrollPage100kLines with highlighting: each
// page is tokenized on the UI thread before it is drawn, from the first line that is not done.
func BenchmarkScrollPage100kLinesHighlighted(b *testing.B) {
	vm, ts := benchHighlighted(b, goLines(100_000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := vm.RunCommand("cursorPageDown", nil); err != nil {
			b.Fatal(err)
		}
		ts.Pump(time.Millisecond)
		if i%1000 == 999 { // back to the top: the tokens are there
			b.StopTimer()
			_ = vm.RunCommand("cursorTop", nil)
			ts.Pump(time.Millisecond)
			b.StartTimer()
		}
	}
}

// BenchmarkTypeCharacter10kLines is the frame that shows a typed character in a file of 10,000
// lines: the command that types it, then the frame (SPEC §14: under 4 ms).
func BenchmarkTypeCharacter10kLines(b *testing.B) {
	vm, ts := benchEditor(b, goLines(10_000))
	if err := vm.RunCommand("cursorDown", nil); err != nil {
		b.Fatal(err)
	}
	for range 50 {
		_ = vm.RunCommand("cursorDown", nil)
	}
	ts.Pump(time.Millisecond)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := vm.RunCommand("type", "x"); err != nil {
			b.Fatal(err)
		}
		ts.Pump(time.Millisecond)
		if i%64 == 63 { // keep the line from growing without end
			b.StopTimer()
			_ = vm.RunCommand("deleteLeft", nil)
			for range 63 {
				_ = vm.RunCommand("deleteLeft", nil)
			}
			ts.Pump(time.Millisecond)
			b.StartTimer()
		}
	}
}

// BenchmarkScrollPage100kLines is the frame that shows a page scrolled in a file of 100,000 lines
// (SPEC §14: no UI-thread frame over 8 ms).
func BenchmarkScrollPage100kLines(b *testing.B) {
	vm, ts := benchEditor(b, goLines(100_000))
	_, height := vm.ViewportSize()
	if height == 0 {
		b.Fatal("the editor has no size")
	}
	page := height - 2*vm.Options().LineHeight
	extent := vm.Projection().Height() - height
	b.ReportAllocs()
	b.ResetTimer()
	top := 0.0
	for i := 0; i < b.N; i++ {
		top += page
		if top > extent {
			top = 0
		}
		vm.scroller(top)
		ts.Pump(time.Millisecond)
	}
}

// BenchmarkOpen10MB is opening a file of 10 MB: reading it, making the document and the view
// model, and the first frame with its text laid out (SPEC §14: under 300 ms).
func BenchmarkOpen10MB(b *testing.B) {
	text := goLines(200_000)
	for len(text) < 10<<20 {
		text += goLines(10_000)
	}
	path := filepath.Join(b.TempDir(), "big.go")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := fileio.Read(context.Background(), path, fileio.ReadOptions{})
		if err != nil {
			b.Fatal(err)
		}
		doc := document.FromRead(document.FileURI(path), res, document.NewOptions())
		vm := NewViewModel(doc, goOptions(), nil)
		ts := testutil.New(b, ui.Size{Width: 1280, Height: 800})
		pumpEditorWith(b, ts, vm, EditorOpts{Autofocus: true})
		ts.Pump(time.Millisecond)
		if vm.Projection().RowCount() < 100_000 {
			b.Fatalf("only %d rows", vm.Projection().RowCount())
		}
	}
}
