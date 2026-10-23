package editor

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

func TestLineHeight(t *testing.T) {
	cases := []struct {
		name          string
		setting, size float64
		want          float64
	}{
		{"default at 14", 0, 14, 19},        // round(1.35 × 14) = round(18.9)
		{"default at 12", 0, 12, 16},        // round(16.2)
		{"default at 16", 0, 16, 22},        // round(21.6)
		{"default at 20", 0, 20, 27},        // 27
		{"default at 6", 0, 6, 8},           // round(8.1)
		{"default at 5 is raised", 0, 5, 8}, // round(6.75) = 7, below the minimum
		{"multiplier 1.5", 1.5, 14, 21},
		{"multiplier 2", 2, 10, 20},
		{"multiplier 0.5 is raised", 0.5, 14, 8}, // 7 is below the minimum
		{"pixels", 24, 14, 24},
		{"exactly 8 is pixels", 8, 14, 8},
		{"just under 8 is a multiplier", 7.5, 2, 15},
		{"pixels are rounded", 20.4, 14, 20},
		{"a small size", 1, 4, 8},
	}
	for _, tc := range cases {
		if got := LineHeight(tc.setting, tc.size); got != tc.want {
			t.Errorf("%s: LineHeight(%v, %v) = %v, want %v", tc.name, tc.setting, tc.size, got, tc.want)
		}
	}
}

func TestIdentityProjection(t *testing.T) {
	p := newIdentityProjection(100, 19)
	if p.RowCount() != 100 || p.Height() != 1900 {
		t.Errorf("rows %d height %v", p.RowCount(), p.Height())
	}
	if first, count := p.LineRows(7); first != 7 || count != 1 {
		t.Errorf("LineRows(7) = %d, %d", first, count)
	}
	if _, count := p.LineRows(100); count != 0 {
		t.Error("a line past the end has no rows")
	}
	if _, count := p.LineRows(-1); count != 0 {
		t.Error("a negative line has no rows")
	}
	if line, start, end := p.RowSpan(42); line != 42 || start != 0 || end != -1 {
		t.Errorf("RowSpan(42) = %d, %d, %d", line, start, end)
	}
	if line, _, _ := p.RowSpan(500); line != 99 {
		t.Errorf("RowSpan clamps: %d", line)
	}
	if p.RowTop(3) != 57 || p.RowHeight(3) != 19 {
		t.Errorf("RowTop %v RowHeight %v", p.RowTop(3), p.RowHeight(3))
	}
	cases := []struct {
		y    float64
		want int
	}{{-5, 0}, {0, 0}, {18.9, 0}, {19, 1}, {1899, 99}, {5000, 99}}
	for _, tc := range cases {
		if got := p.RowAt(tc.y); got != tc.want {
			t.Errorf("RowAt(%v) = %d, want %d", tc.y, got, tc.want)
		}
	}
	if newIdentityProjection(0, 19).RowCount() != 1 {
		t.Error("a projection has at least one row")
	}
	if newIdentityProjection(5, 0).RowAt(10) != 0 {
		t.Error("rows of no height")
	}
}

func TestLineNumberText(t *testing.T) {
	cases := []struct {
		name        string
		mode        LineNumbersMode
		line, caret int
		count       int
		want        string
		current     bool
	}{
		{"on", LineNumbersOn, 0, 5, 100, "1", false},
		{"on, last", LineNumbersOn, 99, 5, 100, "100", false},
		{"off", LineNumbersOff, 3, 5, 100, "", false},
		{"relative, the cursor's line", LineNumbersRelative, 5, 5, 100, "6", true},
		{"relative above", LineNumbersRelative, 2, 5, 100, "3", false},
		{"relative below", LineNumbersRelative, 9, 5, 100, "4", false},
		{"interval, cursor", LineNumbersInterval, 5, 5, 100, "6", false},
		{"interval, tenth", LineNumbersInterval, 9, 5, 100, "10", false},
		{"interval, twentieth", LineNumbersInterval, 19, 5, 100, "20", false},
		{"interval, other", LineNumbersInterval, 7, 5, 100, "", false},
		{"interval, last", LineNumbersInterval, 94, 5, 95, "95", false},
	}
	for _, tc := range cases {
		got, current := LineNumberText(tc.mode, tc.line, tc.caret, tc.count)
		if got != tc.want || current != tc.current {
			t.Errorf("%s: got %q, %v; want %q, %v", tc.name, got, current, tc.want, tc.current)
		}
	}
}

func TestParseLineNumbersMode(t *testing.T) {
	cases := map[string]LineNumbersMode{
		"on": LineNumbersOn, "off": LineNumbersOff, "relative": LineNumbersRelative, "interval": LineNumbersInterval,
		"": LineNumbersOn, "nonsense": LineNumbersOn,
	}
	for in, want := range cases {
		if got := ParseLineNumbersMode(in); got != want {
			t.Errorf("ParseLineNumbersMode(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestComputeGutterWidths(t *testing.T) {
	base := GutterOptions{LineNumbers: LineNumbersOn, LineNumbersMinChars: 5, DigitWidth: 8.4, LineHeight: 19, GlyphMargin: true, LineDecorationsWidth: 10, Folding: true}
	cases := []struct {
		name   string
		mutate func(*GutterOptions)
		lines  int
		want   GutterWidths
	}{
		{"short file: the minimum of 5 characters", func(*GutterOptions) {}, 100, GutterWidths{19, 42, 26}},
		{"long file: the digits of the last line", func(*GutterOptions) {}, 1234567, GutterWidths{19, 59, 26}}, // 7 × 8.4 = 58.8
		{"exactly five digits", func(*GutterOptions) {}, 99999, GutterWidths{19, 42, 26}},
		{"six digits", func(*GutterOptions) {}, 100000, GutterWidths{19, 50, 26}}, // 50.4
		{"no line numbers", func(o *GutterOptions) { o.LineNumbers = LineNumbersOff }, 100, GutterWidths{19, 0, 26}},
		{"no glyph margin", func(o *GutterOptions) { o.GlyphMargin = false }, 100, GutterWidths{0, 42, 26}},
		{"no folding", func(o *GutterOptions) { o.Folding = false }, 100, GutterWidths{19, 42, 10}},
		{"min chars 1", func(o *GutterOptions) { o.LineNumbersMinChars = 1 }, 5, GutterWidths{19, 8, 26}},
		{"an empty document counts one line", func(o *GutterOptions) { o.LineNumbersMinChars = 1 }, 0, GutterWidths{19, 8, 26}},
		{"relative numbers take the same room", func(o *GutterOptions) { o.LineNumbers = LineNumbersRelative }, 100, GutterWidths{19, 42, 26}},
	}
	for _, tc := range cases {
		o := base
		tc.mutate(&o)
		got := ComputeGutterWidths(o, tc.lines)
		if got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
		if got.Total() != got.GlyphMargin+got.LineNumbers+got.Decorations {
			t.Errorf("%s: Total", tc.name)
		}
	}
}

func TestParagraphCacheEvictsTheLeastRecentlyUsed(t *testing.T) {
	c := NewParagraphCache(3)
	keys := []paragraphKey{{text: "a"}, {text: "b"}, {text: "c"}, {text: "d"}}
	for _, k := range keys[:3] {
		c.get(k, func() *ui.Paragraph { return nil })
	}
	if c.Len() != 3 || c.Builds() != 3 || c.Hits() != 0 {
		t.Fatalf("len %d builds %d hits %d", c.Len(), c.Builds(), c.Hits())
	}
	c.get(keys[0], func() *ui.Paragraph { return nil }) // a becomes the most recent
	if c.Hits() != 1 {
		t.Errorf("hits %d", c.Hits())
	}
	c.get(keys[3], func() *ui.Paragraph { return nil }) // evicts b, the least recent
	if c.Len() != 3 {
		t.Errorf("len %d", c.Len())
	}
	c.get(keys[1], func() *ui.Paragraph { return nil }) // b was evicted: a build
	if c.Builds() != 5 {
		t.Errorf("builds %d, want 5", c.Builds())
	}
	c.get(keys[0], func() *ui.Paragraph { return nil }) // a was kept
	if c.Hits() != 2 {
		t.Errorf("hits %d, want 2", c.Hits())
	}
	c.Clear()
	if c.Len() != 0 || c.Builds() != 5 {
		t.Errorf("after Clear: len %d builds %d", c.Len(), c.Builds())
	}
	if NewParagraphCache(0).capacity != DefaultParagraphCacheSize {
		t.Error("the default capacity")
	}
}

func newTestDoc(text string) *document.Document {
	return document.New(document.FileURI("/test.go"), text, document.NewOptions())
}

func testOptions() Options {
	return Options{
		FontFamilies: []string{"Go Mono", "monospace"}, FontSize: 14, LineHeight: 19, TabSize: 4,
		LineNumbers: LineNumbersOn, LineNumbersMinChars: 5, GlyphMargin: true, LineDecorationsWidth: 10, Folding: true, ScrollBeyondLastLine: true,
		RenderLineHighlight: "line", RoundedSelection: true, CursorBlinking: "blink",
	}
}

func TestViewModelFollowsTheDocument(t *testing.T) {
	doc := newTestDoc("a\nb\nc")
	vm := NewViewModel(doc, testOptions(), nil)
	if vm.Projection().RowCount() != 3 || vm.Projection().Height() != 57 {
		t.Fatalf("rows %d", vm.Projection().RowCount())
	}
	if vm.Sync() {
		t.Error("nothing changed")
	}
	_, err := doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: 2, Col: 1}, End: buffer.Position{Line: 2, Col: 1}}, Text: "\nd\ne"}}, document.EditOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.Projection().RowCount(); got != 5 {
		t.Errorf("rows after the edit: %d, want 5", got)
	}
	notified := 0
	vm.AddListener(func() { notified++ })
	vm.DocumentChanged()
	if notified != 1 {
		t.Errorf("listeners told %d times", notified)
	}
}

func TestViewModelOptions(t *testing.T) {
	doc := newTestDoc("a\nb")
	vm := NewViewModel(doc, testOptions(), nil)
	notified := 0
	vm.AddListener(func() { notified++ })

	vm.SetOptions(testOptions())
	if notified != 0 {
		t.Error("the same options are no change")
	}
	opts := testOptions()
	opts.LineHeight = 30
	vm.SetOptions(opts)
	if notified != 1 || vm.Projection().Height() != 60 {
		t.Errorf("notified %d, height %v", notified, vm.Projection().Height())
	}
	opts.FontFamilies = []string{"Other"}
	vm.SetOptions(opts)
	if notified != 2 {
		t.Errorf("a font change is a change: %d", notified)
	}
	opts.FontFeatures = []FontFeature{{"liga", 0}}
	vm.SetOptions(opts)
	if notified != 3 || len(vm.Options().FontFeatures) != 1 {
		t.Errorf("a feature change is a change: %d", notified)
	}
	vm.SetOptions(opts)
	if notified != 3 {
		t.Error("the same features are no change")
	}
}

func TestViewState(t *testing.T) {
	doc := newTestDoc("one\ntwo\nthree")
	vm := NewViewModel(doc, testOptions(), nil)
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 1, Col: 1}, Active: buffer.Position{Line: 2, Col: 3}}})
	vm.SetScrollTop(120)
	vm.SetScrollLeft(30)
	state := vm.ViewState()
	if len(state.Selections) != 1 || state.ScrollTop != 120 || state.ScrollLeft != 30 {
		t.Fatalf("state: %+v", state)
	}

	// Another editor of the same document, with a state that points past the end of a
	// document that has since become shorter.
	other := NewViewModel(newTestDoc("x"), testOptions(), nil)
	other.RestoreViewState(state)
	got := other.ViewState()
	if got.ScrollTop != 120 || got.ScrollLeft != 30 {
		t.Errorf("scroll: %+v", got)
	}
	want := buffer.Selection{Anchor: buffer.Position{Line: 0, Col: 1}, Active: buffer.Position{Line: 0, Col: 1}}
	if len(got.Selections) != 1 || got.Selections[0] != want {
		t.Errorf("selections are moved into the text: %+v, want %+v", got.Selections, want)
	}

	other.RestoreViewState(ViewState{ScrollTop: -4, ScrollLeft: -1})
	if s := other.ViewState(); s.ScrollTop != 0 || s.ScrollLeft != 0 || len(s.Selections) != 1 {
		t.Errorf("a negative scroll is 0 and no selections keeps the cursor: %+v", s)
	}
}

func TestViewModelLayoutIsALineLayout(t *testing.T) {
	doc := newTestDoc("one\ntwo\nthree\nfour\nfive\nsix\nseven\neight")
	vm := NewViewModel(doc, testOptions(), nil)
	layout := vm.Layout()
	if layout.PageRows() != edit.DefaultPageRows {
		t.Errorf("without a viewport a page is %d rows", layout.PageRows())
	}
	vm.SetViewportHeight(190) // ten rows of 19: a page is nine
	if got := vm.Layout().PageRows(); got != 9 {
		t.Errorf("PageRows = %d, want 9", got)
	}
	vm.SetViewportHeight(10)
	if got := vm.Layout().PageRows(); got != 1 {
		t.Errorf("PageRows is at least 1: %d", got)
	}
	layout = vm.Layout()
	pos, _ := layout.MoveVertically(buffer.Position{Line: 1, Col: 2}, 2, -1)
	if pos != (buffer.Position{Line: 3, Col: 2}) {
		t.Errorf("MoveVertically = %+v", pos)
	}
	if layout.RowStart(buffer.Position{Line: 2, Col: 3}) != (buffer.Position{Line: 2, Col: 0}) ||
		layout.RowEnd(buffer.Position{Line: 2, Col: 1}) != (buffer.Position{Line: 2, Col: 5}) {
		t.Error("RowStart and RowEnd")
	}
	vm.SetScrollTop(-3)
	if vm.ScrollTop() != 0 {
		t.Error("a negative scroll top is 0")
	}
	vm.SetScrollLeft(-3)
	if vm.ScrollLeft() != 0 {
		t.Error("a negative scroll left is 0")
	}
	if vm.Document() != doc || vm.Cursors() == nil || vm.Paragraphs() == nil {
		t.Error("accessors")
	}
}

func TestScrollExtent(t *testing.T) {
	cases := []struct {
		content, viewport, line float64
		beyond                  bool
		want                    float64
	}{
		{1900, 800, 19, true, 1881},
		{1900, 800, 19, false, 1100},
		{500, 800, 19, true, 481},
		{500, 800, 19, false, 0},
		{10, 800, 19, true, 0},
	}
	for _, tc := range cases {
		if got := scrollExtent(tc.content, tc.viewport, tc.line, tc.beyond); got != tc.want {
			t.Errorf("scrollExtent(%v, %v, %v, %v) = %v, want %v", tc.content, tc.viewport, tc.line, tc.beyond, got, tc.want)
		}
	}
}
