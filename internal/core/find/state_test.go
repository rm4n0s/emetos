package find

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func TestStateChangeEvents(t *testing.T) {
	s := NewFindState()
	var events []ChangeEvent
	unsubscribe := s.OnChange(func(e ChangeEvent) { events = append(events, e) })

	s.Change(NewState{SearchString: Some("foo")}, true, true)
	if len(events) != 1 || !events[0].SearchString || !events[0].MoveCursor || !events[0].UpdateHistory || events[0].ReplaceString {
		t.Fatalf("events %+v", events)
	}
	s.Change(NewState{SearchString: Some("foo")}, false, true)
	if len(events) != 1 {
		t.Errorf("a change of nothing tells nobody: %+v", events)
	}
	s.Change(NewState{IsRegex: Some(true), MatchCase: Some(true), WholeWord: Some(true), PreserveCase: Some(true)}, false, false)
	if e := events[1]; !e.IsRegex || !e.MatchCase || !e.WholeWord || !e.PreserveCase || e.UpdateHistory || e.SearchString {
		t.Errorf("switches: %+v", e)
	}
	s.Change(NewState{IsRevealed: Some(true), IsReplaceRevealed: Some(true), ReplaceString: Some("bar"), Loop: Some(false), IsSearching: Some(true)}, false, true)
	if e := events[2]; !e.IsRevealed || !e.IsReplaceRevealed || !e.ReplaceString || !e.Loop || !e.IsSearching {
		t.Errorf("flags: %+v", e)
	}
	if s.SearchString() != "foo" || s.ReplaceString() != "bar" || !s.IsRevealed() || !s.IsReplaceRevealed() || s.IsSearching() == false {
		t.Errorf("getters: %+v", s)
	}
	unsubscribe()
	s.Change(NewState{SearchString: Some("x")}, false, true)
	if len(events) != 3 {
		t.Errorf("an unsubscribed listener hears nothing")
	}
}

func TestStateOverrides(t *testing.T) {
	s := NewFindState()
	var last ChangeEvent
	s.OnChange(func(e ChangeEvent) { last = e })
	s.Change(NewState{IsRegex: Some(false), MatchCase: Some(true), IsRegexOverride: True, MatchCaseOverride: False, WholeWordOverride: True}, false, true)
	if !s.IsRegex() || s.MatchCase() || !s.WholeWord() || s.PreserveCase() {
		t.Errorf("the overrides win: regex %v case %v word %v", s.IsRegex(), s.MatchCase(), s.WholeWord())
	}
	if s.ActualIsRegex() || !s.ActualMatchCase() || s.ActualWholeWord() || s.ActualPreserveCase() {
		t.Errorf("the switches are as they were set")
	}
	if !last.IsRegex || last.MatchCase || !last.WholeWord {
		t.Errorf("the change of what is in effect is reported (not the case: it is off both ways): %+v", last)
	}
	// the next change that does not repeat them drops them
	s.Change(NewState{SearchString: Some("x")}, false, true)
	if s.IsRegex() || !s.MatchCase() || s.WholeWord() {
		t.Errorf("the overrides are gone: regex %v case %v word %v", s.IsRegex(), s.MatchCase(), s.WholeWord())
	}
	if !last.IsRegex || !last.MatchCase || !last.WholeWord {
		t.Errorf("and their going is reported: %+v", last)
	}
}

func TestStateMatchInfo(t *testing.T) {
	s := NewFindState()
	var events []ChangeEvent
	s.OnChange(func(e ChangeEvent) { events = append(events, e) })
	s.ChangeMatchInfo(3, 10, nil)
	r := buffer.Range{End: buffer.Position{Col: 3}}
	s.ChangeMatchInfo(3, 10, &r)
	s.ChangeMatchInfo(3, 10, &r) // nothing new
	if len(events) != 2 || !events[0].MatchesPosition || !events[0].MatchesCount || !events[1].CurrentMatch || events[1].MatchesCount {
		t.Fatalf("events %+v", events)
	}
	if cur := s.CurrentMatch(); cur == nil || *cur != r {
		t.Errorf("current match %v", cur)
	}
	s.ChangeMatchInfo(30, 10, nil)
	if s.MatchesPosition() != 10 {
		t.Errorf("a position past the count is the count: %d", s.MatchesPosition())
	}
	s.ChangeMatchInfo(4, 0, nil)
	if s.MatchesPosition() != 0 || s.MatchesCount() != 0 {
		t.Errorf("no matches, no position: %d of %d", s.MatchesPosition(), s.MatchesCount())
	}
}

func TestStateNavigation(t *testing.T) {
	for _, tc := range []struct {
		name            string
		loop            bool
		pos, count      int
		wantBack, wantF bool
	}{
		{"loops", true, 1, 5, true, true},
		{"first without a loop", false, 1, 5, false, true},
		{"last without a loop", false, 5, 5, true, false},
		{"middle without a loop", false, 3, 5, true, true},
		{"too many matches loop anyway", false, 1, MatchesLimit, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewFindState()
			s.Change(NewState{Loop: Some(tc.loop)}, false, true)
			s.ChangeMatchInfo(tc.pos, tc.count, nil)
			if s.CanNavigateBack() != tc.wantBack || s.CanNavigateForward() != tc.wantF {
				t.Errorf("back %v forward %v, want %v %v", s.CanNavigateBack(), s.CanNavigateForward(), tc.wantBack, tc.wantF)
			}
		})
	}
}

func TestStateSearchScope(t *testing.T) {
	s := NewFindState()
	var events []ChangeEvent
	s.OnChange(func(e ChangeEvent) { events = append(events, e) })
	a, b := rng(1, 1, 2, 1), rng(3, 1, 4, 1)
	s.Change(NewState{SearchScope: Some([]buffer.Range{a})}, false, true)
	s.Change(NewState{SearchScope: Some([]buffer.Range{a})}, false, true) // the same: nothing
	s.Change(NewState{SearchScope: Some([]buffer.Range{a, b})}, false, true)
	s.Change(NewState{SearchScope: Some([]buffer.Range{b})}, false, true)
	s.Change(NewState{SearchScope: Some[[]buffer.Range](nil)}, false, true)
	if len(events) != 4 {
		t.Fatalf("%d events", len(events))
	}
	if s.SearchScope() != nil {
		t.Errorf("scope %v", s.SearchScope())
	}
	s.Change(NewState{SearchScope: Some([]buffer.Range{a, b})}, false, true)
	if got := s.SearchScope(); !slices.Equal(got, []buffer.Range{a, b}) {
		t.Errorf("scope %v", got)
	}
	// the scope that is read is a copy
	got := s.SearchScope()
	got[0] = b
	if s.SearchScope()[0] != a {
		t.Errorf("the scope was changed from outside")
	}
}
