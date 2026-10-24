package editorgroup

import (
	"slices"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Ported from VS Code: src/vs/workbench/services/history/browser/historyService.ts
// (EditorNavigationStack, EditorSelectionState, EditorHelper) and
// src/vs/workbench/browser/parts/editor/textEditor.ts (TextEditorPaneSelection)

// SelectionReason says why the selection of an editor changed (VS Code's EditorPaneSelectionChangeReason).
type SelectionReason int

// The reasons a selection changes.
const (
	// ReasonNone is no reason: the active editor changed.
	ReasonNone SelectionReason = iota
	// ReasonProgrammatic is a change by code: a selection restored from a saved view, for example.
	ReasonProgrammatic
	// ReasonUser is a change by the user with the mouse or the keyboard.
	ReasonUser
	// ReasonEdit is a change made by editing.
	ReasonEdit
	// ReasonNavigation is a change by a navigation action, such as choosing an entry of an outline.
	ReasonNavigation
	// ReasonJump is a change by a jump from inside the editor: go to line, go to definition. A jump always
	// has a source and a target, so the place it came from is a navigation location too.
	ReasonJump
)

// SelectionCompare is the result of comparing two selections (VS Code's EditorPaneSelectionCompareResult).
type SelectionCompare int

// The results of Compare.
const (
	// SelectionIdentical is two selections on the same line.
	SelectionIdentical SelectionCompare = iota
	// SelectionSimilar is two selections that are near each other: less than SelectionThreshold lines apart.
	SelectionSimilar
	// SelectionDifferent is two selections that are far apart.
	SelectionDifferent
)

// SelectionThreshold is the number of lines the selection of a text editor has to move to count as a
// significant change (VS Code's TEXT_EDITOR_SELECTION_THRESHOLD).
const SelectionThreshold = 10

// NavSelection is the selection of a text editor as the history remembers it.
type NavSelection struct {
	Anchor, Active buffer.Position
}

// startLine is the first line of the selection.
func (s NavSelection) startLine() int { return min(s.Anchor.Line, s.Active.Line) }

// Compare says how far apart two selections are, counting the first line of each (VS Code's
// TextEditorPaneSelection.compare): the same line is identical, less than SelectionThreshold lines apart is
// similar.
func (s NavSelection) Compare(other NavSelection) SelectionCompare {
	a, b := s.startLine(), other.startLine()
	switch d := a - b; {
	case d == 0:
		return SelectionIdentical
	case d > -SelectionThreshold && d < SelectionThreshold:
		return SelectionSimilar
	}
	return SelectionDifferent
}

// Pane is the editor that is shown in a group, as the history sees it.
type Pane struct {
	// GroupID is the group.
	GroupID int
	// Input is the active editor of the group; nil when the group is empty.
	Input EditorInput
	// SelectionAware tells that the editor has a selection (a text editor does).
	SelectionAware bool
	// Selection is the selection of the editor, nil when it has none.
	Selection *NavSelection
}

// NavLocation is a place in an editor: the entry of a navigation stack. An editor with a file is remembered
// by the file, so that a location of a closed editor opens the file again; an editor with no file is kept
// as it is and goes away when it is disposed.
type NavLocation struct {
	// GroupID is the group the editor was shown in.
	GroupID int
	// Resource is the file of the editor, when HasResource is set.
	Resource    document.URI
	HasResource bool
	// Input is the editor, when it has no resource.
	Input EditorInput
	// Selection is where the cursor was, or nil.
	Selection *NavSelection
}

// NavLocationOf makes the location of an editor of a group.
func NavLocationOf(groupID int, input EditorInput, sel *NavSelection) NavLocation {
	l := NavLocation{GroupID: groupID, Selection: sel}
	if res, ok := input.Resource(); ok {
		l.Resource, l.HasResource = res, true
	} else {
		l.Input = input
	}
	return l
}

// matchesInput tells whether the location is in the editor of an input.
func (l NavLocation) matchesInput(input EditorInput) bool {
	if input == nil {
		return false
	}
	if l.HasResource {
		res, ok := input.Resource()
		return ok && res == l.Resource
	}
	return l.Input != nil && (l.Input == input || l.Input.Matches(input))
}

// sameEditor tells whether two locations are in the same editor.
func (l NavLocation) sameEditor(o NavLocation) bool {
	switch {
	case l.HasResource && o.HasResource:
		return l.Resource == o.Resource
	case l.HasResource != o.HasResource:
		return false
	}
	return l.Input != nil && o.Input != nil && (l.Input == o.Input || l.Input.Matches(o.Input))
}

// GoFilter says which locations a stack of the history holds (VS Code's GoFilter).
type GoFilter int

// The filters.
const (
	// GoAll is every selection change that VS Code's rules keep: Back and Forward.
	GoAll GoFilter = iota
	// GoEdits is the places that were edited.
	GoEdits
	// GoNavigation is the places that actions such as go to line and go to definition jumped to or from.
	GoNavigation
)

// Opener is what the history needs of the window to show a location.
type Opener interface {
	// OpenLocation shows a location: it activates the editor in the group of the location (the group is the
	// active group when it is gone), opens the file when the editor is closed, and puts the cursor where the
	// location says. It must call done once the editor is shown, which may be later when the file has to be
	// read: the history ignores the changes that showing the location causes until then.
	OpenLocation(loc NavLocation, done func())
	// ActivePane is the active editor of the active group.
	ActivePane() Pane
}

// selectionState is the selection the stack saw last (VS Code's EditorSelectionState).
type selectionState struct {
	loc    NavLocation
	reason SelectionReason
}

// justifiesNewEntry tells whether the state, being the current one, makes the candidate a new entry of the
// stack: another group or editor, an unknown selection, a selection far from this one, or a jump or navigation.
func (s selectionState) justifiesNewEntry(other selectionState) bool {
	if s.loc.GroupID != other.loc.GroupID {
		return true
	}
	if !s.loc.sameEditor(other.loc) {
		return true
	}
	if s.loc.Selection == nil || other.loc.Selection == nil {
		return true
	}
	result := s.loc.Selection.Compare(*other.loc.Selection)
	if result == SelectionSimilar && (other.reason == ReasonNavigation || other.reason == ReasonJump) {
		// a jump wins even when the selection is similar: go to definition adds an entry
		return true
	}
	return result == SelectionDifferent
}

// MaxNavigationStack is the number of locations a stack keeps (VS Code's MAX_STACK_SIZE).
const MaxNavigationStack = 50

// navStack is a stack of locations with a place in it: Back and Forward move the place.
type navStack struct {
	filter  GoFilter
	opener  Opener
	changed func()

	stack         []NavLocation
	index         int
	previousIndex int
	navigating    bool
	// epoch counts the navigations, so that a late done does not end a later one.
	epoch   int
	current *selectionState
}

func newNavStack(filter GoFilter, opener Opener, changed func()) *navStack {
	return &navStack{filter: filter, opener: opener, changed: changed, index: -1, previousIndex: -1}
}

// currentEntry is the entry at the place, or nil.
func (n *navStack) currentEntry() *NavLocation {
	if n.index < 0 || n.index >= len(n.stack) {
		return nil
	}
	return &n.stack[n.index]
}

func (n *navStack) fire() {
	if n.changed != nil {
		n.changed()
	}
}

// notify tells the stack that the active editor or its selection changed (notifyNavigation). A pane with no
// input is no editor.
func (n *navStack) notify(p *Pane, reason SelectionReason) {
	aware := p != nil && p.SelectionAware
	valid := p != nil && p.Input != nil
	// A change made by navigating the stack adds no entry, but the state follows it.
	if n.navigating {
		if aware && valid {
			n.current = &selectionState{loc: NavLocationOf(p.GroupID, p.Input, p.Selection), reason: reason}
		} else {
			n.current = nil
		}
		return
	}
	switch {
	case aware && valid:
		n.onSelectionAware(p, reason)
	default:
		n.current = nil
		if valid {
			n.onNonSelectionAware(p)
		}
	}
}

func (n *navStack) onSelectionAware(p *Pane, reason SelectionReason) {
	loc := NavLocationOf(p.GroupID, p.Input, p.Selection)
	if cur := n.currentEntry(); cur != nil && cur.GroupID == p.GroupID && p.Selection == nil && cur.matchesInput(p.Input) {
		return // the same editor again, with no selection
	}
	candidate := selectionState{loc: loc, reason: reason}
	if n.current == nil || n.current.justifiesNewEntry(candidate) {
		n.addOrReplace(loc, false)
	} else {
		n.addOrReplace(loc, true)
	}
	n.current = &candidate
}

func (n *navStack) onNonSelectionAware(p *Pane) {
	if cur := n.currentEntry(); cur != nil && cur.GroupID == p.GroupID && cur.matchesInput(p.Input) {
		return
	}
	n.addOrReplace(NavLocationOf(p.GroupID, p.Input, nil), false)
}

// shouldReplace tells whether a candidate replaces an entry instead of following it: the same group and
// editor, and a selection that is identical or not known yet.
func shouldReplace(entry, candidate NavLocation) bool {
	if entry.GroupID != candidate.GroupID || !entry.sameEditor(candidate) {
		return false
	}
	if entry.Selection == nil {
		return true
	}
	if candidate.Selection == nil {
		return false
	}
	return entry.Selection.Compare(*candidate.Selection) == SelectionIdentical
}

// addOrReplace adds a location after the place, dropping what was after it, or replaces the entry at the
// place when the location is the same place or forceReplace is set. It does nothing while navigating.
func (n *navStack) addOrReplace(loc NavLocation, forceReplace bool) {
	if n.navigating {
		return
	}
	cur := n.currentEntry()
	replace := cur != nil && (forceReplace || shouldReplace(*cur, loc))
	if replace {
		n.stack[n.index] = loc
	} else {
		n.stack = n.stack[:n.index+1]
		n.stack = append(n.stack, loc)
		if len(n.stack) > MaxNavigationStack {
			n.stack = n.stack[1:]
			if n.previousIndex >= 0 {
				n.previousIndex--
			}
		} else {
			n.previousIndex, n.index = n.index, n.index+1
		}
	}
	n.fire()
}

// add adds the location of a pane without the checks of notify: a jump adds the place it left.
func (n *navStack) add(loc NavLocation) { n.addOrReplace(loc, false) }

// remove drops the entries that match, joins the entries that become the same place and puts the place at
// the end.
func (n *navStack) remove(match func(NavLocation) bool) {
	before := len(n.stack)
	n.stack = slices.DeleteFunc(n.stack, match)
	if len(n.stack) == before {
		return
	}
	n.flatten()
	n.index, n.previousIndex = len(n.stack)-1, -1
	n.fire()
}

// flatten drops an entry that is the same place as the one before it.
func (n *navStack) flatten() {
	var out []NavLocation
	var prev *NavLocation
	for i := range n.stack {
		e := n.stack[i]
		if prev != nil && shouldReplace(e, *prev) {
			continue
		}
		out = append(out, e)
		prev = &out[len(out)-1]
	}
	n.stack = out
}

// move gives the entries of a file the path it moved to.
func (n *navStack) move(follow func(document.URI) (document.URI, bool)) {
	for i := range n.stack {
		e := &n.stack[i]
		if !e.HasResource {
			continue
		}
		if nu, ok := follow(e.Resource); ok && nu != e.Resource {
			e.Resource = nu
		}
	}
}

// clear forgets every location.
func (n *navStack) clear() {
	n.index, n.previousIndex = -1, -1
	n.stack = nil
	n.current = nil
	n.navigating = false
	n.epoch++
}

func (n *navStack) canGoForward() bool { return len(n.stack) > n.index+1 }
func (n *navStack) canGoBack() bool    { return n.index > 0 }
func (n *navStack) canGoLast() bool    { return len(n.stack) > 0 }

func (n *navStack) goForward() {
	if n.maybeGoCurrent() || !n.canGoForward() {
		return
	}
	n.setIndex(n.index + 1)
	n.navigate()
}

func (n *navStack) goBack() {
	if n.maybeGoCurrent() || !n.canGoBack() {
		return
	}
	n.setIndex(n.index - 1)
	n.navigate()
}

func (n *navStack) goPrevious() {
	if n.maybeGoCurrent() {
		return
	}
	if n.previousIndex == -1 {
		n.goBack()
		return
	}
	n.setIndex(n.previousIndex)
	n.navigate()
}

func (n *navStack) goLast() {
	if !n.canGoLast() {
		return
	}
	n.setIndex(len(n.stack) - 1)
	n.navigate()
}

// maybeGoCurrent reveals the entry at the place before moving, when the stack is filtered: not every
// selection change is in such a stack, so the editor may be somewhere else. It tells whether it did.
func (n *navStack) maybeGoCurrent() bool {
	if n.filter == GoAll || n.isCurrentSelectionActive() {
		return false
	}
	n.navigate()
	return true
}

// isCurrentSelectionActive tells whether the active editor is at the entry at the place.
func (n *navStack) isCurrentSelectionActive() bool {
	cur := n.currentEntry()
	if cur == nil || cur.Selection == nil {
		return false
	}
	p := n.opener.ActivePane()
	if !p.SelectionAware || p.Input == nil || p.Selection == nil || p.GroupID != cur.GroupID || !cur.matchesInput(p.Input) {
		return false
	}
	return p.Selection.Compare(*cur.Selection) == SelectionIdentical
}

func (n *navStack) setIndex(i int) {
	n.previousIndex, n.index = n.index, i
	n.fire()
}

// navigate shows the entry at the place. The stack ignores the changes of the editors until the opener says
// it is done.
func (n *navStack) navigate() {
	cur := n.currentEntry()
	if cur == nil {
		return
	}
	n.navigating = true
	n.epoch++
	epoch := n.epoch
	n.opener.OpenLocation(*cur, func() {
		if n.epoch == epoch {
			n.navigating = false
		}
	})
}
