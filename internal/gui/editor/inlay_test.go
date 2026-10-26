package editor

import (
	"github.com/rm4n0s/glutter/ui"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

const hintsText = "package main\n\nfunc main() {\n    print(1, 2)\n}\n"

// hintGeometry shows the text in an editor and sets the hints in it.
func hintGeometry(t *testing.T, tweak func(*Options), hints ...langfeatures.InlayHint) (geometry, *lang129Rig) {
	t.Helper()
	r := newLang129Rig(t, hintsText, tweak)
	ts := pumpEditor(t, r.vm, uiSize(800, 400))
	r.vm.inlaySet(rg(0, 0, 10, 0), hints)
	ts.Pump(16 * time.Millisecond)
	return newGeometry(r.vm, ts.Dispatcher().TextEngine()), r
}

func TestInlayHintsAreBetweenCharactersAndNotInTheText(t *testing.T) {
	// print(1, 2): "a:" before the 1 (col 10) and "b:" before the 2 (col 13)
	g, r := hintGeometry(t, nil, hint(3, 10, "a:"), hint(3, 13, "b:"))
	cw := g.charWidth
	before := r.vm.Document().Text()
	if before != hintsText || r.vm.InlayHintCount() != 2 {
		t.Fatalf("the text is %q with %d hints", before, r.vm.InlayHintCount())
	}
	row := 3
	// the text before the first hint is where it was, and after it the characters are further right by the width of the hint
	if x := g.x(row, 10); !near(x, 10*cw) {
		t.Errorf("x of the character before the hint = %v, want %v", x, 10*cw)
	}
	w1 := g.x(row, 11) - g.x(row, 10) - cw
	if w1 < cw {
		t.Fatalf("the first hint is %v wide", w1)
	}
	if x := g.x(row, 13); !near(x, 13*cw+w1) {
		t.Errorf("x of the second hint's column = %v, want %v", x, 13*cw+w1)
	}
	// a position with a hint is drawn before it; the column after the hint is the end of the character
	if g.x(row, 12) > g.x(row, 13) {
		t.Error("columns are in order")
	}
	// the pointer on the characters hits the columns of the text, skipping the hints
	if got := g.positionAt(g.x(row, 11)+cw*0.4, 3*19+5); got != (buffer.Position{Line: 3, Col: 11}) {
		t.Errorf("pointer left of the character after the first hint: %v", got)
	}
	if got := g.positionAt(g.x(row, 14)+cw*0.6, 3*19+5); got != (buffer.Position{Line: 3, Col: 15}) {
		t.Errorf("pointer right of the 2: %v", got)
	}
	// and on a hint hits the place the hint is before
	mid := g.x(row, 10) + w1/2 + cw*0.1
	if got := g.positionAt(mid, 3*19+5); got != (buffer.Position{Line: 3, Col: 10}) {
		t.Errorf("pointer on the first hint: %v", got)
	}
	inj, part, ok := g.hintAt(row, mid)
	if !ok || inj.entry.hint.Label() != "a:" || part != 0 {
		t.Errorf("hint at %v = %+v, %d, %v", mid, inj, part, ok)
	}
	if _, _, ok := g.hintAt(row, g.x(row, 10)-1); ok {
		t.Error("a point left of the hint is not on it")
	}
	if _, _, ok := g.hintAt(2, mid); ok {
		t.Error("a point on another row is not on the hint")
	}
	// the rectangle of a character starts before the hint that is before it
	if rect := g.rectOf(buffer.Position{Line: 3, Col: 10}); !near(rect.Left, 10*cw) || rect.Right < 10*cw+w1 {
		t.Errorf("rect of the character after the hint = %+v", rect)
	}
	if got := r.vm.Document().Text(); got != hintsText {
		t.Error("measuring changed the text")
	}
}

func TestTheCursorSkipsInlayHints(t *testing.T) {
	_, r := hintGeometry(t, nil, hint(3, 10, "a:"))
	vm := r.vm
	vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 3, Col: 9})})
	var cols []int
	for range 3 {
		if err := vm.RunCommand("cursorRight", nil); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, vm.Cursors().Primary().Position().Col)
	}
	// one key, one character of the text: the hint takes no stop
	if cols[0] != 10 || cols[1] != 11 || cols[2] != 12 {
		t.Errorf("the cursor stopped at %v", cols)
	}
	// selecting across the hint selects the text only
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 3, Col: 8}, Active: buffer.Position{Line: 3, Col: 12}}})
	sel := vm.Cursors().Primary().Selection
	if text := vm.Document().Snapshot().Text(sel.Range()); text != "t(1," {
		t.Errorf("selected text = %q", text)
	}
	// Home and End are for the text too
	if err := vm.RunCommand("cursorLineEnd", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 3, Col: len("    print(1, 2)")}) {
		t.Errorf("End = %v", got)
	}
}

func TestInlayHintModes(t *testing.T) {
	ctrlAlt := MouseModifiers{Ctrl: true, Alt: true}
	tests := []struct {
		mode                          string
		shownAtRest, shownCtrlAlt     bool
		shownWithShift, shownWithCtrl bool
	}{
		{"on", true, true, true, true},
		{"", true, true, true, true},
		{"off", false, false, false, false},
		{"onUnlessPressed", true, false, true, true},
		{"offUnlessPressed", false, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			g, r := hintGeometry(t, func(o *Options) { o.Lens.InlayHints = tt.mode }, hint(3, 10, "a:"))
			vm := r.vm
			cw := g.charWidth
			// the hints are held in every mode; the mode says whether they are drawn
			if vm.InlayHintCount() != 1 {
				t.Fatalf("%d hints are held", vm.InlayHintCount())
			}
			shown := func() bool { return !near(newGeometryOf(vm, g).x(3, 11), 11*cw) }
			if vm.InlayHintsShown() != tt.shownAtRest || shown() != tt.shownAtRest {
				t.Errorf("at rest: shown %v, drawn %v, want %v", vm.InlayHintsShown(), shown(), tt.shownAtRest)
			}
			vm.InlayModifiersChanged(ctrlAlt)
			if vm.InlayHintsShown() != tt.shownCtrlAlt || shown() != tt.shownCtrlAlt {
				t.Errorf("with Ctrl+Alt: shown %v, drawn %v, want %v", vm.InlayHintsShown(), shown(), tt.shownCtrlAlt)
			}
			vm.InlayModifiersChanged(MouseModifiers{Ctrl: true, Alt: true, Shift: true})
			if vm.InlayHintsShown() != tt.shownWithShift {
				t.Errorf("with Ctrl+Alt+Shift: shown %v, want %v (Shift and Meta cancel the combination)", vm.InlayHintsShown(), tt.shownWithShift)
			}
			vm.InlayModifiersChanged(MouseModifiers{Ctrl: true})
			if vm.InlayHintsShown() != tt.shownWithCtrl {
				t.Errorf("with Ctrl: shown %v, want %v", vm.InlayHintsShown(), tt.shownWithCtrl)
			}
			vm.InlayModifiersChanged(MouseModifiers{})
			if vm.InlayHintsShown() != tt.shownAtRest {
				t.Errorf("released: shown %v, want %v", vm.InlayHintsShown(), tt.shownAtRest)
			}
		})
	}
}

// newGeometryOf is a geometry of a view model with the text engine of another.
func newGeometryOf(vm *ViewModel, g geometry) geometry {
	return newGeometry(vm, g.style.factory)
}

func TestInlayHintLabelPaddingAndLength(t *testing.T) {
	tests := []struct {
		name   string
		tweak  func(*Options)
		hint   langfeatures.InlayHint
		text   string
		bgFrom int
		bgTo   int
		nParts int
	}{
		{"a plain label", nil, hint(3, 10, "name:"), "name:", 0, 5, 1},
		{"the padding the server asks for is outside the box", nil,
			langfeatures.InlayHint{Position: buffer.Position{Line: 3, Col: 10}, Parts: []langfeatures.InlayHintPart{{Value: "x"}}, PaddingLeft: true, PaddingRight: true}, " x ", 1, 2, 1},
		{"editor.inlayHints.padding adds room inside the box", func(o *Options) { o.Lens.InlayPadding = true }, hint(3, 10, "x"), " x ", 0, 3, 1},
		{"parts are joined", nil,
			langfeatures.InlayHint{Position: buffer.Position{Line: 3, Col: 10}, Parts: []langfeatures.InlayHintPart{{Value: "ab"}, {Value: "cd"}}}, "abcd", 0, 4, 2},
		{"a label is cut to the length of the line", func(o *Options) { o.Lens.InlayMaxLength = 5 }, hint(3, 10, "abcdefgh"), "abcd…", 0, len("abcd…"), 1},
		{"a length of 0 is no limit", func(o *Options) { o.Lens.InlayMaxLength = 0 }, hint(3, 10, strings.Repeat("w", 100)), strings.Repeat("w", 100), 0, 100, 1},
		{"a new line in a label is a space", nil, hint(3, 10, "a\nb"), "a b", 0, 3, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, r := hintGeometry(t, tt.tweak, tt.hint)
			inj := r.vm.lineInjections(3)
			if len(inj) != 1 || inj[0].text != tt.text || inj[0].bgFrom != tt.bgFrom || inj[0].bgTo != tt.bgTo || len(inj[0].parts) != tt.nParts {
				t.Fatalf("injection = %+v", inj)
			}
		})
	}
	// the length is for the line: the hints that come after it is used up are left out
	_, r := hintGeometry(t, func(o *Options) { o.Lens.InlayMaxLength = 6 }, hint(3, 10, "abcd"), hint(3, 13, "efgh"), hint(3, 14, "ijkl"))
	inj := r.vm.lineInjections(3)
	if len(inj) != 2 || inj[0].text != "abcd" || inj[1].text != "e…" {
		t.Errorf("injections = %+v", inj)
	}
}

func TestInlayHintsAreAskedForTheVisibleLinesAndFollowEdits(t *testing.T) {
	r := newLang129Rig(t, hintsText, nil)
	p := &hintsProvider{hints: []langfeatures.InlayHint{hint(3, 10, "a:")}}
	r.reg.RegisterInlayHints("test", r.selector(), p)
	r.host()
	r.settle()
	if p.calls() != 1 || r.vm.InlayHintCount() != 1 {
		t.Fatalf("%d requests, %d hints", p.calls(), r.vm.InlayHintCount())
	}
	if p.ranges[0].Start.Line != 0 || p.ranges[0].End.Line < 5 {
		t.Errorf("the range = %v: the text and the margin around the visible lines", p.ranges[0])
	}
	// the hint moves with the text typed above it
	r.typeAt(0, 0, "// c\n")
	if inj := r.vm.lineInjections(4); len(inj) != 1 || inj[0].col != 10 {
		t.Errorf("after a line was inserted the hint is %+v", inj)
	}
	if got := r.vm.Document().Text(); !strings.HasPrefix(got, "// c\npackage") {
		t.Errorf("the text = %q", got)
	}
	// a request comes after the delay; the hints of the new answer replace the old
	p.mu.Lock()
	p.hints = []langfeatures.InlayHint{hint(4, 10, "b:")}
	p.mu.Unlock()
	if p.calls() != 1 {
		t.Errorf("a request was made before the delay: %d", p.calls())
	}
	r.clock.advance(40 * time.Millisecond)
	r.settle()
	if p.calls() != 2 || r.vm.InlayHintCount() != 1 || r.vm.lineInjections(4)[0].entry.hint.Label() != "b:" {
		t.Errorf("%d requests, hints %d: %+v", p.calls(), r.vm.InlayHintCount(), r.vm.lineInjections(4))
	}
}

func TestStaleInlayHintAnswersAreDropped(t *testing.T) {
	r := newLang129Rig(t, hintsText, nil)
	p := &hintsProvider{hints: []langfeatures.InlayHint{hint(3, 10, "old:")}, gate: make(chan struct{})}
	r.reg.RegisterInlayHints("test", r.selector(), p)
	r.host()
	deadline := time.Now().Add(5 * time.Second)
	for p.calls() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	// the text changes while the request is in flight: its answer is for a version that is gone
	r.typeAt(0, 0, "x")
	close(p.gate)
	r.settle()
	if r.vm.InlayHintCount() != 0 {
		t.Errorf("the answer for an older version was taken: %d hints", r.vm.InlayHintCount())
	}
	// the request that follows the edit is the one that counts
	p.mu.Lock()
	p.hints = []langfeatures.InlayHint{hint(3, 10, "new:")}
	p.mu.Unlock()
	r.clock.advance(time.Second)
	r.settle()
	if r.vm.InlayHintCount() != 1 || r.vm.lineInjections(3)[0].entry.hint.Label() != "new:" {
		t.Errorf("hints = %d", r.vm.InlayHintCount())
	}
	if last := p.versions[len(p.versions)-1]; last != r.vm.Document().Version() {
		t.Errorf("the last request was for version %d, the document is at %d", last, r.vm.Document().Version())
	}
}

func TestInlayHintsFollowTheModeAndTheProviders(t *testing.T) {
	r := newLang129Rig(t, hintsText, func(o *Options) { o.Lens.InlayHints = "offUnlessPressed" })
	p := &hintsProvider{hints: []langfeatures.InlayHint{hint(3, 10, "a:")}}
	r.reg.RegisterInlayHints("test", r.selector(), p)
	r.host()
	r.settle()
	if p.calls() != 0 {
		t.Errorf("hints that are hidden are not asked for: %d requests", p.calls())
	}
	r.vm.InlayModifiersChanged(MouseModifiers{Ctrl: true, Alt: true})
	r.settle()
	if p.calls() != 1 || r.vm.InlayHintCount() != 1 || len(r.vm.lineInjections(3)) != 1 {
		t.Errorf("with Ctrl+Alt: %d requests, %d hints", p.calls(), r.vm.InlayHintCount())
	}
	// a document of a language with no provider has none
	other := newLang129Rig(t, hintsText, nil)
	other.vm.Document().SetLanguageID("rust")
	other.reg.RegisterInlayHints("test", other.selector(), p)
	other.host()
	other.settle()
	if other.vm.InlayHintCount() != 0 || p.calls() != 1 {
		t.Error("no provider for the language, no request")
	}
	// the hints go with the host
	r.vm.SetSuggest(nil)
	if r.vm.InlayHintCount() != 0 {
		t.Error("the hints stay after the host went")
	}
}

func TestInlayHintHoverAndClick(t *testing.T) {
	r := newLang129Rig(t, hintsText, nil)
	loc := langfeatures.Location{Range: rg(0, 0, 0, 1)}
	loc.URI.Path, loc.URI.Scheme = "/other.go", "file"
	withLoc := langfeatures.InlayHint{Position: buffer.Position{Line: 3, Col: 10}, Parts: []langfeatures.InlayHintPart{
		{Value: "T", Tooltip: &langfeatures.Markup{Value: "the type T"}, Location: &loc}, {Value: ":", Command: &langfeatures.Command{Title: "Show", ID: "show.cmd"}},
	}}
	p := &hintsProvider{hints: []langfeatures.InlayHint{withLoc, hint(3, 13, "b:")}}
	r.reg.RegisterInlayHints("test", r.selector(), p)
	r.host()
	r.settle()
	if r.vm.InlayHintCount() != 2 {
		t.Fatalf("%d hints", r.vm.InlayHintCount())
	}
	ids := r.vm.inlay.ids
	// the pointer on a part shows its tooltip after the delay, with what Ctrl+click does
	r.vm.HoverHintMoved(ids[0], 0)
	r.clock.advance(time.Second)
	r.settle()
	h := r.vm.Hover()
	if h == nil || len(h.Parts) != 1 {
		t.Fatalf("hover = %+v", h)
	}
	var texts []string
	for _, c := range h.Parts[0].Contents {
		texts = append(texts, c.Value)
	}
	if strings.Join(texts, "|") != "the type T|Go to Definition (Ctrl+Click)" {
		t.Errorf("hover contents = %q", texts)
	}
	// the same part keeps the hover; another part replaces it
	r.vm.HoverHintMoved(ids[0], 0)
	if r.vm.Hover() == nil {
		t.Error("the hover went although the pointer is on the same part")
	}
	r.vm.HoverHintMoved(ids[0], 1)
	r.clock.advance(time.Second)
	r.settle()
	if h := r.vm.Hover(); h == nil || h.Parts[0].Contents[0].Value != "Show (Ctrl+Click)" {
		t.Errorf("hover of the command part = %+v", h)
	}
	// a hint without a tooltip is resolved by the server when the pointer rests on it
	r.vm.HoverHintMoved(ids[1], 0)
	r.clock.advance(time.Second)
	r.settle()
	if h := r.vm.Hover(); h == nil || h.Parts[0].Contents[0].Value != "resolved tooltip" || p.resolved != 1 {
		t.Errorf("hover of the resolved hint = %+v, %d resolves", h, p.resolved)
	}
	// the hover goes when the text changes
	r.typeAt(0, 0, " ")
	if r.vm.Hover() != nil {
		t.Error("the hover stayed after an edit")
	}

	// a click without the modifier does nothing; with it the location is shown and the command run
	if r.vm.HintClick(ids[0], 0, MouseModifiers{}) {
		t.Error("a click without Ctrl follows the hint")
	}
	ctrl := MouseModifiers{Ctrl: true}
	if !r.vm.HintClick(ids[0], 0, ctrl) || len(r.opened) != 1 || r.opened[0].URI.Path != "/other.go" {
		t.Errorf("Ctrl+click on the location part: opened %+v", r.opened)
	}
	if !r.vm.HintClick(ids[0], 1, ctrl) || len(r.executedCommands()) != 1 || r.executedCommands()[0].ID != "show.cmd" {
		t.Errorf("Ctrl+click on the command part: %+v", r.executedCommands())
	}
	if r.vm.HintClick(ids[0], -1, ctrl) {
		t.Error("the padding goes nowhere")
	}
}

func TestInlayHintsAreNotRequestedWhenTheEditorIsEmbedded(t *testing.T) {
	r := newLang129Rig(t, hintsText, nil)
	r.vm.embedded = true
	p := &hintsProvider{hints: []langfeatures.InlayHint{hint(3, 10, "a:")}}
	r.reg.RegisterInlayHints("test", r.selector(), p)
	r.host()
	r.settle()
	if p.calls() != 0 {
		t.Error("the editor of a peek view asked for hints")
	}
}

func uiSize(w, h float64) ui.Size { return ui.Size{Width: w, Height: h} }

func TestInlayHintsFollowAChangeOfTheSettings(t *testing.T) {
	r := newLang129Rig(t, hintsText, nil)
	p := &hintsProvider{hints: []langfeatures.InlayHint{hint(3, 10, "a:")}}
	r.reg.RegisterInlayHints("test", r.selector(), p)
	r.host()
	r.settle()
	if len(r.vm.lineInjections(3)) != 1 {
		t.Fatalf("no hint to start with: %d", len(r.vm.lineInjections(3)))
	}
	o := r.vm.Options()
	o.Lens.InlayHints = "off"
	r.vm.SetOptions(o)
	r.settle()
	if got := len(r.vm.lineInjections(3)); got != 0 {
		t.Errorf("%d hints shown with editor.inlayHints.enabled off", got)
	}
	o.Lens.InlayHints = "on"
	r.vm.SetOptions(o)
	r.clock.advance(time.Second)
	r.settle()
	if got := len(r.vm.lineInjections(3)); got != 1 {
		t.Errorf("%d hints shown after the setting came back on", got)
	}
}
