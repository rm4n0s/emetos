package httpclient

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// the paths of the fixture, relative to the collections folder
const (
	pHealth  = "shop-api/health.request.jsonc"
	pUsers   = "shop-api/users"
	pList    = "shop-api/users/list-users.request.jsonc"
	pDelete  = "shop-api/users/delete-user.request.jsonc"
	pCreate  = "shop-api/users/create-user.request.jsonc"
	pCharge  = "payments/charge.request.jsonc"
	pPayment = "payments"
	pShop    = "shop-api"
)

func TestWhatADropDoes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		drag, on   string
		pos        ui.DropPosition
		dest       string
		index      int
		accepted   bool
		beforeHook func(folder string)
	}{
		{"a request into a folder goes last", pHealth, pUsers, ui.DropInside, pUsers, -1, true, nil},
		{"a request after a folder", pHealth, pUsers, ui.DropAfter, pShop, 1, true, nil},
		{"before the folder it is already before changes nothing", pHealth, pUsers, ui.DropBefore, "", 0, false, nil},
		{"a request into another collection", pList, pPayment, ui.DropInside, pPayment, -1, true, nil},
		{"before a request of another folder", pCharge, pList, ui.DropBefore, pUsers, 0, true, nil},
		{"after a request of another folder", pCharge, pList, ui.DropAfter, pUsers, 1, true, nil},
		{"within a folder, up", pDelete, pList, ui.DropBefore, pUsers, 0, true, nil},
		{"within a folder, down", pList, pDelete, ui.DropAfter, pUsers, 3, true, nil},
		{"the place it is in changes nothing", pList, pList, ui.DropAfter, "", 0, false, nil},
		{"just after the row above it changes nothing", pCreate, pList, ui.DropAfter, "", 0, false, nil},
		{"on a request is after it", pDelete, pList, ui.DropInside, pUsers, 1, true, nil},
		{"a folder into a collection", pUsers, pPayment, ui.DropInside, pPayment, -1, true, nil},
		{"a folder into itself", pUsers, pUsers, ui.DropInside, "", 0, false, nil},
		{"a folder onto a request inside it", pUsers, pCreate, ui.DropAfter, "", 0, false, nil},
		{"between collections", pCharge, pPayment, ui.DropBefore, "", 0, false, nil},
		{"onto a request on itself", pHealth, pHealth, ui.DropInside, "", 0, false, nil},
		{"into a collection of a newer version", pHealth, pPayment, ui.DropInside, "", 0, false, func(f string) {
			writeFile(t, f+"/.emetos/http/collections/payments/collection.jsonc", `{"emetosHttp": 9, "name": "Payments"}`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hooks []func(string)
			if tc.beforeHook != nil {
				hooks = append(hooks, tc.beforeHook)
			}
			r := newRig(t, hooks...)
			v := r.ext.collections
			p, ok := v.plan(r.col(tc.on), tc.pos, []string{r.col(tc.drag)})
			if ok != tc.accepted {
				t.Fatalf("accepted = %v, want %v (%+v)", ok, tc.accepted, p)
			}
			if got := v.dragAndDrop().CanDrop(r.col(tc.on), tc.pos, []string{r.col(tc.drag)}); (got != ui.DropEffectNone) != tc.accepted {
				t.Errorf("CanDrop = %v", got)
			}
			if ok && (p.dest.Path != r.col(tc.dest) || p.index != tc.index) {
				t.Errorf("dest %s index %d, want %s %d", strings.TrimPrefix(p.dest.Path, r.col("")), p.index, tc.dest, tc.index)
			}
		})
	}
}

func TestWhatCanBeDragged(t *testing.T) {
	r := newRig(t)
	dnd := r.ext.collections.dragAndDrop()
	for _, tc := range []struct {
		name string
		keys []string
		want bool
	}{
		{"a request", []string{r.col(pHealth)}, true},
		{"a folder", []string{r.col(pUsers)}, true},
		{"a collection", []string{r.col(pShop)}, false},
		{"several rows", []string{r.col(pHealth), r.col(pList)}, false},
		{"a row that is gone", []string{r.col("nothing")}, false},
	} {
		if got := dnd.CanDrag(tc.keys); got != tc.want {
			t.Errorf("%s: CanDrag = %v", tc.name, got)
		}
	}
	if _, ok := r.ext.collections.plan("", ui.DropInside, []string{r.col(pHealth)}); ok {
		t.Error("a drop on the empty part of the view was accepted")
	}
}

func TestADropMovesTheFilesAndTheOrder(t *testing.T) {
	for _, tc := range []struct {
		name       string
		drag, on   string
		pos        ui.DropPosition
		wantFile   string
		order      string // a text the order of the destination's file holds, in this order
		orderFile  string
		oldOrderNo string // the text the old parent's order no longer holds
		oldFile    string
	}{
		{"into a folder", pHealth, pUsers, ui.DropInside, "shop-api/users/health.request.jsonc", `"delete-user.request.jsonc", "health.request.jsonc"`, "shop-api/users/folder.jsonc", "health.request.jsonc", "shop-api/collection.jsonc"},
		{"a body file goes with its request", pCreate, pPayment, ui.DropInside, "payments/create-user.body.json", `"refund.request.jsonc", "create-user.request.jsonc"`, "payments/collection.jsonc", "", ""},
		{"reordering inside a folder", pDelete, pList, ui.DropBefore, "shop-api/users/delete-user.request.jsonc", `"delete-user.request.jsonc", "list-users.request.jsonc"`, "shop-api/users/folder.jsonc", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			dnd := r.ext.collections.dragAndDrop()
			dnd.OnDrop(r.col(tc.on), tc.pos, []string{r.col(tc.drag)})
			r.settle()
			if !r.has(r.col(tc.wantFile)) {
				t.Fatalf("%s is missing", tc.wantFile)
			}
			text := strings.Join(strings.Fields(r.read(r.col(tc.orderFile))), " ")
			if !strings.Contains(text, tc.order) {
				t.Errorf("the order of %s is %s, want it to hold %s", tc.orderFile, text, tc.order)
			}
			if tc.oldOrderNo != "" && strings.Contains(r.read(r.col(tc.oldFile)), tc.oldOrderNo) {
				t.Errorf("%s still lists %s", tc.oldFile, tc.oldOrderNo)
			}
		})
	}
}

func TestADropOfAFolderIntoItselfIsRefusedByTheStoreToo(t *testing.T) {
	r := newRig(t)
	r.ext.move(dropPlan{node: r.node(r.col(pUsers)), dest: r.node(r.col(pUsers)), index: -1})
	r.settle()
	if got := r.notes(); len(got) == 0 || !strings.HasPrefix(got[len(got)-1], "error: Cannot move \"Users\"") {
		t.Errorf("notes = %v", got)
	}
}

// A drag with the mouse from a row to another row moves the request.
func TestDraggingARowWithThePointer(t *testing.T) {
	r := newRig(t)
	ts := r.show(uitest.Themes()[0], CollectionsViewID, 320, 300)
	mouseDown := gluttertest.GestureOpts{Kind: gui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.PrimaryButton)}
	// rows: Payments 0, Charge 1, Refund 2, Shop API 3, Health 4, Users 5
	row := func(i int) float64 { return float64(i)*22 + 11 }
	g := ts.StartGesture(gui.Offset{Dx: 120, Dy: row(4)}, mouseDown)
	g.MoveTo(gui.Offset{Dx: 120, Dy: row(4) + 6})
	g.MoveTo(gui.Offset{Dx: 120, Dy: row(0)})
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	g.Up()
	ts.Pump(time.Millisecond)
	r.settle()
	if !r.has(r.col("payments/health.request.jsonc")) || r.has(r.col(pHealth)) {
		t.Fatal("the request did not move to Payments")
	}
	if key, _ := r.ext.collections.tree.Selected(); key != r.col("payments/health.request.jsonc") {
		t.Errorf("selected %q", key)
	}
}
