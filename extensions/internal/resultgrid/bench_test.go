package resultgrid

import (
	"io"
	"testing"
	"time"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// benchRows is the size of the results of the benchmarks and of the budget test: SPEC §10.21's largest result that the panel is
// asked to scroll without a pause.
const benchRows = 100_000

// BenchmarkScrollFrame is one frame of the grid while it scrolls through 100,000 rows: the wheel moves the rows by five lines and the
// frame is built, laid out and painted.
func BenchmarkScrollFrame(b *testing.B) {
	g := New(Options{})
	defer g.Dispose()
	g.SetSource(numbered(benchRows))
	ts := uitest.Pump(b, uitest.Themes()[0], gui.Size{Width: 900, Height: 420}, g.Widget())
	at := gui.Offset{Dx: 400, Dy: 200}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		d := 110.0
		if i%2000 > 1000 { // back up, so that a long run stays inside the result
			d = -110
		}
		ts.ScrollAt(at, gui.Offset{Dy: d})
		ts.Pump(16 * time.Millisecond)
	}
	b.ReportMetric(float64(g.RowsBuilt())/float64(b.N), "rows-built/op")
}

// BenchmarkFirstFrame is the first frame of a result of 100,000 rows: the source is set (the widths are guessed from the first 200
// rows) and the rows in view are built.
func BenchmarkFirstFrame(b *testing.B) {
	for b.Loop() {
		g := New(Options{})
		g.SetSource(numbered(benchRows))
		ts := uitest.Pump(b, uitest.Themes()[0], gui.Size{Width: 900, Height: 420}, g.Widget())
		ts.Pump(16 * time.Millisecond)
		g.Dispose()
	}
}

// BenchmarkSort sorts 100,000 rows by a column that is the text "row N" (a text compares slower than a number).
func BenchmarkSort(b *testing.B) {
	m := NewModel(ModelOptions{})
	m.SetSource(numbered(benchRows))
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		dir := SortAsc
		if i%2 == 1 {
			dir = SortDesc
		}
		m.SortBy(1, dir)
	}
}

// BenchmarkCopyAll copies the whole result as TSV.
func BenchmarkCopyAll(b *testing.B) {
	m := NewModel(ModelOptions{})
	m.SetSource(numbered(benchRows))
	b.ReportAllocs()
	for b.Loop() {
		if len(m.CopyAll(true)) == 0 {
			b.Fatal("nothing copied")
		}
	}
}

// BenchmarkExportCSV and BenchmarkExportJSON write the result to a writer that discards it.
func BenchmarkExportCSV(b *testing.B) {
	m := NewModel(ModelOptions{})
	m.SetSource(numbered(benchRows))
	b.ReportAllocs()
	for b.Loop() {
		if err := m.Export().WriteCSV(b.Context(), io.Discard, CSVOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExportJSON writes the result as JSON to a writer that discards it.
func BenchmarkExportJSON(b *testing.B) {
	m := NewModel(ModelOptions{})
	m.SetSource(numbered(benchRows))
	b.ReportAllocs()
	for b.Loop() {
		if err := m.Export().WriteJSON(b.Context(), io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
