package httpclient

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

func TestNewCollectionFolderRequestAndEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name  string
		run   func(r *rig)
		check func(t *testing.T, r *rig)
	}{
		{"a collection", func(r *rig) { r.run(CmdNewCollection, nil); r.answer("Orders API") }, func(t *testing.T, r *rig) {
			p := r.col("orders-api")
			if !r.has(p+"/collection.jsonc") || !strings.Contains(r.read(p+"/collection.jsonc"), `"name": "Orders API"`) {
				t.Error("the collection file is missing or wrong")
			}
			if key, _ := r.ext.collections.tree.Selected(); key != p {
				t.Errorf("selected %q, want the new collection", key)
			}
		}},
		{"a folder in a collection", func(r *rig) { r.run(CmdNewFolder, r.col("payments")); r.answer("Webhooks") }, func(t *testing.T, r *rig) {
			if !r.has(r.col("payments/webhooks/folder.jsonc")) || !strings.Contains(r.read(r.col("payments/collection.jsonc")), "webhooks") {
				t.Error("the folder, or the order of its collection, is wrong")
			}
		}},
		{"a request in a folder opens its file", func(r *rig) {
			r.run(CmdNewRequest, map[string]any{"path": r.col("shop-api/users")})
			r.answer("Get user")
		}, func(t *testing.T, r *rig) {
			p := r.col("shop-api/users/get-user.request.jsonc")
			if !r.has(p) || !strings.Contains(r.read(p), `"method": "GET"`) {
				t.Fatal("the request file is missing or wrong")
			}
			if !strings.Contains(r.read(r.col("shop-api/users/folder.jsonc")), "get-user.request.jsonc") {
				t.Error("the order of the folder does not list it")
			}
			shown := r.api.ShownDocuments()
			if len(shown) != 1 || !strings.HasSuffix(shown[0].URI, "get-user.request.jsonc") {
				t.Errorf("shown = %+v", shown)
			}
		}},
		{"an environment opens its editor", func(r *rig) { r.run(CmdNewEnvironment, nil); r.answer("QA") }, func(t *testing.T, r *rig) {
			if !r.has(r.env("qa")) {
				t.Fatal("the environment file is missing")
			}
			tabs := r.api.EditorTabs()
			if len(tabs) != 1 || tabs[0].Kind() != EnvironmentKindID || tabs[0].Title() != "QA" {
				t.Errorf("tabs = %+v", tabs)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			tc.run(r)
			tc.check(t, r)
		})
	}
}

func TestAnEmptyNameIsNotAccepted(t *testing.T) {
	r := newRig(t)
	r.run(CmdNewCollection, nil)
	in := r.api.LastInput()
	if msg := in.Validate("   "); msg == "" {
		t.Error("an empty name passed")
	}
	if msg := in.Validate("Orders"); msg != "" {
		t.Errorf("a name was refused: %s", msg)
	}
	in.Cancel()
	r.settle()
	if len(r.ext.tree.Collections) != 2 {
		t.Error("a cancelled box made a collection")
	}
}

func TestRenameDuplicateCopyAndOpen(t *testing.T) {
	for _, tc := range []struct {
		name  string
		run   func(r *rig)
		check func(t *testing.T, r *rig)
	}{
		{"rename a request renames its files and the order", func(r *rig) {
			r.run(CmdRename, r.col("shop-api/users/create-user.request.jsonc"))
			if in := r.api.LastInput(); in.Value() != "Create user" {
				r.t.Errorf("the box starts with %q", in.Value())
			}
			r.answer("Add user")
		}, func(t *testing.T, r *rig) {
			if !r.has(r.col("shop-api/users/add-user.request.jsonc")) || !r.has(r.col("shop-api/users/add-user.body.json")) || r.has(r.col("shop-api/users/create-user.request.jsonc")) {
				t.Error("the files were not renamed")
			}
			if !strings.Contains(r.read(r.col("shop-api/users/folder.jsonc")), "add-user.request.jsonc") {
				t.Error("the order was not updated")
			}
		}},
		{"rename a collection", func(r *rig) { r.run(CmdRename, r.col("payments")); r.answer("Billing") }, func(t *testing.T, r *rig) {
			if !r.has(r.col("billing/collection.jsonc")) || r.has(r.col("payments")) {
				t.Error("the folder was not renamed")
			}
		}},
		{"duplicate a request", func(r *rig) { r.run(CmdDuplicate, r.col("payments/charge.request.jsonc")) }, func(t *testing.T, r *rig) {
			if len(r.node(r.col("payments")).Children) != 3 {
				t.Errorf("the collection has %d requests", len(r.node(r.col("payments")).Children))
			}
		}},
		{"duplicate a folder", func(r *rig) { r.run(CmdDuplicate, r.col("shop-api/users")) }, func(t *testing.T, r *rig) {
			if len(r.node(r.col("shop-api")).Children) != 3 {
				t.Errorf("shop-api has %d entries", len(r.node(r.col("shop-api")).Children))
			}
		}},
		{"duplicate an environment", func(r *rig) { r.run(CmdDuplicate, r.env("dev")) }, func(t *testing.T, r *rig) {
			if len(r.ext.tree.Environments) != 3 {
				t.Errorf("%d environments", len(r.ext.tree.Environments))
			}
		}},
		{"copy a name", func(r *rig) { r.run(CmdCopyName, r.col("shop-api/users/list-users.request.jsonc")) }, func(t *testing.T, r *rig) {
			if got := r.api.ClipboardTexts(); len(got) != 1 || got[0] != "List users" {
				t.Errorf("clipboard = %v", got)
			}
		}},
		{"open a request", func(r *rig) { r.run(CmdOpenRequest, r.col("payments/refund.request.jsonc")) }, func(t *testing.T, r *rig) {
			if shown := r.api.ShownDocuments(); len(shown) != 1 || !strings.HasSuffix(shown[0].URI, "payments/refund.request.jsonc") {
				t.Errorf("shown = %+v", shown)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			tc.run(r)
			tc.check(t, r)
		})
	}
}

func TestDeleteAsksThenTrashesWithTheBodyFilesAndCanBeUndone(t *testing.T) {
	r := newRig(t)
	r.run(CmdDelete, r.col("shop-api/users/create-user.request.jsonc"))
	d := r.api.OpenDialogs()
	if len(d) != 1 || !strings.Contains(d[0].Spec().Message, "Create user") {
		t.Fatalf("dialogs = %+v", d)
	}
	r.pressDialog("Cancel")
	if len(r.api.TrashedFiles()) != 0 {
		t.Fatal("a cancelled dialog deleted")
	}
	r.run(CmdDelete, r.col("shop-api/users/create-user.request.jsonc"))
	r.pressDialog("Delete")
	trashed := r.api.TrashedFiles()
	if len(trashed) != 1 || len(trashed[0].URIs) != 2 || !strings.HasSuffix(trashed[0].URIs[1], "create-user.body.json") {
		t.Fatalf("trashed = %+v", trashed)
	}
	notes := r.api.Notes()
	if len(notes) == 0 || !slices.Contains(notes[len(notes)-1].Actions, "Undo") {
		t.Fatalf("notes = %v", r.notes())
	}
	r.api.RunNoteAction(len(notes)-1, "Undo")
	r.settle()
	if !trashed[0].Undone() {
		t.Error("Undo did not take it back")
	}
}

func TestAFailingTrashIsReported(t *testing.T) {
	r := newRig(t)
	r.api.FailTrash(errFake)
	r.run(CmdDelete, r.col("payments/charge.request.jsonc"))
	r.pressDialog("Delete")
	if got := r.notes(); len(got) == 0 || !strings.HasPrefix(got[len(got)-1], "error: Cannot delete \"Charge\"") {
		t.Errorf("notes = %v", got)
	}
}

func TestCommandsWithoutAnArgumentUseTheSelectionOrAskWhichOne(t *testing.T) {
	r := newRig(t)
	// nothing is selected: a list of the requests is shown
	r.run(CmdOpenRequest, nil)
	p := r.api.LastPick()
	if p == nil || len(p.Labels()) != 7 {
		t.Fatalf("the list = %v", p)
	}
	r.choose("Health")
	if shown := r.api.ShownDocuments(); len(shown) != 1 || !strings.HasSuffix(shown[0].URI, "health.request.jsonc") {
		t.Errorf("shown = %+v", shown)
	}
	// a selected row is used without asking
	r.ext.collections.tree.SetSelected(r.col("payments/charge.request.jsonc"))
	picks := len(r.api.Picks())
	r.run(CmdRename, nil)
	if in := r.api.LastInput(); in == nil || in.Value() != "Charge" || len(r.api.Picks()) != picks {
		t.Errorf("input %+v", in)
	}
	// a command for one kind of node does not use a selection of another kind
	r.api.LastInput().Cancel()
	r.run(CmdEditEnvironment, nil)
	if got := r.api.LastPick().Labels(); len(got) != 2 {
		t.Errorf("environments to choose = %v", got)
	}
}

func TestAMissingNodeIsReported(t *testing.T) {
	r := newRig(t)
	r.run(CmdRename, r.col("payments/gone.request.jsonc"))
	if got := r.notes(); len(got) != 1 || !strings.Contains(got[0], "does not exist any more") {
		t.Errorf("notes = %v", got)
	}
}

func TestRefreshReadsTheFilesAgain(t *testing.T) {
	r := newRig(t)
	writeFile(t, r.col("payments/capture.request.jsonc"), `{"emetosHttp": 1, "name": "Capture", "method": "POST", "url": "x"}`)
	r.run(CmdRefresh, nil)
	if !r.ext.collections.tree.Has(r.col("payments/capture.request.jsonc")) {
		t.Error("the new request is not in the tree")
	}
}

func TestF2AndDeleteActOnTheSelectedRow(t *testing.T) {
	r := newRig(t)
	ts := r.show(uitest.Themes()[0], CollectionsViewID, 320, 300)
	// rows: Payments, Charge, Refund, Shop API, Health, Users; Health is row 4
	ts.TapAt(gui.Offset{Dx: 120, Dy: 4*22 + 11}, gluttertest.GestureOpts{Kind: gui.PointerDeviceKindMouse})
	ts.Pump(time.Millisecond)
	if n := r.ext.collections.selectedNode(); n == nil || n.Name != "Health" {
		t.Fatalf("selected = %+v", n)
	}
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyF2)
	ts.Pump(time.Millisecond)
	if in := r.api.LastInput(); in == nil || in.Value() != "Health" {
		t.Fatalf("F2 opened %+v", in)
	}
	r.api.LastInput().Cancel()
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyDelete)
	ts.Pump(time.Millisecond)
	if d := r.api.OpenDialogs(); len(d) != 1 || !strings.Contains(d[0].Spec().Message, "Health") {
		t.Errorf("Delete opened %+v", d)
	}
}

func TestMethodBadges(t *testing.T) {
	for method, want := range map[string]string{"GET": "GET", "post": "POST", "DELETE": "DEL", "OPTIONS": "OPT", "": "GET", "PROPFIND": "PROPF", "PATCH": "PATCH"} {
		if got := methodLabel(method); got != want {
			t.Errorf("methodLabel(%q) = %q, want %q", method, got, want)
		}
	}
	if methodColor("GET", colorPut) == methodColor("POST", colorPut) || methodColor("TRACE", colorPut) != colorPut {
		t.Error("the colors of the methods")
	}
	_ = format.Request{}
}
