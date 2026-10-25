package mergeeditor

import (
	"math"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/view/conflictActions.ts (ActionsContentWidget and
// ConflictActionsFactory: the font size of a code lens is 90 % of the editor's, in editorCodeLens.foreground)

// actionRowPad is the space before the first item of a row.
const actionRowPad = 6.0

// actionFontSize is the size of the text of a row of actions: 90 % of the editor's font, whole pixels, at least 5 (VS Code's
// code lens font size).
func actionFontSize(editorFont float64) float64 {
	if size := math.Floor(editorFont * 0.9); size >= 5 {
		return size
	}
	return 5
}

// actionRow is the widget of the zone above a conflict: its Actions, as links separated by bars, in the color of a code
// lens. It builds again when the model changes.
type actionRow struct {
	widgets.StatelessWidget
	model    *Model
	side     Side
	conflict int
}

func newActionRow(model *Model, side Side, conflict int) *actionRow {
	return &actionRow{StatelessWidget: widgets.NewStatelessWidgetBase(), model: model, side: side, conflict: conflict}
}

// Build implements widgets.StatelessWidget.
func (r *actionRow) Build(widgets.BuildContext) widgets.Widget {
	return widgets.NewListenableBuilder(r.model, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget {
		colors := kit.ThemeOf(bc)
		size := actionFontSize(r.model.vms[r.side].Options().FontSize)
		style := kit.TextStyleOf(bc, size, colors.CodeLensForeground)
		var children []widgets.Widget
		for i, item := range r.model.Actions(r.side, r.conflict) {
			if i > 0 {
				children = append(children, widgets.NewText(ActionSeparator, widgets.TextOpts{Style: style}))
			}
			children = append(children, newActionLink(item, style))
		}
		if len(children) == 0 {
			return widgets.NewSizedBox(nil)
		}
		// a row that is wider than the editor is cut, as a code lens is
		return widgets.NewClipRect(widgets.NewOverflowBox(
			widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, actionRowPad),
				widgets.NewRow(children, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter, MainAxisSize: rendering.MainAxisSizeMin})),
			widgets.OverflowBoxOpts{Alignment: painting.AlignmentCenterLeft, MinWidth: foundation.Some(0.0), MaxWidth: foundation.Some(1e9), Fit: rendering.OverflowBoxFitDeferToChild}))
	}, nil)
}

// actionLink is an item of a row: a link underlined while the pointer is over it, or a plain text.
type actionLink struct {
	widgets.StatefulWidget
	item  ActionItem
	style *painting.TextStyle
}

func newActionLink(item ActionItem, style *painting.TextStyle) *actionLink {
	return &actionLink{StatefulWidget: widgets.NewStatefulWidgetBase(), item: item, style: style}
}

// CreateState implements widgets.StatefulWidget.
func (l *actionLink) CreateState() widgets.State { return &actionLinkState{} }

type actionLinkState struct {
	widgets.StateOf[*actionLink]
	hover bool
}

// Build implements widgets.State.
func (s *actionLinkState) Build(widgets.BuildContext) widgets.Widget {
	l := s.Widget()
	style := l.style
	if l.item.Run != nil && s.hover {
		style = style.CopyWith(painting.TextStyleOpts{Decoration: foundation.Some(ui.TextDecorationUnderline)})
	}
	var w widgets.Widget = widgets.NewText(l.item.Text, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1)})
	if l.item.Run != nil {
		w = widgets.NewMouseRegion(
			widgets.NewGestureDetector(w, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: l.item.Run}),
			widgets.MouseRegionOpts{
				Cursor:  services.SystemMouseCursors.Click,
				OnEnter: func(*gestures.PointerEnterEvent) { s.SetState(func() { s.hover = true }) },
				OnExit:  func(*gestures.PointerExitEvent) { s.SetState(func() { s.hover = false }) },
			})
	}
	if l.item.Tooltip != "" {
		w = kit.NewHover(w, kit.HoverOpts{Text: l.item.Tooltip, Compact: true})
	}
	return w
}
