package httpclient

import (
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// showOn draws a view of the extension on the screen with the origin (extension.HostOrigin: the window of the host).
func (r *rig) showOn(th uitest.Theme, origin extension.Origin, view string, w, h float64) *gluttertest.Tester {
	r.t.Helper()
	v := r.api.View(view)
	if v == nil {
		r.t.Fatalf("the view %s was not provided", view)
	}
	return uitest.Pump(r.t, th, gui.Size{Width: w, Height: h}, uitest.OnScreen(origin, widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return v.Content.Build(bc) })))
}

// The side bar on the screen of a client of a hosted workspace (D46, SPEC §10.33 "Privacy and safety"): the host's collections,
// environments and history are not listed, the view says why, and the host's own window is unchanged.
func TestTheViewsSayNothingOfTheHostsCollectionsToAClient(t *testing.T) {
	for _, tc := range []struct {
		view string
		// host is a text the host's view shows and the client's must not
		host string
	}{
		{CollectionsViewID, "Payments"},
		{EnvironmentsViewID, "Dev"},
		{HistoryViewID, "/v1/users"},
	} {
		t.Run(tc.view, func(t *testing.T) {
			r := newRig(t)
			r.seedHistory()
			ts := r.showOn(uitest.Themes()[0], extension.HostOrigin, tc.view, 320, 300)
			if !ts.Exists(gluttertest.ByText(tc.host)) && tc.view != HistoryViewID {
				t.Fatalf("the host's view does not show %q", tc.host)
			}
			if ts.Exists(gluttertest.ByText(clientNoteText)) {
				t.Error("the host's view carries the client's note")
			}
			ts = r.showOn(uitest.Themes()[0], 7, tc.view, 320, 300)
			if !ts.Exists(gluttertest.ByText(clientNoteText)) {
				t.Error("the client's view does not say that the HTTP client is not available")
			}
			for _, name := range []string{"Payments", "Shop API", "Dev", "Staging", "Health"} {
				if ts.Exists(gluttertest.ByText(name)) {
					t.Errorf("the client's view shows %q", name)
				}
			}
		})
	}
}

func TestGoldenTheCollectionsViewOnTheScreenOfAClient(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			ts := r.showOn(th, 7, CollectionsViewID, sideW, 160)
			uitest.MatchesGolden(ts, "collections_client_"+th.Name)
		})
	}
}
