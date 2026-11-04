//go:build emetos_keyring

package httpclient

import (
	"context"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/rm4n0s/emetos/extensions/httpclient/oauth"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// TestRealKeyringHoldsASecretVariableAndAnOAuthToken is the manual check of step 27.9 (docs/MANUAL-CHECKS.md): the secret value of an
// environment variable and an OAuth 2.0 token are stored in, read back from and deleted from the keyring of the desktop session that
// runs it, through the Secret Service on the session bus, with the keys and labels the HTTP client uses. It uses a folder name of its own,
// so that it touches no secret of a real project, and it deletes what it stored. It is never part of the checks of a step: it needs the
// build tag, and the default SecretService refuses to dial under `go test`, so this test gives it a Dial of its own.
//
//	go test -tags emetos_keyring ./extensions/httpclient -run TestRealKeyring -v -count=1
func TestRealKeyringHoldsASecretVariableAndAnOAuthToken(t *testing.T) {
	ss := &dbconn.SecretService{
		Dial:          func(ctx context.Context) (*dbus.Conn, error) { return dbus.ConnectSessionBus(dbus.WithContext(ctx)) },
		PromptTimeout: 2 * time.Minute,
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	folder := "/emetos-manual-check-" + dbconn.NewID()
	values := variables.NewValues(folder, nil, ss)
	owner := variables.EnvironmentOwner("manual-check")
	store := oauth.Store{Secrets: ss, Folder: folder}
	t.Cleanup(func() {
		if err := values.DeleteSecrets(context.Background(), owner, "apiKey"); err != nil {
			t.Errorf("deleting the secret variable of the check: %v", err)
		}
		if err := store.Clear(context.Background(), "manual-check", "client_credentials"); err != nil {
			t.Errorf("deleting the token of the check: %v", err)
		}
	})

	t.Log("storing a secret variable and a token labelled \"Emetos HTTP Client: ...\"; if the keyring is locked it asks you to unlock it now")
	if err := values.Set(ctx, owner, "apiKey", "first-secret", true); err != nil {
		t.Fatalf("Set the secret variable: %v", err)
	}
	got, found, err := values.Secret(ctx, owner, "apiKey")
	if err != nil || !found || got != "first-secret" {
		t.Fatalf("Secret = %q, %v, %v", got, found, err)
	}
	if err := values.Set(ctx, owner, "apiKey", "second-secret", true); err != nil {
		t.Fatalf("Set again: %v", err)
	}
	if got, found, err = values.Secret(ctx, owner, "apiKey"); err != nil || !found || got != "second-secret" {
		t.Fatalf("Secret after the replace = %q, %v, %v", got, found, err)
	}

	tok := oauth.Token{AccessToken: "access-manual-check", TokenType: "Bearer", RefreshToken: "refresh-manual-check", ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Second)}
	if err := store.Save(ctx, "manual-check", "client_credentials", tok); err != nil {
		t.Fatalf("Save the token: %v", err)
	}
	back, found, err := store.Load(ctx, "manual-check", "client_credentials")
	if err != nil || !found || back.AccessToken != tok.AccessToken || back.RefreshToken != tok.RefreshToken || !back.ExpiresAt.Equal(tok.ExpiresAt) {
		t.Fatalf("Load = %+v, %v, %v", back, found, err)
	}

	if err := values.Reset(ctx, owner, "apiKey", true); err != nil {
		t.Fatalf("Reset the secret variable: %v", err)
	}
	if _, found, err = values.Secret(ctx, owner, "apiKey"); err != nil || found {
		t.Fatalf("Secret after Reset: found %v, %v", found, err)
	}
	if err := store.Clear(ctx, "manual-check", "client_credentials"); err != nil {
		t.Fatalf("Clear the token: %v", err)
	}
	if _, found, err = store.Load(ctx, "manual-check", "client_credentials"); err != nil || found {
		t.Fatalf("Load after Clear: found %v, %v", found, err)
	}
	t.Log("a secret variable and an OAuth token were stored, read back, replaced, and deleted: the keyring of this session works with the HTTP client")
}
