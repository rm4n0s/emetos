package workbench

import (
	"slices"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func TestBannersModel(t *testing.T) {
	b := NewBanners()
	changes := 0
	stop := b.OnChange(func() { changes++ })
	updateA, removeA := b.Add(Banner{Text: "a"})
	_, removeB := b.Add(Banner{Text: "b", Icon: "remote", Actions: []BannerAction{{Label: "Go"}}})
	texts := func() []string {
		var out []string
		for _, x := range b.Items() {
			out = append(out, x.Text)
		}
		return out
	}
	if got := texts(); !slices.Equal(got, []string{"a", "b"}) || changes != 2 {
		t.Fatalf("items %v after %d changes", got, changes)
	}
	updateA(Banner{Text: "a2"})
	if got := texts(); !slices.Equal(got, []string{"a2", "b"}) {
		t.Errorf("items = %v", got)
	}
	removeA()
	removeA() // twice does nothing
	updateA(Banner{Text: "gone"})
	if got := texts(); !slices.Equal(got, []string{"b"}) {
		t.Errorf("items = %v", got)
	}
	stop()
	n := changes
	removeB()
	if changes != n || len(b.Items()) != 0 {
		t.Errorf("a stopped listener was called, or the banner stayed")
	}
}

func TestBannersAreShownUnderTheTitleBar(t *testing.T) {
	banners := NewBanners()
	pressed := 0
	var remove func()
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, func(o *Options) { o.Banners = banners })
	base := h.rect(PartEditor)
	if h.ts.Exists(gluttertest.ByKey(BannersKey())) {
		t.Fatal("a banner is shown although there is none")
	}
	_, remove = banners.Add(Banner{Text: "Remote Work: laptop B connected", Icon: "remote", Actions: []BannerAction{{Label: "Disconnect", Run: func() { pressed++ }}}})
	h.settle()
	if !h.ts.Exists(gluttertest.ByText("Remote Work: laptop B connected")) {
		t.Fatal("the banner is not shown")
	}
	// it pushes the workbench down by its height
	if r := h.rect(PartEditor); r.Top != base.Top+BannerHeight || r.Height() != base.Height()-BannerHeight {
		t.Errorf("editor area %v, was %v: the banner should take %v px", r, base, BannerHeight)
	}
	h.ts.Tap(gluttertest.ByText("Disconnect"))
	h.settle()
	if pressed != 1 {
		t.Errorf("the button was pressed %d times", pressed)
	}
	remove()
	h.settle()
	if h.ts.Exists(gluttertest.ByKey(BannersKey())) || h.rect(PartEditor) != base {
		t.Error("the banner left a trace")
	}
	noErrors(t, h)
}

func TestGoldenABanner(t *testing.T) {
	for _, th := range []struct {
		name   string
		colors kit.Colors
	}{{"dark", kit.DarkModern()}, {"light", kit.LightModern()}} {
		t.Run(th.name, func(t *testing.T) {
			banners := NewBanners()
			banners.Add(Banner{Text: "Remote Work: laptop B (work) connected", Icon: "remote", Actions: []BannerAction{{Label: "Disconnect", Run: func() {}}}})
			h := newHarness(t, testSize, th.colors, DefaultLayoutState(), nil, func(o *Options) { o.Banners = banners })
			testutil.MatchesGolden(h.ts, "banner_"+th.name)
		})
	}
}
