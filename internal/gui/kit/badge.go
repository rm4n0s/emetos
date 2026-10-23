package kit

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// Metrics of VS Code's count badge (`.monaco-count-badge`).
const (
	badgeFontSize  = 11.0
	badgeLineH     = 11.0
	badgePadV      = 3.0
	badgePadH      = 5.0
	badgeRadius    = 11.0
	badgeMinWidth  = 18.0
	badgeMinHeight = 18.0
)

// BadgeOpts are the options of NewBadge.
type BadgeOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Format is the text of the badge, where {0} stands for the count; the default is "{0}".
	Format string
	// Tooltip is the text of the hover of the badge, where {0} stands for the count (VS Code's
	// titleFormat); without it the badge has no hover.
	Tooltip string
	// HoverDelegate is the group of hovers the tooltip belongs to; the default is a group of its own.
	HoverDelegate *HoverDelegate
}

// Badge is VS Code's count badge: a number in a pill (11 px text, 3 px by 5 px of padding, a radius of
// 11, at least 18 px by 18 px) in badge.background and badge.foreground.
//
// Ported from VS Code: src/vs/base/browser/ui/countBadge/countBadge.ts (CountBadge) and
// countBadge.css (without the `long` variant, which nothing in countBadge.ts sets)
type Badge struct {
	widgets.StatelessWidget
	count int
	opts  BadgeOpts
}

// NewBadge makes a badge that shows a count.
func NewBadge(count int, opts ...BadgeOpts) *Badge {
	b := &Badge{count: count}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	b.StatelessWidget = widgets.NewStatelessWidgetBase(widgets.WidgetOpts{Key: b.opts.Key})
	return b
}

// Count is the number the badge shows.
func (b *Badge) Count() int { return b.count }

// Text is the text of the badge: the format with the count in it.
func (b *Badge) Text() string { return formatCount(b.opts.Format, "{0}", b.count) }

func formatCount(format, fallback string, count int) string {
	if format == "" {
		format = fallback
	}
	return strings.ReplaceAll(format, "{0}", strconv.Itoa(count))
}

// Build implements widgets.Widget.
func (b *Badge) Build(bc widgets.BuildContext) widgets.Widget {
	colors := ThemeOf(bc)
	style := painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: UIFontOf(bc), FontFamilyFallback: []string{"sans-serif"}, FontSize: foundation.Some(badgeFontSize),
		Height: foundation.Some(badgeLineH / badgeFontSize), Color: foundation.Some(colors.BadgeForeground),
	})
	text := widgets.NewConstrainedBox(
		rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MinWidth: foundation.Some(badgeMinWidth - 2*badgePadH)}),
		widgets.NewText(b.Text(), widgets.TextOpts{Style: style, TextAlign: ui.TextAlignCenter, MaxLines: foundation.Some(1)}))
	var body widgets.Widget = widgets.NewConstrainedBox(
		rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MinHeight: foundation.Some(badgeMinHeight)}),
		widgets.NewDecoratedBox(
			painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color:        foundation.Some(colors.BadgeBackground),
				BorderRadius: painting.NewBorderRadiusCircular(badgeRadius),
			}),
			widgets.NewPadding(painting.NewEdgeInsetsSymmetric(badgePadV, badgePadH), text)))
	if b.opts.Tooltip != "" {
		body = NewHover(body, HoverOpts{Text: formatCount(b.opts.Tooltip, "{0}", b.count), Compact: true, Delegate: b.opts.HoverDelegate})
	}
	return body
}
