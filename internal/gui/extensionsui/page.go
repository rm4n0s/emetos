package extensionsui

import (
	"sort"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Metrics and texts of the page.
const (
	pageSide     = 24.0
	pageTop      = 16.0
	titleSize    = 20.0
	rowPad       = 12.0
	nameSize     = 13.0
	detailSize   = 12.0
	maxWidth     = 900.0
	buttonGap    = 12.0
	rowLineGap   = 3.0
	titleText    = "Extensions"
	introText    = "Extensions are compiled into Emetos and are activated in a workspace tab when something they wait for happens there. Turning one off takes it out of every tab; extensions.disabled keeps the choice."
	emptyText    = "No extensions are compiled into this build."
	enableLabel  = "Enable"
	disableLabel = "Disable"
)

// ToggleKey is the key of the Enable or Disable button of an extension, which a test finds it by.
func ToggleKey(extensionID string) foundation.Key {
	return foundation.NewValueKey("extensions:toggle:" + extensionID)
}

// RowKey is the key of the row of an extension.
func RowKey(extensionID string) foundation.Key {
	return foundation.NewValueKey("extensions:row:" + extensionID)
}

// Options are the options of NewPage.
type Options struct {
	// Rows gives the extensions with their state; it is called at every build.
	Rows func() []exthost.Row
	// Subscribe calls fn when the rows may have changed and returns the function that stops it.
	Subscribe func(fn func()) (unsubscribe func())
	// OnToggle is called when the user presses Enable (enable true) or Disable on an extension.
	OnToggle func(extensionID string, enable bool)
}

// Page is the Extensions page.
type Page struct {
	widgets.StatefulWidget
	opts Options
}

// NewPage makes the page.
func NewPage(opts Options) *Page {
	return &Page{StatefulWidget: widgets.NewStatefulWidgetBase(), opts: opts}
}

// CreateState implements widgets.StatefulWidget.
func (p *Page) CreateState() widgets.State { return &pageState{} }

type pageState struct {
	widgets.StateOf[*Page]
	stop func()
}

// InitState follows the host.
func (s *pageState) InitState() { s.follow() }

func (s *pageState) follow() {
	if sub := s.Widget().opts.Subscribe; sub != nil {
		s.stop = sub(func() {
			if s.Mounted() {
				s.SetState(func() {})
			}
		})
	}
}

// DidUpdateWidget follows the host of the new widget.
func (s *pageState) DidUpdateWidget(old *Page) {
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
	s.follow()
}

// Dispose stops following the host.
func (s *pageState) Dispose() {
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
}

// Build implements widgets.State.
func (s *pageState) Build(bc widgets.BuildContext) widgets.Widget {
	o := s.Widget().opts
	colors := kit.ThemeOf(bc)
	style := func(size float64, weight ui.FontWeight, color ui.Color) *painting.TextStyle {
		return painting.NewTextStyle(painting.TextStyleOpts{
			FontFamily: kit.UIFontOf(bc), FontFamilyFallback: []string{"sans-serif"},
			FontSize: foundation.Some(size), FontWeight: weight, Color: foundation.Some(color),
		})
	}
	text := func(t string, st *painting.TextStyle) widgets.Widget {
		return widgets.NewText(t, widgets.TextOpts{Style: st})
	}
	var rows []widgets.Widget
	if o.Rows != nil {
		for _, row := range o.Rows() {
			rows = append(rows, buildRow(row, o, colors, style, text))
		}
	}
	children := []widgets.Widget{
		text(titleText, style(titleSize, ui.FontWeightW600, colors.Foreground)),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(6.0)}),
		text(introText, style(detailSize, ui.FontWeightW400, colors.DescriptionForeground)),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(14.0)}),
	}
	if len(rows) == 0 {
		children = append(children, text(emptyText, style(nameSize, ui.FontWeightW400, colors.DescriptionForeground)))
	}
	children = append(children, rows...)
	content := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(pageSide, pageTop, pageSide, pageTop),
		widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin}))
	limited := widgets.NewAlign(widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(maxWidth)}), content),
		widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})
	return widgets.NewColoredBox(colors.EditorBackground, kit.NewScrollableElement(limited, kit.ScrollableElementOpts{Axes: kit.ScrollAxisVertical}))
}

// buildRow is the row of one extension.
func buildRow(row exthost.Row, o Options, colors kit.Colors, style func(float64, ui.FontWeight, ui.Color) *painting.TextStyle, text func(string, *painting.TextStyle) widgets.Widget) widgets.Widget {
	m := row.Manifest
	title := text(m.Name, style(nameSize, ui.FontWeightW600, colors.Foreground))
	if m.Version != "" {
		title = widgets.NewRow([]widgets.Widget{
			title, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
			text(m.Version, style(detailSize, ui.FontWeightW400, colors.DescriptionForeground)),
		}, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin})
	}
	dim := style(detailSize, ui.FontWeightW400, colors.DescriptionForeground)
	lines := []widgets.Widget{title}
	if m.Description != "" {
		lines = append(lines, text(m.Description, style(detailSize, ui.FontWeightW400, colors.Foreground)))
	}
	detail := m.ID
	if len(m.ActivationEvents) > 0 {
		detail += "  •  activates on " + strings.Join(m.ActivationEvents, ", ")
	}
	lines = append(lines, text(detail, dim))
	status, isError := StatusText(row)
	statusStyle := dim
	if isError {
		statusStyle = style(detailSize, ui.FontWeightW400, colors.ErrorForeground)
	}
	lines = append(lines, text(status, statusStyle))
	spaced := make([]widgets.Widget, 0, 2*len(lines))
	for i, l := range lines {
		if i > 0 {
			spaced = append(spaced, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(rowLineGap)}))
		}
		spaced = append(spaced, l)
	}
	label, enable := disableLabel, false
	if row.Disabled {
		label, enable = enableLabel, true
	}
	id := m.ID
	button := kit.NewButton(label, kit.ButtonOpts{Key: ToggleKey(id), Secondary: !row.Disabled, Small: true, OnPressed: func() {
		if o.OnToggle != nil {
			o.OnToggle(id, enable)
		}
	}})
	body := widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(widgets.NewColumn(spaced, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(buttonGap)}),
		button,
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
	return widgets.NewKeyedSubtree(
		widgets.NewDecoratedBox(
			painting.NewBoxDecoration(painting.BoxDecorationOpts{Border: painting.Border{Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.PanelBorder), Width: foundation.Some(1.0)})}}),
			widgets.NewPadding(painting.NewEdgeInsetsSymmetric(rowPad, 0), body)),
		widgets.WidgetOpts{Key: RowKey(id)})
}

// StatusText says in words what an extension is: where it is active, that it is off, or where it failed and why. The
// bool is true when the text reports a failure.
func StatusText(row exthost.Row) (text string, failure bool) {
	if row.Disabled {
		return "Disabled", false
	}
	var active, failed []string
	for _, s := range row.Scopes {
		switch s.State {
		case exthost.StateActive:
			active = append(active, s.Scope)
		case exthost.StateFailed:
			reason := ""
			if s.Failure != nil {
				reason = ": " + s.Failure.BoxText
			}
			failed = append(failed, s.Scope+reason)
		}
	}
	sort.Strings(active)
	switch {
	case len(failed) > 0 && len(active) > 0:
		return "Failed in " + strings.Join(failed, "; ") + ". Active in " + strings.Join(active, ", ") + ".", true
	case len(failed) > 0:
		return "Failed in " + strings.Join(failed, "; ") + ".", true
	case len(active) > 0:
		return "Active in " + strings.Join(active, ", ") + ".", false
	}
	return "Not active yet.", false
}
