package editor

import (
	"math"

	"github.com/rm4n0s/glutter/foundation"
)

// Ported from VS Code: src/vs/editor/browser/viewParts/minimap/minimap.ts (Minimap, InnerMinimap)

// minimapState is what the view model knows of the minimap: where it is, the layout of its last
// frame and what the pointer is doing to it.
type minimapState struct {
	info     minimapLayoutInfo
	in       minimapInput
	sampling *minimapSampling
	// layout is the slider and the lines of the last frame; drawn says that there was one.
	layout minimapRenderLayout
	drawn  bool
	// hover says that the pointer is over the minimap, active that its slider is being dragged,
	// dragFrom the layout and dragY the pointer's y when the drag began.
	hover, active bool
	dragFrom      minimapRenderLayout
	dragY         float64
	changed       *foundation.ChangeNotifier
}

// SetMinimap tells the view model where the minimap is, as the editor widget worked it out from the
// size of the editor: nil input leaves it as it is. A minimap that is not shown (off, or a large
// file) has no layout.
func (vm *ViewModel) SetMinimap(info minimapLayoutInfo, in minimapInput) {
	m := &vm.mm
	if m.info == info && m.in == in {
		return
	}
	m.info, m.in = info, in
	m.sampling = computeMinimapSampling(info, in)
	m.drawn = false
	m.changed.NotifyListeners()
}

// minimapLineCount is the number of lines of the minimap: the rows, or the rows it samples.
func (vm *ViewModel) minimapLineCount() int {
	if s := vm.mm.sampling; s != nil {
		return len(s.Lines)
	}
	return vm.Projection().RowCount()
}

// minimapRow is the 0-based row of a 1-based line of the minimap.
func (vm *ViewModel) minimapRowOf(line int) int {
	if s := vm.mm.sampling; s != nil {
		return min(max(s.Lines[min(max(line, 1), len(s.Lines))-1]-1, 0), vm.Projection().RowCount()-1)
	}
	return min(max(line-1, 0), vm.Projection().RowCount()-1)
}

// minimapLineOfRow is the 1-based line of the minimap that shows a 0-based row.
func (vm *ViewModel) minimapLineOfRow(row int) int {
	if s := vm.mm.sampling; s != nil {
		return s.rowToMinimapLine(row + 1)
	}
	return row + 1
}

// minimapLayoutNow finds the slider and the lines for the editor as it is scrolled to top, and
// keeps the result for the pointer.
func (vm *ViewModel) minimapLayoutNow(top float64) minimapRenderLayout {
	m := &vm.mm
	proj := vm.Projection()
	first, last := proj.RowAt(top), proj.RowAt(top+vm.viewport)
	view := minimapView{
		ViewportStartLine: vm.minimapLineOfRow(first), ViewportEndLine: vm.minimapLineOfRow(last),
		ViewportStartOffset: proj.RowTop(first), ViewportHeight: vm.viewport,
		LineCount: vm.minimapLineCount(), RealLineCount: proj.RowCount(),
		ScrollTop: top, ScrollHeight: vm.ContentHeight(),
	}
	var prev *minimapRenderLayout
	if m.drawn {
		prev = &m.layout
	}
	m.layout = createMinimapRenderLayout(m.info, m.in, view, prev)
	m.drawn = true
	return m.layout
}

// MinimapPointerDown handles a press of the primary button at a height of the minimap, in pixels
// of the window: on the slider it starts to drag it; elsewhere, in a proportional minimap it
// scrolls so that the line pressed is in the middle of the editor, and in the others it drags the
// slider from its middle. It says whether the press was taken.
//
// Ported from VS Code: minimap.ts (InnerMinimap, the pointer down handlers)
func (vm *ViewModel) MinimapPointerDown(y float64) bool {
	m := &vm.mm
	if !m.info.Show || !m.drawn {
		return false
	}
	l := m.layout
	if y >= l.SliderTop && y <= l.SliderTop+l.SliderHeight && l.SliderNeeded {
		vm.startMinimapDrag(y)
		return true
	}
	if m.info.HeightIsEditorHeight || m.in.Size != "proportional" {
		if l.SliderNeeded {
			vm.startMinimapDrag(l.SliderTop + l.SliderHeight/2)
			vm.MinimapPointerMove(y)
		}
		return true
	}
	inner := float64(m.info.CanvasInnerHeight) / m.info.CanvasOuterHeight
	index := int(math.Floor(inner * y / float64(m.info.LineHeight)))
	line := min(index+l.StartLine-l.TopPaddingLineCount, vm.minimapLineCount())
	vm.revealMinimapRow(vm.minimapRowOf(line))
	return true
}

func (vm *ViewModel) startMinimapDrag(y float64) {
	m := &vm.mm
	m.active, m.dragFrom, m.dragY = true, m.layout, y
	m.changed.NotifyListeners()
}

// MinimapPointerMove follows the pointer while the slider is dragged: the editor scrolls as far as
// the slider moved.
func (vm *ViewModel) MinimapPointerMove(y float64) {
	m := &vm.mm
	if !m.active || vm.scroller == nil {
		return
	}
	vm.scroller(m.dragFrom.desiredScrollTopFromDelta(y - m.dragY))
}

// MinimapPointerUp ends a drag of the slider.
func (vm *ViewModel) MinimapPointerUp() {
	m := &vm.mm
	if m.active {
		m.active = false
		m.changed.NotifyListeners()
	}
}

// MinimapHover tells the view model that the pointer entered or left the minimap.
func (vm *ViewModel) MinimapHover(over bool) {
	if vm.mm.hover != over {
		vm.mm.hover = over
		vm.mm.changed.NotifyListeners()
	}
}

// revealMinimapRow scrolls so that a row is in the middle of the editor.
//
// Ported from VS Code: minimap.ts (Minimap.revealLineNumber), without the smooth scroll
func (vm *ViewModel) revealMinimapRow(row int) {
	if vm.scroller == nil {
		return
	}
	proj := vm.Projection()
	vm.scroller(proj.RowTop(row) + proj.RowHeight(row)/2 - vm.viewport/2)
}
