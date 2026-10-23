package find

import (
	"slices"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// Ported from VS Code: src/vs/editor/contrib/find/browser/findState.ts (MIT)

// MatchesLimit is the most matches that are marked and counted (VS Code's MATCHES_LIMIT).
const MatchesLimit = 19999

// Override says that an option is forced for a search, whatever the user's switch says (VS Code's
// FindOptionOverride): the find widget does that for a find with arguments.
type Override int

const (
	// NotSet leaves the option as the user set it.
	NotSet Override = iota
	// True turns the option on.
	True
	// False turns the option off.
	False
)

func effective(o Override, value bool) bool {
	switch o {
	case True:
		return true
	case False:
		return false
	}
	return value
}

// Opt is a value that may be left out of a change of the state.
type Opt[T any] struct {
	Value T
	Set   bool
}

// Some is the option with a value.
func Some[T any](v T) Opt[T] { return Opt[T]{Value: v, Set: true} }

// NewState is a change of a State: the options that are Set take their values.
type NewState struct {
	SearchString, ReplaceString   Opt[string]
	IsRevealed, IsReplaceRevealed Opt[bool]
	IsRegex, WholeWord            Opt[bool]
	MatchCase, PreserveCase       Opt[bool]
	// SearchScope are the ranges to search in; a nil slice searches everywhere.
	SearchScope Opt[[]buffer.Range]
	Loop        Opt[bool]
	IsSearching Opt[bool]
	// The overrides apply to this change only: they are NotSet again with the next change.
	IsRegexOverride, WholeWordOverride, MatchCaseOverride, PreserveCaseOverride Override
}

// ChangeEvent tells what a change of the state changed.
type ChangeEvent struct {
	// MoveCursor and UpdateHistory are what the change was asked to do.
	MoveCursor, UpdateHistory bool

	SearchString, ReplaceString   bool
	IsRevealed, IsReplaceRevealed bool
	IsRegex, WholeWord            bool
	MatchCase, PreserveCase       bool
	SearchScope                   bool
	MatchesPosition, MatchesCount bool
	CurrentMatch                  bool
	Loop, IsSearching             bool
}

// State is the state of the find widget (VS Code's FindReplaceState): what is searched for and how,
// and what the search found. The model of the matches and the widget follow it through OnChange.
// It is used on the UI thread.
type State struct {
	searchString, replaceString   string
	isRevealed, isReplaceRevealed bool
	isRegex, wholeWord            bool
	matchCase, preserveCase       bool
	isRegexOverride               Override
	wholeWordOverride             Override
	matchCaseOverride             Override
	preserveCaseOverride          Override
	searchScope                   []buffer.Range
	matchesPosition               int
	matchesCount                  int
	currentMatch                  *buffer.Range
	loop                          bool
	isSearching                   bool

	mu        sync.Mutex
	listeners map[int]func(ChangeEvent)
	next      int
}

// NewFindState makes the state of a search that has not started: nothing searched for, and the
// search loops round the text.
func NewFindState() *State {
	return &State{loop: true, listeners: map[int]func(ChangeEvent){}}
}

// SearchString is the text or the pattern searched for.
func (s *State) SearchString() string { return s.searchString }

// ReplaceString is the replacement.
func (s *State) ReplaceString() string { return s.replaceString }

// IsRevealed says that the find widget is shown, and IsReplaceRevealed that its replace row is.
func (s *State) IsRevealed() bool { return s.isRevealed }

// IsReplaceRevealed says that the replace row of the find widget is shown.
func (s *State) IsReplaceRevealed() bool { return s.isReplaceRevealed }

// IsRegex says that the search string is a regular expression (with the override of the change).
func (s *State) IsRegex() bool { return effective(s.isRegexOverride, s.isRegex) }

// WholeWord says that only whole words match.
func (s *State) WholeWord() bool { return effective(s.wholeWordOverride, s.wholeWord) }

// MatchCase says that the search is case sensitive.
func (s *State) MatchCase() bool { return effective(s.matchCaseOverride, s.matchCase) }

// PreserveCase says that a replacement follows the case of the text it replaces.
func (s *State) PreserveCase() bool { return effective(s.preserveCaseOverride, s.preserveCase) }

// ActualIsRegex is the switch of regular expressions, without the override.
func (s *State) ActualIsRegex() bool { return s.isRegex }

// ActualWholeWord is the switch of whole words, without the override.
func (s *State) ActualWholeWord() bool { return s.wholeWord }

// ActualMatchCase is the switch of the case, without the override.
func (s *State) ActualMatchCase() bool { return s.matchCase }

// ActualPreserveCase is the switch of preserving the case, without the override.
func (s *State) ActualPreserveCase() bool { return s.preserveCase }

// SearchScope are the ranges that are searched, or nil for the whole text.
func (s *State) SearchScope() []buffer.Range { return slices.Clone(s.searchScope) }

// MatchesPosition is the number, from 1, of the current match, or 0 when there is none or it is
// not known.
func (s *State) MatchesPosition() int { return s.matchesPosition }

// MatchesCount is the number of matches.
func (s *State) MatchesCount() int { return s.matchesCount }

// CurrentMatch is the match that was moved to last, or nil.
func (s *State) CurrentMatch() *buffer.Range {
	if s.currentMatch == nil {
		return nil
	}
	r := *s.currentMatch
	return &r
}

// IsSearching says that a search is under way.
func (s *State) IsSearching() bool { return s.isSearching }

// OnChange registers a function that is called after every change of the state that changed
// something; the returned function removes it.
func (s *State) OnChange(fn func(ChangeEvent)) (unsubscribe func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id := s.next
	s.listeners[id] = fn
	return func() {
		s.mu.Lock()
		delete(s.listeners, id)
		s.mu.Unlock()
	}
}

// Dispose drops the listeners.
func (s *State) Dispose() {
	s.mu.Lock()
	s.listeners = map[int]func(ChangeEvent){}
	s.mu.Unlock()
}

func (s *State) fire(e ChangeEvent) {
	s.mu.Lock()
	ids := make([]int, 0, len(s.listeners))
	for id := range s.listeners {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	fns := make([]func(ChangeEvent), len(ids))
	for i, id := range ids {
		fns[i] = s.listeners[id]
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn(e)
	}
}

// ChangeMatchInfo records what the search found: the position of the current match and the number
// of matches, and, when current is not nil, the current match itself.
func (s *State) ChangeMatchInfo(position, count int, current *buffer.Range) {
	var e ChangeEvent
	changed := false
	if count == 0 {
		position = 0
	}
	position = min(position, count)
	if s.matchesPosition != position {
		s.matchesPosition, e.MatchesPosition, changed = position, true, true
	}
	if s.matchesCount != count {
		s.matchesCount, e.MatchesCount, changed = count, true, true
	}
	if current != nil && !equalRangePtr(s.currentMatch, current) {
		r := *current
		s.currentMatch, e.CurrentMatch, changed = &r, true, true
	}
	if changed {
		s.fire(e)
	}
}

func equalRangePtr(a, b *buffer.Range) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Change applies a change of the state and tells the listeners what it changed; moveCursor says that
// the search should move to the next match, and updateHistory that the search string is to be
// remembered.
func (s *State) Change(n NewState, moveCursor, updateHistory bool) {
	e := ChangeEvent{MoveCursor: moveCursor, UpdateHistory: updateHistory}
	changed := false
	oldRegex, oldWord, oldCase, oldPreserve := s.IsRegex(), s.WholeWord(), s.MatchCase(), s.PreserveCase()

	setString := func(dst *string, o Opt[string], flag *bool) {
		if o.Set && *dst != o.Value {
			*dst, *flag, changed = o.Value, true, true
		}
	}
	setBool := func(dst *bool, o Opt[bool], flag *bool) {
		if o.Set && *dst != o.Value {
			*dst, *flag, changed = o.Value, true, true
		}
	}
	setString(&s.searchString, n.SearchString, &e.SearchString)
	setString(&s.replaceString, n.ReplaceString, &e.ReplaceString)
	setBool(&s.isRevealed, n.IsRevealed, &e.IsRevealed)
	setBool(&s.isReplaceRevealed, n.IsReplaceRevealed, &e.IsReplaceRevealed)
	if n.IsRegex.Set {
		s.isRegex = n.IsRegex.Value
	}
	if n.WholeWord.Set {
		s.wholeWord = n.WholeWord.Value
	}
	if n.MatchCase.Set {
		s.matchCase = n.MatchCase.Value
	}
	if n.PreserveCase.Set {
		s.preserveCase = n.PreserveCase.Value
	}
	if n.SearchScope.Set && scopeChanges(s.searchScope, n.SearchScope.Value) {
		s.searchScope, e.SearchScope, changed = slices.Clone(n.SearchScope.Value), true, true
	}
	setBool(&s.loop, n.Loop, &e.Loop)
	setBool(&s.isSearching, n.IsSearching, &e.IsSearching)

	// Overrides are set when they come in, and reset whenever something else changes.
	s.isRegexOverride, s.wholeWordOverride = n.IsRegexOverride, n.WholeWordOverride
	s.matchCaseOverride, s.preserveCaseOverride = n.MatchCaseOverride, n.PreserveCaseOverride

	if oldRegex != s.IsRegex() {
		e.IsRegex, changed = true, true
	}
	if oldWord != s.WholeWord() {
		e.WholeWord, changed = true, true
	}
	if oldCase != s.MatchCase() {
		e.MatchCase, changed = true, true
	}
	if oldPreserve != s.PreserveCase() {
		e.PreserveCase, changed = true, true
	}
	if changed {
		s.fire(e)
	}
}

// scopeChanges says whether a new search scope differs from the one there is: VS Code leaves a scope
// alone only when every new range is there already.
func scopeChanges(old, scope []buffer.Range) bool {
	if scope == nil || old == nil {
		return (scope == nil) != (old == nil)
	}
	return !slices.Equal(old, scope)
}

// CanNavigateBack says that there is a match before the current one, or that the search loops.
func (s *State) CanNavigateBack() bool { return s.canNavigateInLoop() || s.matchesPosition != 1 }

// CanNavigateForward says that there is a match after the current one, or that the search loops.
func (s *State) CanNavigateForward() bool {
	return s.canNavigateInLoop() || s.matchesPosition < s.matchesCount
}

func (s *State) canNavigateInLoop() bool { return s.loop || s.matchesCount >= MatchesLimit }
