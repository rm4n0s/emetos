package mergeeditor

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/document"
)

func TestSessionAccept(t *testing.T) {
	for _, tc := range []struct {
		name     string
		conflict int
		choice   diff.Choice
		text     string
		state    diff.RangeState
		changed  bool
	}{
		{"incoming, first conflict", 0, diff.AcceptInput1, "a\nincoming1\nb\nc", diff.RangeState{Kind: diff.StateInput1}, true},
		{"current, first conflict", 0, diff.AcceptInput2, "a\ncurrent1\nb\nc", diff.RangeState{Kind: diff.StateInput2}, true},
		{"both, first conflict: current then incoming", 0, diff.AcceptBoth, "a\ncurrent1\nincoming1\nb\nc", diff.BothState(diff.Input2), true},
		{"base, first conflict: the base has no line there", 0, diff.AcceptBase, "a\nb\nc", diff.RangeState{}, false},
		{"incoming, second conflict", 1, diff.AcceptInput1, "a\nb\ninc2\nc", diff.RangeState{Kind: diff.StateInput1}, true},
		{"current, second conflict", 1, diff.AcceptInput2, "a\nb\ncur2\nc", diff.RangeState{Kind: diff.StateInput2}, true},
		{"both, second conflict", 1, diff.AcceptBoth, "a\nb\ncur2\ninc2\nc", diff.BothState(diff.Input2), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, doc := newMarkerSession(t, twoConflicts, InitialReplace)
			if err := s.Accept(tc.conflict, tc.choice); err != nil {
				t.Fatal(err)
			}
			if doc.Text() != tc.text {
				t.Errorf("the document is %q, want %q", doc.Text(), tc.text)
			}
			conflicts := s.Model().Conflicts()
			if c := conflicts[tc.conflict]; c.State != tc.state || !c.Handled {
				t.Errorf("the conflict is %+v", c)
			}
			if other := conflicts[1-tc.conflict]; other.Handled || other.State.Kind != diff.StateBase {
				t.Errorf("the other conflict is %+v", other)
			}
			if got := s.Model().UnhandledCount(); got != 1 {
				t.Errorf("%d conflicts are unhandled", got)
			}
			// the regions follow: the lines of the conflict are the lines the document has there
			lines := strings.Split(doc.Text(), "\n")
			c := conflicts[tc.conflict]
			if !slices.Equal(lines[c.Result.StartLine-1:c.Result.EndLine-1], s.Model().Result()[c.Result.StartLine-1:c.Result.EndLine-1]) {
				t.Errorf("the region %v does not hold its lines", c.Result)
			}
			if s.Resyncs() != 0 {
				t.Errorf("%d resyncs", s.Resyncs())
			}
			if !tc.changed {
				return
			}
			// one undo takes the accept back: the text, the state and the handled flag
			if _, err := doc.Undo(); err != nil {
				t.Fatal(err)
			}
			if doc.Text() != "a\nb\nc" {
				t.Errorf("one undo gave %q", doc.Text())
			}
			if c := s.Model().Conflicts()[tc.conflict]; c.State.Kind != diff.StateBase || c.Handled || s.Model().UnhandledCount() != 2 {
				t.Errorf("after the undo the conflict is %+v, %d unhandled", c, s.Model().UnhandledCount())
			}
			// and one redo does it again
			if _, err := doc.Redo(); err != nil {
				t.Fatal(err)
			}
			if c := s.Model().Conflicts()[tc.conflict]; doc.Text() != tc.text || c.State != tc.state || !c.Handled || s.Model().UnhandledCount() != 1 {
				t.Errorf("after the redo %q, the conflict is %+v, %d unhandled", doc.Text(), c, s.Model().UnhandledCount())
			}
			if s.Resyncs() != 0 {
				t.Errorf("%d resyncs after the undo and the redo", s.Resyncs())
			}
		})
	}
}

func TestSessionAcceptAll(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before func(*testing.T, *Session)
		choice diff.Choice
		text   string
		undone string
	}{
		{"incoming", nil, diff.AcceptInput1, "a\nincoming1\nb\ninc2\nc", "a\nb\nc"},
		{"current", nil, diff.AcceptInput2, "a\ncurrent1\nb\ncur2\nc", "a\nb\nc"},
		{"both", nil, diff.AcceptBoth, "a\ncurrent1\nincoming1\nb\ncur2\ninc2\nc", "a\nb\nc"},
		{"only the conflicts that are not handled", func(t *testing.T, s *Session) {
			if err := s.Accept(0, diff.AcceptInput2); err != nil {
				t.Fatal(err)
			}
		}, diff.AcceptInput1, "a\ncurrent1\nb\ninc2\nc", "a\ncurrent1\nb\nc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, doc := newMarkerSession(t, twoConflicts, InitialReplace)
			if tc.before != nil {
				tc.before(t, s)
			}
			if err := s.AcceptAll(tc.choice); err != nil {
				t.Fatal(err)
			}
			if doc.Text() != tc.text || s.Model().UnhandledCount() != 0 {
				t.Errorf("the document is %q with %d unhandled, want %q with none", doc.Text(), s.Model().UnhandledCount(), tc.text)
			}
			if _, err := doc.Undo(); err != nil {
				t.Fatal(err)
			}
			wantUnhandled := 2
			if tc.before != nil {
				wantUnhandled = 1
			}
			if doc.Text() != tc.undone || s.Model().UnhandledCount() != wantUnhandled {
				t.Errorf("one undo gave %q with %d unhandled, want %q with %d", doc.Text(), s.Model().UnhandledCount(), tc.undone, wantUnhandled)
			}
			if s.Resyncs() != 0 {
				t.Errorf("%d resyncs", s.Resyncs())
			}
		})
	}
}

func TestSessionAcceptAllWithNothingLeft(t *testing.T) {
	s, doc := newMarkerSession(t, twoConflicts, InitialReplace)
	if err := s.AcceptAll(diff.AcceptInput1); err != nil {
		t.Fatal(err)
	}
	version := doc.Version()
	if err := s.AcceptAll(diff.AcceptInput2); err != nil {
		t.Fatal(err)
	}
	if doc.Version() != version || doc.Text() != "a\nincoming1\nb\ninc2\nc" {
		t.Errorf("a second accept all changed the document to %q", doc.Text())
	}
}

func TestSessionAcceptAllCombination(t *testing.T) {
	s, doc := newMarkerSession(t, twoConflicts, InitialReplace)
	if err := s.Accept(1, diff.AcceptInput1); err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptAllCombination(); err != nil {
		t.Fatal(err)
	}
	// the conflict that has the incoming lines stays, the other gets both, incoming first
	if want := "a\nincoming1\ncurrent1\nb\ninc2\nc"; doc.Text() != want {
		t.Errorf("the document is %q, want %q", doc.Text(), want)
	}
	if c := s.Model().Conflicts()[0]; c.State != diff.BothState(diff.Input1) || !c.Handled {
		t.Errorf("the first conflict is %+v", c)
	}
	if _, err := doc.Undo(); err != nil {
		t.Fatal(err)
	}
	if doc.Text() != "a\nb\ninc2\nc" || s.Model().UnhandledCount() != 1 {
		t.Errorf("one undo gave %q with %d unhandled", doc.Text(), s.Model().UnhandledCount())
	}
}

func TestSessionResetResult(t *testing.T) {
	s, doc := newMarkerSession(t, twoConflicts, InitialReplace)
	if err := s.AcceptAll(diff.AcceptBoth); err != nil {
		t.Fatal(err)
	}
	typeAt(t, doc, 0, 1, "!") // an edit outside the conflicts stays
	if err := s.ResetResult(); err != nil {
		t.Fatal(err)
	}
	if doc.Text() != "a!\nb\nc" || s.Model().UnhandledCount() != 2 {
		t.Errorf("after the reset the document is %q with %d unhandled", doc.Text(), s.Model().UnhandledCount())
	}
	for i, c := range s.Model().Conflicts() {
		if c.State.Kind != diff.StateBase || c.Handled {
			t.Errorf("conflict %d is %+v", i, c)
		}
	}
	// the reset is one undo step
	if _, err := doc.Undo(); err != nil {
		t.Fatal(err)
	}
	if doc.Text() != "a!\ncurrent1\nincoming1\nb\ncur2\ninc2\nc" || s.Model().UnhandledCount() != 0 {
		t.Errorf("one undo gave %q with %d unhandled", doc.Text(), s.Model().UnhandledCount())
	}
	// a reset of a merge that has nothing to reset changes nothing
	if _, err := doc.Redo(); err != nil {
		t.Fatal(err)
	}
	version := doc.Version()
	if err := s.ResetResult(); err != nil {
		t.Fatal(err)
	}
	if doc.Version() != version {
		t.Error("a reset of a result that is reset changed the document")
	}
}

func TestSessionManualEdits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*testing.T, *Session)
		edit    func(*testing.T, *document.Document)
		text    string
		state   diff.StateKind
		handled bool
		// first is the range the first conflict has in the result afterwards, second the line the second one starts at
		first  diff.LineRange
		second int
	}{
		{"typing inside a resolved conflict leaves it handled and none of the states", func(t *testing.T, s *Session) {
			if err := s.Accept(0, diff.AcceptInput1); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, d *document.Document) { typeAt(t, d, 1, 3, "X") },
			"a\nincXoming1\nb\nc", diff.StateUnrecognized, true, diff.LineRange{StartLine: 2, EndLine: 3}, 4},
		{"typing a line into an unhandled conflict does not handle it", nil,
			func(t *testing.T, d *document.Document) { typeAt(t, d, 1, 0, "manual\n") },
			"a\nmanual\nb\nc", diff.StateUnrecognized, false, diff.LineRange{StartLine: 2, EndLine: 3}, 4},
		{"typing outside the conflicts moves them", nil,
			func(t *testing.T, d *document.Document) { typeAt(t, d, 0, 0, "top\n") },
			"top\na\nb\nc", diff.StateBase, false, diff.LineRange{StartLine: 3, EndLine: 3}, 4},
		{"typing the lines of an input makes the state of that input, and does not handle the conflict", nil,
			func(t *testing.T, d *document.Document) { typeAt(t, d, 1, 0, "incoming1\n") },
			"a\nincoming1\nb\nc", diff.StateInput1, false, diff.LineRange{StartLine: 2, EndLine: 3}, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, doc := newMarkerSession(t, twoConflicts, InitialReplace)
			if tc.prepare != nil {
				tc.prepare(t, s)
			}
			tc.edit(t, doc)
			if doc.Text() != tc.text {
				t.Fatalf("the document is %q, want %q", doc.Text(), tc.text)
			}
			conflicts := s.Model().Conflicts()
			if c := conflicts[0]; c.State.Kind != tc.state || c.Handled != tc.handled || c.Result != tc.first {
				t.Errorf("the first conflict is %+v, want state %v, handled %v, range %v", c, tc.state, tc.handled, tc.first)
			}
			if got := conflicts[1].Result.StartLine; got != tc.second {
				t.Errorf("the second conflict starts at line %d, want %d", got, tc.second)
			}
			if s.Resyncs() != 0 {
				t.Errorf("%d resyncs", s.Resyncs())
			}
		})
	}
}

func TestSessionUndoOfAManualEditAfterAnAccept(t *testing.T) {
	s, doc := newMarkerSession(t, twoConflicts, InitialReplace)
	if err := s.Accept(0, diff.AcceptInput2); err != nil {
		t.Fatal(err)
	}
	typeAt(t, doc, 1, 0, "// ")
	if c := s.Model().Conflicts()[0]; c.State.Kind != diff.StateUnrecognized || !c.Handled {
		t.Fatalf("the edited conflict is %+v", c)
	}
	if _, err := doc.Undo(); err != nil { // the typing
		t.Fatal(err)
	}
	if c := s.Model().Conflicts()[0]; c.State.Kind != diff.StateInput2 || !c.Handled {
		t.Errorf("after the first undo the conflict is %+v", c)
	}
	if _, err := doc.Undo(); err != nil { // the accept
		t.Fatal(err)
	}
	if c := s.Model().Conflicts()[0]; c.State.Kind != diff.StateBase || c.Handled || doc.Text() != "a\nb\nc" {
		t.Errorf("after the second undo the conflict is %+v, the text %q", c, doc.Text())
	}
}

func TestSessionAcceptRouteErrors(t *testing.T) {
	ro := func(t *testing.T) *Session {
		t.Helper()
		inputs, err := diff.MergeInputsFromMarkerText(twoConflicts)
		if err != nil {
			t.Fatal(err)
		}
		doc := document.NewVirtual(document.VirtualURI("/r"), document.VirtualContent{Text: "a\nb\nc"})
		s, err := NewSession(t.Context(), SessionOpts{Result: doc, Inputs: inputs, Initial: InitialSync, OnError: func(error) {}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Dispose)
		return s
	}
	for _, tc := range []struct {
		name  string
		do    func(*Session) error
		route string
	}{
		{"a conflict that does not exist", func(s *Session) error { return s.Accept(7, diff.AcceptInput1) },
			"Session.Accept->MergeModel.Accept->MergeModel.SetState->MergeModel.conflictRegion.MergeConflictIndexInvalid"},
		{"a choice that is none", func(s *Session) error { return s.Accept(0, diff.Choice(99)) },
			"Session.Accept->MergeModel.Accept.MergeChoiceInvalid"},
		{"accept all with a choice that is none", func(s *Session) error { return s.AcceptAll(diff.Choice(99)) },
			"Session.AcceptAll->MergeModel.AcceptAll.MergeChoiceInvalid"},
		{"a state that cannot be put", func(s *Session) error { return s.SetState(0, diff.RangeState{Kind: diff.StateUnrecognized}) },
			"Session.SetState->MergeModel.SetState.MergeStateInvalid"},
		{"a state of a conflict that does not exist", func(s *Session) error { return s.SetState(-1, diff.RangeState{}) },
			"Session.SetState->MergeModel.SetState->MergeModel.conflictRegion.MergeConflictIndexInvalid"},
		{"a result that cannot be edited", func(s *Session) error { return s.Accept(0, diff.AcceptInput1) },
			"Session.Accept->Session.finish->Session.applyEdits->Document.Edit->Document.checkWritable.DocumentReadOnly"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s *Session
			if strings.Contains(tc.name, "cannot be edited") {
				s = ro(t)
			} else {
				s, _ = newMarkerSession(t, twoConflicts, InitialReplace)
			}
			err := tc.do(s)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

// ---- the actions above the conflicts ----

func texts(items []ActionItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Text
	}
	return out
}

func TestActionItems(t *testing.T) {
	inputs, _ := goMerge()
	for _, tc := range []struct {
		name                     string
		do                       func(*testing.T, *rig)
		incoming, current, resul []string
	}{
		{"nothing accepted", nil,
			[]string{"Accept Incoming", "Accept Combination (Incoming First)"},
			[]string{"Accept Current", "Accept Combination (Current First)"},
			[]string{"No Changes Accepted"}},
		{"incoming accepted", func(t *testing.T, r *rig) { mustAccept(t, r, 0, diff.AcceptInput1) },
			nil,
			[]string{"Accept Current", "Accept Combination (Current First)"},
			[]string{"Incoming", "Remove Incoming"}},
		{"current accepted", func(t *testing.T, r *rig) { mustAccept(t, r, 0, diff.AcceptInput2) },
			[]string{"Accept Incoming", "Accept Combination (Incoming First)"},
			nil,
			[]string{"Current", "Remove Current"}},
		{"both, current first", func(t *testing.T, r *rig) { mustAccept(t, r, 0, diff.AcceptBoth) },
			nil, nil,
			[]string{"Current + Incoming", "Remove Current", "Remove Incoming"}},
		{"both, incoming first", func(t *testing.T, r *rig) {
			mustAccept(t, r, 0, diff.AcceptInput1)
			if err := r.session.Toggle(0, diff.Input2); err != nil {
				t.Fatal(err)
			}
		}, nil, nil,
			[]string{"Incoming + Current", "Remove Incoming", "Remove Current"}},
		{"edited by hand", func(t *testing.T, r *rig) { typeAt(t, r.result, 5, 4, "!") },
			nil, nil,
			[]string{"Manual Resolution", "Reset to base"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, rigConfig{inputs: inputs})
			if tc.do != nil {
				tc.do(t, r)
			}
			got := [3][]string{texts(r.model.Actions(Incoming, 0)), texts(r.model.Actions(Current, 0)), texts(r.model.Actions(Result, 0))}
			for i, want := range [][]string{tc.incoming, tc.current, tc.resul} {
				if !slices.Equal(got[i], want) {
					t.Errorf("%v: %q, want %q", Side(i), got[i], want)
				}
			}
			// the second conflict was not touched
			if got := texts(r.model.Actions(Result, 1)); !slices.Equal(got, []string{"No Changes Accepted"}) {
				t.Errorf("the second conflict's result row is %q", got)
			}
			// the first item of the result row says what the result has and is no link; every other item is a link
			for _, side := range []Side{Incoming, Current, Result} {
				for i, it := range r.model.Actions(side, 0) {
					if wantText := side == Result && i == 0; wantText != (it.Run == nil) {
						t.Errorf("%v %q: it is a link: %v", side, it.Text, it.Run != nil)
					}
				}
			}
			if r.model.Actions(Result, 9) != nil || r.model.Actions(Result, -1) != nil {
				t.Error("a conflict that does not exist has actions")
			}
			r.noErrors()
		})
	}
}

func mustAccept(t *testing.T, r *rig, conflict int, c diff.Choice) {
	t.Helper()
	if err := r.model.Accept(conflict, c); err != nil {
		t.Fatal(err)
	}
	r.settle()
}

func TestActionLinksDoWhatTheySay(t *testing.T) {
	inputs, _ := goMerge()
	for _, tc := range []struct {
		name  string
		side  Side
		label string
		more  func(*testing.T, *rig)
		state diff.RangeState
		lines []string
	}{
		{"accept incoming", Incoming, "Accept Incoming", nil, diff.RangeState{Kind: diff.StateInput1}, []string{"\tfmt.Println(\"hello,\", name)"}},
		{"accept current", Current, "Accept Current", nil, diff.RangeState{Kind: diff.StateInput2}, []string{"\tfmt.Println(\"hi\", name)"}},
		{"combination, incoming first", Incoming, "Accept Combination (Incoming First)", nil, diff.BothState(diff.Input1),
			[]string{"\tfmt.Println(\"hello,\", name)", "\tfmt.Println(\"hi\", name)"}},
		{"combination, current first", Current, "Accept Combination (Current First)", nil, diff.BothState(diff.Input2),
			[]string{"\tfmt.Println(\"hi\", name)", "\tfmt.Println(\"hello,\", name)"}},
		{"accept the other one after incoming: it comes after", Current, "Accept Current", func(t *testing.T, r *rig) {
			mustAccept(t, r, 0, diff.AcceptInput1)
		}, diff.BothState(diff.Input1), []string{"\tfmt.Println(\"hello,\", name)", "\tfmt.Println(\"hi\", name)"}},
		{"remove incoming", Result, "Remove Incoming", func(t *testing.T, r *rig) { mustAccept(t, r, 0, diff.AcceptInput1) },
			diff.RangeState{}, []string{"\tfmt.Println(\"hello\", name)"}},
		{"remove current out of both", Result, "Remove Current", func(t *testing.T, r *rig) { mustAccept(t, r, 0, diff.AcceptBoth) },
			diff.RangeState{Kind: diff.StateInput1}, []string{"\tfmt.Println(\"hello,\", name)"}},
		{"reset to base", Result, "Reset to base", func(t *testing.T, r *rig) { typeAt(t, r.result, 5, 4, "!") },
			diff.RangeState{}, []string{"\tfmt.Println(\"hello\", name)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, rigConfig{inputs: inputs, size: ui.Size{Width: 1000, Height: 700}})
			if tc.more != nil {
				tc.more(t, r)
				r.settle()
			}
			// the first of the links with that text is the one of the first conflict, which is above the second
			r.ts.Tap(gluttertest.First(gluttertest.ByText(tc.label)))
			r.settle()
			c := r.session.Model().Conflicts()[0]
			if c.State != tc.state || !c.Handled {
				t.Errorf("the conflict is %+v, want state %v handled", c, tc.state)
			}
			got := strings.Split(r.result.Text(), "\n")
			if lines := got[5 : 5+len(tc.lines)]; !slices.Equal(lines, tc.lines) {
				t.Errorf("the lines of the result: %q, want %q", lines, tc.lines)
			}
			if a, ok := r.model.Active(); !ok || a.ConflictIndex != 0 {
				t.Errorf("the active region is %+v", a)
			}
			r.noErrors()
		})
	}
}

func TestActionRowsAreInTheSamePlaceInBothInputs(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	m := r.model
	for _, s := range []Side{Incoming, Current, Result} {
		conflicts, after := m.ActionZones(s)
		if !slices.Equal(conflicts, []int{0, 1}) {
			t.Fatalf("%v has the rows of the conflicts %v", s, conflicts)
		}
		for i, c := range m.sess.Model().Conflicts() {
			if want := RangeIn(c, s).StartLine - 1; after[i] != want {
				t.Errorf("%v: the row of conflict %d is after line %d, want %d", s, i, after[i], want)
			}
		}
	}
	// the lines after every conflict are on the same row in both inputs, with the rows in
	tops := func(s Side, line int) float64 { return lineTops(m.View(s))(line) }
	for i, c := range m.sess.Model().Conflicts() {
		r1, r2 := c.Range.InputRange(diff.Input1), c.Range.InputRange(diff.Input2)
		for _, pair := range [][2]int{{r1.StartLine, r2.StartLine}, {r1.EndLine, r2.EndLine}} {
			if a, b := tops(Incoming, pair[0]), tops(Current, pair[1]); a != b {
				t.Errorf("conflict %d: line %d of incoming is at %v, line %d of current at %v", i, pair[0], a, pair[1], b)
			}
		}
	}
	// the row of the result follows the regions when the text above them changes
	typeAt(t, r.result, 0, 0, "// header\n// more\n")
	mustAccept(t, r, 0, diff.AcceptBoth)
	r.settle()
	conflicts, after := m.ActionZones(Result)
	for i, c := range conflicts {
		if want := m.sess.Model().Conflicts()[c].Result.StartLine - 1; after[i] != want {
			t.Errorf("after the edits the row of conflict %d is after line %d, want %d", c, after[i], want)
		}
	}
	r.noErrors()
}

func TestHeaderButtons(t *testing.T) {
	inputs, _ := goMerge()
	completed := 0
	r := newRig(t, rigConfig{inputs: inputs, size: ui.Size{Width: 1000, Height: 700}, onComplete: func() { completed++ }})
	key := func(name string) gluttertest.Finder { return gluttertest.ByKey(foundation.NewValueKey(childKey(name))) }
	// Complete Merge is a button of the result header, whatever the state of the conflicts
	r.ts.Tap(gluttertest.ByText("Complete Merge"))
	if completed != 1 {
		t.Errorf("the button completed %d times", completed)
	}
	// Accept All of the incoming side: two buttons with this key, the first is the incoming header's
	r.ts.Tap(gluttertest.First(key("acceptAll")))
	r.settle()
	if got := r.session.Model().UnhandledCount(); got != 0 {
		t.Errorf("%d conflicts are unhandled after Accept All Incoming", got)
	}
	for i, c := range r.session.Model().Conflicts() {
		if c.State.Kind != diff.StateInput1 {
			t.Errorf("conflict %d is %v", i, c.State)
		}
	}
	// Reset Result puts them back
	r.ts.Tap(key("reset"))
	r.settle()
	if got := r.session.Model().UnhandledCount(); got != 2 {
		t.Errorf("%d conflicts are unhandled after Reset Result", got)
	}
	if got := r.result.Text(); !strings.Contains(got, "\"hello\", name") || !strings.Contains(got, "greet(\"world\")") {
		t.Errorf("the result after the reset:\n%s", got)
	}
	// Accept All of the current side
	r.ts.Tap(gluttertest.Last(key("acceptAll")))
	r.settle()
	for i, c := range r.session.Model().Conflicts() {
		if c.State.Kind != diff.StateInput2 {
			t.Errorf("conflict %d is %v after Accept All Current", i, c.State)
		}
	}
	r.noErrors()
}

func TestHeaderWithoutCompleteMergeCallback(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	if r.ts.Exists(gluttertest.ByText("Complete Merge")) {
		t.Error("the button shows without a callback")
	}
}

func TestToggleActive(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	m := r.model
	if ok, err := m.ToggleActive(diff.Input1); ok || err != nil {
		t.Errorf("without an active conflict: %v %v", ok, err)
	}
	m.GoToConflict(true)
	if ok, err := m.ToggleActive(diff.Input1); !ok || err != nil {
		t.Fatalf("toggle: %v %v", ok, err)
	}
	if c := m.sess.Model().Conflicts()[0]; c.State.Kind != diff.StateInput1 {
		t.Errorf("the conflict is %+v", c)
	}
	if _, err := m.ToggleActive(diff.Input1); err != nil {
		t.Fatal(err)
	}
	if c := m.sess.Model().Conflicts()[0]; c.State.Kind != diff.StateBase {
		t.Errorf("the conflict after a second toggle is %+v", c)
	}
	r.noErrors()
}

func TestAcceptAllOfTheModel(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	if err := r.model.AcceptAll(diff.Input2); err != nil {
		t.Fatal(err)
	}
	for i, c := range r.session.Model().Conflicts() {
		if c.State.Kind != diff.StateInput2 || !c.Handled {
			t.Errorf("conflict %d is %+v", i, c)
		}
	}
	if err := r.model.ResetResult(); err != nil {
		t.Fatal(err)
	}
	if err := r.model.AcceptAllCombination(); err != nil {
		t.Fatal(err)
	}
	for i, c := range r.session.Model().Conflicts() {
		if c.State != diff.BothState(diff.Input1) {
			t.Errorf("conflict %d is %+v after Accept All Combination", i, c)
		}
	}
	r.settle()
	r.noErrors()
}
