//go:build emetos_docker || emetos_podman

package extensions

// The body of the tests of Connect to Database against a real engine (SPEC §10.24, step 22.1), shared by the Docker test
// (connectdb_docker_test.go, build tag emetos_docker) and the Podman test (connectdb_podman_test.go, emetos_podman). They keep the
// rules of the container tests (CLAUDE.md "Testing notes"): the server is a container named emetos-test-<random> with the label
// dev.emetos.test=1 of a pinned image, published on 127.0.0.1 only and removed when the test ends; the extension lists only what
// carries the label, so the user's own containers are never listed, inspected or touched; the password of the server is random for
// the run and is given to the form the way the user types it.

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/emetos/extensions/internal/containerui"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/dbtest"
)

// connectToARealPostgres runs a labelled postgres container on the engine of the harness, uses Connect to Database of the container
// extension on it, and checks that the form that opens is prefilled with where the server is and the user and database of the image,
// then (with the password typed, as the user does) saves the connection and connects through the SQL extension.
func connectToARealPostgres(t *testing.T, h *containerapitest.Harness, cfg func(containerui.DialFunc, containerui.ListFilters) containerui.Config) {
	t.Helper()
	password := dbtest.RandomPassword(t)
	srv := dbtest.OnHarness(h).Start(t, dbtest.Spec{
		Image: containerapitest.ImagePostgres, Port: 5432,
		Env: map[string]string{"POSTGRES_USER": "app", "POSTGRES_DB": "orders", "POSTGRES_PASSWORD": password},
	})
	// the extension lists the containers of the tests only
	r := startConnectRig(t, cfg(fixedEngine(h.Client), containerui.ListFilters{Containers: containerapitest.LabelFilter()}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	listed, err := h.Client.ListContainers(ctx, containerapi.ListContainersOptions{All: true, Filters: containerapitest.LabelFilter()})
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, c := range listed {
		if c.Name() == srv.Name {
			id = c.ID
		}
	}
	if id == "" {
		t.Fatalf("the container %s is not in the list of the labelled containers", srv.Name)
	}

	r.connectDatabase(id)

	if len(r.asked["emetos.sql"]) != 1 || len(r.asked["emetos.mongodb"]) != 0 || len(r.asked["emetos.redis"]) != 0 {
		t.Fatalf("the requests for the form: %+v", r.asked)
	}
	draft, ok := r.openedForm()
	if !ok {
		t.Fatal("no form was opened")
	}
	if draft.Engine != "postgres" || draft.Name != srv.Name || draft.Host != srv.Host || draft.Port != strconv.Itoa(srv.Port) || draft.User != "app" || draft.Database != "orders" {
		t.Fatalf("the form = %+v, want postgres %s at %s:%d as app on orders", draft, srv.Name, srv.Host, srv.Port)
	}
	if draft.SavePassword || draft.PasswordEnv != "" {
		t.Errorf("the form asks for a password to be kept: %+v", draft)
	}

	// the user types the password and presses Save, then Connect
	var eng dbconn.Engine
	for _, e := range r.hub.Engines() {
		if e.ID == draft.Engine {
			eng = e
		}
	}
	form := dbconn.FormFromDraft(eng, draft)
	form.SetPassword(password)
	if problems := form.Problems(); len(problems) != 0 {
		t.Fatalf("the prefilled form has problems: %+v", problems)
	}
	saved, _, err := r.hub.Save(ctx, form.Connection(), form.PasswordChange())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.hub.Disconnect(saved.ID); err != nil {
			t.Errorf("closing the connection: %v", err)
		}
	})
	var connectErr error
	for deadline := time.Now().Add(3 * time.Minute); ; time.Sleep(time.Second) {
		p, err := r.hub.Prepare(ctx, saved.ID)
		if err != nil {
			t.Fatal(err)
		}
		if p.Ask {
			t.Fatal("the hub asks for the password it was given")
		}
		if connectErr = r.hub.Connect(ctx, p, false); connectErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server did not come up: %v", connectErr)
		}
	}
	if st := r.hub.State(saved.ID); st.Status != dbconn.Connected {
		t.Fatalf("state = %+v", st)
	}
	db, err := r.sql.Service().DB(ctx, saved.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var user, database string
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(time.Second) {
		// the image starts a server for its set-up and then restarts it: the first answers may be dropped
		if err = db.QueryRowContext(ctx, "SELECT current_user, current_database()").Scan(&user, &database); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the query on the connection made by Connect to Database: %v", err)
		}
	}
	if user != "app" || database != "orders" {
		t.Errorf("connected as %s to %s, want app to orders", user, database)
	}
}
