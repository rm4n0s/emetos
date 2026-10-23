package editor

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/fileio"
)

const highlightText = "foo bar foo\nbaz foo.foo\nFoo foobar\n"

// highlightVM makes a view model that follows the cursors and has a fake clock.
func highlightVM(t *testing.T, text string, tweak func(*Options)) (*ViewModel, *fakeTimers) {
	t.Helper()
	opts := testOptions()
	opts.CursorBlinking = "solid" // no timers of its own
	opts.WordSeparators = edit.DefaultWordSeparators
	opts.OccurrencesHighlight, opts.OccurrencesHighlightDelay = "singleFile", 250
	opts.SelectionHighlight, opts.SelectionHighlightMaxLength = true, 200
	if tweak != nil {
		tweak(&opts)
	}
	vm := NewViewModel(newTestDoc(text), opts, nil)
	clock := &fakeTimers{}
	vm.SetTimer(clock.after)
	t.Cleanup(vm.DisposeHighlights)
	return vm, clock
}

func occurrences(vm *ViewModel, owner int) []buffer.Range {
	var out []buffer.Range
	for _, d := range vm.Document().DecorationsOf(vm.owner(owner)) {
		out = append(out, d.Range)
	}
	slices.SortFunc(out, func(a, b buffer.Range) int { return a.Start.Compare(b.Start) })
	return out
}

func rg(l1, c1, l2, c2 int) buffer.Range {
	return buffer.Range{Start: buffer.Position{Line: l1, Col: c1}, End: buffer.Position{Line: l2, Col: c2}}
}

func selectText(vm *ViewModel, l1, c1, l2, c2 int) {
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: l1, Col: c1}, Active: buffer.Position{Line: l2, Col: c2}}})
}

func TestWordHighlight(t *testing.T) {
	foos := []buffer.Range{rg(0, 0, 0, 3), rg(0, 8, 0, 11), rg(1, 4, 1, 7), rg(1, 8, 1, 11)}
	vm, clock := highlightVM(t, highlightText, nil)
	cursorAt(vm, 0, 1)
	if got := occurrences(vm, wordHighlightOwner); len(got) != 0 {
		t.Fatalf("marked before the delay: %v", got)
	}
	clock.advance(249 * time.Millisecond)
	if got := occurrences(vm, wordHighlightOwner); len(got) != 0 {
		t.Fatalf("marked 1 ms early: %v", got)
	}
	clock.advance(time.Millisecond)
	if got := occurrences(vm, wordHighlightOwner); !slices.Equal(got, foos) {
		t.Fatalf("whole words, case sensitive: %v, want %v", got, foos)
	}

	// moving inside a marked word keeps the marks and starts no timer
	cursorAt(vm, 1, 5)
	if clock.pending() != 0 || !slices.Equal(occurrences(vm, wordHighlightOwner), foos) {
		t.Errorf("a cursor in another occurrence: %d timers, marks %v", clock.pending(), occurrences(vm, wordHighlightOwner))
	}

	// another word replaces them after the delay
	cursorAt(vm, 0, 5)
	if got := occurrences(vm, wordHighlightOwner); len(got) != 0 {
		t.Errorf("the old marks stay while waiting: %v", got)
	}
	clock.advance(250 * time.Millisecond)
	if got := occurrences(vm, wordHighlightOwner); !slices.Equal(got, []buffer.Range{rg(0, 4, 0, 7)}) {
		t.Errorf("bar: %v", got)
	}

	// a cursor with no word at it takes the marks away
	cursorAt(vm, 0, 3)
	cursorAt(vm, 0, 7)
	clock.advance(time.Second)
	cursorAt(vm, 0, 100)
	if got := occurrences(vm, wordHighlightOwner); len(got) != 0 {
		t.Errorf("at the end of the line, after a space: %v", got)
	}
}

func TestWordHighlightNeedsTheSelectionInOneWord(t *testing.T) {
	cases := []struct {
		name           string
		l1, c1, l2, c2 int
		want           int
	}{
		{"a cursor in a word", 0, 1, 0, 1, 4},
		{"a selection inside the word", 0, 0, 0, 2, 4},
		{"the whole word", 0, 0, 0, 3, 4},
		{"a selection past the word", 0, 0, 0, 5, 0},
		{"a selection over two lines", 0, 0, 1, 2, 0},
		{"in white space", 0, 3, 0, 3, 4}, // the cursor touches foo
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm, clock := highlightVM(t, highlightText, func(o *Options) { o.SelectionHighlight = false })
			selectText(vm, tc.l1, tc.c1, tc.l2, tc.c2)
			clock.advance(time.Second)
			if got := len(occurrences(vm, wordHighlightOwner)); got != tc.want {
				t.Errorf("%d marks, want %d: %v", got, tc.want, occurrences(vm, wordHighlightOwner))
			}
		})
	}
}

func TestWordHighlightStopsWhenTyping(t *testing.T) {
	vm, clock := highlightVM(t, highlightText, nil)
	cursorAt(vm, 0, 1)
	clock.advance(time.Second)
	if len(occurrences(vm, wordHighlightOwner)) != 4 {
		t.Fatal("no marks")
	}
	if err := vm.RunCommand("type", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	if got := occurrences(vm, wordHighlightOwner); len(got) != 0 {
		t.Errorf("marks after typing: %v", got)
	}
	clock.advance(time.Second)
	if got := occurrences(vm, wordHighlightOwner); len(got) != 0 {
		t.Errorf("marks came back by themselves: %v", got)
	}
	// a move that is not typing marks again
	cursorAt(vm, 1, 5)
	clock.advance(time.Second)
	// the typed letter made the first foo another word
	if got := occurrences(vm, wordHighlightOwner); len(got) != 3 {
		t.Errorf("after moving: %v", got)
	}
}

func TestWordHighlightSettings(t *testing.T) {
	cases := []struct {
		name  string
		tweak func(*Options)
		want  int
	}{
		{"on", nil, 4},
		{"off", func(o *Options) { o.OccurrencesHighlight = "off" }, 0},
		{"empty is off", func(o *Options) { o.OccurrencesHighlight = "" }, 0},
		{"multiFile marks this file", func(o *Options) { o.OccurrencesHighlight = "multiFile" }, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm, clock := highlightVM(t, highlightText, func(o *Options) {
				o.SelectionHighlight = false
				if tc.tweak != nil {
					tc.tweak(o)
				}
			})
			cursorAt(vm, 0, 1)
			clock.advance(time.Second)
			if got := len(occurrences(vm, wordHighlightOwner)); got != tc.want {
				t.Errorf("%d marks, want %d", got, tc.want)
			}
		})
	}
	t.Run("no delay marks at once", func(t *testing.T) {
		vm, _ := highlightVM(t, highlightText, func(o *Options) { o.OccurrencesHighlightDelay = 0 })
		cursorAt(vm, 0, 1)
		if got := len(occurrences(vm, wordHighlightOwner)); got != 4 {
			t.Errorf("%d marks", got)
		}
	})
	t.Run("no timers marks at once", func(t *testing.T) {
		vm, _ := highlightVM(t, highlightText, nil)
		vm.SetTimer(nil)
		cursorAt(vm, 0, 1)
		if got := len(occurrences(vm, wordHighlightOwner)); got != 4 {
			t.Errorf("%d marks", got)
		}
	})
	t.Run("large file mode", func(t *testing.T) {
		doc := document.FromRead(document.FileURI("/big.txt"), fileio.ReadResult{Text: highlightText, LineEnding: fileio.LF, LargeFile: true}, document.NewOptions())
		opts := testOptions()
		opts.CursorBlinking = "solid"
		vm := NewViewModel(doc, opts, nil)
		clock := &fakeTimers{}
		vm.SetTimer(clock.after)
		vm.opts.OccurrencesHighlight, vm.opts.SelectionHighlight = "singleFile", true
		t.Cleanup(vm.DisposeHighlights)
		cursorAt(vm, 0, 1)
		clock.advance(time.Second)
		if got := len(occurrences(vm, wordHighlightOwner)); got != 0 {
			t.Errorf("%d marks in a large file", got)
		}
	})
}

func TestWordHighlightFollowsEdits(t *testing.T) {
	vm, clock := highlightVM(t, highlightText, nil)
	cursorAt(vm, 0, 1)
	clock.advance(time.Second)
	// an edit that does not move the cursors takes the marks away
	if _, err := vm.Document().Edit([]document.Edit{{Range: rg(2, 0, 2, 0), Text: "zz"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := occurrences(vm, wordHighlightOwner); len(got) != 0 {
		t.Errorf("marks after an edit: %v", got)
	}
}

func TestSelectionHighlight(t *testing.T) {
	vm, _ := highlightVM(t, highlightText, nil)
	selectText(vm, 0, 0, 0, 3)
	want := []buffer.Range{rg(0, 8, 0, 11), rg(1, 4, 1, 7), rg(1, 8, 1, 11), rg(2, 0, 2, 3), rg(2, 4, 2, 7)}
	if got := occurrences(vm, selectionHighlightOwner); !slices.Equal(got, want) {
		t.Errorf("without case and parts of words: %v, want %v", got, want)
	}
	// a cursor has none
	cursorAt(vm, 0, 1)
	if got := occurrences(vm, selectionHighlightOwner); len(got) != 0 {
		t.Errorf("a cursor: %v", got)
	}
	// more selections of the same text
	vm.Cursors().SetSelections([]buffer.Selection{
		{Anchor: buffer.Position{Line: 0, Col: 0}, Active: buffer.Position{Line: 0, Col: 3}},
		{Anchor: buffer.Position{Line: 1, Col: 4}, Active: buffer.Position{Line: 1, Col: 7}},
	})
	if got := occurrences(vm, selectionHighlightOwner); len(got) != 4 {
		t.Errorf("two selections: %v", got)
	}
	// other text
	vm.Cursors().SetSelections([]buffer.Selection{
		{Anchor: buffer.Position{Line: 0, Col: 0}, Active: buffer.Position{Line: 0, Col: 3}},
		{Anchor: buffer.Position{Line: 0, Col: 4}, Active: buffer.Position{Line: 0, Col: 7}},
	})
	if got := occurrences(vm, selectionHighlightOwner); len(got) != 0 {
		t.Errorf("different texts: %v", got)
	}
}

func TestSelectionHighlightSettings(t *testing.T) {
	cases := []struct {
		name           string
		tweak          func(*Options)
		l1, c1, l2, c2 int
		want           int
	}{
		{"on", nil, 0, 0, 0, 3, 5},
		{"off", func(o *Options) { o.SelectionHighlight = false }, 0, 0, 0, 3, 0},
		{"too long", func(o *Options) { o.SelectionHighlightMaxLength = 2 }, 0, 0, 0, 3, 0},
		{"no limit", func(o *Options) { o.SelectionHighlightMaxLength = 0 }, 0, 0, 0, 3, 5},
		{"over lines", nil, 0, 8, 1, 3, 0},
		{"over lines when allowed", func(o *Options) { o.SelectionHighlightMultiline = true }, 0, 8, 1, 3, 0},
		{"only spaces", nil, 0, 3, 0, 4, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm, _ := highlightVM(t, highlightText, func(o *Options) {
				o.OccurrencesHighlight = "off"
				if tc.tweak != nil {
					tc.tweak(o)
				}
			})
			selectText(vm, tc.l1, tc.c1, tc.l2, tc.c2)
			if got := len(occurrences(vm, selectionHighlightOwner)); got != tc.want {
				t.Errorf("%d marks, want %d: %v", got, tc.want, occurrences(vm, selectionHighlightOwner))
			}
		})
	}
}

func TestSelectionHighlightUsesTheFindSwitches(t *testing.T) {
	vm, _ := highlightVM(t, highlightText, nil)
	vm.find = edit.SearchOptions{MatchCase: true, WholeWord: true}
	selectText(vm, 0, 0, 0, 3)
	want := []buffer.Range{rg(0, 8, 0, 11), rg(1, 4, 1, 7), rg(1, 8, 1, 11)}
	if got := occurrences(vm, selectionHighlightOwner); !slices.Equal(got, want) {
		t.Errorf("whole words with case: %v, want %v", got, want)
	}
}

func TestSelectionHighlightLeavesTheFindMatchesAlone(t *testing.T) {
	vm, _ := highlightVM(t, highlightText, nil)
	if err := vm.RunCommand("editor.actions.findWithArgs", map[string]any{"searchString": "foo", "matchCase": false}); err != nil {
		t.Fatal(err)
	}
	selectText(vm, 0, 0, 0, 3)
	if got := occurrences(vm, selectionHighlightOwner); len(got) != 0 {
		t.Errorf("the find widget looks for the same text: %v", got)
	}
	selectText(vm, 0, 4, 0, 7)
	if got := occurrences(vm, selectionHighlightOwner); !slices.Equal(got, []buffer.Range{rg(2, 7, 2, 10)}) {
		t.Errorf("other text than the find widget's: %v", got)
	}
	vm.closeFind()
	selectText(vm, 0, 0, 0, 3)
	if got := occurrences(vm, selectionHighlightOwner); len(got) != 5 {
		t.Errorf("the widget is closed: %v", got)
	}
}

func TestSelectionHighlightFollowsEdits(t *testing.T) {
	vm, clock := highlightVM(t, highlightText, func(o *Options) { o.OccurrencesHighlight = "off" })
	selectText(vm, 0, 0, 0, 3)
	if len(occurrences(vm, selectionHighlightOwner)) != 5 {
		t.Fatal("no marks")
	}
	if _, err := vm.Document().Edit([]document.Edit{{Range: rg(2, 0, 2, 3), Text: "bar"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	// the marks move with the text until the highlight is made again
	if clock.pending() != 1 {
		t.Fatalf("%d timers after an edit, want 1", clock.pending())
	}
	clock.advance(299 * time.Millisecond)
	if len(occurrences(vm, selectionHighlightOwner)) != 5 {
		t.Errorf("made again before the delay")
	}
	clock.advance(time.Millisecond)
	if got := len(occurrences(vm, selectionHighlightOwner)); got != 4 {
		t.Errorf("after the edit: %d marks, want 4", got)
	}
	if clock.pending() != 0 {
		t.Errorf("%d timers left", clock.pending())
	}
}

func TestDisposeHighlights(t *testing.T) {
	vm, clock := highlightVM(t, highlightText, nil)
	cursorAt(vm, 0, 1)
	vm.DisposeHighlights()
	if clock.pending() != 0 {
		t.Errorf("%d timers after disposing", clock.pending())
	}
	selectText(vm, 0, 0, 0, 3)
	vm.DisposeHighlights()
	if len(vm.Document().DecorationsOf(vm.owner(selectionHighlightOwner)))+len(vm.Document().DecorationsOf(vm.owner(wordHighlightOwner))) != 0 {
		t.Error("decorations after disposing")
	}
	// disposing twice is fine, and so is a document with no highlights
	vm.DisposeHighlights()
	vm2, _ := highlightVM(t, strings.Repeat("x\n", 3), nil)
	vm2.DisposeHighlights()
}

func TestHighlightsFollowTheSettings(t *testing.T) {
	vm, clock := highlightVM(t, highlightText, nil)
	selectText(vm, 0, 0, 0, 3)
	clock.advance(time.Second)
	if len(occurrences(vm, wordHighlightOwner)) == 0 || len(occurrences(vm, selectionHighlightOwner)) == 0 {
		t.Fatal("no marks")
	}
	opts := vm.Options()
	opts.OccurrencesHighlight = "off"
	vm.SetOptions(opts)
	if got := occurrences(vm, wordHighlightOwner); len(got) != 0 {
		t.Errorf("word marks after turning it off: %v", got)
	}
	opts.SelectionHighlight = false
	vm.SetOptions(opts)
	if got := occurrences(vm, selectionHighlightOwner); len(got) != 0 {
		t.Errorf("selection marks after turning it off: %v", got)
	}
	opts.SelectionHighlight, opts.OccurrencesHighlight = true, "singleFile"
	vm.SetOptions(opts)
	clock.advance(time.Second)
	if len(occurrences(vm, wordHighlightOwner)) == 0 || len(occurrences(vm, selectionHighlightOwner)) == 0 {
		t.Errorf("no marks after turning them on: %v %v", occurrences(vm, wordHighlightOwner), occurrences(vm, selectionHighlightOwner))
	}
}
