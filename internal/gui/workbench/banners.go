package workbench

import (
	"sync"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// BannerHeight is the height of one banner.
const BannerHeight = 34.0

// Banner is a bar across the top of the workbench, under the title bar: an icon, a line of text and buttons. The extensions
// show them for something that goes on in the window and that the user should always see and be able to end (SPEC §10.15,
// Remote Work's "connected" banner).
type Banner struct {
	// Text is the message and Icon the name of the Codicon before it; empty for none.
	Text, Icon string
	// Actions are the buttons at the right end.
	Actions []BannerAction
}

// BannerAction is a button of a banner.
type BannerAction struct {
	// Label is the text of the button.
	Label string
	// Run is called on the UI thread when the button is pressed.
	Run func()
}

// Banners is the model of the banners of one window tab: the workbench shows them in the order they were added and
// follows the changes. It is safe for concurrent use; the listeners run on the goroutine that changed it.
type Banners struct {
	mu        sync.Mutex
	next      int
	items     []bannerEntry
	listeners map[int]func()
}

type bannerEntry struct {
	id     int
	banner Banner
}

// NewBanners makes an empty model.
func NewBanners() *Banners { return &Banners{listeners: map[int]func(){}} }

// Add shows a banner at the end of the list and returns the function that replaces it and the one that takes it away.
func (b *Banners) Add(banner Banner) (update func(Banner), remove func()) {
	b.mu.Lock()
	b.next++
	id := b.next
	b.items = append(b.items, bannerEntry{id: id, banner: banner})
	b.mu.Unlock()
	b.changed()
	update = func(nb Banner) {
		b.mu.Lock()
		for i := range b.items {
			if b.items[i].id == id {
				b.items[i].banner = nb
			}
		}
		b.mu.Unlock()
		b.changed()
	}
	remove = func() {
		b.mu.Lock()
		for i := range b.items {
			if b.items[i].id == id {
				b.items = append(b.items[:i], b.items[i+1:]...)
				break
			}
		}
		b.mu.Unlock()
		b.changed()
	}
	return update, remove
}

// Items returns the banners to show, the oldest first.
func (b *Banners) Items() []Banner {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Banner, len(b.items))
	for i, e := range b.items {
		out[i] = e.banner
	}
	return out
}

// OnChange calls fn after the list changed and returns the function that stops it.
func (b *Banners) OnChange(fn func()) (stop func()) {
	b.mu.Lock()
	b.next++
	id := b.next
	b.listeners[id] = fn
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		delete(b.listeners, id)
		b.mu.Unlock()
	}
}

func (b *Banners) changed() {
	b.mu.Lock()
	fns := make([]func(), 0, len(b.listeners))
	for _, f := range b.listeners {
		fns = append(fns, f)
	}
	b.mu.Unlock()
	for _, f := range fns {
		f()
	}
}

// bannerKey is the key of the column of banners, which a test finds it by.
var bannerKey = foundation.NewValueKey("workbench:banners")

// BannersKey is the key of the column that holds the banners; it is in the tree only while there is a banner.
func BannersKey() foundation.Key { return bannerKey }

// bannerBar is the banners as a column of strips, in the colors of the notifications.
func bannerBar(bc widgets.BuildContext, colors kit.Colors, banners []Banner) widgets.Widget {
	rows := make([]widgets.Widget, len(banners))
	for i, bn := range banners {
		row := []widgets.Widget{widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(10.0)})}
		if code, ok := fonts.CodiconByName(bn.Icon); ok && bn.Icon != "" {
			row = append(row, widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(colors.NotificationsInfoIconForeground)}),
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}))
		}
		row = append(row, widgets.NewExpanded(widgets.NewText(bn.Text, widgets.TextOpts{
			Style: kit.TextStyleOf(bc, 13, colors.NotificationsForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
		})))
		for _, a := range bn.Actions {
			a := a
			row = append(row, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
				kit.NewButton(a.Label, kit.ButtonOpts{Small: true, OnPressed: a.Run}))
		}
		row = append(row, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(10.0)}))
		strip := widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
		rows[i] = widgets.NewSizedBox(bordered(colors.NotificationsBackground, colors.NotificationsBorder, sideBottom, strip),
			widgets.SizedBoxOpts{Height: foundation.Some(BannerHeight)})
	}
	return widgets.NewKeyedSubtree(widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}), widgets.WidgetOpts{Key: bannerKey})
}
