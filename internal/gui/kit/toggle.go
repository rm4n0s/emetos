package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// ToggleOpts are the options of NewToggle.
type ToggleOpts struct {
	// Checked says whether the toggle is on.
	Checked bool
	// OnChanged is called with the new value when the toggle is clicked. A toggle without it
	// is disabled.
	OnChanged func(checked bool)
}

// Toggle is VS Code's icon toggle (`monaco-custom-toggle`), such as the match case switch of
// the find widget: a Codicon in a box that has a border and a tinted background while it is on.
// This is the first version; step 6.5 adds focus, tooltips and the keyboard.
type Toggle struct {
	widgets.StatefulWidget
	code int
	opts ToggleOpts
}

// NewToggle makes a toggle that shows the Codicon of a code point.
func NewToggle(code int, opts ...ToggleOpts) *Toggle {
	t := &Toggle{StatefulWidget: widgets.NewStatefulWidgetBase(), code: code}
	if len(opts) > 0 {
		t.opts = opts[0]
	}
	return t
}

// Code is the code point of the Codicon the toggle shows.
func (t *Toggle) Code() int { return t.code }

// Checked says whether the toggle is on.
func (t *Toggle) Checked() bool { return t.opts.Checked }

// CreateState implements widgets.StatefulWidget.
func (t *Toggle) CreateState() widgets.State { return &toggleState{} }

type toggleState struct {
	widgets.StateOf[*Toggle]
	hover bool
}

// Build implements widgets.State.
func (s *toggleState) Build(bc widgets.BuildContext) widgets.Widget {
	colors := ThemeOf(bc)
	o := s.Widget().opts
	foreground := colors.IconForeground
	border := ui.NewColor(0)
	var background ui.Color
	switch {
	case o.Checked:
		foreground, border, background = colors.InputOptionActiveForeground, colors.InputOptionActiveBorder, colors.InputOptionActiveBackground
	case s.hover && o.OnChanged != nil:
		background = colors.InputOptionHoverBackground
	}
	decoration := painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Color:        foundation.Some(background),
		Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)}),
		BorderRadius: painting.NewBorderRadiusCircular(toggleRadius),
	})
	box := widgets.NewContainer(
		widgets.NewIcon(fonts.CodiconIcon(s.Widget().code), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(foreground)}),
		widgets.ContainerOpts{Width: foundation.Some(toggleSize), Height: foundation.Some(toggleSize), Alignment: painting.AlignmentCenter, Decoration: decoration})
	cursor := services.SystemMouseCursors.Basic
	if o.OnChanged != nil {
		cursor = services.SystemMouseCursors.Click
	}
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(box, widgets.GestureDetectorOpts{OnTap: s.tapped}),
		widgets.MouseRegionOpts{
			Cursor:  cursor,
			OnEnter: func(*gestures.PointerEnterEvent) { s.setHover(true) },
			OnExit:  func(*gestures.PointerExitEvent) { s.setHover(false) },
		})
}

func (s *toggleState) setHover(h bool) {
	if s.hover != h && s.Mounted() {
		s.SetState(func() { s.hover = h })
	}
}

func (s *toggleState) tapped() {
	if o := s.Widget().opts; o.OnChanged != nil {
		o.OnChanged(!o.Checked)
	}
}
