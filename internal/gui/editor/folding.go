package editor

import (
	"log/slog"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/folding"
)

// Ported from VS Code: src/vs/editor/contrib/folding/browser/folding.ts (FoldingController)

// foldingOwner is the slot of the decorations of the folding regions (see ViewModel.owner).
const foldingOwner = 2

// foldLink is what a view model knows of the folding of its document.
type foldLink struct {
	model *folding.Model
	// unsubscribe stops following the model, and stopUpdater the background computation of
	// the regions.
	unsubscribe, stopUpdater func()
	// epoch counts the changes of the model; the projection was made for syncedEpoch.
	epoch, syncedEpoch int
	// hidden are the spans of hidden lines of the projection (the same as its own).
	hidden *foldedProjection
	// busy counts the changes of the folds that are under way: the cursors that change while it
	// is not 0 are not revealed (they are being kept out of a fold, or are those of a command
	// that folds).
	busy int
	// markers are what the commands that fold by region markers look for.
	markers *folding.Markers
	// memento is the folded state to apply when the regions are first known.
	memento folding.Memento
	// hoverGutter says that the pointer is over the gutter, which shows the controls of
	// editor.showFoldingControls "mouseover".
	hoverGutter bool
}

// foldDecoration is the options of the decoration of a folding region: it never grows when typed
// at the edges, and the class says what the region is.
func foldDecoration(collapsed, hidden, _ bool) document.DecorationOptions {
	opts := document.DecorationOptions{Stickiness: document.NeverGrowsWhenTypingAtEdges}
	switch {
	case hidden:
		opts.ClassName = "folding-hidden"
	case collapsed:
		opts.ClassName = "folding-collapsed"
	default:
		opts.ClassName = "folding-expanded"
	}
	return opts
}

// StartFolding makes the folding model of the document: its regions are the ones the provider
// finds in the background after every pause of the editing (a nil provider finds none, which
// is for a test that gives the regions with UpdateFolding). The returned function stops the
// folding and unfolds everything; it must be called on the UI thread.
func (vm *ViewModel) StartFolding(provider folding.Provider, delay time.Duration) (stop func()) {
	if vm.fold.model != nil {
		vm.stopFolding()
	}
	m := folding.NewModel(vm.doc, vm.owner(foldingOwner), foldDecoration)
	vm.fold.model = m
	vm.fold.unsubscribe = m.OnChange(func(folding.ChangeEvent) { vm.foldingChanged() })
	if provider != nil {
		vm.fold.stopUpdater = folding.Attach(vm.doc, provider, folding.UpdaterOptions{Delay: delay}, func(r *folding.Regions) {
			run := func() {
				if vm.fold.model != m {
					return
				}
				if err := vm.UpdateFolding(r); err != nil {
					slog.Error("cannot update the folding regions", "err", err)
				}
			}
			if post := vm.poster(); post != nil {
				post(run)
				return
			}
			run()
		})
	}
	return func() {
		if vm.fold.model == m {
			vm.stopFolding()
		}
	}
}

// stopFolding drops the folding model, its decorations and its background work.
func (vm *ViewModel) stopFolding() {
	f := &vm.fold
	if f.stopUpdater != nil {
		f.stopUpdater()
	}
	if f.unsubscribe != nil {
		f.unsubscribe()
	}
	f.model.Dispose()
	*f = foldLink{markers: f.markers, epoch: f.epoch + 1, syncedEpoch: f.syncedEpoch}
	vm.Sync()
	vm.NotifyListeners()
}

// FoldingModel is the folding model, or nil when the editor does not fold.
func (vm *ViewModel) FoldingModel() *folding.Model { return vm.fold.model }

// SetFoldingMarkers gives the commands that fold by kind the patterns of the lines that start
// and end a region in the language of the document (nil for none).
func (vm *ViewModel) SetFoldingMarkers(m *folding.Markers) { vm.fold.markers = m }

// UpdateFolding makes the regions a provider found the regions of the model, keeping what is
// folded and what the user made; a region with a cursor in it that changed is unfolded. It
// applies the folded state of a view state that waits for the regions.
func (vm *ViewModel) UpdateFolding(regions *folding.Regions) error {
	m := vm.fold.model
	if m == nil {
		return nil
	}
	if err := m.Update(regions, folding.LinesOf(vm.selectedLines())); err != nil {
		return err
	}
	if vm.fold.memento != nil {
		memento := vm.fold.memento
		vm.fold.memento = nil
		return m.ApplyMemento(memento)
	}
	return nil
}

// foldingEnabled says whether the editor folds: it has a model and editor.folding is on.
func (vm *ViewModel) foldingEnabled() bool { return vm.fold.model != nil && vm.opts.Folding }

// selectedLines are the first lines of the selections (zero-based).
func (vm *ViewModel) selectedLines() []int {
	sels := vm.cursors.Selections()
	lines := make([]int, len(sels))
	for i, s := range sels {
		lines[i] = s.Start().Line
	}
	return lines
}

// hiddenLines are the spans of the hidden lines now.
func (vm *ViewModel) hiddenLines() []folding.LineSpan {
	if !vm.foldingEnabled() {
		return nil
	}
	return vm.fold.model.HiddenLines()
}

// HiddenSpan is the span of hidden lines that has a line; ok is false for a line that is shown.
func (vm *ViewModel) HiddenSpan(line int) (first, last int, ok bool) {
	vm.Sync()
	if p := vm.fold.hidden; p != nil {
		return p.hiddenSpan(line)
	}
	return 0, 0, false
}

// FoldedLine says whether a line is shown with a folded range after it.
func (vm *ViewModel) FoldedLine(line int) bool {
	first, _, ok := vm.HiddenSpan(line + 1)
	return ok && first == line+1
}

// foldingChanged follows a change of the model: the projection is made again, the cursors the
// fold hides go to the line that holds it, and the layers are redrawn.
func (vm *ViewModel) foldingChanged() {
	vm.fold.epoch++
	vm.Sync()
	vm.keepCursorsOutOfFolds()
	vm.NotifyListeners()
	vm.sel.NotifyListeners()
}

// keepCursorsOutOfFolds moves the ends of the selections that are in hidden lines to the end of
// the line that holds the fold.
//
// Ported from VS Code: src/vs/editor/contrib/folding/browser/hiddenRangeModel.ts (adjustSelections)
func (vm *ViewModel) keepCursorsOutOfFolds() {
	if vm.fold.hidden == nil {
		return
	}
	sels := vm.cursors.Selections()
	changed := false
	adjust := func(p buffer.Position) buffer.Position {
		first, _, ok := vm.fold.hidden.hiddenSpan(p.Line)
		if !ok {
			return p
		}
		changed = true
		return vm.lineEndOf(buffer.Position{Line: first - 1})
	}
	for i, s := range sels {
		sels[i] = buffer.Selection{Anchor: adjust(s.Anchor), Active: adjust(s.Active)}
	}
	if !changed {
		return
	}
	vm.fold.busy++
	vm.cursors.SetSelections(sels)
	vm.fold.busy--
}

// revealCursors unfolds the folds that hide the first line of a selection, which a command that
// moved the cursor there (go to line, find, undo) asks for.
//
// Ported from VS Code: src/vs/editor/contrib/folding/browser/folding.ts (revealCursor)
func (vm *ViewModel) revealCursors() {
	if vm.fold.busy > 0 || !vm.foldingEnabled() || vm.fold.hidden == nil || len(vm.fold.hidden.spans) == 0 {
		return
	}
	var toggle []folding.Region
	for _, s := range vm.cursors.Selections() {
		line := s.Anchor.Line
		if _, _, hidden := vm.fold.hidden.hiddenSpan(line); !hidden {
			continue
		}
		toggle = append(toggle, vm.fold.model.AllRegionsAtLine(line, func(r folding.Region, _ int) bool {
			return r.Collapsed() && line > r.Start()
		})...)
	}
	if len(toggle) == 0 {
		return
	}
	vm.fold.busy++
	vm.fold.model.ToggleCollapseState(toggle)
	vm.fold.busy--
}

// cursorsChanged is told by the cursors that they changed.
func (vm *ViewModel) cursorsChanged() {
	vm.autoClosed.Validate(vm.cursors.Selections())
	vm.revealCursors()
	vm.highlightsCursorsChanged()
	vm.snippetCursorsChanged()
}

// ToggleFoldAt folds or unfolds the region that starts on a line, as a click on its control
// does: Alt works on the regions that are not around it, and Shift (or the middle button) on
// the regions inside it too. It does nothing when no region starts there.
//
// Ported from VS Code: src/vs/editor/contrib/folding/browser/folding.ts (onEditorMouseUp)
func (vm *ViewModel) ToggleFoldAt(line int, controlClicked, surrounding, recursive bool) {
	if !vm.foldingEnabled() {
		return
	}
	m := vm.fold.model
	region, ok := m.RegionAtLine(line)
	if !ok || region.Start() != line {
		return
	}
	collapsed := region.Collapsed()
	if !controlClicked && !collapsed {
		return
	}
	var toggle []folding.Region
	switch {
	case surrounding:
		maybe := m.RegionsInside(folding.Region{}, false, func(r folding.Region, _ int) bool {
			return !r.ContainedBy(region.Start(), region.End()) && !(region.ContainedBy(r.Start(), r.End()))
		})
		for _, r := range maybe {
			if r.Collapsed() {
				toggle = append(toggle, r)
			}
		}
		if len(toggle) == 0 {
			toggle = maybe
		}
	default:
		if recursive {
			for _, r := range m.RegionsInside(region, true, nil) {
				if r.Collapsed() == collapsed {
					toggle = append(toggle, r)
				}
			}
		}
		if collapsed || !recursive || len(toggle) == 0 {
			toggle = append(toggle, region)
		}
	}
	vm.fold.busy++
	m.ToggleCollapseState(toggle)
	vm.fold.busy--
}

// FoldingState says what the folding control of a line shows.
type FoldingState int

const (
	// FoldingNone is a line no region starts on.
	FoldingNone FoldingState = iota
	// FoldingExpanded is the first line of a region that is open.
	FoldingExpanded
	// FoldingCollapsed is the first line of a region that is folded.
	FoldingCollapsed
)

// FoldingStateOf is what the folding control of a line shows.
func (vm *ViewModel) FoldingStateOf(line int) FoldingState {
	if !vm.foldingEnabled() {
		return FoldingNone
	}
	region, ok := vm.fold.model.RegionAtLine(line)
	switch {
	case !ok || region.Start() != line:
		return FoldingNone
	case vm.FoldedLine(line):
		return FoldingCollapsed
	default:
		return FoldingExpanded
	}
}

// foldingMemento is the folded state of the editor for a view state.
func (vm *ViewModel) foldingMemento() folding.Memento {
	if vm.fold.model == nil {
		return vm.fold.memento
	}
	return vm.fold.model.Memento()
}

// restoreFolding applies the folded state of a view state, now or, when the regions are not
// known yet, as soon as they are.
func (vm *ViewModel) restoreFolding(m folding.Memento) error {
	if vm.fold.model == nil {
		vm.fold.memento = m
		return nil
	}
	if vm.fold.model.Regions().Len() == 0 {
		vm.fold.memento = m
		return nil
	}
	vm.fold.busy++
	defer func() { vm.fold.busy-- }()
	return vm.fold.model.ApplyMemento(m)
}

// setGutterHover tells that the pointer entered or left the gutter.
func (vm *ViewModel) setGutterHover(over bool) {
	if vm.fold.hoverGutter == over {
		return
	}
	vm.fold.hoverGutter = over
	if vm.opts.ShowFoldingControls == "mouseover" {
		vm.sel.NotifyListeners()
	}
}
