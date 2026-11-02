package editor

import (
	"strings"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The tests in the editor (SPEC §10.32 "The editor", step 26.3): the glyphs of the tests in the glyph margin, and the failure messages
// written after their lines. The editor knows nothing of the test model: the window gives it the marks, and the functions to call when
// the user presses a glyph (TestHost). The marks are decorations of the document that this editor owns, so they follow its edits at once;
// another editor of the same document is given its own. The peek under a failing line is a zone made by internal/gui/testpeek.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingDecorations.ts

// testingOwner is the slot of the decorations of the test glyphs (see ViewModel.owner).
const testingOwner = 16

// TestGlyphMark is the glyph of the tests that start on a line: the icon of the run or of the state of the last result.
type TestGlyphMark struct {
	// Line is the line, counted from 0.
	Line int
	// IDs are the tests the glyph runs, in the order of the line.
	IDs []string
	// Icon is the Codicon (a fonts.Codicon... constant) and ColorID the id of the theme color it is drawn in; an empty id draws it in the
	// foreground of the editor.
	Icon    int
	ColorID string
	// Hover is the Markdown of the hover of the glyph.
	Hover string
	// Failed says that the state of the glyph is a failure, which marks the overview ruler.
	Failed bool
}

// TestMessageMark is a message of a test shown after the end of its line.
type TestMessageMark struct {
	// Line is the line, counted from 0, and Text the text after it (its first line).
	Line int
	Text string
	// Hover is the Markdown of the hover of the message; empty for none.
	Hover string
	// Info says that the message is a logged output, not a failure.
	Info bool
}

// TestPress is a press of the primary button on a test glyph.
type TestPress struct {
	// Line is counted from 0 and IDs are the tests of the glyph.
	Line int
	IDs  []string
	// Mods are the modifiers held; Alt asks for the alternate action.
	Mods MouseModifiers
	// At is the place of the pointer in the window, where a menu opens.
	At ui.Offset
}

// TestHost is what the window gives the editor to act on the tests. Every function is called on the UI thread; any may be nil.
type TestHost struct {
	// Enabled says whether the glyphs show (testing.gutterEnabled).
	Enabled func() bool
	// GlyphPress is a press of the primary button on a glyph.
	GlyphPress func(TestPress)
}

// TestMenuTarget is where the secondary button was pressed last, for the menu of a test glyph.
type TestMenuTarget struct {
	// Line is counted from 0 and IDs are the tests of the glyph; InGlyph says that the press was on a test glyph.
	Line    int
	IDs     []string
	InGlyph bool
}

// testData is what a decoration of the testing owner carries.
type testData struct {
	glyph *TestGlyphMark
}

// testingState is the tests in a view model.
type testingState struct {
	host *TestHost
	ids  []int
	// epoch changes with the marks, for the caches that depend on them.
	epoch int
	menu  TestMenuTarget
}

// SetTestHost gives the editor the means to act on the tests; nil takes them away.
func (vm *ViewModel) SetTestHost(h *TestHost) {
	vm.tst.host = h
	vm.tstChanged()
}

// TestHostSet tells whether the editor has a test host.
func (vm *ViewModel) TestHostSet() bool { return vm.tst.host != nil }

// tstChanged makes the layers draw the marks again, and lays the editor out when the glyph margin changed its width.
func (vm *ViewModel) tstChanged() {
	vm.tst.epoch++
	vm.inlayRelayout()
}

// testGlyphsShown tells whether the editor draws test glyphs: it has a glyph margin, the host says so and there is a glyph.
func (vm *ViewModel) testGlyphsShown() bool {
	h := vm.tst.host
	return h != nil && vm.opts.GlyphMargin && len(vm.tst.ids) > 0 && (h.Enabled == nil || h.Enabled())
}

// glyphLanes is the number of lanes of the glyph margin: the glyphs of the tests have the left one, the breakpoints and the other marks the
// right one. With no test glyph the margin has one lane, as it had before there were tests.
func (vm *ViewModel) glyphLanes() int {
	if vm.testGlyphsShown() {
		return 2
	}
	return 1
}

// SetTestGlyphs replaces the glyphs of the tests the editor shows. A glyph of a line the document does not have is not kept. A glyph sits
// at the first character of its line that is not a space (a decoration needs a width).
func (vm *ViewModel) SetTestGlyphs(list []TestGlyphMark) {
	snap := vm.doc.Snapshot()
	add := make([]document.NewDecoration, 0, len(list))
	for _, g := range list {
		text, err := snap.LineContent(g.Line)
		if err != nil {
			continue
		}
		r, ok := vm.markRange(g.Line, firstNonSpace(text))
		if !ok {
			continue
		}
		mark := g
		add = append(add, document.NewDecoration{Range: r, Options: document.DecorationOptions{
			Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: "testGlyph", Data: testData{glyph: &mark},
		}})
	}
	if len(add) == 0 && len(vm.tst.ids) == 0 {
		return
	}
	vm.tst.ids = vm.doc.DeltaDecorations(vm.owner(testingOwner), vm.tst.ids, add)
	vm.tstChanged()
}

// TestGlyphs are the glyphs of the editor as they are now (moved by the edits), in the order of the document.
func (vm *ViewModel) TestGlyphs() []TestGlyphMark {
	var out []TestGlyphMark
	for _, d := range vm.doc.DecorationsOf(vm.owner(testingOwner)) {
		if data, ok := d.Options.Data.(testData); ok && data.glyph != nil {
			g := *data.glyph
			g.Line = d.Range.Start.Line
			out = append(out, g)
		}
	}
	return out
}

// testGlyphsIn collects the glyphs of the lines from to to, by line.
func (vm *ViewModel) testGlyphsIn(from, to int) map[int]*TestGlyphMark {
	if len(vm.tst.ids) == 0 {
		return nil
	}
	span := buffer.Range{Start: buffer.Position{Line: from}, End: buffer.Position{Line: to + 1}}
	var out map[int]*TestGlyphMark
	for _, d := range vm.doc.DecorationsIn(span, vm.owner(testingOwner)) {
		data, ok := d.Options.Data.(testData)
		if !ok || data.glyph == nil {
			continue
		}
		if out == nil {
			out = map[int]*TestGlyphMark{}
		}
		g := *data.glyph
		g.Line = d.Range.Start.Line
		if _, seen := out[g.Line]; !seen {
			out[g.Line] = &g
		}
	}
	return out
}

// SetTestMessages replaces the messages of the tests written after their lines: line annotations of a group of their own, drawn after
// the inline values of the debugger and before the other annotations.
func (vm *ViewModel) SetTestMessages(list []TestMessageMark) {
	as := make([]LineAnnotation, 0, len(list))
	for _, m := range list {
		text := strings.TrimSpace(m.Text)
		if text == "" {
			continue
		}
		a := LineAnnotation{Line: m.Line, Text: " " + text + " ", Hover: m.Hover, Tight: true}
		if m.Info {
			a.Color = "testing.message.info.decorationForeground"
		} else {
			a.Color, a.Background = "testing.message.error.badgeForeground", "testing.message.error.badgeBackground"
		}
		as = append(as, a)
	}
	vm.setAnnotations(&vm.tmsg, as)
}

// TestMessages are the messages of the editor as they are now, by line (counted from 0); the lines moved with the edits.
func (vm *ViewModel) TestMessages() map[int]string {
	out := map[int]string{}
	for id, e := range vm.tmsg.entries {
		d, ok := vm.doc.Decoration(id)
		if !ok {
			continue
		}
		out[d.Range.Start.Line] = strings.TrimSpace(e.Text)
	}
	return out
}

// DisposeTesting takes the test marks off the editor.
func (vm *ViewModel) DisposeTesting() {
	if len(vm.tst.ids) > 0 {
		vm.doc.DeltaDecorations(vm.owner(testingOwner), vm.tst.ids, nil)
		vm.tst.ids = nil
	}
	if len(vm.tmsg.ids) > 0 {
		vm.doc.DeltaDecorations(vm.owner(annotationsOwner), vm.tmsg.ids, nil)
		vm.tmsg = annotationState{generation: vm.tmsg.generation + 1}
	}
	vm.tst.host = nil
	vm.tstChanged()
}

// testLaneContains says whether a distance from the left edge of the gutter is in the lane of the test glyphs.
func (vm *ViewModel) testLaneContains(x float64) bool {
	return vm.glyphLanes() == 2 && x >= 0 && x < vm.opts.LineHeight
}

// testGlyphAt is the glyph under a point of the gutter, when the point is in the lane of the glyphs of the tests.
func (vm *ViewModel) testGlyphAt(x, y float64) (*TestGlyphMark, bool) {
	if !vm.testLaneContains(x) {
		return nil, false
	}
	line, ok := vm.glyphLineAt(x, y)
	if !ok {
		return nil, false
	}
	g := vm.testGlyphsIn(line, line)[line]
	return g, g != nil
}

// testGlyphPress is a press of the primary button on the glyph margin: it tells the host when the press is on a test glyph. It says whether
// the press was used.
func (vm *ViewModel) testGlyphPress(x, y float64, at ui.Offset, mods MouseModifiers) bool {
	h := vm.tst.host
	if h == nil || h.GlyphPress == nil {
		return false
	}
	g, ok := vm.testGlyphAt(x, y)
	if !ok {
		return false
	}
	h.GlyphPress(TestPress{Line: g.Line, IDs: append([]string(nil), g.IDs...), Mods: mods, At: at})
	return true
}

// noteTestMenuTarget remembers where the secondary button was pressed, for the menu of a test glyph.
func (vm *ViewModel) noteTestMenuTarget(x, y float64) {
	vm.tst.menu = TestMenuTarget{}
	if g, ok := vm.testGlyphAt(x, y); ok {
		vm.tst.menu = TestMenuTarget{Line: g.Line, IDs: append([]string(nil), g.IDs...), InGlyph: true}
	}
}

// TakeTestMenuTarget gives the place of the last secondary press and forgets it: the window asks when it opens the context menu, and shows
// the menu of the test when the press was on a test glyph.
func (vm *ViewModel) TakeTestMenuTarget() TestMenuTarget {
	t := vm.tst.menu
	vm.tst.menu = TestMenuTarget{}
	return t
}

// SetGlyphPointer tells the editor where the pointer is in the gutter (x from its left edge, y from its top; y -1 when it left): over a
// test glyph its hover shows, elsewhere the breakpoint hint and hover of the debugger.
func (vm *ViewModel) SetGlyphPointer(x, y float64) {
	if y >= 0 {
		if g, ok := vm.testGlyphAt(x, y); ok {
			vm.SetDebugGutterPointer(-1, false)
			vm.hoverStaticMoved(testGlyphKey, g.Line, buffer.Position{Line: g.Line}, g.Hover, nil)
			return
		}
	}
	vm.hoverStaticLeave(testGlyphKey)
	left := float64(0)
	if vm.glyphLanes() == 2 {
		left = vm.opts.LineHeight
	}
	vm.SetDebugGutterPointer(y, x >= left && x < vm.glyphWidth())
}

// testGlyphKey is the key of the static hover of a test glyph.
const testGlyphKey = "test-glyph"

// paintTestGlyphs draws the glyphs of the tests in the left lane of the glyph margin.
func (r *RenderEditorGutter) paintTestGlyphs(canvas *ui.Canvas, offset ui.Offset, top float64, first, last int, widths GutterWidths) {
	vm := r.vm
	if widths.GlyphMargin <= 0 || !vm.testGlyphsShown() {
		return
	}
	proj := vm.Projection()
	firstLine, _, _ := proj.RowSpan(first)
	lastLine, _, _ := proj.RowSpan(last)
	by := vm.testGlyphsIn(firstLine, lastLine)
	if len(by) == 0 {
		return
	}
	icons := vm.Options()
	icons.FontFamilies, icons.FontSize, icons.FontWeight, icons.LetterSpacing, icons.FontFeatures = []string{fonts.Codicon}, vm.glyphSize(), 0, 0, nil
	for row := first; row <= last; row++ {
		line, start, _ := proj.RowSpan(row)
		if _, _, zone := proj.RowZone(row); start != 0 || zone {
			continue // the glyph stands at the first row of a line
		}
		g := by[line]
		if g == nil {
			continue
		}
		color := r.colors.Test.Named(g.ColorID, r.colors.Test.Fallback)
		p := newTextStyle(r.factory, icons, color).paragraph(vm.Paragraphs(), string(rune(g.Icon)))
		x := offset.Dx + (vm.opts.LineHeight-p.LongestLine())/2
		canvas.DrawParagraph(p, ui.Offset{Dx: x, Dy: offset.Dy + proj.RowTop(row) - top})
	}
}

// TestColors are the colors of the test marks: the theme colors the glyphs name, found by id.
type TestColors struct {
	// Fallback is the color of a glyph that names none, or one the theme does not have.
	Fallback ui.Color
	// Lookup finds a theme color by id; nil finds none.
	Lookup func(id string) (ui.Color, bool)
}

// Named is the color of the theme color with the id, else the fallback.
func (c TestColors) Named(id string, fallback ui.Color) ui.Color {
	if id != "" && c.Lookup != nil {
		if col, ok := c.Lookup(id); ok && col.Alpha() != 0 {
			return col
		}
	}
	return fallback
}
