package find

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// The sample text of findModel.test.ts.
var sampleText = strings.Join([]string{
	"// my cool header",
	`#include "cool.h"`,
	"#include <iostream>",
	"",
	"int main() {",
	`    cout << "hello world, Hello!" << endl;`,
	`    cout << "hello world again" << endl;`,
	`    cout << "Hello world again" << endl;`,
	`    cout << "helloworld again" << endl;`,
	"}",
	"// blablablaciao",
	"",
}, "\n")

// testOwner is the owner of the decorations of the model in the tests.
const testOwner = 7

// findHarness is an editor for a find model: a document, a selection and a clock of its own.
type findHarness struct {
	t        *testing.T
	doc      *document.Document
	sels     []buffer.Selection
	revealed []buffer.Range
	timers   []*timer
	model    *Model
	// moveOnType is editor.find.cursorMoveOnType.
	moveOnType bool
}

type timer struct {
	fn        func()
	cancelled bool
}

func newFindHarness(t *testing.T, text string) *findHarness {
	t.Helper()
	return &findHarness{
		t: t, doc: document.New(document.UntitledURI(1), text, document.NewOptions()),
		sels: []buffer.Selection{buffer.NewCursor(buffer.Position{})}, moveOnType: true,
	}
}

// Host.
func (h *findHarness) Selection() buffer.Selection     { return h.sels[0] }
func (h *findHarness) SetSelection(s buffer.Selection) { h.sels = []buffer.Selection{s} }
func (h *findHarness) SetSelections(s []buffer.Selection) {
	h.sels = slices.Clone(s)
}
func (h *findHarness) Reveal(r buffer.Range)  { h.revealed = append(h.revealed, r) }
func (h *findHarness) WordSeparators() string { return edit.DefaultWordSeparators }
func (h *findHarness) CursorMoveOnType() bool { return h.moveOnType }
func (h *findHarness) After(_ time.Duration, fn func()) func() {
	tm := &timer{fn: fn}
	h.timers = append(h.timers, tm)
	return func() { tm.cancelled = true }
}

// flush runs the timers that are waiting.
func (h *findHarness) flush() {
	timers := h.timers
	h.timers = nil
	for _, tm := range timers {
		if !tm.cancelled {
			tm.fn()
		}
	}
}

func (h *findHarness) newModel(state *State) *Model {
	h.model = NewModel(h, h.doc, state, testOwner)
	return h.model
}

func (h *findHarness) must(err error) {
	h.t.Helper()
	if err != nil {
		h.t.Fatal(err)
	}
}

// setPosition puts the cursor at a one-based position of the (ASCII) text, as the API does: the
// find model is not told that the user moved it.
func (h *findHarness) setPosition(line, col int) {
	h.sels = []buffer.Selection{buffer.NewCursor(at(line, col))}
}

func (h *findHarness) setSelection(r buffer.Range) {
	h.sels = []buffer.Selection{{Anchor: r.Start, Active: r.End}}
}

// moveTo is a click: the user puts the cursor there.
func (h *findHarness) moveTo(line, col int) {
	h.setPosition(line, col)
	if h.model != nil {
		h.model.CursorMoved(true)
	}
}

func spanOf(r buffer.Range) span {
	return span{r.Start.Line + 1, r.Start.Col + 1, r.End.Line + 1, r.End.Col + 1}
}

// assertFindState checks the cursor, the match that is highlighted (nil for none) and the matches.
func (h *findHarness) assertFindState(cursor span, highlighted *span, matches []span) {
	h.t.Helper()
	if got := spanOf(h.Selection().Range()); got != cursor {
		h.t.Errorf("cursor %v, want %v", got, cursor)
	}
	var current, all []span
	var currentRanges, allRanges []buffer.Range
	for _, d := range h.doc.AllDecorations() {
		switch d.Options.ClassName {
		case ClassCurrentFindMatch:
			currentRanges = append(currentRanges, d.Range)
			allRanges = append(allRanges, d.Range)
		case ClassFindMatch:
			allRanges = append(allRanges, d.Range)
		}
	}
	slices.SortFunc(currentRanges, compareRangesUsingStarts)
	slices.SortFunc(allRanges, compareRangesUsingStarts)
	for _, r := range currentRanges {
		current = append(current, spanOf(r))
	}
	for _, r := range allRanges {
		all = append(all, spanOf(r))
	}
	var wantCurrent []span
	if highlighted != nil {
		wantCurrent = []span{*highlighted}
	}
	if !slices.Equal(current, wantCurrent) {
		h.t.Errorf("highlighted %v, want %v", current, wantCurrent)
	}
	if !slices.Equal(all, matches) && (len(all) != 0 || len(matches) != 0) {
		h.t.Errorf("matches %v, want %v", all, matches)
	}
}

func (h *findHarness) assertLine(line int, want string) {
	h.t.Helper()
	got, err := h.doc.Snapshot().LineContent(line - 1)
	if err != nil || got != want {
		h.t.Errorf("line %d is %q, want %q (%v)", line, got, want, err)
	}
}

func (h *findHarness) eqInt(what string, got, want int) {
	h.t.Helper()
	if got != want {
		h.t.Errorf("%s = %d, want %d", what, got, want)
	}
}

func (h *findHarness) eqBool(what string, got, want bool) {
	h.t.Helper()
	if got != want {
		h.t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func (h *findHarness) assertSelections(want ...span) {
	h.t.Helper()
	var got []span
	for _, s := range h.sels {
		got = append(got, spanOf(s.Range()))
	}
	if !slices.Equal(got, want) {
		h.t.Errorf("selections %v, want %v", got, want)
	}
}

func (h *findHarness) assertPrimary(want span) {
	h.t.Helper()
	if got := spanOf(h.Selection().Range()); got != want {
		h.t.Errorf("primary selection %v, want %v", got, want)
	}
}
