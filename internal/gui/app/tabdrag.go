package app

import (
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Dragging a tab (SPEC §10.3). A tab is pressed in its strip and dragged over the strips and the
// editor areas of all the groups: over a strip it goes between two tabs (a line shows where), over an
// editor area it goes into that group (the area is highlighted) or, near an edge, into a new group
// beside it (the half of the area on that side is highlighted). Ctrl copies instead of moving, Alt
// turns the splitting at the edges off or on, Escape cancels, and a strip scrolls when the pointer
// rests near its end. The strips and the areas are found by their places on the screen, which the
// widgets of the groups report.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/multiEditorTabsControl.ts and
// src/vs/workbench/browser/parts/editor/editorDropTarget.ts

const (
	// tabDragScrollInterval is the time between two steps of the scrolling of a strip (a frame).
	tabDragScrollInterval = 16 * time.Millisecond
	// dragLabelOffset is how far the label of a drag is from the pointer.
	dragLabelOffset = 12.0
	// dragLabelMaxWidth is the widest the label of a drag gets.
	dragLabelMaxWidth = 260.0
)

// dropKind says what a drag is over.
type dropKind int

const (
	// dropNone is over nothing a tab can be dropped on.
	dropNone dropKind = iota
	// dropStrip is over the tab strip of a group.
	dropStrip
	// dropArea is over the editor area of a group.
	dropArea
)

// dropTarget is the place a dragged tab would go to.
type dropTarget struct {
	kind  dropKind
	group *editorgroup.Group
	// index is the place among the tabs of a strip.
	index int
	// zone is the part of an editor area, and rect the window's rectangle that it highlights.
	zone editorgroup.DropZone
	rect ui.Rect
}

// tabDrag is the drag that goes on: the tab, the pointer and what the pointer is over.
type tabDrag struct {
	source *editorgroup.Group
	editor editorgroup.EditorInput
	label  string
	// at is the position of the pointer in the window.
	at ui.Offset
	// copied is whether Ctrl is held: the tab is copied, not moved.
	copied bool
	target dropTarget
	// hovered is the tab of a strip that the pointer is over, which was revealed when the pointer came to
	// it; -1 for none.
	hovered int
	scroll  *gestures.Timer
}

// dragFeed tells the layer that shows the drag to build again.
type dragFeed struct {
	foundation.ChangeNotifier
}

func newDragFeed() *dragFeed { return &dragFeed{ChangeNotifier: *foundation.NewChangeNotifier()} }

// dragStart begins the drag of the tab at an index of a group; it says whether the tab is dragged.
func (s *shellState) dragStart(group *editorgroup.Group, index int) bool {
	e := group.Model.EditorAt(index)
	if e == nil || s.group.drag != nil {
		return false
	}
	s.group.drag = &tabDrag{source: group, editor: e, label: e.Name(), hovered: -1}
	s.group.dragFeed.NotifyListeners()
	return true
}

// dragMove follows the pointer of the drag.
func (s *shellState) dragMove(at ui.Offset) {
	if d := s.group.drag; d != nil {
		d.at = at
		s.dragUpdate(true)
	}
}

// dragEnd ends the drag: a drop (the pointer released) does what the place under the pointer asks, a
// cancel (Escape) does nothing.
func (s *shellState) dragEnd(at ui.Offset, dropped bool) {
	d := s.group.drag
	if d == nil {
		return
	}
	var target dropTarget
	if dropped {
		d.at = at
		s.dragUpdate(false)
		target = d.target
	}
	s.stopDrag()
	if !dropped {
		return
	}
	var err error
	switch target.kind {
	case dropStrip:
		err = s.group.grid.DropOnTabs(d.source, d.editor, target.group, target.index, d.copied)
	case dropArea:
		_, err = s.group.grid.DropOnZone(d.source, d.editor, target.group, target.zone, d.copied)
	case dropNone:
		return
	}
	if err != nil {
		win := PeekWindow(s.Context())
		win.Process.Logger.Error("cannot drop the tab", "err", err)
		win.Process.Notify.Error("Emetos", "Cannot move the tab: "+describe(err))
	}
	s.askFocusSoon()
}

// stopDrag forgets the drag: the timer, the lines and the highlight go.
func (s *shellState) stopDrag() {
	d := s.group.drag
	if d == nil {
		return
	}
	if d.scroll != nil {
		d.scroll.Cancel()
	}
	s.group.drag = nil
	for _, v := range s.group.views {
		v.feedback.Set(-1)
	}
	s.group.dragFeed.NotifyListeners()
}

// dragUpdate finds what the pointer is over, shows it, and scrolls a strip the pointer rests at the end
// of. A tab that the pointer comes to by moving (not by the scrolling of the strip) is shown whole
// when reveal is set.
func (s *shellState) dragUpdate(reveal bool) {
	d := s.group.drag
	win := PeekWindow(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	d.copied = binding.Keyboard().IsControlPressed()
	target, hovered := s.dropTargetAt(d, win, binding.Keyboard().IsAltPressed())
	d.target = target
	for group, v := range s.group.views {
		if target.kind == dropStrip && target.group == group {
			v.feedback.Set(target.index)
		} else {
			v.feedback.Set(-1)
		}
	}
	// the tab the pointer comes to is shown whole
	switch {
	case target.kind != dropStrip:
		d.hovered = -1
	case hovered != d.hovered:
		d.hovered = hovered
		if hovered >= 0 && reveal {
			s.view(target.group).tabs.Reveal(hovered)
		}
	}
	s.group.dragFeed.NotifyListeners()
	s.scrollNearEdge()
}

// dropTargetAt finds the place under the pointer of a drag: the tab strip of a group (and the tab the
// pointer is over), or the editor area of a group (the zone of it). alt turns the splitting on the
// edges around.
//
// Ported from VS Code: multiEditorTabsControl.ts (computeDropTarget) and editorDropTarget.ts
// (DropOverlay.positionOverlay)
func (s *shellState) dropTargetAt(d *tabDrag, win *Window, alt bool) (dropTarget, int) {
	groups := s.group.grid.Groups(editorgroup.GridAppearance)
	for _, g := range groups {
		v := s.view(g)
		strip, ok := v.tabs.Bounds()
		if !ok || g.Model.Count() == 0 || !strip.Contains(d.at) {
			continue
		}
		var spans []editorgroup.Span
		hovered := -1
		for i := 0; i < g.Model.Count(); i++ {
			r, ok := v.tabs.TabRect(i)
			if !ok {
				break
			}
			spans = append(spans, editorgroup.Span{Start: r.Left, End: r.Right})
			if d.at.Dx >= r.Left && d.at.Dx < r.Right {
				hovered = i
			}
		}
		return dropTarget{kind: dropStrip, group: g, index: editorgroup.TabDropIndex(spans, d.at.Dx)}, hovered
	}
	split := settings.Get[bool](win.Settings, "workbench.editor.splitOnDragAndDrop", "")
	if alt {
		split = !split
	}
	opts := editorgroup.ZoneOptions{Split: split, PreferVertical: s.sideBySideDirection() == editorgroup.DirRight}
	for _, g := range groups {
		v := s.view(g)
		area, ok := v.area.Rect()
		if !ok || !area.Contains(d.at) {
			continue
		}
		// the area below the tab strip, when there is one
		if strip, ok := v.tabs.Bounds(); ok && g.Model.Count() > 0 {
			area = ui.NewRectFromLTRB(area.Left, strip.Bottom, area.Right, area.Bottom)
		}
		if !area.Contains(d.at) || !editorgroup.DropAllowed(d.source, g, d.copied) {
			return dropTarget{}, -1
		}
		zone := editorgroup.ZoneAt(d.at.Dx-area.Left, d.at.Dy-area.Top, area.Width(), area.Height(), opts)
		l, t, w, h := editorgroup.ZoneRect(zone, area.Width(), area.Height())
		return dropTarget{kind: dropArea, group: g, zone: zone, rect: ui.NewRectFromLTWH(area.Left+l, area.Top+t, w, h)}, -1
	}
	return dropTarget{}, -1
}

// edgeStepAt is how far the strip under the pointer of a drag scrolls in a step, and which strip.
func (s *shellState) edgeStepAt(d *tabDrag) (*kit.TabsController, float64) {
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		v := s.view(g)
		if strip, ok := v.tabs.Bounds(); ok && g.Model.Count() > 0 && strip.Contains(d.at) {
			return v.tabs, kit.TabEdgeStep(d.at.Dx, strip.Left, strip.Right)
		}
	}
	return nil, 0
}

// scrollNearEdge starts the scrolling when the pointer is near an end of a strip.
func (s *shellState) scrollNearEdge() {
	d := s.group.drag
	if d == nil || d.scroll != nil {
		return
	}
	if _, step := s.edgeStepAt(d); step == 0 {
		return
	}
	d.scroll = widgets.WidgetsBindingOf(s.Context()).Gestures().NewTimer(tabDragScrollInterval, s.scrollTick)
}

// scrollTick scrolls the strip a step, and finds what the pointer is over again.
func (s *shellState) scrollTick() {
	d := s.group.drag
	if d == nil {
		return
	}
	d.scroll = nil
	tabs, step := s.edgeStepAt(d)
	if tabs == nil || step == 0 {
		return
	}
	tabs.ScrollBy(step)
	s.dragUpdate(false)
}

// dragLayer is what shows the drag above the window: the highlight of the part of the editor area the
// tab would go to, and the label that follows the pointer. It takes no pointer events.
func (s *shellState) dragLayer() widgets.Widget {
	return widgets.NewPositionedFill(widgets.NewIgnorePointer(
		widgets.NewListenableBuilder(s.group.dragFeed, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget {
			d := s.group.drag
			if d == nil {
				return widgets.NewSizedBox(nil)
			}
			colors := kit.ThemeOf(bc)
			var layers []widgets.Widget
			if d.target.kind == dropArea && colors.EditorGroupDropBackground.Alpha() != 0 {
				r := d.target.rect
				layers = append(layers, widgets.NewPositioned(widgets.NewColoredBox(colors.EditorGroupDropBackground, widgets.NewSizedBox(nil)),
					widgets.PositionedOpts{Left: foundation.Some(r.Left), Top: foundation.Some(r.Top), Width: foundation.Some(r.Width()), Height: foundation.Some(r.Height())}))
			}
			label := widgets.NewContainer(
				widgets.NewText(d.label, widgets.TextOpts{
					Style: uiTextStyle(bc, 13, colors.TabActiveForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis}),
				widgets.ContainerOpts{
					Padding: painting.NewEdgeInsetsSymmetric(10, 6),
					Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
						Color:        foundation.Some(colors.TabActiveBackground),
						Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.TabActiveBorderTop), Width: foundation.Some(1.0)}),
						BorderRadius: painting.NewBorderRadiusCircular(3),
						BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: 6})},
					}),
				})
			layers = append(layers, widgets.NewPositionedFill(widgets.NewCustomSingleChildLayout(
				&dragLabelLayout{SingleChildLayoutDelegateBase: rendering.NewSingleChildLayoutDelegateBase(nil), at: d.at}, label)))
			return widgets.NewStack(layers, widgets.StackOpts{Fit: rendering.StackFitExpand})
		}, nil)))
}

// dragLabelLayout puts the label of a drag at the pointer, a little below and to the right of it, and
// keeps it inside the window: near the right or the bottom edge it moves to the other side of the pointer.
type dragLabelLayout struct {
	rendering.SingleChildLayoutDelegateBase
	at ui.Offset
}

// GetConstraintsForChild limits the width of the label.
func (l *dragLabelLayout) GetConstraintsForChild(c rendering.BoxConstraints) rendering.BoxConstraints {
	return rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(min(c.MaxWidth, dragLabelMaxWidth))})
}

// GetPositionForChild is the place of the label.
func (l *dragLabelLayout) GetPositionForChild(size, child ui.Size) ui.Offset {
	return placeDragLabel(l.at, child, size)
}

// ShouldRelayout is true when the pointer moved.
func (l *dragLabelLayout) ShouldRelayout(old rendering.SingleChildLayoutDelegate) bool {
	return l.at != old.(*dragLabelLayout).at
}

// placeDragLabel is the place of the top left corner of the label of a drag of a size, in a window of
// a size, for a pointer at a position: 12 px below and to the right of it, or to its left or above it
// where the window has no room.
func placeDragLabel(at ui.Offset, label, window ui.Size) ui.Offset {
	x, y := at.Dx+dragLabelOffset, at.Dy+dragLabelOffset
	if x+label.Width > window.Width {
		x = at.Dx - dragLabelOffset - label.Width
	}
	if y+label.Height > window.Height {
		y = at.Dy - dragLabelOffset - label.Height
	}
	return ui.Offset{Dx: max(x, 0), Dy: max(y, 0)}
}
