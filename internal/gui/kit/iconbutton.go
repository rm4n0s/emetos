package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of VS Code's icon buttons and toggles (the find widget's `.button` and
// `.monaco-custom-toggle`).
const (
	iconSize         = 16.0
	iconButtonSize   = 22.0
	iconButtonRadius = 5.0
	toggleSize       = 20.0
	toggleRadius     = 3.0
)

// IconButtonOpts are the options of NewIconButton.
type IconButtonOpts struct {
	// OnPressed is called when the button is clicked. A button without it is disabled.
	OnPressed func()
	// Disabled dims the button and makes it ignore the pointer.
	Disabled bool
}

// IconButton is a button that shows one Codicon and has the background of a toolbar action
// under the pointer. This is the first version; step 6.5 adds focus, tooltips and the
// keyboard.
type IconButton struct {
	widgets.StatefulWidget
	code int
	opts IconButtonOpts
}

// NewIconButton makes a button that shows the Codicon of a code point (see the Codicon...
// constants of package fonts).
func NewIconButton(code int, opts ...IconButtonOpts) *IconButton {
	b := &IconButton{StatefulWidget: widgets.NewStatefulWidgetBase(), code: code}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	return b
}

// Code is the code point of the Codicon the button shows.
func (b *IconButton) Code() int { return b.code }

// CreateState implements widgets.StatefulWidget.
func (b *IconButton) CreateState() widgets.State { return &iconButtonState{} }

type iconButtonState struct {
	widgets.StateOf[*IconButton]
	hover bool
}

func (s *iconButtonState) enabled() bool {
	o := s.Widget().opts
	return o.OnPressed != nil && !o.Disabled
}

// Build implements widgets.State.
func (s *iconButtonState) Build(bc widgets.BuildContext) widgets.Widget {
	colors := ThemeOf(bc)
	color := colors.IconForeground
	if !s.enabled() {
		color = color.WithAlpha(0x66)
	}
	box := painting.NewBoxDecoration(painting.BoxDecorationOpts{BorderRadius: painting.NewBorderRadiusCircular(iconButtonRadius)})
	if s.hover && s.enabled() {
		box = painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color: foundation.Some(colors.ToolbarHoverBackground), BorderRadius: painting.NewBorderRadiusCircular(iconButtonRadius),
		})
	}
	icon := widgets.NewContainer(
		widgets.NewIcon(fonts.CodiconIcon(s.Widget().code), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(color)}),
		widgets.ContainerOpts{Width: foundation.Some(iconButtonSize), Height: foundation.Some(iconButtonSize), Alignment: painting.AlignmentCenter, Decoration: box})
	cursor := services.SystemMouseCursors.Basic
	if s.enabled() {
		cursor = services.SystemMouseCursors.Click
	}
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(icon, widgets.GestureDetectorOpts{OnTap: s.tapped}),
		widgets.MouseRegionOpts{
			Cursor:  cursor,
			OnEnter: func(*gestures.PointerEnterEvent) { s.setHover(true) },
			OnExit:  func(*gestures.PointerExitEvent) { s.setHover(false) },
		})
}

func (s *iconButtonState) setHover(h bool) {
	if s.hover != h && s.Mounted() {
		s.SetState(func() { s.hover = h })
	}
}

func (s *iconButtonState) tapped() {
	if s.enabled() {
		s.Widget().opts.OnPressed()
	}
}
