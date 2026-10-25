package mergeeditor

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// HeaderHeight is the height of the row above each editor, with its title and its label (VS Code's 30 px).
const HeaderHeight = 30.0

// dividerWidth is the width of the lines between the editors.
const dividerWidth = 1.0

// WidgetOpts configure the Widget; every field is optional.
type WidgetOpts struct {
	// Incoming, Current and Result are the options of the three editors. The merge editor gives each the model's
	// focus handle when it has none, and puts the three in a scope of context keys of its own below Result.Keys,
	// which has isMergeEditor set; the result editor has a scope of its own below that, with isMergeResultEditor.
	Incoming, Current, Result editor.EditorOpts
	// OnCompleteMerge is called when the "Complete Merge" button of the result header is pressed (the window asks about the
	// conflicts that are left, saves the result and closes the merge editor). Without it the button is left out.
	OnCompleteMerge func()
}

// Widget is the merge editor (SPEC §9.11): the incoming editor on the left and the current editor on the right of
// the top half, of the same width, and the result below, each with a header over it; the inputs have a gutter of
// checkboxes at the left. The model keeps the editors lined up and scrolled together and outlines the conflicts; this
// widget lays them out.
type Widget struct {
	widgets.StatefulWidget
	model *Model
	opts  WidgetOpts
}

// NewWidget makes the merge editor of a model.
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
	scope, resultScope *contextkey.Scope
}

// InitState makes the scopes of context keys of the merge editor.
func (s *widgetState) InitState() {
	w := s.Widget()
	if parent := w.opts.Result.Keys; parent != nil {
		s.scope = parent.Child()
		s.scope.Set("isMergeEditor", true)
		s.resultScope = s.scope.Child()
		s.resultScope.Set("isMergeResultEditor", true)
	}
}

// Dispose gives the scopes back.
func (s *widgetState) Dispose() {
	if s.resultScope != nil {
		s.resultScope.Dispose()
		s.resultScope = nil
	}
	if s.scope != nil {
		s.scope.Dispose()
		s.scope = nil
	}
}

// childKey keys the children of the merge editor so that the editors keep their state when it rebuilds.
type childKey string

// editorOf is the editor widget of a side.
func (s *widgetState) editorOf(side Side) *editor.Editor {
	w := s.Widget()
	var opts editor.EditorOpts
	switch side {
	case Incoming:
		opts = w.opts.Incoming
	case Current:
		opts = w.opts.Current
	default:
		opts = w.opts.Result
	}
	switch {
	case side == Result && s.resultScope != nil:
		opts.Keys = s.resultScope
	case s.scope != nil:
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
	colors := kit.ThemeOf(bc)
	w.model.SetColors(colors)
	return widgets.NewListenableBuilder(w.model, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget {
		return s.layout(bc, colors)
	}, nil)
}

// layout is the top row of the inputs and the result below it, split in halves.
func (s *widgetState) layout(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	divider := func(name string, w, h float64) widgets.Widget {
		return widgets.NewKeyedSubtree(
			widgets.NewColoredBox(colors.EditorGroupBorder, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: sizeOpt(w), Height: sizeOpt(h)})),
			widgets.WidgetOpts{Key: foundation.NewValueKey(childKey(name))})
	}
	top := widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(s.pane(bc, colors, Incoming), widgets.ExpandedOpts{Key: foundation.NewValueKey(childKey("incoming"))}),
		divider("vdivider", dividerWidth, 0),
		widgets.NewExpanded(s.pane(bc, colors, Current), widgets.ExpandedOpts{Key: foundation.NewValueKey(childKey("current"))}),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return widgets.NewColumn([]widgets.Widget{
		widgets.NewExpanded(top, widgets.ExpandedOpts{Key: foundation.NewValueKey(childKey("top"))}),
		divider("hdivider", 0, dividerWidth),
		widgets.NewExpanded(s.pane(bc, colors, Result), widgets.ExpandedOpts{Key: foundation.NewValueKey(childKey("result"))}),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

func sizeOpt(v float64) foundation.Opt[float64] {
	if v == 0 {
		return foundation.Opt[float64]{}
	}
	return foundation.Some(v)
}

// pane is an editor with its header, and the gutter of checkboxes for an input.
func (s *widgetState) pane(bc widgets.BuildContext, colors kit.Colors, side Side) widgets.Widget {
	w := s.Widget()
	var body widgets.Widget = widgets.NewKeyedSubtree(s.editorOf(side), widgets.WidgetOpts{Key: foundation.NewValueKey(childKey("editor"))})
	if _, ok := side.Input(); ok {
		gutter := widgets.NewColoredBox(colors.EditorBackground, widgets.NewSizedBox(
			widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
				height := c.MaxHeight
				return widgets.NewListener(
					widgets.NewCustomPaint(nil, widgets.CustomPaintOpts{Painter: newGutterPainter(w.model, side, colors)}),
					widgets.ListenerOpts{
						Behavior: rendering.HitTestBehaviorOpaque,
						OnPointerDown: func(e *gestures.PointerDownEvent) {
							off := e.LocalPosition()
							w.model.ClickGutter(side, off, height)
						},
					})
			}), widgets.SizedBoxOpts{Width: foundation.Some(GutterWidth)}))
		body = widgets.NewRow([]widgets.Widget{gutter, widgets.NewExpanded(body)}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	}
	return widgets.NewColumn([]widgets.Widget{
		s.header(bc, colors, side),
		widgets.NewExpanded(body),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// header is the row over an editor: its title, the label after it and, over the result, the count of the conflicts that
// are left, which goes to the next one when pressed.
func (s *widgetState) header(bc widgets.BuildContext, colors kit.Colors, side Side) widgets.Widget {
	w := s.Widget()
	title := widgets.NewText(side.Title(), widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.Foreground), MaxLines: foundation.Some(1)})
	label := widgets.NewText(w.model.Label(side), widgets.TextOpts{
		Style: kit.TextStyleOf(bc, 12, colors.DescriptionForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
	})
	children := []widgets.Widget{title, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}), widgets.NewExpanded(label)}
	if side == Result {
		color := colors.Foreground
		unhandled := w.model.Session().Model().UnhandledCount()
		if unhandled == 0 {
			color = colors.DescriptionForeground
		}
		remaining := widgets.NewText(w.model.Remaining(), widgets.TextOpts{Style: kit.TextStyleOf(bc, 12, color), MaxLines: foundation.Some(1)})
		var action widgets.Widget = remaining
		if unhandled > 0 {
			action = widgets.NewMouseRegion(
				widgets.NewGestureDetector(remaining, widgets.GestureDetectorOpts{
					Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { w.model.GoToConflict(true) },
				}), widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
		}
		children = append(children, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(10.0)}), action,
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
			kit.NewIconButton(fonts.CodiconDiscard, kit.IconButtonOpts{
				Key: foundation.NewValueKey(childKey("reset")), Tooltip: "Reset Result",
				OnPressed: func() { w.model.report(w.model.ResetResult()) },
			}))
		if w.opts.OnCompleteMerge != nil {
			children = append(children, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
				kit.NewButton("Complete Merge", kit.ButtonOpts{
					Key: foundation.NewValueKey(childKey("complete")), Small: true, Tooltip: "Complete Merge", OnPressed: w.opts.OnCompleteMerge,
				}))
		}
	}
	if in, ok := side.Input(); ok {
		title := "Accept All Incoming Changes from Left"
		if in == diff.Input2 {
			title = "Accept All Current Changes from Right"
		}
		children = append(children, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
			kit.NewIconButton(fonts.CodiconCheckAll, kit.IconButtonOpts{
				Key: foundation.NewValueKey(childKey("acceptAll")), Tooltip: title,
				Disabled:  w.model.Session().Model().UnhandledCount() == 0,
				OnPressed: func() { w.model.report(w.model.AcceptAll(in)) },
			}))
	}
	return widgets.NewColoredBox(colors.EditorBackground, widgets.NewSizedBox(
		widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 10),
			widgets.NewRow(children, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
		widgets.SizedBoxOpts{Height: foundation.Some(HeaderHeight)}))
}
