package dbconn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn/secrettest"
)

// ssRig is a Secret Service client against a fake service on a private bus. No test here talks to the bus or the keyring of the
// user: the SecretService has a Dial to the private bus, and the default Dial refuses under `go test`.
type ssRig struct {
	bus *secrettest.Bus
	svc *secrettest.Service
	ss  *SecretService
}

func newSSRig(t *testing.T, opts secrettest.Options, tune ...func(*SecretService)) *ssRig {
	t.Helper()
	bus := secrettest.StartBus(t)
	svc := secrettest.StartService(t, bus, opts)
	ss := &SecretService{Dial: bus.Dial, PromptTimeout: 5 * time.Second, NoEncryption: opts.NoEncryption}
	for _, f := range tune {
		f(ss)
	}
	return &ssRig{bus: bus, svc: svc, ss: ss}
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestSecretServiceStoresLooksUpAndDeletes(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts secrettest.Options
		wire string // what the service must have seen
	}{
		{"with the key exchange", secrettest.Options{}, "dh-ietf1024-sha256-aes128-cbc-pkcs7"},
		{"with a plain session", secrettest.Options{NoEncryption: true}, "plain"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newSSRig(t, tt.opts)
			c := ctx(t)
			const pw = "pässword with spaces & ünïcode 日本"
			if err := r.ss.Store(c, "conn1", "Emetos: Local", pw); err != nil {
				t.Fatal(err)
			}
			items := r.svc.Items()
			if len(items) != 1 || items[0].Secret != pw || items[0].Label != "Emetos: Local" {
				t.Fatalf("the service holds %+v", items)
			}
			wantAttrs := map[string]string{"application": "emetos", "kind": "database-connection", "connection": "conn1"}
			for k, v := range wantAttrs {
				if items[0].Attributes[k] != v {
					t.Errorf("attribute %s = %q, want %q", k, items[0].Attributes[k], v)
				}
			}
			got, found, err := r.ss.Lookup(c, "conn1")
			if err != nil || !found || got != pw {
				t.Fatalf("Lookup = %q, %v, %v", got, found, err)
			}
			if _, found, err := r.ss.Lookup(c, "other"); err != nil || found {
				t.Errorf("Lookup of another connection = found %v, %v", found, err)
			}
			// a second Store replaces the secret
			if err := r.ss.Store(c, "conn1", "Emetos: Local", "second"); err != nil {
				t.Fatal(err)
			}
			if items := r.svc.Items(); len(items) != 1 || items[0].Secret != "second" {
				t.Fatalf("after the second Store the service holds %+v", items)
			}
			if err := r.ss.Delete(c, "conn1"); err != nil {
				t.Fatal(err)
			}
			if len(r.svc.Items()) != 0 {
				t.Error("the item is still there")
			}
			if _, found, err := r.ss.Lookup(c, "conn1"); err != nil || found {
				t.Errorf("Lookup after Delete = found %v, %v", found, err)
			}
			if err := r.ss.Delete(c, "conn1"); err != nil {
				t.Errorf("Delete of nothing = %v", err)
			}
			if !contains(r.svc.Calls(), "OpenSession("+tt.wire+")") {
				t.Errorf("the session was not opened with %s: %v", tt.wire, r.svc.Calls())
			}
			if tt.wire != "plain" && r.svc.SawPlainOnTheWire() {
				t.Error("a secret crossed the bus in plain text although the service has the key exchange")
			}
		})
	}
}

func TestSecretServiceFallsBackToPlainWhenTheServiceRefusesTheExchange(t *testing.T) {
	r := newSSRig(t, secrettest.Options{NoEncryption: true}, func(s *SecretService) { s.NoEncryption = false })
	c := ctx(t)
	if err := r.ss.Store(c, "c", "L", "pw"); err != nil {
		t.Fatal(err)
	}
	if got, found, err := r.ss.Lookup(c, "c"); err != nil || !found || got != "pw" {
		t.Fatalf("Lookup = %q, %v, %v", got, found, err)
	}
	calls := r.svc.Calls()
	if !contains(calls, "OpenSession(dh-ietf1024-sha256-aes128-cbc-pkcs7)") || !contains(calls, "OpenSession(plain)") {
		t.Errorf("the client did not try the exchange and then fall back: %v", calls)
	}
}

func TestSecretServiceFindsASecretAnotherProgramPut(t *testing.T) {
	r := newSSRig(t, secrettest.Options{})
	r.svc.Put("Emetos: Old", map[string]string{"application": "emetos", "kind": "database-connection", "connection": "old"}, "from-before")
	got, found, err := r.ss.Lookup(ctx(t), "old")
	if err != nil || !found || got != "from-before" {
		t.Fatalf("Lookup = %q, %v, %v", got, found, err)
	}
}

func TestALockedKeyringIsUnlockedThroughThePrompt(t *testing.T) {
	r := newSSRig(t, secrettest.Options{Locked: true})
	c := ctx(t)
	if err := r.ss.Store(c, "c", "L", "pw"); err != nil {
		t.Fatal(err)
	}
	if r.svc.Locked() {
		t.Error("the keyring is still locked")
	}
	calls := r.svc.Calls()
	if !contains(calls, "Unlock") || !contains(calls, "Prompt") {
		t.Errorf("the client did not unlock through the prompt: %v", calls)
	}
	// locked again: Lookup unlocks the items it found
	r2 := newSSRig(t, secrettest.Options{Locked: true})
	r2.svc.Put("L", map[string]string{"application": "emetos", "kind": "database-connection", "connection": "c"}, "pw2")
	got, found, err := r2.ss.Lookup(c, "c")
	if err != nil || !found || got != "pw2" {
		t.Fatalf("Lookup in a locked keyring = %q, %v, %v", got, found, err)
	}
	// and so does Delete
	r3 := newSSRig(t, secrettest.Options{Locked: true})
	r3.svc.Put("L", map[string]string{"application": "emetos", "kind": "database-connection", "connection": "c"}, "pw3")
	if err := r3.ss.Delete(c, "c"); err != nil {
		t.Fatal(err)
	}
	if len(r3.svc.Items()) != 0 {
		t.Error("Delete in a locked keyring left the item")
	}
}

func TestADismissedPromptLeavesTheKeyringLocked(t *testing.T) {
	r := newSSRig(t, secrettest.Options{Locked: true, Dismiss: true})
	c := ctx(t)
	wantRoute(t, r.ss.Store(c, "c", "L", "pw"), "ssConn.unlock.KeyringLocked")
	r.svc.Put("L", map[string]string{"application": "emetos", "kind": "database-connection", "connection": "c"}, "pw")
	_, _, err := r.ss.Lookup(c, "c")
	wantRoute(t, err, "ssConn.unlock.KeyringLocked")
	wantRoute(t, r.ss.Delete(c, "c"), "ssConn.unlock.KeyringLocked")
	if !r.svc.Locked() {
		t.Error("a dismissed prompt unlocked the keyring")
	}
}

func TestAPromptNobodyAnswersEnds(t *testing.T) {
	r := newSSRig(t, secrettest.Options{Locked: true, NeverAnswerPrompt: true}, func(s *SecretService) { s.PromptTimeout = 200 * time.Millisecond })
	start := time.Now()
	wantRoute(t, r.ss.Store(ctx(t), "c", "L", "pw"), "ssConn.runPrompt.KeyringPromptFailed")
	if time.Since(start) > 10*time.Second {
		t.Error("the wait for the prompt took far longer than its timeout")
	}
	if !contains(r.svc.Calls(), "Prompt.Dismiss") {
		t.Error("the client did not ask the service to take the prompt away")
	}
}

func TestACancelledContextEndsTheCall(t *testing.T) {
	r := newSSRig(t, secrettest.Options{Locked: true, NeverAnswerPrompt: true})
	c, cancel := context.WithCancel(ctx(t))
	time.AfterFunc(200*time.Millisecond, cancel)
	err := r.ss.Store(c, "c", "L", "pw")
	if err == nil {
		t.Fatal("Store succeeded")
	}
}

func TestADesktopWithNoDefaultKeyring(t *testing.T) {
	r := newSSRig(t, secrettest.Options{NoCollection: true})
	wantRoute(t, r.ss.Store(ctx(t), "c", "L", "pw"), "ssConn.defaultCollection.KeyringNoCollection")
}

func TestNoBusToTalkTo(t *testing.T) {
	missing := &SecretService{Dial: func(context.Context) (*dbus.Conn, error) {
		return dbus.Connect("unix:path=/nonexistent/emetos-test-bus")
	}}
	c := ctx(t)
	wantRoute(t, missing.Store(c, "c", "L", "pw"), "SecretService.openConn.KeyringBusUnavailable")
	_, _, err := missing.Lookup(c, "c")
	wantRoute(t, err, "SecretService.openConn.KeyringBusUnavailable")
	wantRoute(t, missing.Delete(c, "c"), "SecretService.openConn.KeyringBusUnavailable")
}

func TestATestCannotReachTheSessionBusOfTheUser(t *testing.T) {
	// the default Dial refuses under `go test`, whatever DBUS_SESSION_BUS_ADDRESS says
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/should-never-be-dialed")
	ss := NewSecretService()
	wantRoute(t, ss.Store(ctx(t), "c", "L", "pw"), "SecretService.openConn.KeyringTestBusRefused")
}

func TestNoServiceOnTheBus(t *testing.T) {
	bus := secrettest.StartBus(t) // a bus with nobody owning org.freedesktop.secrets
	ss := &SecretService{Dial: bus.Dial}
	wantRoute(t, ss.Store(ctx(t), "c", "L", "pw"), "ssConn.call.KeyringServiceFailed")
}

func TestKeyringThroughPasswords(t *testing.T) {
	r := newSSRig(t, secrettest.Options{})
	pw := NewPasswords(r.ss, func(string) (string, bool) { return "", false })
	c := Connection{ID: "id1", Name: "Prod", SavePassword: true}
	c2 := ctx(t)
	got := pw.Save(c2, c, "s3")
	if got.In != SourceKeyring || got.KeyringErr != nil {
		t.Fatalf("Save = %+v", got)
	}
	if items := r.svc.Items(); len(items) != 1 || items[0].Label != "Emetos: Prod" || !strings.Contains(items[0].Attributes["connection"], "id1") {
		t.Errorf("the keyring holds %+v", items)
	}
	res, err := pw.Resolve(c2, c)
	if err != nil || res.Password != "s3" || res.Source != SourceKeyring {
		t.Fatalf("Resolve = %+v, %v", res, err)
	}
	if err := pw.Delete(c2, "id1"); err != nil {
		t.Fatal(err)
	}
	if len(r.svc.Items()) != 0 {
		t.Error("Delete left the secret in the keyring")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
