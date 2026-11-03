package httpclient

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

// refreshTab builds the widget of the tab again when the tab asks for it.
type refreshTab struct {
	extension.EditorTab
	changed *foundation.ChangeNotifier
}

// Refresh implements extension.EditorTab.
func (t refreshTab) Refresh() {
	t.EditorTab.Refresh()
	t.changed.NotifyListeners()
}

// showEnv shows the editor tab of an environment in a window.
func (r *rig) showEnv(th uitest.Theme, t *envTab, w, h float64) *gluttertest.Tester {
	r.t.Helper()
	changed := foundation.NewChangeNotifier()
	t.tab = refreshTab{t.tab, changed}
	body := widgets.NewListenableBuilder(changed, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget { return t.build(bc) }, nil)
	return uitest.Pump(r.t, th, gui.Size{Width: w, Height: h}, body)
}

// openEnv opens the editor of an environment and shows it.
func (r *rig) openEnv(key string) (*envTab, *gluttertest.Tester) {
	r.t.Helper()
	r.run(CmdEditEnvironment, r.env(key))
	t := r.ext.envTabs[len(r.ext.envTabs)-1]
	return t, r.showEnv(uitest.Themes()[0], t, 900, 420)
}

func fakeTab(r *rig, t *envTab) interface {
	Dirty() bool
	Title() string
} {
	for _, tab := range r.api.EditorTabs() {
		if tab.ID() == t.tab.ID() {
			return tab
		}
	}
	r.t.Fatal("no such tab")
	return nil
}

func TestTheEditorShowsTheVariablesOfTheFile(t *testing.T) {
	r := newRig(t)
	tab, ts := r.openEnv("dev")
	if len(tab.rows) != 3 || tab.title.Value() != "Dev" {
		t.Fatalf("rows %d, title %q", len(tab.rows), tab.title.Value())
	}
	row := func(i int) (string, string, bool, bool) {
		x := tab.rows[i]
		return x.name.Value(), x.initial.Value(), x.secret, x.enabled
	}
	if n, v, s, e := row(0); n != "baseUrl" || v != "http://localhost:8080" || s || !e {
		t.Errorf("row 0 = %q %q %v %v", n, v, s, e)
	}
	if n, v, s, e := row(1); n != "apiKey" || v != "" || !s || !e {
		t.Errorf("row 1 = %q %q %v %v", n, v, s, e)
	}
	if n, _, _, e := row(2); n != "debug" || e {
		t.Errorf("row 2 = %q enabled %v", n, e)
	}
	for _, want := range []string{"Variable", "Initial value", "Current value", "Secret", "Add Variable", "Save", "Revert"} {
		if !ts.Exists(gluttertest.ByTextContaining(want)) {
			t.Errorf("the editor has no %q", want)
		}
	}
	if fakeTab(r, tab).Dirty() {
		t.Error("a tab that was only opened is dirty")
	}
}

func TestEditingMakesTheTabDirtyAndRevertingCleansIt(t *testing.T) {
	r := newRig(t)
	tab, ts := r.openEnv("dev")
	tab.rows[0].initial.SetValue("http://localhost:9999")
	ts.Pump(time.Millisecond)
	if !fakeTab(r, tab).Dirty() {
		t.Fatal("the tab is not dirty after an edit")
	}
	tab.rows[0].initial.SetValue("http://localhost:8080")
	ts.Pump(time.Millisecond)
	if fakeTab(r, tab).Dirty() {
		t.Error("the tab is dirty after the edit was undone")
	}
	tab.rows[0].initial.SetValue("x")
	tab.addRow()
	ts.Pump(time.Millisecond)
	if len(tab.rows) != 4 || !fakeTab(r, tab).Dirty() {
		t.Fatalf("rows %d", len(tab.rows))
	}
	tab.revert()
	ts.Pump(time.Millisecond)
	if len(tab.rows) != 3 || tab.rows[0].initial.Value() != "http://localhost:8080" || fakeTab(r, tab).Dirty() {
		t.Errorf("after Revert: rows %d, %q", len(tab.rows), tab.rows[0].initial.Value())
	}
}

func TestSavingWritesTheFileTheCurrentValuesAndTheSecrets(t *testing.T) {
	r := newRig(t)
	tab, ts := r.openEnv("dev")
	tab.rows[0].initial.SetValue("http://localhost:9000") // initial value: in the file
	tab.rows[0].current.SetValue("http://127.0.0.1:7000") // current value: in the state
	tab.rows[1].current.SetValue("s3cr3t-key-value")      // secret: in the keyring
	tab.rows[2].enabled = true                            // enabled
	tab.addRow()
	tab.rows[3].name.SetValue("token")
	tab.rows[3].initial.SetValue("abc")
	ts.Pump(time.Millisecond)
	tab.save()
	r.settle()
	ts.Pump(time.Millisecond)

	text := r.read(r.env("dev"))
	for _, want := range []string{`"value": "http://localhost:9000"`, `"name": "token"`, `"secret": true`} {
		if !strings.Contains(text, want) {
			t.Errorf("the file lacks %s:\n%s", want, text)
		}
	}
	for _, never := range []string{"s3cr3t-key-value", "127.0.0.1:7000"} {
		if strings.Contains(text, never) {
			t.Errorf("the file holds %q:\n%s", never, text)
		}
	}
	owner := variables.EnvironmentOwner("dev")
	if v, ok := r.ext.values.Get(owner, "baseUrl"); !ok || v != "http://127.0.0.1:7000" {
		t.Errorf("current value = %q, %v", v, ok)
	}
	if v, found, err := r.ext.values.Secret(context.Background(), owner, "apiKey"); err != nil || !found || v != "s3cr3t-key-value" {
		t.Errorf("secret = %q, %v, %v", v, found, err)
	}
	if r.kr.Len() != 1 {
		t.Errorf("the keyring holds %d secrets", r.kr.Len())
	}
	if raw, _ := r.api.State().Get(variables.StateKey); strings.Contains(string(raw), "s3cr3t-key-value") {
		t.Error("the secret is in the state")
	}
	if fakeTab(r, tab).Dirty() || tab.rows[1].current.Value() != "" {
		t.Errorf("after saving: dirty %v, the secret box holds %q", fakeTab(r, tab).Dirty(), tab.rows[1].current.Value())
	}
	if !strings.Contains(tab.note.Text, "Saved") {
		t.Errorf("note = %+v", tab.note)
	}
}

func TestResetAndPersistAndRemove(t *testing.T) {
	r := newRig(t)
	owner := variables.EnvironmentOwner("dev")
	if err := r.ext.values.Set(context.Background(), owner, "baseUrl", "http://127.0.0.1:1", false); err != nil {
		t.Fatal(err)
	}
	tab, ts := r.openEnv("dev")
	if tab.rows[0].current.Value() != "http://127.0.0.1:1" {
		t.Fatalf("the current value is %q", tab.rows[0].current.Value())
	}
	// Persist makes the current value the initial value
	tab.persist(tab.rows[0])
	ts.Pump(time.Millisecond)
	if tab.rows[0].initial.Value() != "http://127.0.0.1:1" || tab.rows[0].current.Value() != "" {
		t.Errorf("after Persist: %q / %q", tab.rows[0].initial.Value(), tab.rows[0].current.Value())
	}
	// a secret is not persisted
	tab.rows[1].current.SetValue("x")
	tab.persist(tab.rows[1])
	if tab.rows[1].initial.Value() != "" {
		t.Error("a secret was written to the initial value")
	}
	tab.removeRow(tab.rows[2])
	tab.save()
	r.settle()
	text := r.read(r.env("dev"))
	if !strings.Contains(text, "http://127.0.0.1:1") || strings.Contains(text, `"debug"`) {
		t.Errorf("file:\n%s", text)
	}
	// saving with an empty current value drops the one that was kept
	if _, ok := r.ext.values.Get(owner, "baseUrl"); ok {
		t.Error("the current value stayed after Persist")
	}
}

func TestSavingRefusesWhatIsWrong(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(tab *envTab)
		want string
	}{
		{"an empty name", func(tab *envTab) { tab.title.SetValue("  ") }, "needs a name"},
		{"a variable twice", func(tab *envTab) { tab.rows[2].name.SetValue("baseUrl") }, "twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			tab, _ := r.openEnv("dev")
			before := r.read(r.env("dev"))
			tc.edit(tab)
			tab.save()
			r.settle()
			if !tab.note.Bad || !strings.Contains(tab.note.Text, tc.want) {
				t.Errorf("note = %+v", tab.note)
			}
			if r.read(r.env("dev")) != before {
				t.Error("the file was written")
			}
		})
	}
}

func TestRenamingInTheEditorRenamesTheFile(t *testing.T) {
	r := newRig(t)
	r.run(CmdSelectEnvironment, "dev")
	tab, _ := r.openEnv("dev")
	tab.title.SetValue("Local")
	tab.save()
	r.settle()
	if !r.has(r.env("local")) || r.has(r.env("dev")) {
		t.Fatal("the file was not renamed")
	}
	if r.active() != "local" || tab.path != r.env("local") || fakeTab(r, tab).Title() != "Local" {
		t.Errorf("active %q, path %q, title %q", r.active(), tab.path, fakeTab(r, tab).Title())
	}
}

func TestAFailingSaveIsShownInTheTab(t *testing.T) {
	r := newRig(t)
	tab, _ := r.openEnv("dev")
	tab.rows[1].current.SetValue("a-secret")
	r.kr.Err = errFake
	tab.save()
	r.settle()
	if !tab.note.Bad || !strings.Contains(tab.note.Text, "Cannot save") {
		t.Errorf("note = %+v", tab.note)
	}
	if tab.busy {
		t.Error("the tab stays busy")
	}
}

func TestADirtyEditorKeepsItsEditsWhenTheFileChanges(t *testing.T) {
	r := newRig(t)
	tab, _ := r.openEnv("staging")
	tab.rows[0].initial.SetValue("https://mine.example.com")
	writeFile(t, r.env("staging"), `{"emetosHttp": 1, "name": "Staging", "variables": [{"name": "baseUrl", "value": "https://theirs.example.com"}]}`)
	r.run(CmdRefresh, nil)
	if tab.rows[0].initial.Value() != "https://mine.example.com" {
		t.Errorf("the edit was lost: %q", tab.rows[0].initial.Value())
	}
	// a clean editor follows the file
	tab.revert()
	writeFile(t, r.env("staging"), `{"emetosHttp": 1, "name": "Staging", "variables": [{"name": "baseUrl", "value": "https://theirs2.example.com"}]}`)
	r.run(CmdRefresh, nil)
	if tab.rows[0].initial.Value() != "https://theirs2.example.com" {
		t.Errorf("a clean editor did not follow the file: %q", tab.rows[0].initial.Value())
	}
}

func TestATabComesBackAfterARestartAndSaysWhenItsFileIsGone(t *testing.T) {
	r := newRig(t)
	tab, _ := r.openEnv("dev")
	state := r.api.EditorTabs()[0].State()
	if !strings.Contains(string(state), "dev.jsonc") {
		t.Fatalf("state = %s", state)
	}
	tab.tab.Close()
	if len(r.ext.envTabs) != 0 {
		t.Error("a closed tab is still followed")
	}
	restored := r.api.RestoreTab(EnvironmentKindID, "restored", state)
	if restored == nil || restored.Title() != "Dev" {
		t.Fatalf("restored = %+v", restored)
	}
	// a file that is gone
	r.api.RestoreTab(EnvironmentKindID, "gone", []byte(`{"path": "`+r.env("nope")+`"}`))
	r.api.Pump()
	var gone *envTab
	for _, x := range r.ext.envTabs {
		if strings.HasSuffix(x.path, "nope.jsonc") {
			gone = x
		}
	}
	if gone == nil || !gone.gone {
		t.Fatalf("gone = %+v", gone)
	}
	ts := r.showEnv(uitest.Themes()[0], gone, 600, 200)
	if !ts.Exists(gluttertest.ByTextContaining("deleted or moved")) {
		t.Error("the tab does not say its file is gone")
	}
}

func TestATabWithoutItsStateIsRefused(t *testing.T) {
	r := newRig(t)
	for _, tc := range []struct{ state, route string }{
		{"", "Extension.createEnvTab.HttpEnvironmentTabStateMissing"},
		{"[1]", "Extension.createEnvTab.HttpEnvironmentTabStateInvalid"},
	} {
		_, err := r.ext.createEnvTab(nil, []byte(tc.state))
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("state %q: expected *errors.Error, got %T", tc.state, err)
		}
		if !appErr.HasRoute(tc.route) {
			t.Errorf("state %q: unexpected failure path: %s", tc.state, appErr.Route())
		}
	}
}

func TestANewerEnvironmentIsReadOnly(t *testing.T) {
	r := newRig(t, func(folder string) {
		writeFile(t, folder+"/.emetos/http/environments/future.jsonc", `{"emetosHttp": 9, "name": "Future", "variables": [{"name": "a", "value": "1"}]}`)
	})
	tab, ts := r.openEnv("future")
	if !tab.readOnly || len(tab.rows) != 1 {
		t.Fatalf("readOnly %v rows %d", tab.readOnly, len(tab.rows))
	}
	before := r.read(r.env("future"))
	tab.save()
	tab.addRow()
	r.settle()
	if r.read(r.env("future")) != before || len(tab.rows) != 1 {
		t.Error("a read-only environment changed")
	}
	if !ts.Exists(gluttertest.ByTextContaining("newer version")) {
		t.Error("no notice")
	}
}
