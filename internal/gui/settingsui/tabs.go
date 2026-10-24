package settingsui

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// A target tab, as VS Code's settings tabs widget: 13 px text with 8 px at the sides, and a 1 px line under the
// checked one in the color of the header.
const (
	tabFontSize = 13.0
	tabPadH     = 8.0
	tabPadTop   = 7.0
	tabPadBot   = 6.5
)

// tabButton is one tab of the strip: User or Workspace.
type tabButton struct {
	label    string
	checked  bool
	disabled bool
	tooltip  string
	onTap    func()
}

// tabStrip lays out the tabs of the targets.
func tabStrip(bc widgets.BuildContext, colors kit.Colors, tabs []tabButton) widgets.Widget {
	items := make([]widgets.Widget, len(tabs))
	for i, t := range tabs {
		items[i] = tabWidget(bc, colors, t)
	}
	return widgets.NewRow(items, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin})
}

func tabWidget(bc widgets.BuildContext, colors kit.Colors, t tabButton) widgets.Widget {
	color := colors.Foreground.WithOpacity(0.9)
	switch {
	case t.disabled:
		color = colors.Foreground.WithOpacity(0.4)
	case t.checked:
		color = colors.SettingsHeaderForeground
	}
	line := ui.Color{}
	if t.checked && !t.disabled {
		line = colors.SettingsHeaderForeground
	}
	label := widgets.NewContainer(textOf(bc, t.label, tabFontSize, color, 0), widgets.ContainerOpts{
		Padding: painting.NewEdgeInsetsFromLTRB(tabPadH, tabPadTop, tabPadH, tabPadBot),
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Border: painting.Border{Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(line), Width: foundation.Some(1.0)})},
		}),
	})
	cursor := services.SystemMouseCursors.Click
	var tap func()
	if !t.disabled {
		tap = t.onTap
	} else {
		cursor = services.SystemMouseCursors.Basic
	}
	var w widgets.Widget = widgets.NewMouseRegion(
		widgets.NewGestureDetector(label, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: tap}),
		widgets.MouseRegionOpts{Cursor: cursor})
	if t.tooltip != "" {
		w = kit.NewHover(w, kit.HoverOpts{Text: t.tooltip, Compact: true})
	}
	return w
}
