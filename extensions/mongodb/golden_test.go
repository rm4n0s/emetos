package mongodb

import (
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// viewWidget builds the widget of the view of the Databases extension, as the box of a view does.
func viewWidget(r *rig) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return r.dbAPI.View(dbconn.ViewID).Content.Build(bc) })
}

// open selects the row with the label and opens it with the Right arrow, then waits for what is below it to be listed and shown. The
// view is rebuilt from what the extension posted, so the posts are run first; if the row was not yet an expandable one when the key
// came (a post of the hub that was still on its way), the key is sent again.
func open(t *testing.T, r *rig, ts *gluttertest.Tester, label string, wantLabel string) {
	t.Helper()
	for attempt := 0; attempt < 4; attempt++ {
		r.dbAPI.Pump()
		ts.Pump(time.Millisecond)
		ts.Tap(gluttertest.ByText(label))
		ts.Pump(time.Millisecond)
		ts.SendKeyEvent(services.LogicalKeyboardKeyArrowRight)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if ts.Exists(gluttertest.ByTextContaining(wantLabel)) {
				return
			}
			r.dbAPI.Pump()
			ts.Pump(time.Millisecond)
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatalf("%q did not show after opening %q", wantLabel, label)
}

// goldenExplorer makes the explorer open down to the fields of a collection: a connection that is open, one that failed and one
// that is read-only, and the nodes of the fixture server.
func goldenExplorer(t *testing.T, th uitest.Theme) (*rig, *gluttertest.Tester) {
	t.Helper()
	r := newRig(t)
	r.shop()
	failed := r.save(dbconn.Connection{Name: "Staging", Engine: EngineID, Host: "staging.example.com", Port: 27017, ReadOnly: true, Color: "#f0ad4e"})
	r.hub.Fail(failed.ID, errors.New("FakeServerSelection", "server selection timeout: no reachable servers"))
	r.dbAPI.Pump()
	ts := uitest.Pump(t, th, gui.Size{Width: 420, Height: 600}, viewWidget(r))
	open(t, r, ts, "Shop", "Archive")
	open(t, r, ts, "shop", "customers")
	open(t, r, ts, "orders", "Schema")
	open(t, r, ts, "Indexes", "sku_1_created_-1")
	open(t, r, ts, "Schema", "note")
	return r, ts
}

func TestGoldenTheExplorerOfADatabaseServer(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			_, ts := goldenExplorer(t, th)
			uitest.MatchesGolden(ts, "explorer_"+th.Name)
		})
	}
}

func TestGoldenTheSchemaOfADocumentField(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			r.shop()
			ts := uitest.Pump(t, th, gui.Size{Width: 360, Height: 560}, viewWidget(r))
			open(t, r, ts, "Shop", "Archive")
			open(t, r, ts, "shop", "customers")
			open(t, r, ts, "orders", "Schema")
			open(t, r, ts, "Schema", "tags")
			open(t, r, ts, "customer", "address")
			open(t, r, ts, "address", "city")
			uitest.MatchesGolden(ts, "schema_nested_"+th.Name)
		})
	}
}

func TestGoldenTheExplorerWithAMessage(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			r.shop()
			ts := uitest.Pump(t, th, gui.Size{Width: 360, Height: 330}, viewWidget(r))
			open(t, r, ts, "Shop", "Archive")
			open(t, r, ts, "shop", "customers")
			open(t, r, ts, "customers", "Schema")
			open(t, r, ts, "Indexes", "No indexes")
			open(t, r, ts, "Schema", "No documents")
			uitest.MatchesGolden(ts, "schema_empty_"+th.Name)
		})
	}
}
