package outline

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Keys of the widgets of the view, which tests find them by.
var (
	// TreeKey is the key of the tree of symbols.
	TreeKey = foundation.NewValueKey("outline:tree")
	// MessageKey is the key of the message that stands in place of the tree.
	MessageKey = foundation.NewValueKey("outline:message")
)

// NoSymbols is the message of an editor that cannot provide symbols.
const NoSymbols = "The active editor cannot provide outline information."

// Body is the body of the Outline view: the tree of symbols, or the message that says there are none.
type Body struct {
	widgets.StatefulWidget
	outline *Outline
}

// NewBody makes the body of an outline.
func NewBody(o *Outline) *Body {
	return &Body{StatefulWidget: widgets.NewStatefulWidgetBase(), outline: o}
}

// CreateState implements widgets.StatefulWidget.
func (b *Body) CreateState() widgets.State { return &bodyState{} }

type bodyState struct {
	widgets.StateOf[*Body]
	stop func()
}

// InitState follows the outline.
func (s *bodyState) InitState() { s.follow() }

func (s *bodyState) follow() {
	s.stop = s.Widget().outline.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// DidUpdateWidget follows another outline.
func (s *bodyState) DidUpdateWidget(old *Body) {
	if old.outline != s.Widget().outline {
		s.stop()
		s.follow()
	}
}

// Dispose stops following the outline.
func (s *bodyState) Dispose() { s.stop() }

// Build implements widgets.State.
func (s *bodyState) Build(bc widgets.BuildContext) widgets.Widget {
	o := s.Widget().outline
	colors := kit.ThemeOf(bc)
	if !o.HasSymbols() {
		return widgets.NewKeyedSubtree(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 8, 20, 8),
			widgets.NewText(NoSymbols, widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.Foreground)})),
			widgets.WidgetOpts{Key: MessageKey})
	}
	return kit.NewTree(o.tree, kit.TreeOpts{
		Key: TreeKey, Controller: o.ctl,
		Render: func(bc widgets.BuildContext, n kit.TreeNode, st kit.TreeRowState) widgets.Widget {
			return o.render(bc, n, st)
		},
		OnSelect: func(n kit.TreeNode) { o.Choose(n.Key) },
	})
}

// render builds a row: the icon of the kind of the symbol, its name, and its detail dimmed; a deprecated symbol is struck
// through.
func (o *Outline) render(bc widgets.BuildContext, n kit.TreeNode, st kit.TreeRowState) widgets.Widget {
	item, ok := n.Data.(Item)
	if !ok {
		return kit.NewHighlightedLabel(bc, n.Label, st)
	}
	colors := kit.ThemeOf(bc)
	var cells []widgets.Widget
	if o.IconsOn() {
		code, color := editor.SymbolIcon(colors, item.Symbol.Kind)
		cells = append(cells, widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(color)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(3.0)}))
	}
	base := kit.TextStyleOf(bc, 13, st.Foreground)
	if item.Symbol.Deprecated {
		base = base.CopyWith(painting.TextStyleOpts{Decoration: foundation.Some(ui.TextDecorationLineThrough)})
	}
	spans := []painting.InlineSpan{painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(item.Symbol.Name), Style: base})}
	if d := item.Symbol.Detail; d != "" {
		dim := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.DescriptionForeground)})
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + d), Style: dim}))
	}
	cells = append(cells, widgets.NewExpanded(widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans}),
		widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})))
	return widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}
