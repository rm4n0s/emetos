//go:build emetos_keyring

package dbconn

import (
	"context"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// TestRealKeyring is the manual check of step 19.1 (docs/MANUAL-CHECKS.md): it talks to the keyring of the desktop session that runs
// it, through the Secret Service on the session bus. It is never part of the checks of a step: it needs the build tag, and the
// default SecretService refuses to dial under `go test`, so this test gives it a Dial of its own. It stores a secret under a label and
// an attribute of its own, reads it back, replaces it, deletes it, and shows what the keyring did (whether it asked to be unlocked).
//
//	go test -tags emetos_keyring ./extensions/internal/dbconn -run TestRealKeyring -v -count=1
func TestRealKeyring(t *testing.T) {
	ss := &SecretService{
		Dial:          func(ctx context.Context) (*dbus.Conn, error) { return dbus.ConnectSessionBus(dbus.WithContext(ctx)) },
		PromptTimeout: 2 * time.Minute,
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	id := "emetos-manual-check-" + NewID()
	t.Cleanup(func() {
		if err := ss.Delete(context.Background(), id); err != nil {
			t.Errorf("deleting the secret of the check: %v", err)
		}
	})
	t.Logf("storing a secret labelled \"Emetos: manual check\" (connection %s); if the keyring is locked it asks you to unlock it now", id)
	if err := ss.Store(ctx, id, "Emetos: manual check", "first-secret"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, found, err := ss.Lookup(ctx, id)
	if err != nil || !found || got != "first-secret" {
		t.Fatalf("Lookup = %q, %v, %v", got, found, err)
	}
	if err := ss.Store(ctx, id, "Emetos: manual check", "second-secret"); err != nil {
		t.Fatalf("Store again: %v", err)
	}
	if got, found, err = ss.Lookup(ctx, id); err != nil || !found || got != "second-secret" {
		t.Fatalf("Lookup after the replace = %q, %v, %v", got, found, err)
	}
	if err := ss.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, err = ss.Lookup(ctx, id); err != nil || found {
		t.Fatalf("Lookup after Delete = found %v, %v", found, err)
	}
	t.Log("stored, read back, replaced and deleted: the keyring of this session works with Emetos")
}
