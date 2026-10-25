package mergeeditor

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/gui/diffeditor"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/view/viewModel.ts (the active conflict, the
// navigation between conflicts), view/scrollSynchronizer.ts (the scroll of the three editors), view/mergeEditor.ts
// (the zones that line the inputs up), view/editors/inputCodeEditorView.ts and view/editors/resultCodeEditorView.ts
// (the decorations), view/conflictActions.ts and view/colors.ts

// Side is one of the three editors of the merge editor.
type Side int

// The editors of the merge editor (SPEC §9.11): the incoming side (input 1) on the left of the top row, the current
// side (input 2) on its right, and the result below.
const (
	// Incoming is the editor of input 1.
	Incoming Side = iota
	// Current is the editor of input 2.
	Current
	// Result is the editor of the result.
	Result
)

// Input is the input of the merge that the editor of the side shows; false for the result.
func (s Side) Input() (diff.Input, bool) {
	switch s {
	case Incoming:
		return diff.Input1, true
	case Current:
		return diff.Input2, true
	}
	return 0, false
}

// Title is the name in the header of the editor.
func (s Side) Title() string {
	switch s {
	case Incoming:
		return "Incoming"
	case Current:
		return "Current"
	}
	return "Result"
}

// ModelOpts are the options of NewModel.
type ModelOpts struct {
	// Session holds the merge model and the result document. Required.
	Session *Session
	// Incoming, Current and Result are the view models of the three editors: the first two over the read-only
	// documents of the inputs, the third over the result document of the session. The model puts the alignment
	// zones, the marks and the borders on them and keeps them scrolled together.
	Incoming, Current, Result *editor.ViewModel
	// Focus gives the keyboard focus to each editor (the widget hands them to the editors it builds); the model
	// makes its own for a side that has none.
	Focus [3]*editor.EditorFocus
	// IncomingLabel and CurrentLabel are what the headers of the inputs say after their titles (the branch of the
	// marker, the name of the file of --merge); ResultLabel is what the header of the result says (the name of its file).
	IncomingLabel, CurrentLabel, ResultLabel string
	// Post hands a function to the UI thread (the binding's Post). Required.
	Post func(func())
	// OnError is told about a failure of a toggle of a checkbox or of the zones.
	OnError func(error)
}

// Model is the state of a merge editor: the session, the zones that line the two inputs up, the marks and borders that
// show the conflicts, the scroll position the three editors share, the active conflict and the navigation between the
// conflicts. It works on the UI thread only.
type Model struct {
	foundation.ChangeNotifier
	opts   ModelOpts
	sess   *Session
	vms    [3]*editor.ViewModel
	focus  [3]*editor.EditorFocus
	colors kit.Colors

	// zones are the alignment zones of the two inputs.
	zones [2][]editor.ZoneID
	// actionZones are the zones of the rows of actions above the conflicts, in each of the three editors (actions.go).
	actionZones [3][]actionZone
	// syncing is set while one editor scrolls the others, so that the scroll does not come back.
	syncing bool
	// leader is the editor that scrolled last; the others follow it when the sizes change.
	leader Side
	// active is the region the cursor of the editor that was used last is in, -1 for none; last is that editor.
	active int
	last   Side
	// mirroring is set while the model puts the cursors of the editors, which is not a move of the cursor by the user.
	mirroring bool
	// cursor is where the cursor of each editor was when its selection was last looked at.
	cursor [3]buffer.Position

	scrolls  [3]foundation.ListenerHandle
	metrics  [3]foundation.ListenerHandle
	sels     [3]foundation.ListenerHandle
	stops    []func()
	realigns bool
	disposed bool
	// repaint tells the gutters of the inputs that their checkboxes moved: the editor scrolled, the sizes or the regions changed.
	repaint *foundation.ChangeNotifier
}

// NewModel makes the model of a merge editor over a session and three view models, lines the inputs up and marks the
// conflicts.
func NewModel(opts ModelOpts) *Model {
	m := &Model{ChangeNotifier: *foundation.NewChangeNotifier(), opts: opts, sess: opts.Session, active: -1, last: Result, leader: Result, repaint: foundation.NewChangeNotifier()}
	m.vms = [3]*editor.ViewModel{opts.Incoming, opts.Current, opts.Result}
	m.focus = opts.Focus
	for i := range m.focus {
		if m.focus[i] == nil {
			m.focus[i] = &editor.EditorFocus{}
		}
	}
	for i, vm := range m.vms {
		side := Side(i)
		m.scrolls[i] = vm.AddScrollListener(func() { m.scrolled(side) })
		m.metrics[i] = vm.AddMetricsListener(func() { m.metricsChanged() })
		m.sels[i] = vm.AddSelectionListener(func() { m.selectionChanged(side) })
	}
	m.stops = append(m.stops, m.sess.OnChange(m.sessionChanged))
	m.alignInputs()
	m.addActionZones()
	m.decorate()
	return m
}

// Session is the session the model shows.
func (m *Model) Session() *Session { return m.sess }

// View is the view model of an editor.
func (m *Model) View(s Side) *editor.ViewModel { return m.vms[s] }

// Focus gives the keyboard focus to the editor of a side while it is on screen.
func (m *Model) Focus(s Side) *editor.EditorFocus { return m.focus[s] }

// FocusedSide tells which editor has the keyboard focus; false when none does.
func (m *Model) FocusedSide() (Side, bool) {
	for _, s := range []Side{Result, Incoming, Current} {
		if m.focus[s].HasFocus() {
			return s, true
		}
	}
	return Result, false
}

// FocusSide gives the keyboard focus to an editor.
func (m *Model) FocusSide(s Side) bool { return m.focus[s].Request() }

// Label is what the header of a side says after its title.
func (m *Model) Label(s Side) string {
	switch s {
	case Incoming:
		return m.opts.IncomingLabel
	case Current:
		return m.opts.CurrentLabel
	}
	return m.opts.ResultLabel
}

// Remaining is the count of the header of the result: "1 Conflict Remaining" or "N Conflicts Remaining" (VS Code's
// words, without the space at the end of the plural).
func (m *Model) Remaining() string {
	n := m.sess.Model().UnhandledCount()
	if n == 1 {
		return "1 Conflict Remaining"
	}
	return strconv.Itoa(n) + " Conflicts Remaining"
}

// RemainingTooltip is the hover of the count: where it leads, or that the merge can be completed.
func (m *Model) RemainingTooltip() string {
	if m.sess.Model().UnhandledCount() > 0 {
		return "Go to next conflict"
	}
	return "All conflicts handled, the merge can be completed now."
}

// SetColors sets the colors of the marks and the borders; they are made again when the colors differ.
func (m *Model) SetColors(c kit.Colors) {
	if c == m.colors {
		return
	}
	m.colors = c
	m.opts.Post(func() {
		if !m.disposed {
			m.decorate()
			m.NotifyListeners()
		}
	})
}

// ---- the zones of the inputs ----

// alignInputs puts the spacers that line the two inputs up in them. The inputs are never edited, so this is done once.
func (m *Model) alignInputs() {
	var ranges []diff.ModifiedBaseRange
	for _, r := range m.sess.Model().Regions() {
		ranges = append(ranges, r.Range)
	}
	s1, s2 := InputSpacers(ranges)
	for i, spacers := range [][]Spacer{s1, s2} {
		vm := m.vms[i]
		err := vm.ChangeZones(func(c editor.ZoneChanges) error {
			for _, sp := range spacers {
				id, err := c.Add(editor.ZoneSpec{AfterLine: sp.Before - 1, Rows: sp.Rows, ShowInHiddenAreas: true, Widget: diffeditor.NewFiller()})
				if err != nil {
					return err
				}
				m.zones[i] = append(m.zones[i], id)
			}
			return nil
		})
		if err != nil && m.opts.OnError != nil {
			m.opts.OnError(err)
		}
	}
}

// Spacers are the alignment zones an input has: after which line, how many rows. The inputs are the first two sides.
func (m *Model) Spacers(s Side) []Spacer {
	if s == Result {
		return nil
	}
	var out []Spacer
	for _, z := range m.vms[s].Zones() {
		for _, id := range m.zones[s] {
			if id == z.ID {
				out = append(out, Spacer{Before: z.AfterLine + 1, Rows: z.Rows})
			}
		}
	}
	return out
}

// ---- regions ----

// RangeIn is the lines (from 1, the end excluded) a region has in the editor of a side now.
func RangeIn(r diff.Region, s Side) diff.LineRange {
	if in, ok := s.Input(); ok {
		return r.Range.InputRange(in)
	}
	return r.Result
}

// Ranges are the lines every region has in the editor of a side, in the order of Session.Model().Regions().
func (m *Model) Ranges(s Side) []diff.LineRange {
	regions := m.sess.Model().Regions()
	out := make([]diff.LineRange, len(regions))
	for i, r := range regions {
		out[i] = RangeIn(r, s)
	}
	return out
}

// Active is the region the cursor of the editor that was used last is in (VS Code's activeModifiedBaseRange); false
// when it is in none. It is what the borders show as focused.
func (m *Model) Active() (diff.Region, bool) {
	regions := m.sess.Model().Regions()
	if m.active < 0 || m.active >= len(regions) {
		return diff.Region{}, false
	}
	return regions[m.active], true
}

// selectionChanged follows the cursor of an editor: the editor that moved last decides the active region. The
// listener also hears of changes of the document and of the focus, so only a cursor that is somewhere else counts.
func (m *Model) selectionChanged(s Side) {
	if m.disposed || m.mirroring {
		return
	}
	pos := m.vms[s].Cursors().Primary().Position()
	if pos == m.cursor[s] {
		return
	}
	m.cursor[s] = pos
	// the editor the user works in decides: the other two move with it
	if side, focused := m.FocusedSide(); focused && side != s {
		return
	}
	idx, ok := ActiveConflict(m.Ranges(s), pos.Line+1)
	if !ok {
		idx = -1
	}
	if s == m.last && idx == m.active {
		return
	}
	m.last, m.active = s, idx
	m.decorate()
	m.repaint.NotifyListeners()
	m.NotifyListeners()
}

// ---- decorations ----

// decorate makes the marks and the borders of the three editors from the regions and the active region.
func (m *Model) decorate() {
	c := m.colors
	regions := m.sess.Model().Regions()
	result := m.vms[Result].Document().Snapshot()
	for _, s := range []Side{Incoming, Current, Result} {
		vm := m.vms[s]
		var marks []editor.BackgroundMark
		var blocks []editor.LineBlock
		snap := vm.Document().Snapshot()
		lines := snap.LineCount()
		for i, r := range regions {
			if !r.Conflict {
				continue // the changes that do not conflict are in the result already
			}
			rng := RangeIn(r, s)
			blocks = append(blocks, m.block(r, i == m.active, rng))
			switch in, isInput := s.Input(); {
			case isInput:
				for _, d := range r.Range.InputDiffs(in) {
					marks = append(marks, wholeLines(snap, d.Modified, c.MergeChange)...)
					for _, inner := range d.Inner {
						mod := inner.Modified
						if mod.Start == mod.End || mod.Start.Line < 1 || mod.End.Line > lines {
							continue
						}
						marks = append(marks, editor.BackgroundMark{
							Range: buffer.Range{
								Start: buffer.Position{Line: mod.Start.Line - 1, Col: mod.Start.Column - 1},
								End:   buffer.Position{Line: mod.End.Line - 1, Col: mod.End.Column - 1},
							},
							Background: c.MergeChangeWord,
						})
					}
				}
			default:
				// the lines of the result that are not the base's lines (VS Code colors the diffs of the result against the base)
				if resultDiffersFromBase(result, rng, r.Range.BaseLines()) {
					marks = append(marks, wholeLines(snap, rng, c.MergeChange)...)
				}
			}
		}
		vm.SetBackgroundMarks(marks)
		vm.SetLineBlocks(blocks)
	}
}

// block is the border of a conflict in an editor: 1 px, in the color of its state; a focused unhandled conflict has 2 px.
func (m *Model) block(r diff.Region, focused bool, rng diff.LineRange) editor.LineBlock {
	c := m.colors
	b := editor.LineBlock{StartLine: rng.StartLine - 1, EndLine: rng.EndLine - 1, Width: 1}
	switch {
	case !r.Handled && focused:
		b.Color, b.Width = c.MergeUnhandledFocusedBorder, 2
	case !r.Handled:
		b.Color = c.MergeUnhandledUnfocusedBorder
	case focused:
		b.Color = c.MergeHandledFocusedBorder
	default:
		b.Color = c.MergeHandledUnfocusedBorder
	}
	return b
}

// wholeLines is the mark of the lines of a range of an editor, none for an empty range or one the text does not have.
func wholeLines(snap buffer.Snapshot, r diff.LineRange, color ui.Color) []editor.BackgroundMark {
	if r.IsEmpty() || r.StartLine < 1 || r.EndLine-1 > snap.LineCount() || color.Alpha() == 0 {
		return nil
	}
	last := r.EndLine - 2
	n, _ := snap.LineLength(last)
	return []editor.BackgroundMark{{
		Range:     buffer.Range{Start: buffer.Position{Line: r.StartLine - 1}, End: buffer.Position{Line: last, Col: n}},
		WholeLine: true, Background: color,
	}}
}

func resultDiffersFromBase(snap buffer.Snapshot, rng diff.LineRange, base []string) bool {
	if rng.Len() != len(base) {
		return true
	}
	for i := range base {
		got, err := snap.LineContent(rng.StartLine - 1 + i)
		if err != nil || got != base[i] {
			return true
		}
	}
	return false
}

// ---- the session changed ----

// sessionChanged follows a change of the result or of the regions: the borders and marks are made again and the
// editors are scrolled to where the regions are now.
func (m *Model) sessionChanged() {
	if m.disposed {
		return
	}
	m.decorate()
	m.queueRealign()
	m.repaint.NotifyListeners()
	m.NotifyListeners()
}

// queueRealign scrolls the editors again after the layout that is going on; several requests are one.
func (m *Model) queueRealign() {
	if m.realigns {
		return
	}
	m.realigns = true
	m.opts.Post(func() {
		m.realigns = false
		if !m.disposed {
			m.syncActionZones()
			m.follow(m.leader)
		}
	})
}

// ---- scrolling ----

// scrolled keeps the other editors where this one is.
func (m *Model) scrolled(s Side) {
	if m.disposed {
		return
	}
	m.repaint.NotifyListeners()
	if m.syncing {
		return
	}
	m.leader = s
	m.follow(s)
}

// metricsChanged follows a change of the sizes of an editor: the editor that was scrolled last keeps the lead.
func (m *Model) metricsChanged() {
	if m.disposed {
		return
	}
	m.repaint.NotifyListeners()
	if m.syncing {
		return
	}
	m.follow(m.leader)
}

// follow scrolls the other editors to where the editor of a side is (VS Code's ScrollSynchronizer in the mixed
// layout): every editor has the same horizontal scroll; the inputs, which the zones line up, have the same
// vertical scroll; the result is scrolled to the place that corresponds to the top of an input, and an input to the
// place that corresponds to the top of the result, through the ranges of the regions.
func (m *Model) follow(s Side) {
	from := m.vms[s]
	m.syncing = true
	defer func() { m.syncing = false }()
	for i, vm := range m.vms {
		if Side(i) != s && vm.ScrollLeft() != from.ScrollLeft() {
			vm.SetScrollLeft(from.ScrollLeft())
		}
	}
	regions := m.sess.Model().Regions()
	for i, to := range m.vms {
		other := Side(i)
		if other == s {
			continue
		}
		var target float64
		_, fromInput := s.Input()
		_, toInput := other.Input()
		switch {
		case fromInput && toInput:
			target = from.ScrollTop() // the zones line the inputs up
		case fromInput:
			in, _ := s.Input()
			target = InputMap(regions, in).ScrollFor(topLine(from), from.ScrollTop(), lineTops(from), lineTops(to))
		default:
			in, _ := other.Input()
			target = InputMap(regions, in).Reverse().ScrollFor(topLine(from), from.ScrollTop(), lineTops(from), lineTops(to))
		}
		if target != to.ScrollTop() {
			to.ScrollTo(max(target, 0))
		}
	}
}

// topLine is the line (counted from 1) at the top of an editor.
func topLine(vm *editor.ViewModel) int {
	line, _, _ := vm.Projection().RowSpan(vm.Projection().RowAt(vm.ScrollTop()))
	return line + 1
}

// lineTops gives the pixel at which a line (counted from 1) begins in an editor, zones above it included; for the line
// after the last one, the height of all the rows.
func lineTops(vm *editor.ViewModel) func(line int) float64 {
	proj := vm.Projection()
	lines := vm.Document().Snapshot().LineCount()
	return func(line int) float64 {
		if line-1 >= lines {
			return proj.Height()
		}
		first, _ := proj.LineRows(max(line-1, 0))
		return proj.RowTop(first)
	}
}

// ---- navigation and the checkboxes ----

// GoToConflict moves the cursor to the next or the previous conflict that is not handled, wrapping around at the
// ends, and shows it in the middle of the editor that was used last (VS Code's goToNextModifiedBaseRange and
// goToPreviousModifiedBaseRange; the editor was the last focused one, the result at first). The cursors of the other
// editors go to the same conflict, and they follow the scroll. It focuses the editor and reports false when no
// conflict is left to go to.
func (m *Model) GoToConflict(next bool) bool {
	side := m.last
	if s, ok := m.FocusedSide(); ok {
		side = s
	}
	regions := m.sess.Model().Regions()
	var cands []diff.LineRange
	var index []int
	for i, r := range regions {
		if r.Conflict && !r.Handled {
			cands = append(cands, RangeIn(r, side))
			index = append(index, i)
		}
	}
	line := m.vms[side].Cursors().Primary().Position().Line + 1
	pick, ok := ConflictToGo(cands, line, next)
	if !ok {
		return false
	}
	m.reveal(regions[index[pick]], side)
	return true
}

// reveal makes a region the active one: the cursors of the three editors go to its start, the editor of the side
// shows it in the middle and takes the focus.
func (m *Model) reveal(r diff.Region, side Side) {
	m.mirroring = true
	for i, vm := range m.vms {
		snap := vm.Document().Snapshot()
		first, _ := RevealLines(RangeIn(r, Side(i)), snap.LineCount())
		pos := buffer.Position{Line: first - 1, Col: firstNonBlank(snap, first-1)}
		vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(pos)})
		vm.SelectionsChanged()
		m.cursor[i] = pos
	}
	m.mirroring = false
	m.last, m.active = side, r.Index
	vm := m.vms[side]
	vm.RevealPositionInCenter(vm.Cursors().Primary().Position())
	m.focus[side].Request()
	m.decorate()
	m.repaint.NotifyListeners()
	m.NotifyListeners()
}

// firstNonBlank is the column of the first character of a line that is not white space, or the end of the line.
func firstNonBlank(snap buffer.Snapshot, line int) int {
	text, err := snap.LineContent(line)
	if err != nil {
		return 0
	}
	if i := strings.IndexFunc(text, func(r rune) bool { return r != ' ' && r != '\t' }); i >= 0 {
		return i
	}
	return len(text)
}

// Toggle includes the lines of an input in the result of a conflict, or takes them out, as the checkbox of the input
// does (VS Code's setState with the toggled state): one undoable edit of the result. The conflict becomes the active
// one. conflict is the index in Session().Model().Conflicts().
func (m *Model) Toggle(conflict int, in diff.Input) {
	conflicts := m.sess.Model().Conflicts()
	if conflict >= 0 && conflict < len(conflicts) {
		m.active = conflicts[conflict].Index
	}
	if err := m.sess.Toggle(conflict, in); err != nil && m.opts.OnError != nil {
		m.opts.OnError(err)
	}
}

// ---- the checkboxes of the inputs ----

// GutterItem is a checkbox of the gutter of an input: the conflict it is for, where its lines are, and what it shows.
type GutterItem struct {
	// Conflict is the index of the conflict in Session().Model().Conflicts().
	Conflict int
	// Region is the conflict's region.
	Region diff.Region
	// Top and Height are the extent of the conflict's lines in the editor, relative to the top of the view (so
	// negative above it, and a range with no line has height 0).
	Top, Height float64
	// Check is what the checkbox shows: whether the result has the lines of the input.
	Check ChecksOf
	// Handled and Focused are the state of the conflict.
	Handled, Focused bool
}

// GutterItems are the checkboxes of an input, one for each conflict that the input changed (VS Code shows a checkbox
// where the input has changes in the range and the range is a conflict or is not handled), with their places.
func (m *Model) GutterItems(s Side) []GutterItem {
	in, ok := s.Input()
	if !ok {
		return nil
	}
	vm := m.vms[s]
	proj := vm.Projection()
	lines := vm.Document().Snapshot().LineCount()
	scroll := vm.ScrollTop()
	var out []GutterItem
	for _, r := range m.sess.Model().Conflicts() {
		if len(r.Range.InputDiffs(in)) == 0 {
			continue
		}
		rng := r.Range.InputRange(in)
		var top, bottom float64
		switch {
		case lines == 0:
		case rng.IsEmpty():
			top = lineTops(vm)(min(rng.StartLine, lines+1))
			bottom = top
		default:
			first, _ := proj.LineRows(rng.StartLine - 1)
			lastFirst, count := proj.LineRows(min(rng.EndLine-1, lines) - 1)
			last := lastFirst + max(count, 1) - 1
			top, bottom = proj.RowTop(first), proj.RowTop(last)+proj.RowHeight(last)
		}
		check := CheckEmpty
		switch st := r.State; {
		case st.Kind == diff.StateUnrecognized:
			check = CheckUnrecognized
		case st.InputState(in) == diff.InputFirst:
			check = CheckFirst
		case st.InputState(in) == diff.InputSecond:
			check = CheckSecond
		}
		out = append(out, GutterItem{
			Conflict: r.ConflictIndex, Region: r, Top: top - scroll, Height: bottom - top,
			Check: check, Handled: r.Handled, Focused: r.Index == m.active,
		})
	}
	return out
}

// Dispose stops the listeners and takes the zones, the marks and the borders off the editors.
func (m *Model) Dispose() {
	if m.disposed {
		return
	}
	m.disposed = true
	for _, stop := range m.stops {
		stop()
	}
	m.stops = nil
	for i, vm := range m.vms {
		vm.RemoveScrollListener(m.scrolls[i])
		vm.RemoveMetricsListener(m.metrics[i])
		vm.RemoveSelectionListener(m.sels[i])
		vm.DisposeMarks()
		vm.SetLineBlocks(nil)
		var ids []editor.ZoneID
		if i < 2 {
			ids = m.zones[i]
		}
		for _, z := range m.actionZones[i] {
			ids = append(ids, z.id)
		}
		if err := vm.ChangeZones(func(c editor.ZoneChanges) error {
			var first error
			for _, id := range ids {
				if err := c.Remove(id); err != nil && first == nil {
					first = err
				}
			}
			return first
		}); err != nil && m.opts.OnError != nil {
			m.opts.OnError(err)
		}
		if i < 2 {
			m.zones[i] = nil
		}
		m.actionZones[i] = nil
	}
}
