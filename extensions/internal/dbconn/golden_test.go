package dbconn

import (
	"testing"

	"github.com/rm4n0s/errors"
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// goldenView makes a view with a connected connection opened down to its tables, one that is connecting, one that failed, one that
// has a color and is read-only, and one whose extension is not active.
func goldenView(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, pgDriver())
	r.env["PW"] = "x"
	conn := pgConn("Production")
	conn.PasswordEnv, conn.Color = "${env:PW}", "#d9534f"
	prod := r.save(conn, PasswordChange{})
	r.run(Connect, prod.ID)
	r.api.WaitFor("the connection", func() bool { return r.hub.State(prod.ID).Status == Connected })
	r.ext.view.tree.Expand(connKey(prod.ID))
	r.api.WaitFor("the databases", func() bool { return len(r.rows()) == 3 })
	r.ext.view.tree.Expand(childKey(connKey(prod.ID), "db:app"))
	r.api.WaitFor("the tables", func() bool { return len(r.rows()) == 5 })

	staging := pgConn("Staging (read-only)")
	staging.ReadOnly = true
	staging.Color = "#f0ad4e"
	st := r.save(staging, PasswordChange{})
	r.hub.setState(st.ID, State{Status: Connecting})

	broken := pgConn("Broken")
	broken.Host = "db.down.example.com"
	bk := r.save(broken, PasswordChange{})
	r.hub.Fail(bk.ID, errors.New("DriverFake", "connection refused"))

	other := Connection{Name: "Orders cache", Engine: "redis", Scope: ScopeUser, Host: "localhost", Port: 6379}
	if _, err := r.hub.Store().Save(other); err != nil {
		t.Fatal(err)
	}
	r.ext.view.rebuild()
	r.api.Pump()
	return r
}

func TestGoldenTheViewOfConnections(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := goldenView(t)
			ts := uitest.Pump(t, th, gui.Size{Width: 320, Height: 230}, viewWidget(r))
			uitest.MatchesGolden(ts, "view_connections_"+th.Name)
		})
	}
}

func TestGoldenTheEmptyView(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, pgDriver())
			ts := uitest.Pump(t, th, gui.Size{Width: 320, Height: 100}, viewWidget(r))
			uitest.MatchesGolden(ts, "view_empty_"+th.Name)
		})
	}
}

func TestGoldenTheViewWithNoEngine(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			ts := uitest.Pump(t, th, gui.Size{Width: 320, Height: 70}, viewWidget(r))
			uitest.MatchesGolden(ts, "view_noengine_"+th.Name)
		})
	}
}

func TestGoldenTheFormOfANewConnection(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, pgDriver())
			_, f := r.openNew()
			f.model.Name, f.model.Host, f.model.User, f.model.Database = "Production", "db.example.com", "app", "app"
			f.model.TLS, f.model.Color, f.model.SavePassword = TLSVerifyFull, "#d9534f", true
			ts := uitest.Pump(t, th, gui.Size{Width: 640, Height: 900}, formWidget(f))
			uitest.MatchesGolden(ts, "form_new_"+th.Name)
		})
	}
}

func TestGoldenTheFormOfAnEditedMongoConnection(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, &fakeDriver{engines: []Engine{mongoEngine}})
			c := Connection{ID: "m1", Name: "Atlas", Engine: "mongodb", Scope: ScopeUser, URL: "mongodb+srv://app@cluster0.example.net/shop",
				SavePassword: true, ReadOnly: true, Options: map[string]string{"authSource": "admin", "mechanism": "SCRAM-SHA-256"}}
			f := &formTab{e: r.ext, model: EditFormModel(mongoEngine, c, r.folder), inputs: map[string]*ui.InputBoxController{}}
			f.tab = r.api.RestoreTab(FormKindID, "x", mustJSON(t, formOpen{Draft: f.model.Draft()}))
			ts := uitest.Pump(t, th, gui.Size{Width: 640, Height: 980}, formWidget(f))
			uitest.MatchesGolden(ts, "form_edit_mongo_"+th.Name)
		})
	}
}

func TestGoldenTheFormWithProblems(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, pgDriver())
			_, f := r.openNew()
			f.model.Port, f.model.Color = "port", "red"
			f.showProblems = true
			f.note = pageui.Bad("A connection needs a name.")
			ts := uitest.Pump(t, th, gui.Size{Width: 640, Height: 900}, formWidget(f))
			uitest.MatchesGolden(ts, "form_problems_"+th.Name)
		})
	}
}
