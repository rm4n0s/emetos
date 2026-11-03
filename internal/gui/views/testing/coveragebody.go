package testing

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The widgets of the coverage: the bars of a file (the percentage and a bar for the statements, the functions and the branches) and the
// body of the Test Coverage view.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testCoverageBars.ts and testCoverageView.ts

// BarSource is the numbers the bars show.
type BarSource struct {
	Statement   coretesting.Count
	Branch      *coretesting.Count
	Declaration *coretesting.Count
}

// BarsOf is the source of the bars of a file or folder.
func BarsOf(f coretesting.FileCoverage) BarSource {
	return BarSource{Statement: f.Statement, Branch: f.Branch, Declaration: f.Declaration}
}

// CoverageBarsOpts are the options of NewCoverageBars.
type CoverageBarsOpts struct {
	// Compact shows only the overall bar after the percentage, with no decimals; the details are in the hover.
	Compact bool
	// NoOverall leaves the percentage out.
	NoOverall bool
	// Percent is testing.displayedCoveragePercent and Thresholds testing.coverageBarThresholds.
	Percent    string
	Thresholds coretesting.BarThresholds
	// Key is the key of the widget.
	Key foundation.Key
}

// Sizes of the bars inside their frame: 16 px wide and 8 px high as VS Code's.
const (
	coverageBarWidth  = 16.0
	coverageBarHeight = 8.0
	coveragePctWidth  = 56.0
)

// barColor is the color of a bar that is pct full, and whether the bar is drawn faded because it has nothing to cover.
func barColor(colors kit.Colors, pct float64, zero bool, t coretesting.BarThresholds) ui.Color {
	if zero {
		return colors.Foreground.WithOpacity(0.5)
	}
	id := "charts." + coretesting.CoverageColor(pct, t)
	if c, ok := colors.Named(id); ok && c.Alpha() != 0 {
		return c
	}
	return colors.Foreground
}

// coverageBar is one bar: a frame of the color with the filled part in it at 70% opacity, floored to a pixel of 16 (VS Code's .bar).
func coverageBar(color ui.Color, pct float64) widgets.Widget {
	fill := float64(coretesting.BarFill(pct))
	var filled widgets.Widget = widgets.NewSizedBox(nil)
	if fill > 0 {
		filled = widgets.NewAlign(widgets.NewSizedBox(widgets.NewColoredBox(color.WithOpacity(0.7), widgets.NewSizedBox(nil)), widgets.SizedBoxOpts{Width: foundation.Some(fill), Height: foundation.Some(coverageBarHeight)}),
			widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})
	}
	return widgets.NewSizedBox(
		widgets.NewDecoratedBox(painting.NewBoxDecoration(painting.BoxDecorationOpts{
			BorderRadius: painting.NewBorderRadiusCircular(2),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(color), Width: foundation.Some(1.0)}),
		}), widgets.NewClipRRect(filled, widgets.ClipRRectOpts{BorderRadius: painting.NewBorderRadiusCircular(1)})),
		widgets.SizedBoxOpts{Width: foundation.Some(coverageBarWidth + 2), Height: foundation.Some(coverageBarHeight + 2)})
}

// NewCoverageBars draws the percentage and the bars of a file: in the compact form the percentage and the bar of the displayed
// percentage, otherwise (unless NoOverall) the percentage with two decimals and a bar each for the statements, the functions and the
// branches that were measured.
func NewCoverageBars(bc widgets.BuildContext, src BarSource, opts CoverageBarsOpts) widgets.Widget {
	colors := kit.ThemeOf(bc)
	if opts.Percent == "" {
		opts.Percent = coretesting.PercentTotal
	}
	if opts.Thresholds == (coretesting.BarThresholds{}) {
		opts.Thresholds = coretesting.DefaultBarThresholds
	}
	overall := coretesting.DisplayedStat(src.Statement, src.Branch, src.Declaration, opts.Percent)
	precision := 2
	if opts.Compact {
		precision = 0
	}
	var cells []widgets.Widget
	gap := func() widgets.Widget {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(3.0)})
	}
	if !opts.NoOverall {
		cells = append(cells, widgets.NewSizedBox(
			widgets.NewText(coretesting.DisplayPercent(overall, precision), widgets.TextOpts{
				Style: kit.TextStyleOf(bc, 12, colors.Foreground.WithOpacity(0.8)), TextAlign: ui.TextAlignRight, MaxLines: foundation.Some(1)}),
			widgets.SizedBoxOpts{Width: foundation.Some(coveragePctWidth)}), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(5.0)}))
	}
	if opts.Compact {
		cells = append(cells, coverageBar(barColor(colors, overall, false, opts.Thresholds), overall))
	} else {
		cells = append(cells, coverageBar(barColor(colors, coretesting.ClampedPercent(src.Statement), src.Statement.Total == 0, opts.Thresholds), coretesting.ClampedPercent(src.Statement)))
		for _, c := range []*coretesting.Count{src.Declaration, src.Branch} {
			if c == nil {
				continue
			}
			cells = append(cells, gap(), coverageBar(barColor(colors, coretesting.ClampedPercent(*c), c.Total == 0, opts.Thresholds), coretesting.ClampedPercent(*c)))
		}
	}
	row := widgets.NewRow(cells, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	return widgets.NewKeyedSubtree(row, widgets.WidgetOpts{Key: opts.Key})
}

// BarsTooltip is the text of the hover of the bars: the statements, the functions and the branches that were covered.
func BarsTooltip(src BarSource) string {
	text := func(c coretesting.Count, what string) string {
		return fmtCount(c, what)
	}
	out := text(src.Statement, "statements")
	if src.Declaration != nil {
		out += "\n\n" + text(*src.Declaration, "functions")
	}
	if src.Branch != nil {
		out += "\n\n" + text(*src.Branch, "branches")
	}
	return out
}

func fmtCount(c coretesting.Count, what string) string {
	return thousands(c.Covered) + "/" + thousands(c.Total) + " " + what + " covered (" + coretesting.DisplayPercent(coretesting.ClampedPercent(c), 2) + ")"
}

// thousands writes a number with commas between the thousands (VS Code's Intl.NumberFormat).
func thousands(n int) string {
	s := itoaInt(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func itoaInt(n int) string {
	if n < 0 {
		return "-" + itoaInt(-n)
	}
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// CoverageBody is the body of the Test Coverage view.
type CoverageBody struct {
	widgets.StatefulWidget
	cov *Coverage
}

// NewCoverageBody makes the body of the view.
func NewCoverageBody(c *Coverage) *CoverageBody {
	return &CoverageBody{StatefulWidget: widgets.NewStatefulWidgetBase(), cov: c}
}

// CreateState implements widgets.StatefulWidget.
func (b *CoverageBody) CreateState() widgets.State { return &coverageBodyState{} }

type coverageBodyState struct {
	widgets.StateOf[*CoverageBody]
	stop func()
}

func (s *coverageBodyState) InitState() { s.follow() }

func (s *coverageBodyState) follow() {
	s.stop = s.Widget().cov.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

func (s *coverageBodyState) DidUpdateWidget(old *CoverageBody) {
	if old.cov != s.Widget().cov {
		s.stop()
		s.follow()
	}
}

func (s *coverageBodyState) Dispose() { s.stop() }

// Build implements widgets.State.
func (s *coverageBodyState) Build(bc widgets.BuildContext) widgets.Widget {
	c := s.Widget().cov
	return kit.NewTree(c.model, kit.TreeOpts{
		Key: CoverageTreeKey, Controller: c.ctl, TypeToFind: foundation.Some(false), ToggleOnClick: foundation.Some(false),
		Render: func(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
			return s.render(bc, n, st)
		},
		OnSelect: func(n kit.TreeNode) {
			if cn, ok := c.Node(n.Key); ok && !n.Expandable && c.opts.OnOpen != nil {
				c.opts.OnOpen(cn)
			}
		},
		OnOpen: func(n kit.TreeNode) {
			if cn, ok := c.Node(n.Key); ok && c.opts.OnOpen != nil && cn.Kind != CoverageFolder {
				c.opts.OnOpen(cn)
			}
		},
	})
}

// render is a row: the icon, the label, and the percentage and bars at the end.
func (s *coverageBodyState) render(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
	c := s.Widget().cov
	cn, ok := n.Data.(*CoverageNode)
	if !ok {
		return kit.NewHighlightedLabel(bc, n.Label, st)
	}
	colors := kit.ThemeOf(bc)
	base := kit.TextStyleOf(bc, 13, st.Foreground)
	var icon widgets.Widget
	var src *BarSource
	label := cn.Label
	style := base
	if cn.Kind == CoverageDeclaration {
		code, color := fonts.CodiconCircle, colors.Foreground.WithOpacity(0.6)
		if cn.Executed > 0 {
			code, color = fonts.CodiconPass, StateColor(colors, coretesting.StatePassed, false)
		} else {
			style = base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(st.Foreground.WithOpacity(0.6))})
		}
		icon = widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(color)})
		if cn.Attr != nil {
			b := BarsOf(*cn.Attr)
			src = &b
		}
	} else {
		icon = s.fileIcon(bc, cn, n.Expanded, colors)
		b := BarsOf(cn.Coverage)
		src = &b
	}
	cells := []widgets.Widget{icon, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}),
		widgets.NewExpanded(widgets.NewText(label, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis}))}
	if src != nil {
		cells = append(cells, NewCoverageBars(bc, *src, CoverageBarsOpts{Percent: c.Percent(), Thresholds: c.Thresholds()}), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}))
	}
	return widgets.NewKeyedSubtree(widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), widgets.WidgetOpts{Key: CoverageRowKey(n.Key)})
}

// fileIcon is the icon of the file icon theme for a file or a folder; a Codicon without a theme.
func (s *coverageBodyState) fileIcon(bc widgets.BuildContext, cn *CoverageNode, expanded bool, colors kit.Colors) widgets.Widget {
	c := s.Widget().cov
	if c.opts.Icons != nil {
		if th := c.opts.Icons(); th != nil {
			q := theme.FileIconQuery{Kind: theme.FileIconFile, Name: cn.Label, Expanded: expanded, Type: colors.Type}
			if cn.Kind == CoverageFolder {
				q.Kind = theme.FileIconFolder
			} else if c.opts.Language != nil {
				q.LanguageID = c.opts.Language(uriPath(cn.URI))
			}
			if ic, ok := th.IconFor(q); ok {
				return kit.NewFileIcon(ic, colors.Foreground)
			}
		}
	}
	code := fonts.CodiconFileCode
	if cn.Kind == CoverageFolder {
		code = fonts.CodiconFolder
	}
	return widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(colors.Foreground)})
}
