package terminal

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// paneSize is the window of the pane tests: 80 columns and 14 rows of the test font fit.
var paneSize = gui.Size{Width: 700, Height: 280}

// focus gives the pane the keyboard focus, as a click on it does.
func (r *paneRig) focus() {
	r.pv.requestFocus()
	r.settle()
}

// cell is the middle of a cell of the pane.
func (r *paneRig) cell(col, row int) gui.Offset {
	a, _ := r.pv.atlases()
	return gui.Offset{Dx: padLeft + (float64(col)+0.5)*a.m.w, Dy: (float64(row) + 0.5) * a.m.h}
}

// waitInput waits until the program has received what the terminal sent, and returns it.
func (r *paneRig) waitInput(t *testing.T, want string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := r.proc.input()
		if strings.Contains(got, want) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("the program received %q, want it to have %q", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

// key sends a key with the modifiers held, and releases them.
func (r *paneRig) key(logical services.LogicalKeyboardKey, mods ...services.LogicalKeyboardKey) {
	for _, m := range mods {
		r.ts.SendKeyDownEvent(m)
	}
	r.ts.SendKeyDownEvent(logical)
	r.ts.SendKeyUpEvent(logical)
	for i := len(mods) - 1; i >= 0; i-- {
		r.ts.SendKeyUpEvent(mods[i])
	}
	r.settle()
}

func TestKeysAreEncodedForTheProgram(t *testing.T) {
	tests := []struct {
		name string
		key  services.LogicalKeyboardKey
		mods []services.LogicalKeyboardKey
		want string
	}{
		{"enter", services.LogicalKeyboardKeyEnter, nil, "\r"},
		{"backspace", services.LogicalKeyboardKeyBackspace, nil, "\x7f"},
		{"arrow up", services.LogicalKeyboardKeyArrowUp, nil, "\x1b[A"},
		{"ctrl+arrow right", services.LogicalKeyboardKeyArrowRight, []services.LogicalKeyboardKey{services.LogicalKeyboardKeyControl}, "\x1b[1;5C"},
		{"ctrl+c", services.LogicalKeyboardKeyKeyC, []services.LogicalKeyboardKey{services.LogicalKeyboardKeyControl}, "\x03"},
		{"ctrl+l", services.LogicalKeyboardKeyKeyL, []services.LogicalKeyboardKey{services.LogicalKeyboardKeyControl}, "\x0c"},
		{"alt+b", services.LogicalKeyboardKeyKeyB, []services.LogicalKeyboardKey{services.LogicalKeyboardKeyAlt}, "\x1bb"},
		{"tab", services.LogicalKeyboardKeyTab, nil, "\t"},
		{"escape", services.LogicalKeyboardKeyEscape, nil, "\x1b"},
		{"page up", services.LogicalKeyboardKeyPageUp, nil, "\x1b[5~"},
		{"f5", services.LogicalKeyboardKeyF5, nil, "\x1b[15~"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
			r.focus()
			r.key(tt.key, tt.mods...)
			r.waitInput(t, tt.want)
			if got := r.proc.input(); got != tt.want {
				t.Errorf("the program received %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplicationCursorKeysAndFocusEvents(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	r.write(t, "\x1b[?1h\x1b[?1004h")
	r.focus()
	r.waitInput(t, "\x1b[I") // the focus event the program asked for
	r.key(services.LogicalKeyboardKeyArrowUp)
	r.waitInput(t, "\x1bOA")
}

// A tab that replaces the pane of the terminal takes the pane's widget out of the tree with the focus: the Focus widget's
// OnFocusChange is not called then (Glutter 1.9.5), and the pane learns it from its own focus node.
func TestAPaneThatLeavesTheTreeWithTheFocusLosesIt(t *testing.T) {
	tests := []struct {
		name    string
		focused bool
		// told is how the extension is told about the focus, in order
		told []bool
	}{
		{"the pane has the focus", true, []bool{true, false}},
		{"the pane has not", false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
			var told []bool
			r.env.focusChanged = func(_ *paneView, has bool) { told = append(told, has) }
			if tt.focused {
				r.focus()
				if !r.pv.focused {
					t.Fatal("the pane did not take the focus")
				}
			}
			r.ts.PumpWidget(widgets.NewSizedBox(nil))
			r.settle()
			if r.pv.render != nil {
				t.Errorf("the pane is still mounted")
			}
			if r.pv.focused {
				t.Errorf("the pane still thinks it has the focus")
			}
			if len(told) != len(tt.told) {
				t.Fatalf("the extension was told %v, want %v", told, tt.told)
			}
			for i := range told {
				if told[i] != tt.told[i] {
					t.Errorf("the extension was told %v, want %v", told, tt.told)
				}
			}
		})
	}
}

func TestMouseSelectsAndCopies(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	r.write(t, "alpha beta gamma\r\nsecond line here\r\n")
	// a drag from the "b" of beta to the last "a" of gamma
	from, to := r.cell(6, 0), r.cell(15, 0)
	r.ts.DragFrom(from, to.Sub(from))
	r.settle()
	if got := r.pv.selectedText(); got != "beta gamma" {
		t.Errorf("the selection is %q after a drag", got)
	}
	if text, ok := r.ts.PrimaryText(); !ok || text != "beta gamma" {
		t.Errorf("the primary selection is %q (%v)", text, ok)
	}
	r.pv.copySelection()
	r.settle()
	if text, ok := r.ts.ClipboardText(); !ok || text != "beta gamma" {
		t.Errorf("the clipboard has %q (%v)", text, ok)
	}
	// typing ends the selection
	r.focus()
	r.key(services.LogicalKeyboardKeyEnter)
	if r.pv.hasSelection() {
		t.Error("the selection stays after the user typed")
	}
}

func TestDoubleAndTripleClickSelectAWordAndALine(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	r.write(t, "alpha (beta) gamma\r\nsecond line here\r\n")
	at := r.cell(8, 0) // inside "beta": the parenthesis is a separator
	r.ts.TapAt(at)
	r.ts.TapAt(at)
	r.settle()
	if got := r.pv.selectedText(); got != "beta" {
		t.Errorf("a double click selects %q, want beta", got)
	}
	r.ts.TapAt(at)
	r.settle()
	if got := r.pv.selectedText(); got != "alpha (beta) gamma" {
		t.Errorf("a triple click selects %q, want the line", got)
	}
}

func TestSelectAllTakesTheScrollbackToo(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	var sb strings.Builder
	for i := 1; i <= 40; i++ {
		sb.WriteString("line ")
		sb.WriteString(strings.Repeat("x", i%7))
		sb.WriteString("\r\n")
	}
	r.write(t, sb.String())
	r.pv.selectAll()
	got := r.pv.selectedText()
	if n := strings.Count(got, "line"); n != 40 {
		t.Errorf("select all holds %d lines of the output, want 40", n)
	}
}

func TestPasteIsBracketedWhenTheProgramAsksAndWarnsForSeveralLines(t *testing.T) {
	tests := []struct {
		name      string
		setup     string
		tune      func(*config)
		text      string
		wantInput string
		wantAsk   int // lines the user was asked about; 0 for no question
	}{
		{"one line", "", nil, "ls -l", "ls -l", 0},
		{"bracketed paste", "\x1b[?2004h", nil, "a\nb", "\x1b[200~a\rb\x1b[201~", 0},
		{"several lines without bracketed paste ask", "", nil, "a\nb\nc", "a\rb\rc", 3},
		{"never ask", "", func(c *config) { c.multiLinePaste = "never" }, "a\nb", "a\rb", 0},
		{"always ask, also with bracketed paste", "\x1b[?2004h", func(c *config) { c.multiLinePaste = "always" }, "a\nb", "\x1b[200~a\rb\x1b[201~", 2},
		{"ignoring bracketed paste", "\x1b[?2004h", func(c *config) { c.ignoreBracket, c.multiLinePaste = true, "never" }, "a\nb", "a\rb", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newFakePane(t, uitest.Themes()[0], paneSize, tt.tune)
			asked, confirm := 0, func() {}
			r.env.confirmPaste = func(lines int, paste func()) { asked, confirm = lines, paste }
			if tt.setup != "" {
				r.write(t, tt.setup)
			}
			r.ts.SetClipboardText(tt.text)
			r.pv.pasteClipboard()
			r.settle()
			if asked != tt.wantAsk {
				t.Fatalf("asked about %d lines, want %d", asked, tt.wantAsk)
			}
			if asked > 0 {
				if got := r.proc.input(); got != "" {
					t.Fatalf("the program received %q before the user agreed", got)
				}
				confirm()
			}
			r.waitInput(t, tt.wantInput)
			if got := r.proc.input(); got != tt.wantInput {
				t.Errorf("the program received %q, want %q", got, tt.wantInput)
			}
		})
	}
}

func TestMiddleClickPastesThePrimarySelection(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	r.ts.SetPrimaryText("from the desktop")
	r.pv.pastePrimary()
	r.settle()
	r.waitInput(t, "from the desktop")
}

func TestTheWheelScrollsTheScrollbackAndAKeyComesBack(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		sb.WriteString("row ")
		sb.WriteString(strings.Repeat("y", 1+i%5))
		sb.WriteString("\r\n")
	}
	r.write(t, sb.String())
	if r.pv.scroll != 0 || r.pv.frame.Scrollback < 80 {
		t.Fatalf("scroll %d, scrollback %d before the wheel", r.pv.scroll, r.pv.frame.Scrollback)
	}
	r.ts.ScrollAt(r.cell(5, 5), gui.Offset{Dy: -3 * notch})
	r.settle()
	if r.pv.scroll == 0 {
		t.Fatal("the wheel did not scroll back")
	}
	back := r.pv.scroll
	r.ts.ScrollAt(r.cell(5, 5), gui.Offset{Dy: 3 * notch})
	r.settle()
	if r.pv.scroll >= back {
		t.Errorf("the wheel down left the scroll at %d (was %d)", r.pv.scroll, back)
	}
	// Alt makes it fast; the scroll command goes to the top; a key goes to the bottom
	r.pv.scrollTo(r.pv.frame.Scrollback)
	r.focus()
	r.key(services.LogicalKeyboardKeyEnter)
	if r.pv.scroll != 0 {
		t.Errorf("a key left the view %d lines above the bottom", r.pv.scroll)
	}
}

func TestTheWheelGoesToAProgramThatTracksTheMouse(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	r.write(t, "\x1b[?1000h\x1b[?1006h")
	r.ts.ScrollAt(r.cell(10, 3), gui.Offset{Dy: -notch})
	r.settle()
	// a wheel up is button 64 in the SGR encoding; the cell is 1-based
	r.waitInput(t, "\x1b[<64;11;4M")
	if r.pv.scroll != 0 {
		t.Error("the view scrolled although the program tracks the mouse")
	}
}

func TestClicksGoToAProgramThatTracksTheMouseButShiftSelects(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	r.write(t, "\x1b[?1000h\x1b[?1006hhello world\r\n")
	r.ts.TapAt(r.cell(2, 0))
	r.settle()
	r.waitInput(t, "\x1b[<0;3;1M")
	r.waitInput(t, "\x1b[<0;3;1m")
	if r.pv.hasSelection() {
		t.Error("a click selected text although the program tracks the mouse")
	}
}

func TestTheScrollbarThumbAppearsWithScrollback(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	if _, _, ok := r.pv.thumb(paneSize); ok {
		t.Error("a thumb with no scrollback")
	}
	r.write(t, strings.Repeat("line\r\n", 60))
	top, h, ok := r.pv.thumb(paneSize)
	if !ok || h < 24 || top+h > paneSize.Height+0.5 || top < 0 {
		t.Errorf("thumb = %v %v %v", top, h, ok)
	}
	r.pv.scrollTo(r.pv.frame.Scrollback)
	if top0, _, _ := r.pv.thumb(paneSize); top0 > 0.5 {
		t.Errorf("the thumb is %v px from the top when the view is at the top", top0)
	}
}

func TestAPaneThatDoesNotChangeDrawsNothingMore(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	r.write(t, "idle\r\n")
	before := r.pv.paints
	for range 10 {
		r.settle()
	}
	if got := r.pv.paints; got != before {
		t.Errorf("an idle pane painted %d times in 40 frames", got-before)
	}
	// a change paints it once
	r.write(t, "more\r\n")
	if r.pv.paints == before {
		t.Error("output did not paint the pane")
	}
}

func TestTheAlternateScreenIsReportedAndLeftAlone(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	alt := 0
	r.env.altChanged = func(*paneView) { alt++ }
	r.write(t, "main text\r\n\x1b[?1049h\x1b[2J\x1b[1;1Halt text")
	if alt != 1 || !strings.Contains(r.devText(), "alt text") {
		t.Errorf("alt changes %d, screen %q", alt, r.devText())
	}
	r.write(t, "\x1b[?1049l")
	if alt != 2 || !strings.Contains(r.devText(), "main text") {
		t.Errorf("alt changes %d, screen %q", alt, r.devText())
	}
}

// devText is the text the pane shows.
func (r *paneRig) devText() string {
	return r.pv.render.DevTerminal().(devState).Text
}

func TestClearKeepsThePromptLine(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	r.write(t, "one\r\ntwo\r\nthree\r\n$ typed")
	r.pv.clear()
	r.settle()
	if got := r.devText(); got != "$ typed" {
		t.Errorf("after Clear the screen is %q", got)
	}
	if r.pv.frame.Scrollback != 0 {
		t.Errorf("after Clear the scrollback is %d lines", r.pv.frame.Scrollback)
	}
}

func TestTheMenuOffersWhatVSCodeOffers(t *testing.T) {
	// the menu itself is the extension's (panel_test); here the pane asks for it on the secondary button
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	var at gui.Offset
	r.env.menu = func(_ *paneView, p gui.Offset) { at = p }
	r.ts.TapAt(r.cell(1, 1), gluttertest.GestureOpts{Kind: gui.PointerDeviceKindMouse, Buttons: foundation.Some(2)})
	r.settle()
	if at == (gui.Offset{}) {
		t.Error("the secondary button did not ask for the menu")
	}
}

func TestSizeToContentWidthMakesTheTerminalAsWideAsItsLongestLine(t *testing.T) {
	long := strings.Repeat("0123456789", 12) // 120 columns on a pane of about 55
	tests := []struct {
		name     string
		output   string
		toggles  int
		wantCols func(paneCols int) int // the columns the terminal has after the toggles
		wantFix  bool
	}{
		{"a long line makes the terminal that wide", long + "\r\n", 1, func(int) int { return 120 }, true},
		{"a long line that wrapped counts whole", long, 1, func(int) int { return 120 }, true},
		{"content that fits changes nothing", "short\r\n", 1, func(c int) int { return c }, false},
		{"the second toggle goes back to the width of the pane", long + "\r\n", 2, func(c int) int { return c }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
			paneCols := r.pv.cols
			r.write(t, tt.output)
			r.settle()
			for range tt.toggles {
				r.pv.toggleSizeToContent()
				r.settle()
			}
			if got, want := r.pv.cols, tt.wantCols(paneCols); got != want {
				t.Errorf("the terminal has %d columns, want %d", got, want)
			}
			if (r.pv.fixedCols > 0) != tt.wantFix {
				t.Errorf("fixedCols = %d, want fixed %v", r.pv.fixedCols, tt.wantFix)
			}
			if cols, _ := r.pv.term.Size(); cols != r.pv.cols {
				t.Errorf("the process is told %d columns, the pane has %d", cols, r.pv.cols)
			}
		})
	}
}

func TestAWidePaneScrollsSidewaysAndStaysInsideItsContent(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	r.write(t, strings.Repeat("0123456789", 12)+"\r\n")
	r.settle()
	r.pv.toggleSizeToContent()
	r.settle()
	a, _ := r.pv.atlases()
	over := padLeft + float64(r.pv.fixedCols)*a.m.w + scrollbarW - paneSize.Width
	if over <= 0 {
		t.Fatalf("the content is not wider than the pane: %v", over)
	}
	tests := []struct {
		name string
		by   float64
		want float64
	}{
		{"scrolls right", 40, 40},
		{"does not go past the end", 100000, over},
		{"scrolls back left", -30, over - 30},
		{"does not go before the start", -100000, 0},
	}
	for _, tt := range tests {
		r.pv.scrollX(tt.by)
		if r.pv.hscroll < tt.want-0.01 || r.pv.hscroll > tt.want+0.01 {
			t.Errorf("%s: hscroll = %v, want %v", tt.name, r.pv.hscroll, tt.want)
		}
	}
	// a click at the left of the view, when it is scrolled by n columns, is in the cell n columns further
	r.pv.scrollX(a.m.w * 10)
	if col, _ := r.pv.cellAt(gui.Offset{Dx: padLeft + a.m.w/2, Dy: 4}); col != 10 {
		t.Errorf("the first visible column after a scroll of 10 columns is %d", col)
	}
	// a pane that is not wide does not scroll
	r.pv.toggleSizeToContent()
	r.settle()
	r.pv.scrollX(50)
	if r.pv.hscroll != 0 {
		t.Errorf("a narrow pane scrolled sideways by %v", r.pv.hscroll)
	}
}
