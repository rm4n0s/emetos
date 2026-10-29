package redis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// explorer is the Databases view of the shared hub, activated over a second fake API, so that the tests see the tree as the user does.
type explorer struct {
	r   *rig
	api *fakeapi.API
}

func newExplorer(t *testing.T, r *rig) *explorer {
	t.Helper()
	e := dbconn.Factory(r.hub)()
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: r.folder, ID: r.folder})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	return &explorer{r: r, api: api}
}

func (x *explorer) widget() widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return x.api.View(dbconn.ViewID).Content.Build(bc) })
}

// key sends a key and lets the frames run, pumping what the extensions posted.
func (x *explorer) key(ts *gluttertest.Tester, k services.LogicalKeyboardKey) {
	ts.SendKeyDownEvent(k)
	ts.SendKeyUpEvent(k)
	x.settle(ts)
}

// settle runs frames and the posted work until both are quiet.
func (x *explorer) settle(ts *gluttertest.Tester) {
	for i := 0; i < 40; i++ {
		ran := x.api.Pump()
		if x.r.api.Pump() {
			ran = true
		}
		ts.Pump(5 * time.Millisecond)
		if !ran && i > 3 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// openDB0 clicks the connection (a click on a row of a tree opens it), waits for its databases, and moves to db0 and opens it.
func (x *explorer) openDB0(t *testing.T, ts *gluttertest.Tester) {
	t.Helper()
	ts.Tap(gluttertest.ByText("cache"))
	x.waitText(t, ts, "db0")
	x.key(ts, services.LogicalKeyboardKeyArrowRight) // to db0
	x.key(ts, services.LogicalKeyboardKeyArrowRight) // opens it
}

// waitText settles until the text is on screen.
func (x *explorer) waitText(t *testing.T, ts *gluttertest.Tester, text string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !hasText(ts, text) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q on screen", text)
		}
		x.settle(ts)
	}
}

func TestTheExplorerShowsTheKeysOfARedis(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, nil)
			r.connect()
			x := newExplorer(t, r)
			ts := uitest.Pump(t, th, gui.Size{Width: 340, Height: 330}, x.widget())
			x.settle(ts)
			x.openDB0(t, ts)
			x.waitText(t, ts, "greeting")
			if !hasText(ts, "db0") || !hasText(ts, "greeting") || !hasText(ts, "user") {
				t.Fatalf("the tree is not open: db0 %v greeting %v user %v", hasText(ts, "db0"), hasText(ts, "greeting"), hasText(ts, "user"))
			}
			// open the folder "user" and its folder "42"
			for _, f := range []string{"user", "42"} {
				ts.Tap(gluttertest.At(gluttertest.ByText(f), 0))
				x.waitText(t, ts, map[string]string{"user": "42", "42": "tags"}[f])
			}
			uitest.MatchesGolden(ts, "explorer_"+th.Name)
		})
	}
}

func TestDoubleClickingAKeyOpensItsEditor(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	x := newExplorer(t, r)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 340, Height: 330}, x.widget())
	x.settle(ts)
	x.openDB0(t, ts)
	x.waitText(t, ts, "scores")
	ts.Tap(gluttertest.ByText("scores"))
	x.key(ts, services.LogicalKeyboardKeyEnter)
	r.api.WaitFor("the key editor", func() bool { return len(r.keyTabs()) == 1 && r.keyTabs()[0].loaded })
	if r.keyTabs()[0].ref.Key != "scores" {
		t.Errorf("opened %q", r.keyTabs()[0].ref.Key)
	}
}

func TestTheRefreshCommandOfTheViewScansAgain(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	x := newExplorer(t, r)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 340, Height: 330}, x.widget())
	x.settle(ts)
	x.openDB0(t, ts)
	x.waitText(t, ts, "greeting")
	if hasText(ts, "fresh") {
		t.Fatal("fixture")
	}
	r.store.put(0, "fresh", &fakeKey{typ: TypeString, str: "new"})
	if err := x.api.Run(dbconn.Refresh, nil); err != nil {
		t.Fatal(err)
	}
	x.waitText(t, ts, "fresh")
}

func TestLoadMoreInTheViewListsTheNextPage(t *testing.T) {
	store := newFakeServer()
	for i := 0; i < 25; i++ {
		store.put(0, fmt.Sprintf("key%02d", i), &fakeKey{typ: TypeString, str: "v"})
	}
	r := newRig(t, store, func(c *dbconn.Connection) { c.Options[OptSeparator] = "none" })
	r.api.SetSetting(SettingScanCount, 10)
	r.connect()
	x := newExplorer(t, r)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 340, Height: 700}, x.widget())
	x.settle(ts)
	x.openDB0(t, ts)
	x.waitText(t, ts, "Load more keys...")
	if !hasText(ts, "10 of 25") || !hasText(ts, "key09") || hasText(ts, "key10") {
		t.Fatal("the first page is not what the setting says")
	}
	ts.Tap(gluttertest.ByTextContaining("Load more keys"))
	x.key(ts, services.LogicalKeyboardKeyEnter)
	x.waitText(t, ts, "key19")
	x.waitText(t, ts, "20 of 25")
	ts.Tap(gluttertest.ByTextContaining("Load more keys"))
	x.key(ts, services.LogicalKeyboardKeyEnter)
	x.waitText(t, ts, "key24")
	if hasText(ts, "Load more keys...") {
		t.Error("the Load more row is still there after the last page")
	}
	if n := r.ext.state(r.conn.ID, 0).tree.Len(); n != 25 {
		t.Errorf("keys loaded = %d, want 25", n)
	}
}
