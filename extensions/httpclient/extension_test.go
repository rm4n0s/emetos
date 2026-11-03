package httpclient

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
)

func TestTheManifestIsValid(t *testing.T) {
	m := (&Extension{}).Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.ViewContainers) != 1 || m.Contributes.ViewContainers[0].Icon != "globe" {
		t.Errorf("containers = %+v", m.Contributes.ViewContainers)
	}
	var views []string
	for _, v := range m.Contributes.Views {
		if v.Container != ContainerID {
			t.Errorf("the view %s is in %s", v.ID, v.Container)
		}
		views = append(views, v.Name)
	}
	if strings.Join(views, ",") != "Collections,Environments,History" {
		t.Errorf("views = %v", views)
	}
}

func TestEveryCommandOfTheManifestHasAHandler(t *testing.T) {
	r := newRig(t)
	for _, c := range r.ext.Manifest().Contributes.Commands {
		if !r.api.HasHandler(c.ID) {
			t.Errorf("%s has no handler", c.ID)
		}
		if c.Category != "HTTP Client" || !strings.HasPrefix(c.ID, ID+".") {
			t.Errorf("command %+v", c)
		}
	}
}

// The settings are those of the SPEC under httpClient.*, with their defaults; the ones that decide where requests go and what they trust
// are the user's alone.
func TestTheSettings(t *testing.T) {
	r := newRig(t)
	for _, tc := range []struct {
		key   string
		want  any
		scope string
	}{
		{SettingTimeoutMs, float64(30000), ""},
		{SettingVerifyTLS, true, "machine"},
		{SettingProxy, "", "machine"},
		{SettingClientCerts, []any{}, "machine"},
		{SettingHistoryLimit, float64(200), ""},
		{SettingResponseLayout, "vertical", ""},
		{SettingAllowedProcessEnv, []any{}, "machine"},
		{SettingMaxResponseMiB, float64(50), "machine"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			got, err := r.api.Settings().Get(tc.key)
			if err != nil {
				t.Fatal(err)
			}
			if gs, ok := got.([]any); ok {
				if len(gs) != 0 {
					t.Errorf("default = %v", got)
				}
			} else if got != tc.want {
				t.Errorf("default = %v, want %v", got, tc.want)
			}
			for _, s := range r.ext.Manifest().Contributes.Settings {
				if s.Key == tc.key && s.Scope != tc.scope {
					t.Errorf("scope = %q, want %q", s.Scope, tc.scope)
				}
			}
		})
	}
}

func TestTheTreeIsReadInTheBackground(t *testing.T) {
	r := newRig(t)
	if len(r.ext.tree.Collections) != 2 || len(r.ext.tree.Environments) != 2 {
		t.Fatalf("collections %d, environments %d", len(r.ext.tree.Collections), len(r.ext.tree.Environments))
	}
	// the Collections view lists them, in the order of the files: Payments, Shop API (by folder name), the requests in order
	rows := r.ext.collections.tree.Rows()
	var labels []string
	for _, row := range rows {
		labels = append(labels, row.Label)
	}
	want := "Payments,Charge,Refund,Shop API,Health,Users"
	if got := strings.Join(labels, ","); got != want {
		t.Errorf("rows = %s, want %s", got, want)
	}
}

func TestWithoutAFolderTheViewsSayWhatToDo(t *testing.T) {
	isolate(t)
	r := activate(t, "")
	if r.ext.store != nil {
		t.Error("a store was opened without a folder")
	}
	r.run(CmdNewCollection, nil)
	if got := r.notes(); len(got) != 1 || !strings.Contains(got[0], "Open a folder") {
		t.Errorf("notes = %v", got)
	}
	if r.api.LastInput() != nil {
		t.Error("a name was asked for without a folder")
	}
	if r.ext.status == nil {
		t.Fatal("no status item")
	}
	if it := r.api.StatusItem(StatusEnvironmentID); it == nil || !it.Hidden {
		t.Errorf("status item = %+v", it)
	}
}

func TestAFolderWithoutTheClientHasNoStatusItem(t *testing.T) {
	isolate(t)
	r := activate(t, t.TempDir())
	if it := r.api.StatusItem(StatusEnvironmentID); it == nil || !it.Hidden {
		t.Errorf("status item = %+v", it)
	}
	if len(r.ext.tree.Collections) != 0 {
		t.Error("collections out of nothing")
	}
}

func TestABrokenRequestFileIsShownWithItsError(t *testing.T) {
	r := newRig(t, func(folder string) {
		writeFile(t, folder+"/.emetos/http/collections/payments/broken.request.jsonc", `{"emetosHttp": 1, "name": `)
		writeFile(t, folder+"/.emetos/http/collections/payments/newer.request.jsonc", `{"emetosHttp": 9, "name": "From the future", "method": "GET", "url": "x"}`)
	})
	var broken, newer *store.Node
	r.ext.tree.Walk(func(n *store.Node) bool {
		switch n.Key {
		case "broken.request.jsonc":
			broken = n
		case "newer.request.jsonc":
			newer = n
		}
		return true
	})
	if broken == nil || broken.Err == nil || newer == nil || !newer.ReadOnly {
		t.Fatalf("broken %+v, newer %+v", broken, newer)
	}
	// they are rows of the tree; renaming the read-only one is refused with a message
	if !r.ext.collections.tree.Has(broken.Path) || !r.ext.collections.tree.Has(newer.Path) {
		t.Error("the rows are missing")
	}
	r.run(CmdRename, newer.Path)
	if r.api.LastInput() != nil {
		t.Error("a read-only request was offered a new name")
	}
	if got := r.notes(); len(got) == 0 || !strings.Contains(got[len(got)-1], "read-only") {
		t.Errorf("notes = %v", got)
	}
}
