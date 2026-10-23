package find

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// The tests of findModel.test.ts that build their text or use the document's API in a way the
// converter does not follow, written by hand.

func TestFindModelListensToModelContentChanges(t *testing.T) {
	h := newFindHarness(t, sampleText)
	state := NewFindState()
	state.Change(NewState{SearchString: Some("hello"), ReplaceString: Some("hi"), WholeWord: Some(true)}, false, true)
	model := h.newModel(state)
	h.assertFindState(span{1, 1, 1, 1}, nil, []span{{6, 14, 6, 19}, {6, 27, 6, 32}, {7, 14, 7, 19}, {8, 14, 8, 19}})
	h.doc.SetText("hello\nhi")
	h.assertFindState(span{1, 1, 1, 1}, nil, nil)
	// the matches are searched again soon after
	h.flush()
	h.assertFindState(span{1, 1, 1, 1}, nil, []span{{1, 1, 1, 6}})
	model.Dispose()
	state.Dispose()
}

func TestFindModelReplaceAllWithCaretOnMoreThanAThousandMatches(t *testing.T) {
	// issue #32522
	var text, want strings.Builder
	for i := range 1100 {
		fmt.Fprintf(&text, "line%d\n", i)
		fmt.Fprintf(&want, "a line%d\n", i)
	}
	want.WriteString("a ")
	h := newFindHarness(t, text.String())
	state := NewFindState()
	state.Change(NewState{SearchString: Some("^"), ReplaceString: Some("a "), IsRegex: Some(true)}, false, true)
	model := h.newModel(state)
	h.must(model.ReplaceAll())
	if got := h.doc.Snapshot().String(); got != want.String() {
		t.Errorf("the text after replacing all is wrong")
	}
	model.Dispose()
	state.Dispose()
}

func TestFindModelMatchesPositionAfterAThousandMatches(t *testing.T) {
	// issue #288515: the position of the current match is right with more than a thousand matches
	h := newFindHarness(t, strings.Repeat("hello\n", 1000)+"hello")
	h.setSelection(rng(900, 1, 900, 6))
	state := NewFindState()
	state.Change(NewState{SearchString: Some("hello")}, false, true)
	model := h.newModel(state)
	h.eqInt("matchesCount", state.MatchesCount(), 1001)
	h.eqInt("matchesPosition", state.MatchesPosition(), 900)
	model.Dispose()
}

func TestLargeReplaceAllOfMoreThanTheLimit(t *testing.T) {
	// more matches than are marked: the whole text is replaced at once
	text := strings.Repeat("ab abc\n", MatchesLimit/2+10)
	h := newFindHarness(t, text)
	state := NewFindState()
	state.Change(NewState{SearchString: Some("ab"), ReplaceString: Some("X"), WholeWord: Some(true)}, false, true)
	model := h.newModel(state)
	h.eqInt("matchesCount", state.MatchesCount(), MatchesLimit/2+10)
	// make the count reach the limit with a regular expression that matches twice a line
	state.Change(NewState{SearchString: Some("(?:a|b)"), ReplaceString: Some("-"), IsRegex: Some(true), WholeWord: Some(false)}, false, true)
	if state.MatchesCount() < MatchesLimit {
		t.Fatalf("only %d matches", state.MatchesCount())
	}
	h.must(model.ReplaceAll())
	if got := h.doc.Snapshot().String(); strings.ContainsAny(got, "ab") || !strings.HasPrefix(got, "-- --c\n") {
		t.Errorf("the large replace left %q…", got[:20])
	}
	model.Dispose()
}

func TestFindModelFollowsTheUsersCursor(t *testing.T) {
	h := newFindHarness(t, "foo\nfoo\nfoo\nfoo")
	state := NewFindState()
	state.Change(NewState{SearchString: Some("foo")}, false, true)
	model := h.newModel(state)
	model.MoveToNextMatch()
	h.assertFindState(span{1, 1, 1, 4}, &span{1, 1, 1, 4}, []span{{1, 1, 1, 4}, {2, 1, 2, 4}, {3, 1, 3, 4}, {4, 1, 4, 4}})
	h.moveTo(3, 2) // a click: the search goes on from there
	model.MoveToNextMatch()
	h.assertPrimary(span{4, 1, 4, 4})
	// typing the search string moves to the first match after the cursor (editor.find.cursorMoveOnType)
	h.moveTo(2, 2)
	state.Change(NewState{SearchString: Some("fo")}, true, true)
	h.assertPrimary(span{3, 1, 3, 3})
	h.moveOnType = false
	h.moveTo(2, 2)
	state.Change(NewState{SearchString: Some("f")}, true, true)
	h.assertPrimary(span{2, 2, 2, 2})
	model.Dispose()
	if got := len(h.doc.DecorationsOf(testOwner)); got != 0 {
		t.Errorf("%d decorations are left", got)
	}
}

func TestFindModelReplaceInAReadOnlyDocumentFails(t *testing.T) {
	h := newFindHarness(t, "foo\nfoo")
	h.doc.SetReadOnly(true)
	state := NewFindState()
	state.Change(NewState{SearchString: Some("foo"), ReplaceString: Some("bar")}, false, true)
	model := h.newModel(state)
	model.MoveToNextMatch() // select the first
	for name, err := range map[string]error{"Replace": model.Replace(), "ReplaceAll": model.ReplaceAll()} {
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("%s: expected *errors.Error, got %T (%v)", name, err, err)
		}
		if !appErr.HasRoute("Document.checkWritable.DocumentReadOnly") && !appErr.HasRoute("DocumentReadOnly") {
			t.Errorf("%s: unexpected failure path: %s", name, appErr.Route())
		}
	}
	model.Dispose()
}

func TestFindModelSearchScopeMovesWithEdits(t *testing.T) {
	h := newFindHarness(t, "foo\nfoo\nfoo\nfoo")
	state := NewFindState()
	state.Change(NewState{SearchString: Some("foo"), SearchScope: Some([]buffer.Range{rng(2, 1, 3, 1)})}, false, true)
	model := h.newModel(state)
	h.assertFindState(span{1, 1, 1, 1}, nil, []span{{2, 1, 2, 4}})
	// an edit above the scope moves it
	h.must2(h.doc.Edit([]document.Edit{{Range: rng(1, 1, 1, 1), Text: "x\n"}}, document.EditOptions{}))
	h.flush()
	h.assertFindState(span{1, 1, 1, 1}, nil, []span{{3, 1, 3, 4}})
	model.Dispose()
}

func (h *findHarness) must2(_ document.EditResult, err error) {
	h.t.Helper()
	h.must(err)
}
