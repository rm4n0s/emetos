package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/tokens"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// A repaint is a call of the view model's listeners: the editor sets its state, which is a frame. A keystroke in a tokenized line is
// one repaint, the one the edit itself makes: the tokens the frame needs are made in the frame, and the view model is not told
// about them (a second frame for every key of a held key costs more than the period of its repeat under a software renderer).
// A view that did not make the edit, and lines the background loop tokenized, repaint.
func TestTokensRepaintOnlyForWhatTheFrameDidNotDraw(t *testing.T) {
	var text strings.Builder
	for range 30 {
		text.WriteString("var x = 1 // a line\n")
	}
	tests := []struct {
		name string
		// slow leaves the lines of the screen but the first to the background loop (the frame's budget is spent at once).
		slow bool
		// act does what the test is about, after the editor is shown and settled.
		act func(t *testing.T, vm, other *ViewModel)
		// own and others are the repaints the editor and a second view of the document should make.
		own, others func(n int) bool
	}{
		{
			name: "a key typed in a tokenized line",
			act:  func(t *testing.T, vm, _ *ViewModel) { typeX(t, vm) },
			own:  func(n int) bool { return n == 1 },
			// the second view is not in a window, but it shows the lines
			others: func(n int) bool { return n >= 1 },
		},
		{
			name: "two keys typed one after the other",
			act: func(t *testing.T, vm, _ *ViewModel) {
				typeX(t, vm)
				typeX(t, vm)
			},
			own:    func(n int) bool { return n == 2 },
			others: func(n int) bool { return n >= 1 },
		},
		{
			name:   "the background loop tokenizes the lines on the screen",
			slow:   true,
			act:    func(t *testing.T, vm, _ *ViewModel) { vm.Tokenizer().Settle() },
			own:    func(n int) bool { return n >= 1 },
			others: func(n int) bool { return n >= 1 },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm, tok := highlighted(t, text.String())
			if tt.slow {
				vm.tok.budget = time.Nanosecond
			}
			ts := testutil.New(t, ui.Size{Width: 500, Height: 300})
			pumpHighlighted(t, ts, vm, kit.DarkModern(), darkModernTokens(t))
			for range 3 {
				ts.Pump(16 * time.Millisecond)
			}
			if !tt.slow {
				tok.Settle()
				ts.Pump(16 * time.Millisecond)
				ts.Pump(16 * time.Millisecond)
			}
			// a second view of the document, as a split editor has: it shows the first lines
			other := NewViewModel(vm.Document(), goOptions(), nil)
			other.SetTokenizer(tok)
			other.tok.first, other.tok.last, other.tok.visible = 0, 20, true

			var own, others int
			vm.AddListener(func() { own++ })
			other.AddListener(func() { others++ })
			tt.act(t, vm, other)
			for range 3 {
				ts.Pump(16 * time.Millisecond)
			}
			if !tt.own(own) {
				t.Errorf("the editor repainted %d times", own)
			}
			if !tt.others(others) {
				t.Errorf("the second view repainted %d times", others)
			}
			noErrors(t, ts)
		})
	}
}

func typeX(t *testing.T, vm *ViewModel) {
	t.Helper()
	if err := vm.RunCommand("type", "x"); err != nil {
		t.Fatal(err)
	}
}

// tokenizedPrefix is how many lines from the first have tokens.
func tokenizedPrefix(vm *ViewModel, n int) int {
	for i := 0; i < n; i++ {
		if vm.lineTokens(i) == nil {
			return i
		}
	}
	return n
}

// The lines a frame could not tokenize within its budget are tokenized by the frames that follow, each repainting for the next,
// when no background loop runs (it does in the application, and tells the view model what it tokenized). This holds for the lines
// the editor draws, and for the lines a zone draws from the view model of another document, which the zone is told about by the
// listeners of that view model: the frame that ensures them is not the only one that needs to know.
func TestLinesLeftByTheBudgetOfAFrameAreTokenizedByTheNext(t *testing.T) {
	var text strings.Builder
	for range 3000 {
		text.WriteString("var x = 1 // a line\n")
	}
	tests := []struct {
		name string
		// show puts the lines on the screen.
		show func(t *testing.T, ts *gluttertest.Tester, vm *ViewModel)
		// reach is how many lines the frames go on tokenizing up to; complete says they must get there (the lines of the editor are
		// few), else it is enough that every few frames tokenize more (400 lines take longer than a test should wait under the
		// race detector).
		reach    int
		complete bool
	}{
		{
			name: "the lines of the editor",
			show: func(t *testing.T, ts *gluttertest.Tester, vm *ViewModel) {
				vm.tok.budget = time.Microsecond
				pumpHighlighted(t, ts, vm, kit.DarkModern(), darkModernTokens(t))
			},
			reach: 6, complete: true,
		},
		{
			name: "the lines of a zone",
			show: func(t *testing.T, ts *gluttertest.Tester, vm *ViewModel) {
				vm.SetPoster(ts.Binding.Post)
				ts.PumpWidget(kit.NewTheme(kit.DarkModern(), widgets.NewDirectionality(ui.TextDirectionLTR,
					widgets.NewSizedBox(NewLinesView(LinesViewOpts{Source: vm, FirstLine: 400, Count: 5}), widgets.SizedBoxOpts{
						Width: foundation.Some(400.0), Height: foundation.Some(100.0),
					})), kit.ThemeOpts{Tokens: darkModernTokens(t)}))
			},
			reach: 405,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm, _ := highlighted(t, text.String())
			ts := testutil.New(t, ui.Size{Width: 500, Height: 300})
			tt.show(t, ts, vm)
			ts.Pump(16 * time.Millisecond)
			early := tokenizedPrefix(vm, tt.reach)
			for range 150 {
				ts.Pump(16 * time.Millisecond)
				if tokenizedPrefix(vm, tt.reach) >= tt.reach {
					break
				}
			}
			if got := tokenizedPrefix(vm, tt.reach); got <= early || (tt.complete && got < tt.reach) {
				t.Errorf("%d lines have tokens after the first frames and %d after more of them, want more (up to %d)", early, got, tt.reach)
			}
			noErrors(t, ts)
		})
	}
}

// A document with no grammar has nothing to tokenize: its frames ask for no repaint (an editor of plain text that repainted for
// the lines it could not tokenize would draw frames for ever).
func TestAnEditorWithNoGrammarDoesNotRepaintForItsTokens(t *testing.T) {
	doc := newTestDoc("one\ntwo\nthree\n")
	tok := tokens.New(doc, nil, tokens.Options{})
	t.Cleanup(tok.Close)
	vm := NewViewModel(doc, goOptions(), nil)
	vm.SetTokenizer(tok)
	ts := testutil.New(t, ui.Size{Width: 400, Height: 200})
	pumpHighlighted(t, ts, vm, kit.DarkModern(), darkModernTokens(t))
	for range 3 {
		ts.Pump(16 * time.Millisecond)
	}
	repaints := 0
	vm.AddListener(func() { repaints++ })
	for range 20 {
		ts.Pump(16 * time.Millisecond)
	}
	if repaints != 0 {
		t.Errorf("the editor repainted %d times with nothing to tokenize", repaints)
	}
	noErrors(t, ts)
}
