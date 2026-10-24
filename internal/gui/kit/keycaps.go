package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// Ported from VS Code: src/vs/base/browser/ui/keybindingLabel/keybindingLabel.ts and keybindingLabel.css (the
// look of a keybinding: each key in a box with a thicker border below)

// DefaultKeyCapFontSize is the size of the text of a key cap.
const DefaultKeyCapFontSize = 11.0

// KeyCapsOpts are the options of NewKeyCaps.
type KeyCapsOpts struct {
	// FontSize is the size of the text; the default is DefaultKeyCapFontSize.
	FontSize float64
	// Lit draws the caps of a row that is selected or under the pointer: no background, the borders in the
	// color of the widget shadow and the text in Foreground.
	Lit bool
	// Foreground is the color of the text of a lit cap.
	Foreground ui.Color
	// Highlight, when set, says for the key at an index of a chord (counted from 0, the modifiers first)
	// whether a search matched it: it is drawn in bold in the highlight color of the list.
	Highlight func(chord, key int) bool
}

// NewKeyCaps draws a keybinding as key caps: each key in a small box with a border that is thicker below, a plus
// sign between the keys of a chord and a gap between chords. chords is the names of the keys of each chord, such as
// [["Ctrl", "K"], ["Ctrl", "C"]].
func NewKeyCaps(bc widgets.BuildContext, chords [][]string, opts KeyCapsOpts) widgets.Widget {
	colors := ThemeOf(bc)
	size := opts.FontSize
	if size <= 0 {
		size = DefaultKeyCapFontSize
	}
	capFg, border, bottom := colors.KeybindingLabelForeground, colors.KeybindingLabelBorder, colors.KeybindingLabelBottomBorder
	bg := colors.KeybindingLabelBackground
	if opts.Lit {
		capFg, bg, border, bottom = opts.Foreground, ui.Color{}, colors.WidgetShadow, colors.WidgetShadow
	}
	var parts []widgets.Widget
	for ci, chord := range chords {
		if ci > 0 {
			parts = append(parts, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}))
		}
		for ki, key := range chord {
			if ki > 0 {
				parts = append(parts, widgets.NewText("+", widgets.TextOpts{Style: TextStyleOf(bc, size, capFg)}))
			}
			style := TextStyleOf(bc, size, capFg)
			if opts.Highlight != nil && opts.Highlight(ci, ki) {
				style = style.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.ListHighlightForeground), FontWeight: ui.FontWeightBold})
			}
			box := painting.BoxDecorationOpts{
				Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)}),
				BorderRadius: painting.NewBorderRadiusCircular(3),
			}
			if bg.Alpha() > 0 {
				box.Color = foundation.Some(bg)
			}
			cap := widgets.NewStack([]widgets.Widget{
				widgets.NewContainer(
					widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(4, 1, 4, 2), widgets.NewText(key, widgets.TextOpts{Style: style})),
					widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(box)}),
				// the border below is thicker and has a color of its own
				widgets.NewPositioned(widgets.NewIgnorePointer(widgets.NewContainer(nil, widgets.ContainerOpts{
					Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(bottom)})})),
					widgets.PositionedOpts{Left: foundation.Some(2.0), Right: foundation.Some(2.0), Bottom: foundation.Some(0.0), Height: foundation.Some(2.0)}),
			}, widgets.StackOpts{Fit: rendering.StackFitLoose})
			parts = append(parts, widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 1), cap))
		}
	}
	return widgets.NewRow(parts, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}
