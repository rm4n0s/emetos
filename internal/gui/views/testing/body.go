package testing

import (
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Keys of the widgets of the view, which tests find them by.
var (
	// TreeKey is the key of the tree, MessageKey that of the text in place of a tree with no rows, FilterKey that of the filter box,
	// FilterMenuKey that of the button of its menu and SummaryKey that of the summary line.
	TreeKey       = foundation.NewValueKey("testing:tree")
	MessageKey    = foundation.NewValueKey("testing:message")
	FilterKey     = foundation.NewValueKey("testing:filter")
	FilterMenuKey = foundation.NewValueKey("testing:filter-menu")
	SummaryKey    = foundation.NewValueKey("testing:summary")
)

// RowKey is the key of the widget of a row of the tree, which tests find it by.
func RowKey(rowKey string) foundation.Key { return foundation.NewValueKey("testing:row:" + rowKey) }

// FilterPlaceholder is the text of the empty filter box.
const FilterPlaceholder = "Filter (e.g. text, !exclude, @tag)"

// BodyOpts are the options of NewBody.
type BodyOpts struct {
	// Select is called when the user selects a row (a click, Enter) and Open for Enter and a double click.
	Select func(*Element)
	Open   func(*Element)
	// Menu opens the context menu of a row at a position of the window; the row is nil for the part below the rows.
	Menu func(row *Element, at ui.Offset)
	// FilterMenu opens the menu of the filter at a position of the window.
	FilterMenu func(at ui.Offset)
	// Actions gives the buttons of a row, shown while the pointer is over it or it is selected; RunAction runs one.
	Actions   func(*Element) []kit.Action
	RunAction func(row *Element, id string)
	// SummaryActions are the buttons after the summary line (rerun the last run, debug it) and RunSummaryAction runs one.
	SummaryActions   func(SummaryState) []kit.Action
	RunSummaryAction func(id string)
	OnFocusChange    func(has bool)
	// Summary gives the summary line.
	Summary func() SummaryState
	// Message is the text in place of the tree when it has no rows because no test was found; the filter's own message when rows exist
	// but the filter shows none.
	Message string
}

// Body is the body of the Test Explorer: the filter box, the summary line and the tree.
type Body struct {
	widgets.StatefulWidget
	ex   *Explorer
	opts BodyOpts
}

// NewBody makes the body of the explorer.
func NewBody(ex *Explorer, opts ...BodyOpts) *Body {
	b := &Body{StatefulWidget: widgets.NewStatefulWidgetBase(), ex: ex}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	return b
}

// CreateState implements widgets.StatefulWidget.
func (b *Body) CreateState() widgets.State { return &bodyState{} }

type bodyState struct {
	widgets.StateOf[*Body]
	stop func()
}

// InitState follows the explorer.
func (s *bodyState) InitState() { s.follow() }

func (s *bodyState) follow() {
	s.stop = s.Widget().ex.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// DidUpdateWidget follows another explorer.
func (s *bodyState) DidUpdateWidget(old *Body) {
	if old.ex != s.Widget().ex {
		s.stop()
		s.follow()
	}
}

// Dispose stops following the explorer.
func (s *bodyState) Dispose() { s.stop() }

// Build implements widgets.State.
func (s *bodyState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	ex := w.ex
	colors := kit.ThemeOf(bc)
	var content widgets.Widget
	if ex.Model().Len() == 0 {
		msg := w.opts.Message
		if len(ex.Roots()) > 0 {
			msg = "No tests match the filter."
		}
		content = widgets.NewKeyedSubtree(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 8, 20, 8),
			widgets.NewText(msg, widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.DescriptionForeground)})), widgets.WidgetOpts{Key: MessageKey})
	} else {
		content = kit.NewTree(ex.Model(), kit.TreeOpts{
			Key: TreeKey, Controller: ex.Controller(), TypeToFind: foundation.Some(false), MultiSelect: true,
			Render: func(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
				return s.render(bc, n, st)
			},
			OnSelect: func(n kit.TreeNode) {
				if e, ok := ex.Row(n.Key); ok && w.opts.Select != nil {
					w.opts.Select(e)
				}
			},
			OnOpen: func(n kit.TreeNode) {
				if e, ok := ex.Row(n.Key); ok && w.opts.Open != nil {
					w.opts.Open(e)
				}
			},
			OnContextMenu: func(key string, at ui.Offset) {
				if w.opts.Menu == nil {
					return
				}
				e, _ := ex.Row(key)
				w.opts.Menu(e, at)
			},
		})
	}
	return widgets.NewColumn([]widgets.Widget{s.filterRow(bc), s.summaryRow(bc), widgets.NewExpanded(content)},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// filterRow is the filter box with the button of its menu.
func (s *bodyState) filterRow(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	ex := w.ex
	colors := kit.ThemeOf(bc)
	input := kit.NewInputBox(nil, nil, kit.InputBoxOpts{
		Key: FilterKey, Controller: ex.Input(), Placeholder: FilterPlaceholder,
		OnChanged: ex.SetFilterText, OnFocusChange: w.opts.OnFocusChange,
	})
	icon := widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconFilter), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(colors.Foreground)})
	if ex.HasHidden() || !ex.Filter().Empty() {
		icon = widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconFilter), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(accent(colors))})
	}
	button := widgets.NewKeyedSubtree(widgets.NewGestureDetector(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(4, 4), icon),
		widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTapDown: func(d gestures.TapDownDetails) {
			if w.opts.FilterMenu != nil {
				w.opts.FilterMenu(ui.Offset{Dx: d.GlobalPosition.Dx, Dy: d.GlobalPosition.Dy + 10})
			}
		}}), widgets.WidgetOpts{Key: FilterMenuKey})
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 6, 8, 4),
		widgets.NewRow([]widgets.Widget{widgets.NewExpanded(input), button}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
}

// summaryRow is the line that says how the last run went, with its duration and the buttons that run it again.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerView.ts (ResultSummaryView)
func (s *bodyState) summaryRow(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	colors := kit.ThemeOf(bc)
	var sum SummaryState
	if w.opts.Summary != nil {
		sum = w.opts.Summary()
	} else {
		sum = SummaryOf(w.ex.Service(), func(r *coretesting.Result) time.Duration { return time.Since(r.StartedAt()) })
	}
	style := kit.TextStyleOf(bc, 12, colors.DescriptionForeground)
	if sum.None {
		return widgets.NewKeyedSubtree(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 2, 12, 6),
			widgets.NewText("No test results yet.", widgets.TextOpts{Style: style})), widgets.WidgetOpts{Key: SummaryKey})
	}
	text := sum.Text
	if sum.Duration > 0 || !sum.Running {
		sep := ", "
		if strings.HasSuffix(text, "...") {
			sep = " "
		}
		text += sep + SummaryDuration(sum.Duration)
	}
	cells := []widgets.Widget{
		widgets.NewIcon(fonts.CodiconIcon(StateIcon(sum.Dominant)), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(StateColor(colors, sum.Dominant, false))}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
		widgets.NewFlexible(widgets.NewText(text, widgets.TextOpts{Style: style, MaxLines: foundation.Some(2), Overflow: painting.TextOverflowEllipsis})),
	}
	if w.opts.SummaryActions != nil {
		if actions := w.opts.SummaryActions(sum); len(actions) > 0 {
			cells = append(cells, kit.NewActionBar(actions, kit.ActionBarOpts{OnRun: func(a kit.Action) {
				if w.opts.RunSummaryAction != nil {
					w.opts.RunSummaryAction(a.ID)
				}
			}}))
		}
	}
	return widgets.NewKeyedSubtree(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 2, 8, 6),
		widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})), widgets.WidgetOpts{Key: SummaryKey})
}

// render draws a row: the icon of the state, the label, the description and the duration; and, while the pointer is over the row or it is
// selected, the buttons of the row after it.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerView.ts (TestItemRenderer, ErrorRenderer)
func (s *bodyState) render(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
	w := s.Widget()
	e, ok := n.Data.(*Element)
	if !ok {
		return kit.NewHighlightedLabel(bc, n.Label, st)
	}
	colors := kit.ThemeOf(bc)
	base := kit.TextStyleOf(bc, 13, st.Foreground)
	dim := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(st.Foreground.WithOpacity(0.65))})
	var inner widgets.Widget
	if e.Kind == KindError {
		red := StateColor(colors, coretesting.StateFailed, false)
		inner = widgets.NewRow([]widgets.Widget{
			widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconError), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(red)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}),
			widgets.NewFlexible(widgets.NewText(e.Error, widgets.TextOpts{Style: dim, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	} else {
		state := e.State
		if e.Busy {
			state = coretesting.StateRunning
		}
		spans := []painting.InlineSpan{}
		if e.Label == "" || isBlank(e.Label) {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(" "), Style: base}))
		} else {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(e.Label), Style: base}))
		}
		desc := e.Description
		if e.HasDuration() {
			if desc != "" {
				desc += ": " + FormatDuration(e.Duration)
			} else {
				desc = FormatDuration(e.Duration)
			}
		}
		if desc != "" {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + desc), Style: dim}))
		}
		cells := []widgets.Widget{
			widgets.NewIcon(fonts.CodiconIcon(StateIcon(state)), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(StateColor(colors, state, e.Retired))}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}),
			widgets.NewFlexible(widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans}),
				widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
		}
		if e.Hidden {
			cells = append(cells, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}),
				widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconEyeClosed), widgets.IconOpts{Size: foundation.Some(14.0), Color: foundation.Some(st.Foreground.WithOpacity(0.65))}))
		}
		inner = widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	}
	if w.opts.Actions == nil || e.Kind == KindError || (!st.Hovered && !st.Focused && !st.Selected) {
		return widgets.NewKeyedSubtree(inner, widgets.WidgetOpts{Key: RowKey(n.Key)})
	}
	actions := w.opts.Actions(e)
	if len(actions) == 0 {
		return widgets.NewKeyedSubtree(inner, widgets.WidgetOpts{Key: RowKey(n.Key)})
	}
	bar := kit.NewActionBar(actions, kit.ActionBarOpts{OnRun: func(a kit.Action) {
		if w.opts.RunAction != nil {
			w.opts.RunAction(e, a.ID)
		}
	}})
	return widgets.NewKeyedSubtree(widgets.NewRow([]widgets.Widget{widgets.NewExpanded(inner), bar},
		widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), widgets.WidgetOpts{Key: RowKey(n.Key)})
}

func isBlank(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

// accent is the color of the filter button while the filter or a hidden test is in effect.
func accent(colors kit.Colors) ui.Color {
	if c, ok := colors.Named("textLink.foreground"); ok && c.Alpha() != 0 {
		return c
	}
	return colors.Foreground
}
