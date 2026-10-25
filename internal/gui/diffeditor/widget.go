package diffeditor

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// RulerWidth is the width of the overview ruler at the right edge of the diff editor, the width of the
// scrollbar of an editor.
const RulerWidth = kit.DefaultVerticalScrollbarSize

// dividerWidth is the width of the line between the two editors.
const dividerWidth = 1.0

// WidgetOpts configure the Widget; every field is optional.
type WidgetOpts struct {
	// Original and Modified are the options of the two editors. The diff editor gives each the model's focus
	// handle when it has none, and puts both in a scope of context keys of its own below Original.Keys, which has
	// isInDiffEditor, diffEditorOriginalWritable and diffEditorModifiedWritable set.
	Original, Modified editor.EditorOpts
}

// Widget is the diff editor (SPEC §9.11): the original editor on the left and the modified editor on the right,
// of the same width, with a line between them and the overview ruler at the right edge. The model keeps the
// editors lined up and scrolled together and colors the changes; this widget lays them out.
type Widget struct {
	widgets.StatefulWidget
	model *Model
	opts  WidgetOpts
}

// NewWidget makes the diff editor of a model.
func NewWidget(model *Model, opts ...WidgetOpts) *Widget {
	w := &Widget{StatefulWidget: widgets.NewStatefulWidgetBase(), model: model}
	if len(opts) > 0 {
		w.opts = opts[0]
	}
	return w
}

// Model is the model the widget shows.
func (w *Widget) Model() *Model { return w.model }

// CreateState implements widgets.StatefulWidget.
func (w *Widget) CreateState() widgets.State { return &widgetState{} }

type widgetState struct {
	widgets.StateOf[*Widget]
	scope   *contextkey.Scope
	pressed bool
}

// InitState makes the scope of context keys of the diff editor.
func (s *widgetState) InitState() {
	w := s.Widget()
	if parent := w.opts.Original.Keys; parent != nil {
		s.scope = parent.Child()
		s.scope.Set("isInDiffEditor", true)
		s.scope.Set("diffEditorOriginalWritable", false)
		s.scope.Set("diffEditorModifiedWritable", !w.model.View(Modified).ReadOnly())
	}
}

// Dispose gives the scope back.
func (s *widgetState) Dispose() {
	if s.scope != nil {
		s.scope.Dispose()
		s.scope = nil
	}
}

// editorOf is the editor widget of a side.
func (s *widgetState) editorOf(side Side) *editor.Editor {
	w := s.Widget()
	opts := w.opts.Original
	if side == Modified {
		opts = w.opts.Modified
	}
	if s.scope != nil {
		opts.Keys = s.scope
	}
	if opts.Focus == nil {
		opts.Focus = w.model.Focus(side)
	}
	return editor.NewEditor(w.model.View(side), opts)
}

// Build implements widgets.State.
func (s *widgetState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	colors := ColorsOf(kit.ThemeOf(bc))
	w.model.SetColors(colors)
	if s.scope != nil {
		s.scope.Set("diffEditorModifiedWritable", !w.model.View(Modified).ReadOnly())
	}
	divider := widgets.NewColoredBox(colors.Border, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(dividerWidth)}))
	ruler := widgets.NewSizedBox(widgets.NewListener(
		widgets.NewCustomPaint(nil, widgets.CustomPaintOpts{Painter: newRulerPainter(w.model, colors, kit.ThemeOf(bc).EditorBackground)}),
		widgets.ListenerOpts{
			Behavior:      rendering.HitTestBehaviorOpaque,
			OnPointerDown: func(e *gestures.PointerDownEvent) { s.pressed = true; s.rulerTo(e.LocalPosition().Dy, bc) },
			OnPointerMove: func(e *gestures.PointerMoveEvent) {
				if s.pressed {
					s.rulerTo(e.LocalPosition().Dy, bc)
				}
			},
			OnPointerUp:     func(*gestures.PointerUpEvent) { s.pressed = false },
			OnPointerCancel: func(*gestures.PointerCancelEvent) { s.pressed = false },
		}), widgets.SizedBoxOpts{Width: foundation.Some(RulerWidth)})
	return widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(s.editorOf(Original)),
		divider,
		widgets.NewExpanded(s.editorOf(Modified)),
		ruler,
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// rulerTo scrolls the editors so that the place of the overview ruler at a height is in the middle of the view.
func (s *widgetState) rulerTo(y float64, bc widgets.BuildContext) {
	ro, ok := bc.FindRenderObject().(rendering.Box)
	height := 0.0
	if ok {
		height = ro.Size().Height
	}
	s.Widget().model.ScrollToFraction(y, height)
}
