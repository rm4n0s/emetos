package editorgroup

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// navRig is a history over a fake window: the editors are named by the file they show, a selection is a line,
// and showing a location does what the window does, which is to tell the history about the editor and the
// selection it shows.
type navRig struct {
	t      *testing.T
	h      *History
	pane   Pane
	inputs map[string]*testInput
	// opened are the locations the history asked to show.
	opened []string
	// hook, when set, shows the locations in place of the rig (to finish later).
	hook func(loc NavLocation, done func())
}

func newNavRig(t *testing.T, scope NavigationScope) *navRig {
	t.Helper()
	r := &navRig{t: t, inputs: map[string]*testInput{}}
	r.h = NewHistory(r, scope)
	return r
}

func (r *navRig) input(name string) *testInput {
	if in, ok := r.inputs[name]; ok {
		return in
	}
	uri := document.FileURI("/w/" + name)
	in := &testInput{id: name, resource: &uri}
	r.inputs[name] = in
	return in
}

func lineSel(line int) *NavSelection {
	p := buffer.Position{Line: line}
	return &NavSelection{Anchor: p, Active: p}
}

func (r *navRig) paneOf(group int, name string, line int) Pane {
	return Pane{GroupID: group, Input: r.input(name), SelectionAware: true, Selection: lineSel(line)}
}

// ActivePane implements Opener.
func (r *navRig) ActivePane() Pane { return r.pane }

// OpenLocation implements Opener: it shows the location as the window does, then says it is done.
func (r *navRig) OpenLocation(loc NavLocation, done func()) {
	if r.hook != nil {
		r.hook(loc, done)
		return
	}
	name := strings.TrimPrefix(loc.Resource.Path, "/w/")
	line := 0
	if loc.Selection != nil {
		line = loc.Selection.Active.Line
	}
	r.opened = append(r.opened, fmt.Sprintf("%d:%s@%d", loc.GroupID, name, line))
	r.show(loc.GroupID, name, line, ReasonProgrammatic)
	done()
}

// show makes an editor active with a selection and tells the history.
func (r *navRig) show(group int, name string, line int, reason SelectionReason) {
	r.pane = r.paneOf(group, name, line)
	r.h.ActiveEditorChanged(r.pane)
	if reason != ReasonNone {
		r.h.SelectionChanged(r.pane, reason)
	}
}

// open is the user opening a file in a group: the editor changes, with its cursor at a line.
func (r *navRig) open(group int, name string, line int) {
	r.pane = r.paneOf(group, name, line)
	r.h.ActiveEditorChanged(r.pane)
}

// move is the cursor of the active editor moving to a line.
func (r *navRig) move(line int, reason SelectionReason) {
	r.pane.Selection = lineSel(line)
	r.h.SelectionChanged(r.pane, reason)
}

// stack writes a stack as "group:file@line" entries with the place in brackets.
func (r *navRig) stack(f GoFilter) string {
	locs, index := r.h.Locations(f)
	var parts []string
	for i, l := range locs {
		line := -1
		if l.Selection != nil {
			line = l.Selection.Active.Line
		}
		s := fmt.Sprintf("%d:%s@%d", l.GroupID, strings.TrimPrefix(l.Resource.Path, "/w/"), line)
		if i == index {
			s = "[" + s + "]"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

type navStep struct {
	op     string // open, move
	group  int
	file   string
	line   int
	reason SelectionReason
}

func open(group int, file string, line int) navStep {
	return navStep{op: "open", group: group, file: file, line: line}
}
func move(line int, reason SelectionReason) navStep {
	return navStep{op: "move", line: line, reason: reason}
}

func (r *navRig) run(steps []navStep) {
	for _, s := range steps {
		switch s.op {
		case "open":
			r.open(s.group, s.file, s.line)
		case "move":
			r.move(s.line, s.reason)
		}
	}
}

func TestSelectionCompare(t *testing.T) {
	sel := func(anchor, active int) NavSelection {
		return NavSelection{Anchor: buffer.Position{Line: anchor}, Active: buffer.Position{Line: active}}
	}
	for _, tc := range []struct {
		name string
		a, b NavSelection
		want SelectionCompare
	}{
		{"same line", sel(4, 4), sel(4, 4), SelectionIdentical},
		{"same line other column", NavSelection{Active: buffer.Position{Line: 4, Col: 9}}, NavSelection{Active: buffer.Position{Line: 4}}, SelectionIdentical},
		{"one line apart", sel(4, 4), sel(5, 5), SelectionSimilar},
		{"nine lines apart", sel(0, 0), sel(9, 9), SelectionSimilar},
		{"nine lines apart, the other way", sel(9, 9), sel(0, 0), SelectionSimilar},
		{"ten lines apart", sel(0, 0), sel(10, 10), SelectionDifferent},
		{"far apart", sel(100, 100), sel(2, 2), SelectionDifferent},
		{"the first line of a backwards selection counts", sel(20, 3), sel(5, 5), SelectionSimilar},
		{"the first line of a forwards selection counts", sel(3, 20), sel(5, 5), SelectionSimilar},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Compare(tc.b); got != tc.want {
				t.Errorf("Compare = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHistoryRecording(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps []navStep
		want  string
	}{
		{"the first editor is a location", []navStep{open(1, "a", 0)}, "[1:a@0]"},
		{"a new active editor is a location", []navStep{open(1, "a", 0), open(1, "b", 0)}, "1:a@0 [1:b@0]"},
		{"the same editor again is not", []navStep{open(1, "a", 0), open(1, "a", 0)}, "[1:a@0]"},
		{"the same file in another group is", []navStep{open(1, "a", 0), open(2, "a", 0)}, "1:a@0 [2:a@0]"},
		{"a small move replaces the location", []navStep{open(1, "a", 0), move(5, ReasonUser)}, "[1:a@5]"},
		{"nine lines is still a small move", []navStep{open(1, "a", 0), move(9, ReasonUser)}, "[1:a@9]"},
		{"ten lines is a new location", []navStep{open(1, "a", 0), move(10, ReasonUser)}, "1:a@0 [1:a@10]"},
		{"a far move is a new location", []navStep{open(1, "a", 0), move(40, ReasonUser)}, "1:a@0 [1:a@40]"},
		{"two small moves replace twice", []navStep{open(1, "a", 0), move(3, ReasonUser), move(6, ReasonUser)}, "[1:a@6]"},
		{"a jump near the line is a new location", []navStep{open(1, "a", 0), move(3, ReasonJump)}, "1:a@0 [1:a@3]"},
		{"a navigation near the line is a new location", []navStep{open(1, "a", 0), move(3, ReasonNavigation)}, "1:a@0 [1:a@3]"},
		{"a programmatic move near the line is not", []navStep{open(1, "a", 0), move(3, ReasonProgrammatic)}, "[1:a@3]"},
		{"an edit near the line is not", []navStep{open(1, "a", 0), move(3, ReasonEdit)}, "[1:a@3]"},
		{"an edit far away is a new location", []navStep{open(1, "a", 0), move(30, ReasonEdit)}, "1:a@0 [1:a@30]"},
		{"the same line again is the same location", []navStep{open(1, "a", 4), move(4, ReasonUser), move(4, ReasonUser)}, "[1:a@4]"},
		{"three editors are three locations", []navStep{open(1, "a", 0), open(1, "b", 0), open(1, "c", 0)}, "1:a@0 1:b@0 [1:c@0]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newNavRig(t, ScopeDefault)
			r.run(tc.steps)
			if got := r.stack(GoAll); got != tc.want {
				t.Errorf("stack\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestHistoryStackLimit(t *testing.T) {
	r := newNavRig(t, ScopeDefault)
	for i := 0; i < MaxNavigationStack+10; i++ {
		r.open(1, fmt.Sprintf("f%d", i), 0)
	}
	locs, index := r.h.Locations(GoAll)
	if len(locs) != MaxNavigationStack || index != MaxNavigationStack-1 {
		t.Fatalf("%d locations at %d, want %d at %d", len(locs), index, MaxNavigationStack, MaxNavigationStack-1)
	}
	if first, last := locs[0].Resource.Path, locs[len(locs)-1].Resource.Path; first != "/w/f10" || last != "/w/f59" {
		t.Errorf("first %s last %s, want /w/f10 and /w/f59", first, last)
	}
}

func TestHistoryBackAndForward(t *testing.T) {
	r := newNavRig(t, ScopeDefault)
	r.open(1, "a", 0)
	r.open(1, "b", 5)
	r.open(1, "c", 8)
	steps := []struct {
		do     func()
		opened string
		stack  string
		back   bool
		fwd    bool
	}{
		{func() {}, "", "1:a@0 1:b@5 [1:c@8]", true, false},
		{func() { r.h.Back(GoAll) }, "1:b@5", "1:a@0 [1:b@5] 1:c@8", true, true},
		{func() { r.h.Back(GoAll) }, "1:a@0", "[1:a@0] 1:b@5 1:c@8", false, true},
		{func() { r.h.Back(GoAll) }, "", "[1:a@0] 1:b@5 1:c@8", false, true},
		{func() { r.h.Forward(GoAll) }, "1:b@5", "1:a@0 [1:b@5] 1:c@8", true, true},
		{func() { r.h.Forward(GoAll) }, "1:c@8", "1:a@0 1:b@5 [1:c@8]", true, false},
		{func() { r.h.Forward(GoAll) }, "", "1:a@0 1:b@5 [1:c@8]", true, false},
	}
	for i, s := range steps {
		r.opened = nil
		s.do()
		got := strings.Join(r.opened, ",")
		if got != s.opened {
			t.Errorf("step %d opened %q, want %q", i, got, s.opened)
		}
		if st := r.stack(GoAll); st != s.stack {
			t.Errorf("step %d stack\n got %s\nwant %s", i, st, s.stack)
		}
		if st := r.h.State(); st.CanBack != s.back || st.CanForward != s.fwd {
			t.Errorf("step %d CanBack %v CanForward %v, want %v and %v", i, st.CanBack, st.CanForward, s.back, s.fwd)
		}
	}
}

func TestHistoryNavigatingAddsNothing(t *testing.T) {
	r := newNavRig(t, ScopeDefault)
	r.open(1, "a", 0)
	r.open(1, "b", 40)
	r.h.Back(GoAll)
	// showing a location moved the cursor far and changed the editor: the stack is as it was
	if got, want := r.stack(GoAll), "[1:a@0] 1:b@40"; got != want {
		t.Fatalf("stack %s, want %s", got, want)
	}
	// the cursor moving after the navigation is a change of the user again
	r.move(60, ReasonUser)
	if got, want := r.stack(GoAll), "1:a@0 [1:a@60]"; got != want {
		t.Errorf("stack %s, want %s: a new location after going back drops the forward ones", got, want)
	}
}

func TestHistoryDoneCanComeLater(t *testing.T) {
	r := newNavRig(t, ScopeDefault)
	r.open(1, "a", 0)
	r.open(1, "b", 0)
	var finish func()
	r.hook = func(_ NavLocation, done func()) { finish = done }
	r.h.Back(GoAll)
	// the file is being read: the editor changes that other code reports now are not new locations
	r.show(1, "other", 99, ReasonUser)
	if got, want := r.stack(GoAll), "[1:a@0] 1:b@0"; got != want {
		t.Errorf("while navigating, stack %s, want %s", got, want)
	}
	finish()
	r.move(80, ReasonUser)
	// the user moves the cursor again: a location, which follows the place and drops what was after it
	if got, want := r.stack(GoAll), "1:a@0 [1:other@80]"; got != want {
		t.Errorf("after the navigation, stack %s, want %s", got, want)
	}
}

func TestHistoryPrevious(t *testing.T) {
	r := newNavRig(t, ScopeDefault)
	r.open(1, "a", 0)
	r.open(1, "b", 0)
	r.open(1, "c", 0)
	r.h.Previous(GoAll) // never navigated: goes back
	if got := strings.Join(r.opened, ","); got != "1:b@0" {
		t.Fatalf("opened %s", got)
	}
	r.h.Back(GoAll)
	r.opened = nil
	r.h.Previous(GoAll) // back to where the place was before the last move
	if got := strings.Join(r.opened, ","); got != "1:b@0" {
		t.Errorf("opened %s, want 1:b@0", got)
	}
}

func TestHistoryScopes(t *testing.T) {
	// two groups; the user visits a and c in group 1 and b in group 2
	visit := func(r *navRig) {
		r.open(1, "a", 0)
		r.open(2, "b", 0)
		r.open(1, "c", 0)
	}
	t.Run("default is one stack for the window", func(t *testing.T) {
		r := newNavRig(t, ScopeDefault)
		visit(r)
		if got, want := r.stack(GoAll), "1:a@0 2:b@0 [1:c@0]"; got != want {
			t.Fatalf("stack %s, want %s", got, want)
		}
		r.h.Back(GoAll)
		if got := strings.Join(r.opened, ","); got != "2:b@0" {
			t.Errorf("Back opened %s, want 2:b@0", got)
		}
	})
	t.Run("editorGroup is one stack for each group", func(t *testing.T) {
		r := newNavRig(t, ScopeEditorGroup)
		visit(r)
		if got, want := r.stack(GoAll), "1:a@0 [1:c@0]"; got != want {
			t.Fatalf("stack of group 1 %s, want %s", got, want)
		}
		r.h.Back(GoAll)
		if got := strings.Join(r.opened, ","); got != "1:a@0" {
			t.Errorf("Back opened %s, want 1:a@0", got)
		}
		r.pane = r.paneOf(2, "b", 0)
		if got, want := r.stack(GoAll), "[2:b@0]"; got != want {
			t.Errorf("stack of group 2 %s, want %s", got, want)
		}
	})
	t.Run("editor is one stack for each editor", func(t *testing.T) {
		r := newNavRig(t, ScopeEditor)
		r.open(1, "a", 0)
		r.move(40, ReasonUser)
		r.open(1, "b", 0)
		r.move(30, ReasonUser)
		if got, want := r.stack(GoAll), "1:b@0 [1:b@30]"; got != want {
			t.Fatalf("stack of b %s, want %s", got, want)
		}
		r.pane = r.paneOf(1, "a", 40)
		if got, want := r.stack(GoAll), "1:a@0 [1:a@40]"; got != want {
			t.Errorf("stack of a %s, want %s", got, want)
		}
		r.h.EditorClosed(1, r.input("a"))
		if got := r.stack(GoAll); got != "" {
			t.Errorf("stack of a after it closed: %s", got)
		}
	})
	t.Run("changing the scope starts afresh", func(t *testing.T) {
		r := newNavRig(t, ScopeDefault)
		visit(r)
		r.h.SetScope(ScopeEditorGroup)
		if got := r.stack(GoAll); got != "" {
			t.Errorf("stack %s", got)
		}
		if r.h.Scope() != ScopeEditorGroup {
			t.Errorf("scope %s", r.h.Scope())
		}
	})
	t.Run("an unknown scope is the default one", func(t *testing.T) {
		if got := NewHistory(newNavRig(t, "").h.opener, "bogus").Scope(); got != ScopeDefault {
			t.Errorf("scope %s", got)
		}
	})
}

func TestHistoryEditLocations(t *testing.T) {
	r := newNavRig(t, ScopeDefault)
	r.open(1, "a", 0)
	r.move(5, ReasonEdit)
	r.open(1, "b", 0)
	r.move(40, ReasonUser) // not an edit
	r.move(41, ReasonEdit)
	r.open(1, "c", 0)
	if got, want := r.stack(GoEdits), "1:a@5 [1:b@41]"; got != want {
		t.Fatalf("edits %s, want %s", got, want)
	}
	if st := r.h.State(); !st.CanLastEdit || !st.CanBackInEdits || st.CanForwardInEdits {
		t.Errorf("state %+v", st)
	}
	// Go to Last Edit NavLocation shows the last place that was edited
	r.opened = nil
	r.h.Last(GoEdits)
	if got := strings.Join(r.opened, ","); got != "1:b@41" {
		t.Errorf("opened %s, want 1:b@41", got)
	}
	// the edit locations are not in the navigation stack
	if got := r.stack(GoNavigation); got != "" {
		t.Errorf("navigations %s", got)
	}
}

func TestHistoryEditsStackBackFirstShowsTheCurrentEdit(t *testing.T) {
	r := newNavRig(t, ScopeDefault)
	r.open(1, "a", 0)
	r.move(5, ReasonEdit)
	r.open(1, "b", 0)
	r.move(50, ReasonEdit)
	r.open(1, "c", 0)
	r.opened = nil
	// the editor is not at the edit that the place is at: Back shows that edit first, then moves
	r.h.Back(GoEdits)
	r.h.Back(GoEdits)
	if got := strings.Join(r.opened, ","); got != "1:b@50,1:a@5" {
		t.Errorf("opened %s, want 1:b@50,1:a@5", got)
	}
}

func TestHistoryNavigationLocations(t *testing.T) {
	r := newNavRig(t, ScopeDefault)
	r.open(1, "a", 0)
	r.move(40, ReasonJump) // go to definition: the place it left and the place it went to
	if got, want := r.stack(GoNavigation), "1:a@0 [1:a@40]"; got != want {
		t.Fatalf("navigations %s, want %s", got, want)
	}
	if got, want := r.stack(GoAll), "1:a@0 [1:a@40]"; got != want {
		t.Fatalf("selections %s, want %s", got, want)
	}
	// typing after the jump
	r.move(41, ReasonUser)
	r.opened = nil
	r.h.Back(GoNavigation)
	r.h.Back(GoNavigation)
	// the first Back shows the place of the jump, the second goes back to its source
	if got := strings.Join(r.opened, ","); got != "1:a@40,1:a@0" {
		t.Errorf("opened %s, want 1:a@40,1:a@0", got)
	}
	r.opened = nil
	r.h.Forward(GoNavigation)
	if got := strings.Join(r.opened, ","); got != "1:a@40" {
		t.Errorf("Forward opened %s", got)
	}
	r.opened = nil
	r.h.Last(GoNavigation)
	if got := strings.Join(r.opened, ","); got != "1:a@40" {
		t.Errorf("Last opened %s", got)
	}
}

func TestHistoryJumpWhileNavigatingNavigationsAddsNothing(t *testing.T) {
	r := newNavRig(t, ScopeDefault)
	r.open(1, "a", 0)
	r.move(40, ReasonJump)
	r.h.Back(GoNavigation)
	// showing the source was a programmatic change in the editor, not a jump: nothing new
	if got, want := r.stack(GoNavigation), "[1:a@0] 1:a@40"; got != want {
		t.Errorf("navigations %s, want %s", got, want)
	}
}

func TestHistoryRemoving(t *testing.T) {
	setup := func(t *testing.T) *navRig {
		r := newNavRig(t, ScopeDefault)
		r.open(1, "a", 0)
		r.open(1, "b", 0)
		r.open(1, "a", 0)
		r.open(2, "c", 0)
		return r
	}
	t.Run("a deleted file drops its locations and joins the ones around it", func(t *testing.T) {
		r := setup(t)
		r.h.RemoveResource(document.FileURI("/w/b"))
		// the two locations of a that b separated become one
		locs, _ := r.h.Locations(GoAll)
		var names []string
		for _, l := range locs {
			names = append(names, fmt.Sprintf("%d:%s", l.GroupID, l.Resource.Path))
		}
		if got := strings.Join(names, " "); got != "1:/w/a 2:/w/c" {
			t.Errorf("locations %s", got)
		}
	})
	t.Run("a group that is gone drops its locations", func(t *testing.T) {
		r := setup(t)
		r.h.GroupRemoved(2)
		locs, index := r.h.Locations(GoAll)
		if len(locs) != 3 || index != 2 {
			t.Errorf("%d locations at %d", len(locs), index)
		}
		for _, l := range locs {
			if l.GroupID == 2 {
				t.Errorf("a location of group 2 is left")
			}
		}
	})
	t.Run("clear forgets everything", func(t *testing.T) {
		r := setup(t)
		r.h.Closed().Record(ClosedEditor{Resource: document.FileURI("/w/a")})
		r.h.Clear()
		if locs, _ := r.h.Locations(GoAll); len(locs) != 0 || r.h.Closed().Len() != 0 {
			t.Errorf("%d locations, %d closed", len(locs), r.h.Closed().Len())
		}
		if st := r.h.State(); st.CanBack || st.CanReopenClosed {
			t.Errorf("state %+v", st)
		}
	})
	t.Run("an editor with no file goes away when it is disposed", func(t *testing.T) {
		r := newNavRig(t, ScopeDefault)
		scratch := &testInput{id: "scratch"}
		r.open(1, "a", 0)
		r.pane = Pane{GroupID: 1, Input: scratch}
		r.h.ActiveEditorChanged(r.pane)
		if locs, _ := r.h.Locations(GoAll); len(locs) != 2 || locs[1].Input != scratch {
			t.Fatalf("locations %v", locs)
		}
		r.h.InputDisposed(scratch)
		if locs, _ := r.h.Locations(GoAll); len(locs) != 1 {
			t.Errorf("%d locations after the dispose", len(locs))
		}
	})
	t.Run("a moved file keeps its locations at the new path", func(t *testing.T) {
		r := setup(t)
		r.h.Closed().Record(ClosedEditor{Resource: document.FileURI("/w/b"), Batch: 1})
		r.h.Follow(func(u document.URI) (document.URI, bool) {
			if u.Path == "/w/b" {
				return document.FileURI("/w/sub/b"), true
			}
			return u, true
		})
		locs, _ := r.h.Locations(GoAll)
		if locs[1].Resource.Path != "/w/sub/b" {
			t.Errorf("location %s", locs[1].Resource.Path)
		}
		if got := r.h.Closed().List(); got[0].Resource.Path != "/w/sub/b" {
			t.Errorf("closed %s", got[0].Resource.Path)
		}
	})
	t.Run("a file that Follow says is gone is removed", func(t *testing.T) {
		r := setup(t)
		r.h.Closed().Record(ClosedEditor{Resource: document.FileURI("/w/b"), Batch: 1})
		r.h.Follow(func(u document.URI) (document.URI, bool) { return u, u.Path != "/w/b" })
		locs, _ := r.h.Locations(GoAll)
		for _, l := range locs {
			if l.Resource.Path == "/w/b" {
				t.Errorf("a location of b is left")
			}
		}
		if r.h.Closed().Len() != 0 {
			t.Errorf("closed editors left")
		}
	})
}

func TestHistoryIgnoresTheSameActiveEditorTwice(t *testing.T) {
	r := newNavRig(t, ScopeDefault)
	changes := 0
	r.h.OnChange(func() { changes++ })
	r.open(1, "a", 0)
	n := changes
	r.open(1, "a", 0)
	if changes != n {
		t.Errorf("the history changed again for the same editor")
	}
}

func TestHistoryEmptyGroupIsNotALocation(t *testing.T) {
	r := newNavRig(t, ScopeDefault)
	r.open(1, "a", 0)
	r.pane = Pane{GroupID: 1}
	r.h.ActiveEditorChanged(r.pane)
	r.h.Back(GoAll)
	if len(r.opened) != 0 {
		t.Errorf("opened %v", r.opened)
	}
	if st := r.h.State(); st.CanBack {
		t.Errorf("state %+v", st)
	}
}

func TestClosedEditors(t *testing.T) {
	file := func(n string) document.URI { return document.FileURI("/w/" + n) }
	t.Run("the last batch comes back together, in the order it was closed", func(t *testing.T) {
		c := NewClosedEditors()
		b1 := c.NewBatch()
		c.Record(ClosedEditor{Resource: file("a"), Batch: b1})
		b2 := c.NewBatch()
		c.Record(ClosedEditor{Resource: file("b"), Batch: b2, Index: 0})
		c.Record(ClosedEditor{Resource: file("c"), Batch: b2, Index: 1})
		got := c.TakeLastBatch()
		if len(got) != 2 || got[0].Resource != file("b") || got[1].Resource != file("c") {
			t.Fatalf("batch %v", got)
		}
		got = c.TakeLastBatch()
		if len(got) != 1 || got[0].Resource != file("a") {
			t.Fatalf("batch %v", got)
		}
		if c.TakeLastBatch() != nil || c.Len() != 0 {
			t.Errorf("not empty")
		}
	})
	t.Run("a file closed again moves to the end", func(t *testing.T) {
		c := NewClosedEditors()
		c.Record(ClosedEditor{Resource: file("a"), Batch: 1})
		c.Record(ClosedEditor{Resource: file("b"), Batch: 2})
		c.Record(ClosedEditor{Resource: file("a"), Batch: 3})
		var names []string
		for _, e := range c.List() {
			names = append(names, e.Resource.Path)
		}
		if got := strings.Join(names, " "); got != "/w/b /w/a" {
			t.Errorf("list %s", got)
		}
	})
	t.Run("it keeps the last twenty", func(t *testing.T) {
		c := NewClosedEditors()
		for i := 0; i < MaxRecentlyClosedEditors+5; i++ {
			c.Record(ClosedEditor{Resource: file(fmt.Sprint(i)), Batch: i})
		}
		list := c.List()
		if len(list) != MaxRecentlyClosedEditors || list[0].Resource != file("5") || list[len(list)-1].Resource != file("24") {
			t.Errorf("%d editors from %s to %s", len(list), list[0].Resource.Path, list[len(list)-1].Resource.Path)
		}
	})
	t.Run("the view state, the place and the group are kept", func(t *testing.T) {
		c := NewClosedEditors()
		c.Record(ClosedEditor{Resource: file("a"), GroupID: 3, Index: 2, Sticky: true, ViewState: "state", Batch: 1})
		e := c.TakeLastBatch()[0]
		if e.GroupID != 3 || e.Index != 2 || !e.Sticky || e.ViewState != "state" {
			t.Errorf("editor %+v", e)
		}
	})
}

func TestRecentStack(t *testing.T) {
	a, b, c := Ref{GroupID: 1, Input: &testInput{id: "a"}}, Ref{GroupID: 1, Input: &testInput{id: "b"}}, Ref{GroupID: 1, Input: &testInput{id: "c"}}
	fresh := func() []Ref { return []Ref{a, b, c} }
	var r RecentStack
	steps := []struct {
		delta int
		want  Ref
	}{{+1, b}, {+1, c}, {+1, c}, {-1, b}, {-1, a}, {-1, a}}
	for i, s := range steps {
		got, ok := r.Step(fresh, s.delta)
		if !ok || got != s.want {
			t.Errorf("step %d: %v, %v", i, got.Input, ok)
		}
	}
	// a change of the order of use starts the walk again, unless the walk caused it
	r.Reset()
	if got, _ := r.Step(func() []Ref { return []Ref{c, b, a} }, +1); got != b {
		t.Errorf("after the reset: %v", got.Input)
	}
	r.SetNavigating(true)
	r.Reset()
	r.SetNavigating(false)
	if got, _ := r.Step(fresh, +1); got != a {
		t.Errorf("the reset of the walk itself was not ignored: %v", got.Input)
	}
	if _, ok := (&RecentStack{}).Step(func() []Ref { return nil }, 1); ok {
		t.Errorf("an empty list gave an editor")
	}
}
