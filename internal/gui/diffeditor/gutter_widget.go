package diffeditor

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The toolbar in the gutter of the modified editor (SPEC §10.30 "Staging"): while the pointer is over the lines of a change, a button at the
// start of its first line runs the actions of MenuDiffEditorGutterHunk (Git's Stage Block), and over a selection that touches changes the
// actions of MenuDiffEditorGutterSelection (Stage Selection). The commands get the text the original would have with the hunk, or the selected
// lines, applied.
//
// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/features/gutterFeature.ts (the gutter menus of a hunk and of a selection)

// GutterContext is what a command of the toolbar is run with (VS Code's DiffEditorSelectionHunkToolbarContext).
type GutterContext struct {
	// OriginalURI and ModifiedURI are the documents of the two sides.
	OriginalURI, ModifiedURI string
	// OriginalWithModifiedChanges is the original text with the hunk, or the selected lines, applied.
	OriginalWithModifiedChanges string
}

// GutterAction is a button of the toolbar: a Codicon with a tooltip, and what it does with the context.
type GutterAction struct {
	// Icon is the Codicon (a fonts.Codicon... constant) and Tooltip the hover.
	Icon    int
	Tooltip string
	// Run is called when the button is pressed.
	Run func(GutterContext)
}

// gutterHover is where the pointer is over the modified editor.
type gutterHover struct {
	foundation.ChangeNotifier
	y      float64
	inside bool
}

// set records the pointer and tells the toolbar when it changed.
func (h *gutterHover) set(y float64, inside bool) {
	if h.y == y && h.inside == inside {
		return
	}
	h.y, h.inside = y, inside
	h.NotifyListeners()
}

// withGutter puts the toolbar over the modified editor when the diff editor has actions for it.
func (s *widgetState) withGutter(ed widgets.Widget) widgets.Widget {
	w := s.Widget()
	if w.opts.HunkActions == nil && w.opts.SelectionActions == nil {
		return ed
	}
	if s.hover == nil {
		s.hover = &gutterHover{ChangeNotifier: *foundation.NewChangeNotifier()}
	}
	h := s.hover
	return widgets.NewMouseRegion(widgets.NewStack([]widgets.Widget{ed, newGutterToolbar(w.model, w.opts, h)}, widgets.StackOpts{Fit: rendering.StackFitExpand}),
		widgets.MouseRegionOpts{
			HitTestBehavior: rendering.HitTestBehaviorTranslucent,
			OnHover:         func(e *gestures.PointerHoverEvent) { h.set(e.LocalPosition().Dy, true) },
			OnExit:          func(*gestures.PointerExitEvent) { h.set(0, false) },
		})
}

// gutterToolbar is the layer that draws the buttons.
type gutterToolbar struct {
	widgets.StatefulWidget
	model *Model
	opts  WidgetOpts
	hover *gutterHover
}

func newGutterToolbar(m *Model, opts WidgetOpts, h *gutterHover) widgets.Widget {
	return &gutterToolbar{StatefulWidget: widgets.NewStatefulWidgetBase(), model: m, opts: opts, hover: h}
}

// CreateState implements widgets.StatefulWidget.
func (t *gutterToolbar) CreateState() widgets.State { return &gutterToolbarState{} }

type gutterToolbarState struct {
	widgets.StateOf[*gutterToolbar]
	handles struct {
		hover, model, scroll, selection foundation.ListenerHandle
	}
}

// InitState follows the pointer, the diff, the scrolling and the selection.
func (s *gutterToolbarState) InitState() {
	t := s.Widget()
	redraw := func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	}
	vm := t.model.View(Modified)
	s.handles.hover = t.hover.AddListener(redraw)
	s.handles.model = t.model.AddListener(redraw)
	s.handles.scroll = vm.AddScrollListener(redraw)
	s.handles.selection = vm.AddSelectionListener(redraw)
}

// Dispose stops following.
func (s *gutterToolbarState) Dispose() {
	t := s.Widget()
	vm := t.model.View(Modified)
	t.hover.RemoveListener(s.handles.hover)
	t.model.RemoveListener(s.handles.model)
	vm.RemoveScrollListener(s.handles.scroll)
	vm.RemoveSelectionListener(s.handles.selection)
}

// toolbarSize is the height of a button (kit's icon button).
const toolbarSize = 22.0

// Build implements widgets.State.
func (s *gutterToolbarState) Build(bc widgets.BuildContext) widgets.Widget {
	t := s.Widget()
	none := widgets.NewStack(nil, widgets.StackOpts{Fit: rendering.StackFitExpand})
	m := t.model
	vm := m.View(Modified)
	if !m.Computed() || vm.ReadOnly() {
		return none
	}
	line, actions, text, ok := s.target(vm)
	if !ok || len(actions) == 0 {
		return none
	}
	ctx := GutterContext{OriginalURI: m.View(Original).Document().URI().String(), ModifiedURI: vm.Document().URI().String()}
	proj := vm.Projection()
	row := vm.RowOfPosition(positionOfLine(line))
	rowTop := proj.RowTop(row) - vm.ScrollTop()
	// the buttons are taller than a row: they are centered on it, and kept inside the editor while any of the row shows
	_, h := vm.ViewportSize()
	if rowTop+proj.RowHeight(row) <= 0 || rowTop >= h {
		return none
	}
	top := min(max(rowTop+(proj.RowHeight(row)-toolbarSize)/2, 0), max(h-toolbarSize, 0))
	colors := kit.ThemeOf(bc)
	var buttons []widgets.Widget
	for _, a := range actions {
		run := a.Run
		buttons = append(buttons, kit.NewIconButton(a.Icon, kit.IconButtonOpts{Tooltip: a.Tooltip, OnPressed: func() {
			c := ctx
			c.OriginalWithModifiedChanges = text()
			run(c)
		}}))
	}
	box := widgets.NewColoredBox(colors.EditorGutterBackground, widgets.NewRow(buttons, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin}))
	return widgets.NewStack([]widgets.Widget{widgets.NewPositioned(box, widgets.PositionedOpts{Left: foundation.Some(2.0), Top: foundation.Some(top)})},
		widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// target says which toolbar to draw and where: the line it sits at (from 1), its actions, and the function that makes the text for the
// command. A selection that touches a change wins over the hunk under the pointer when the pointer is on the selected lines.
func (s *gutterToolbarState) target(vm *editor.ViewModel) (line int, actions []GutterAction, text func() string, ok bool) {
	t := s.Widget()
	m := t.model
	hoverLine := 0
	if t.hover.inside {
		proj := vm.Projection()
		if row := proj.RowAt(t.hover.y + vm.ScrollTop()); row >= 0 && row < proj.RowCount() {
			if _, isZone := zoneRow(proj, row); !isZone {
				l, _, _ := proj.RowSpan(row)
				hoverLine = l + 1
			}
		}
	}
	sel := vm.Cursors().Primary().Selection
	first, last := 0, 0
	if !sel.IsEmpty() {
		r := sel.Range()
		first, last = r.Start.Line+1, r.End.Line+1
		if r.End.Col == 0 && last > first {
			last--
		}
	}
	if first > 0 && t.opts.SelectionActions != nil && (hoverLine == 0 || hoverLine >= first && hoverLine <= last) {
		if _, touches := m.TextWithLines(first, last); touches {
			return first, t.opts.SelectionActions(), func() string { out, _ := m.TextWithLines(first, last); return out }, true
		}
	}
	if hoverLine > 0 && t.opts.HunkActions != nil {
		if i, in := m.HunkAt(hoverLine); in {
			return m.changes[i].Modified.StartLine, t.opts.HunkActions(), func() string { return m.TextWithHunk(i) }, true
		}
	}
	return 0, nil, nil, false
}

// positionOfLine is the start of a line counted from 1.
func positionOfLine(line int) buffer.Position { return buffer.Position{Line: line - 1} }

// zoneRow tells whether a row of the editor belongs to a view zone (the deleted lines of the inline view, the fillers).
func zoneRow(proj editor.Projection, row int) (editor.ZoneID, bool) {
	id, _, ok := proj.RowZone(row)
	return id, ok
}
