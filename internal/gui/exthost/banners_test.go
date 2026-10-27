package exthost

import (
	"context"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func TestBannersAreShownUpdatedAndDisposed(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	pressed := 0
	h, err := st.api.Banners().Show(extension.Banner{Text: "Remote Work: laptop B connected", Icon: "remote",
		Actions: []extension.BannerAction{{Label: "Disconnect", Run: func() { pressed++ }}, {Label: "Nothing"}}})
	if err != nil {
		t.Fatal(err)
	}
	items := tb.banners.Items()
	if len(items) != 1 || items[0].Text != "Remote Work: laptop B connected" || items[0].Icon != "remote" || len(items[0].Actions) != 2 {
		t.Fatalf("banners = %+v", items)
	}
	items[0].Actions[0].Run()
	if pressed != 1 || items[0].Actions[1].Run != nil {
		t.Errorf("pressed %d times; a button with no function got one: %v", pressed, items[0].Actions[1].Run != nil)
	}
	second, err := st.api.Banners().Show(extension.Banner{Text: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if got := tb.banners.Items(); len(got) != 2 || got[1].Text != "second" {
		t.Fatalf("the second banner is not below the first: %+v", got)
	}
	h.Update(extension.Banner{Text: "changed"})
	h.Update(extension.Banner{Text: "  "}) // a banner with no text is not an update
	if got := tb.banners.Items(); got[0].Text != "changed" {
		t.Errorf("banners = %+v", got)
	}
	h.Dispose()
	h.Dispose() // twice does nothing
	h.Update(extension.Banner{Text: "after"})
	if got := tb.banners.Items(); len(got) != 1 || got[0].Text != "second" {
		t.Errorf("banners after Dispose = %+v", got)
	}
	// a banner that was not disposed goes with the activation
	_ = second
	st.rig.host.Shutdown()
	if got := tb.banners.Items(); len(got) != 0 {
		t.Errorf("banners after the activation = %+v", got)
	}
}

func TestBannerRefusals(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	st.rig.noBanners["one"] = true
	st.startTab("one")
	_, err := st.api.Banners().Show(extension.Banner{Text: "x"})
	wantRoute(t, err, "bannersAPI.Show.ExtensionBannersUnavailable")
	st2 := newServerTest(t, simple(), nil)
	st2.startTab("one")
	_, err = st2.api.Banners().Show(extension.Banner{Text: "  "})
	wantRoute(t, err, "bannersAPI.Show.ExtensionBannerInvalid")
	st2.rig.host.Shutdown()
	_, err = st2.api.Banners().Show(extension.Banner{Text: "x"})
	wantRoute(t, err, "bannersAPI.Show.ExtensionDeactivated")
}

func TestABannerButtonThatPanicsFailsTheExtension(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	if _, err := st.api.Banners().Show(extension.Banner{Text: "x", Actions: []extension.BannerAction{{Label: "Boom", Run: func() { panic("boom") }}}}); err != nil {
		t.Fatal(err)
	}
	tb.banners.Items()[0].Actions[0].Run()
	tb.pump()
	waitFor(t, tb, "the extension to fail", func() bool { return tb.scope.State(testExt) == StateFailed })
	appErr, ok := errors.FromError(tb.scope.Failure(testExt).Err)
	if !ok || appErr.Tag != "ExtensionBannerActionPanicked" || !appErr.HasRoute("Scope.runBannerAction.func1.ExtensionBannerActionPanicked") {
		t.Errorf("failure = %v", tb.scope.Failure(testExt).Err)
	}
}

func TestFileChooserAsksForAFile(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	tb.chooserPath = "/home/user/id.pub"
	path, err := st.api.FileChooser().Open(context.Background(), extension.FileChooserOptions{Title: "Choose", AcceptLabel: "Add", Filters: []extension.FileFilter{{Name: "Keys", Patterns: []string{"*.pub"}}}})
	if err != nil || path != "/home/user/id.pub" {
		t.Fatalf("Open = %q, %v", path, err)
	}
	if len(tb.chooser) != 1 || tb.chooser[0].Title != "Choose" || tb.chooser[0].Filters[0].Patterns[0] != "*.pub" {
		t.Errorf("options = %+v", tb.chooser)
	}
	// the error of the desktop comes back as it is
	tb.chooserErr = errors.New("PortalFailed", "no portal")
	_, err = st.api.FileChooser().Open(context.Background(), extension.FileChooserOptions{})
	wantRoute(t, err, "PortalFailed")
}

func TestFileChooserRefusals(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	st.rig.noChooser["one"] = true
	st.startTab("one")
	_, err := st.api.FileChooser().Open(context.Background(), extension.FileChooserOptions{})
	wantRoute(t, err, "fileChooserAPI.Open.ExtensionFileChooserUnavailable")
	st.rig.host.Shutdown()
	_, err = st.api.FileChooser().Open(context.Background(), extension.FileChooserOptions{})
	wantRoute(t, err, "fileChooserAPI.Open.ExtensionDeactivated")
}
