package dbconn

import (
	"context"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn/secrettest"
)

// The keyring is looked for on a private bus: nobody owns the name, then a fake Secret Service does. The check opens no session with it.
func TestTheRequirementOfTheKeyring(t *testing.T) {
	m := (&Extension{}).Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Requirements) != 1 || m.Requirements[0].ID != RequirementKeyring || !m.Requirements[0].Optional {
		t.Fatalf("requirements %+v", m.Requirements)
	}
	bus := secrettest.StartBus(t)
	ss := &SecretService{Dial: bus.Dial}
	r := KeyringRequirement("keeps passwords", ss)
	got := r.Check(context.Background(), extension.RequirementEnv{})
	if got.State != extension.RequirementMissing || got.Detail != "no Secret Service is on the session bus, so secrets cannot be saved: they are kept in memory until Emetos quits" {
		t.Errorf("no service: %+v", got)
	}
	svc := secrettest.StartService(t, bus, secrettest.Options{})
	got = r.Check(context.Background(), extension.RequirementEnv{})
	if got.State != extension.RequirementFound || got.Detail != "a Secret Service runs on the session bus" {
		t.Errorf("a service: %+v", got)
	}
	if calls := svc.Calls(); len(calls) != 0 {
		t.Errorf("the check called the service: %v", calls)
	}
	// without a Dial, a test never reaches the user's bus
	if got := KeyringRequirement("p", NewSecretService()).Check(context.Background(), extension.RequirementEnv{}); got.State != extension.RequirementUnknown {
		t.Errorf("the default under test: %+v", got)
	}
	failing := &SecretService{Dial: func(context.Context) (*dbus.Conn, error) { return nil, context.DeadlineExceeded }}
	if got := failing.Available(context.Background()); got.State != extension.RequirementMissing {
		t.Errorf("no bus: %+v", got)
	}
}
