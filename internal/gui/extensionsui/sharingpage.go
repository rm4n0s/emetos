package extensionsui

import (
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Texts of the page "Extensions > Event Sharing" (SPEC §10.15 "Event sharing").
const (
	sharingTitle     = "Extensions › Event Sharing"
	sharingIntro     = "The events of a window tab reach another tab only along a link you allowed, in the direction of the arrow, and only the topics an extension asks for. The links are kept in event-sharing.json in Emetos's state folder; no settings file and no extension can create one."
	linksHeading     = "Links"
	scopesHeading    = "Window tabs"
	noLinksText      = "No tab shares events. Nothing crosses between tabs until an extension asks for it and you allow it."
	noScopesText     = "No window tab has a folder yet."
	allTopicsText    = "every topic an extension asks for"
	sessionNote      = "until Emetos quits"
	removeLabel      = "Remove"
	privateLabel     = "Private: never send events to other tabs"
	manageSharingBtn = "Event Sharing..."
	arrow            = " → "
)

// SharingRemoveKey is the key of the Remove button of a link, which a test finds it by.
func SharingRemoveKey(from, to string, session bool) foundation.Key {
	kind := "saved"
	if session {
		kind = "session"
	}
	return foundation.NewValueKey("sharing:remove:" + kind + ":" + from + ">" + to)
}

// SharingPrivateKey is the key of the Private switch of a window tab.
func SharingPrivateKey(id string) foundation.Key {
	return foundation.NewValueKey("sharing:private:" + id)
}

// ManageSharingKey is the key of the button on the Extensions page that opens the Event Sharing page.
func ManageSharingKey() foundation.Key { return foundation.NewValueKey("extensions:manage-sharing") }

// SharingOptions are the options of NewSharingPage.
type SharingOptions struct {
	// Snapshot gives the links and the window tabs; it is called at every build.
	Snapshot func() exthost.SharingSnapshot
	// Subscribe calls fn when the snapshot may have changed and returns the function that stops it.
	Subscribe func(fn func()) (unsubscribe func())
	// OnRemove is called when the user presses Remove on a link.
	OnRemove func(link exthost.SharingEntry)
	// OnPrivate is called when the user flips the Private switch of the window tab with the ID.
	OnPrivate func(id string, private bool)
}

// SharingPage is the page "Extensions > Event Sharing": the links between window tabs as `from → to` with their topics and
// a Remove button, the links of the session, and the Private switch of each window tab.
type SharingPage struct {
	widgets.StatefulWidget
	opts SharingOptions
}

// NewSharingPage makes the page.
func NewSharingPage(opts SharingOptions) *SharingPage {
	return &SharingPage{StatefulWidget: widgets.NewStatefulWidgetBase(), opts: opts}
}

// CreateState implements widgets.StatefulWidget.
func (p *SharingPage) CreateState() widgets.State { return &sharingState{} }

type sharingState struct {
	widgets.StateOf[*SharingPage]
	stop func()
}

// InitState follows the host.
func (s *sharingState) InitState() { s.follow() }

func (s *sharingState) follow() {
	if sub := s.Widget().opts.Subscribe; sub != nil {
		s.stop = sub(func() {
			if s.Mounted() {
				s.SetState(func() {})
			}
		})
	}
}

// DidUpdateWidget follows the host of the new widget.
func (s *sharingState) DidUpdateWidget(old *SharingPage) {
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
	s.follow()
}

// Dispose stops following the host.
func (s *sharingState) Dispose() {
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
}

// styler makes the text styles of a page.
type styler struct {
	bc     widgets.BuildContext
	colors kit.Colors
}

func (st styler) style(size float64, weight ui.FontWeight, color ui.Color) *painting.TextStyle {
	return painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: kit.UIFontOf(st.bc), FontFamilyFallback: []string{"sans-serif"},
		FontSize: foundation.Some(size), FontWeight: weight, Color: foundation.Some(color),
	})
}

func (st styler) text(t string, size float64, weight ui.FontWeight, color ui.Color) widgets.Widget {
	return widgets.NewText(t, widgets.TextOpts{Style: st.style(size, weight, color)})
}

func gap(h float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(h)})
}

// Build implements widgets.State.
func (s *sharingState) Build(bc widgets.BuildContext) widgets.Widget {
	o := s.Widget().opts
	colors := kit.ThemeOf(bc)
	st := styler{bc: bc, colors: colors}
	var snap exthost.SharingSnapshot
	if o.Snapshot != nil {
		snap = o.Snapshot()
	}
	children := []widgets.Widget{
		st.text(sharingTitle, titleSize, ui.FontWeightW600, colors.Foreground),
		gap(6),
		st.text(sharingIntro, detailSize, ui.FontWeightW400, colors.DescriptionForeground),
		gap(18),
		st.text(linksHeading, nameSize+1, ui.FontWeightW600, colors.Foreground),
		gap(6),
	}
	if len(snap.Links) == 0 {
		children = append(children, st.text(noLinksText, nameSize, ui.FontWeightW400, colors.DescriptionForeground))
	}
	for _, l := range snap.Links {
		children = append(children, linkRow(l, o, colors, st))
	}
	children = append(children, gap(22), st.text(scopesHeading, nameSize+1, ui.FontWeightW600, colors.Foreground), gap(6))
	if len(snap.Scopes) == 0 {
		children = append(children, st.text(noScopesText, nameSize, ui.FontWeightW400, colors.DescriptionForeground))
	}
	for _, sc := range snap.Scopes {
		children = append(children, scopeRow(sc, o, colors, st))
	}
	return pageFrame(colors, children)
}

// pageFrame puts the children of a page in its scrolling column.
func pageFrame(colors kit.Colors, children []widgets.Widget) widgets.Widget {
	content := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(pageSide, pageTop, pageSide, pageTop),
		widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin}))
	limited := widgets.NewAlign(widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(maxWidth)}), content),
		widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})
	return widgets.NewColoredBox(colors.EditorBackground, kit.NewScrollableElement(limited, kit.ScrollableElementOpts{Axes: kit.ScrollAxisVertical}))
}

// divided puts a bottom border under a row.
func divided(colors kit.Colors, key foundation.Key, body widgets.Widget) widgets.Widget {
	return widgets.NewKeyedSubtree(
		widgets.NewDecoratedBox(
			painting.NewBoxDecoration(painting.BoxDecorationOpts{Border: painting.Border{Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.PanelBorder), Width: foundation.Some(1.0)})}}),
			widgets.NewPadding(painting.NewEdgeInsetsSymmetric(rowPad, 0), body)),
		widgets.WidgetOpts{Key: key})
}

// LinkText is the text of a link row: `from → to`.
func LinkText(l exthost.SharingEntry) string { return l.FromName + arrow + l.ToName }

// TopicsText says which topics a link carries.
func TopicsText(l exthost.SharingEntry) string {
	topics := allTopicsText
	if len(l.Events) > 0 {
		topics = strings.Join(l.Events, ", ")
	}
	if l.Session {
		topics += "  •  " + sessionNote
	}
	return topics
}

// linkRow is the row of one link.
func linkRow(l exthost.SharingEntry, o SharingOptions, colors kit.Colors, st styler) widgets.Widget {
	lines := []widgets.Widget{
		st.text(LinkText(l), nameSize, ui.FontWeightW600, colors.Foreground), gap(rowLineGap),
		st.text(TopicsText(l), detailSize, ui.FontWeightW400, colors.Foreground), gap(rowLineGap),
		st.text(l.From+arrow+l.To, detailSize, ui.FontWeightW400, colors.DescriptionForeground),
	}
	button := kit.NewButton(removeLabel, kit.ButtonOpts{Key: SharingRemoveKey(l.From, l.To, l.Session), Secondary: true, Small: true, OnPressed: func() {
		if o.OnRemove != nil {
			o.OnRemove(l)
		}
	}})
	body := widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(widgets.NewColumn(lines, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(buttonGap)}),
		button,
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
	return divided(colors, foundation.NewValueKey("sharing:link:"+l.From+">"+l.To+":"+TopicsText(l)), body)
}

// scopeRow is the row of one window tab with its Private switch.
func scopeRow(sc exthost.SharingScope, o SharingOptions, colors kit.Colors, st styler) widgets.Widget {
	state := "open"
	if !sc.Open {
		state = "not open"
	}
	lines := []widgets.Widget{
		st.text(sc.Name, nameSize, ui.FontWeightW600, colors.Foreground), gap(rowLineGap),
		st.text(sc.ID+"  •  "+state, detailSize, ui.FontWeightW400, colors.DescriptionForeground),
	}
	id := sc.ID
	box := kit.NewCheckbox(privateLabel, kit.CheckboxOpts{Key: SharingPrivateKey(id), Checked: sc.Private, OnChanged: func(on bool) {
		if o.OnPrivate != nil {
			o.OnPrivate(id, on)
		}
	}})
	body := widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(widgets.NewColumn(lines, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(buttonGap)}),
		box,
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	return divided(colors, foundation.NewValueKey("sharing:scope:"+id), body)
}
