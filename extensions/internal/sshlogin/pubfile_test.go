package sshlogin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPublicKeyFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.pub")
	if err := os.WriteFile(good, []byte("ssh-ed25519 AAAA x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.pub")
	if err := os.WriteFile(big, make([]byte, maxPublicKeyFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadPublicKeyFile(good); err != nil || got != "ssh-ed25519 AAAA x\n" {
		t.Errorf("ReadPublicKeyFile = %q, %v", got, err)
	}
	_, err := ReadPublicKeyFile(filepath.Join(dir, "none"))
	wantRoute(t, err, "ReadPublicKeyFile.SSHLoginPubFileUnreadable")
	_, err = ReadPublicKeyFile(big)
	wantRoute(t, err, "ReadPublicKeyFile.SSHLoginPubFileInvalid")
	_, err = ReadPublicKeyFile(dir)
	wantRoute(t, err, "ReadPublicKeyFile.SSHLoginPubFileInvalid")
}
