package workbench

import (
	"math"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The tabs of the panel that do not fit in its title strip (step 17b.6, D4). The actions of the open view keep their room and the
// tabs get what is left (decision 370, SPEC §10.13 "The title strip of the panel").
//
// Ported from VS Code: src/vs/workbench/browser/parts/compositeBar.ts and paneCompositeBar.ts (the overflow of the composite
// bar: the items that do not fit go into the "..." action's menu, the active one stays in the bar)

// panelTabWidth is the width a tab has in the strip: its title in capitals, the count after it when it has one, and the space on
// each side.
func (s *workbenchState) panelTabWidth(bc widgets.BuildContext, tab PanelTab) float64 {
	w := 2*panelTabPadding + kit.MeasureText(bc, strings.ToUpper(tab.Title), kit.TextStyleOf(bc, 11, ui.Color{}))
	if tab.Badge > 0 {
		count := strconv.Itoa(tab.Badge)
		if tab.Badge > 999 {
			count = "999+"
		}
		badge := kit.TextStyleOf(bc, 9, ui.Color{}).Merge(painting.NewTextStyle(painting.TextStyleOpts{FontWeight: ui.FontWeightW600}))
		w += panelBadgeGap + max(16, kit.MeasureText(bc, count, badge)+10)
	}
	return w
}

// panelTabsWidth is the width the shown tabs take, with the "..." button when some are hidden.
func (s *workbenchState) panelTabsWidth(bc widgets.BuildContext, shown []PanelTab, overflow bool) float64 {
	total := 0.0
	for _, t := range shown {
		total += s.panelTabWidth(bc, t)
	}
	if overflow {
		total += panelOverflowWidth
	}
	return total
}

// panelTabsLeast is the least room the tabs are left with when the actions of the view take what they ask for: panelTabsMinWidth
// (VS Code's), or the room for the active tab with the "..." button when that is more, so that the active tab shows whole.
func (s *workbenchState) panelTabsLeast(bc widgets.BuildContext, tabs []PanelTab, active string) float64 {
	least := panelTabsMinWidth
	for _, t := range tabs {
		if t.ID != active {
			continue
		}
		w := s.panelTabWidth(bc, t)
		if len(tabs) > 1 {
			w += panelOverflowWidth
		}
		least = max(least, w)
	}
	return least
}

// fitPanelTabs splits the tabs into those that are shown and those that go into the menu of the "..." button, for a strip
// that has room for them (what the buttons that maximize and hide the panel and the actions of the view leave, at least
// panelTabsMinWidth, as VS Code lays out its composite bar): all of them when they fit, else as many from the left as fit with
// the "..." button, and the active tab always shows, in the place of the last one when it would be hidden. A tab is shown whole
// or not at all.
func (s *workbenchState) fitPanelTabs(bc widgets.BuildContext, tabs []PanelTab, room float64) (shown, hidden []PanelTab) {
	room = max(room, panelTabsMinWidth)
	if math.IsInf(room, 1) || s.panelTabsWidth(bc, tabs, false) <= room+panelFitSlack {
		return tabs, nil
	}
	room -= panelOverflowWidth
	active := s.Widget().opts.ActivePanel
	used, n := 0.0, 0
	for _, t := range tabs {
		w := s.panelTabWidth(bc, t)
		if used+w > room {
			break
		}
		used += w
		n++
	}
	picked := make([]bool, len(tabs))
	for i := range n {
		picked[i] = true
	}
	// the active tab shows: it takes the place of the last shown one, or of as many as it needs
	for i, t := range tabs {
		if t.ID != active || picked[i] {
			continue
		}
		used += s.panelTabWidth(bc, t)
		for j := n - 1; j >= 0 && used > room; j-- {
			used -= s.panelTabWidth(bc, tabs[j])
			picked[j] = false
		}
		picked[i] = true
	}
	for i, t := range tabs {
		if picked[i] {
			shown = append(shown, t)
		} else {
			hidden = append(hidden, t)
		}
	}
	return shown, hidden
}

// panelOverflow is the "..." button at the end of the tabs of the panel: its menu lists the tabs that do not fit, the active one
// checked, and choosing one shows its view.
type panelOverflow struct {
	widgets.StatefulWidget
	hidden   []PanelTab
	active   string
	onSelect func(id string)
}

// PanelOverflowKey is the key of the "..." button of the title strip of the panel, which a test finds it by.
var PanelOverflowKey = foundation.NewValueKey("workbench:panel-overflow")

func newPanelOverflow(hidden []PanelTab, active string, onSelect func(id string)) widgets.Widget {
	return widgets.NewKeyedSubtree(&panelOverflow{StatefulWidget: widgets.NewStatefulWidgetBase(), hidden: hidden, active: active, onSelect: onSelect},
		widgets.WidgetOpts{Key: PanelOverflowKey})
}

// CreateState implements widgets.StatefulWidget.
func (w *panelOverflow) CreateState() widgets.State { return &panelOverflowState{} }

type panelOverflowState struct {
	widgets.StateOf[*panelOverflow]
	menu   *kit.ContextMenuController
	bounds kit.Bounds
}

// InitState makes the controller of the menu.
func (s *panelOverflowState) InitState() { s.menu = kit.NewContextMenuController() }

// show opens the menu under the button.
func (s *panelOverflowState) show() {
	if s.menu.IsShowing() {
		s.menu.Hide()
		return
	}
	w := s.Widget()
	items := make([]kit.MenuItem, len(w.hidden))
	for i, t := range w.hidden {
		label := t.Title
		if t.Badge > 0 {
			label += " (" + strconv.Itoa(t.Badge) + ")"
		}
		items[i] = kit.MenuItem{ID: t.ID, Label: strings.ReplaceAll(label, "&", "&&"), Checked: t.ID == w.active}
	}
	at := ui.Offset{}
	if r, ok := s.bounds.Rect(); ok {
		at = ui.Offset{Dx: r.Left, Dy: r.Bottom}
	}
	s.menu.Show(at, items)
}

// Build implements widgets.State.
func (s *panelOverflowState) Build(widgets.BuildContext) widgets.Widget {
	button := kit.NewBoundsProbe(kit.NewIconButton(fonts.CodiconEllipsis, kit.IconButtonOpts{Tooltip: "More Views...", OnPressed: s.show}), &s.bounds)
	return kit.NewContextMenu(
		widgets.NewSizedBox(widgets.NewCenter(button), widgets.SizedBoxOpts{Width: foundation.Some(panelOverflowWidth)}),
		kit.ContextMenuOpts{
			Controller: s.menu,
			OnRun: func(it kit.MenuItem) {
				if f := s.Widget().onSelect; f != nil {
					f(it.ID)
				}
			},
		})
}
