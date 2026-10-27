package sshlogin

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// keyFile writes a private key file in a temporary folder.
func keyFile(t *testing.T, data []byte) string {
	t.Helper()
	return writeFile(t, "id_key", string(data))
}

func TestThePublicKeyOfAKeyFileIsReadWithoutThePassphrase(t *testing.T) {
	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	asked := 0
	ask := func(int) ([]byte, bool) { asked++; return []byte("secret"), true }
	cases := []struct {
		name string
		priv any
		pass string
	}{
		{"Ed25519", edPriv, ""},
		{"Ed25519 with a passphrase", edPriv, "secret"},
		{"ECDSA with a passphrase", ecPriv, "secret"},
		{"RSA", rsaPriv, ""},
		{"RSA with a passphrase", rsaPriv, "secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var block *pem.Block
			var err error
			if tc.pass == "" {
				block, err = ssh.MarshalPrivateKey(tc.priv, "me@laptop")
			} else {
				block, err = ssh.MarshalPrivateKeyWithPassphrase(tc.priv, "me@laptop", []byte(tc.pass))
			}
			if err != nil {
				t.Fatal(err)
			}
			signer, err := ssh.NewSignerFromKey(tc.priv)
			if err != nil {
				t.Fatal(err)
			}
			asked = 0
			pub, err := PublicKeyOfKeyFile(keyFile(t, pem.EncodeToMemory(block)), ask)
			if err != nil {
				t.Fatal(err)
			}
			if !SameKey(pub, signer.PublicKey()) {
				t.Errorf("the key is %s, want %s", ssh.FingerprintSHA256(pub), ssh.FingerprintSHA256(signer.PublicKey()))
			}
			if asked != 0 {
				t.Errorf("the passphrase was asked for %d times: the public half is in the clear", asked)
			}
			// and with no way to ask at all
			if _, err := PublicKeyOfKeyFile(keyFile(t, pem.EncodeToMemory(block)), nil); err != nil {
				t.Errorf("with no ask: %v", err)
			}
		})
	}
}

func TestAPEMKeyIsParsedForItsPublicKey(t *testing.T) {
	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(ecPriv)
	if err != nil {
		t.Fatal(err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	signer, _ := ssh.NewSignerFromKey(ecPriv)
	pub, err := PublicKeyOfKeyFile(keyFile(t, pemData), nil)
	if err != nil || !SameKey(pub, signer.PublicKey()) {
		t.Fatalf("a PEM key: %v, %v", pub, err)
	}
	// a PEM key with a passphrase asks for it, and the key is the same
	rsaPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	//nolint:staticcheck // the legacy PEM encryption is what old key files have; there is no other way to make one for the test
	enc, err := x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(rsaPriv), []byte("pw"), x509.PEMCipherAES128)
	if err != nil {
		t.Fatal(err)
	}
	encSigner, _ := ssh.NewSignerFromKey(rsaPriv)
	path := keyFile(t, pem.EncodeToMemory(enc))
	_, err = PublicKeyOfKeyFile(path, nil)
	wantRoute(t, err, "PublicKeyOfKeyFile->KeyFileAuth.SSHLoginPassphraseRequired")
	asked := 0
	pub, err = PublicKeyOfKeyFile(path, func(int) ([]byte, bool) { asked++; return []byte("pw"), true })
	if err != nil || asked != 1 || !SameKey(pub, encSigner.PublicKey()) {
		t.Fatalf("a PEM key with a passphrase: %v, asked %d, %v", pub, asked, err)
	}
	_, err = PublicKeyOfKeyFile(path, func(int) ([]byte, bool) { return []byte("wrong"), true })
	wantRoute(t, err, "PublicKeyOfKeyFile->KeyFileAuth.SSHLoginWrongPassphrase")
	_, err = PublicKeyOfKeyFile(path, func(int) ([]byte, bool) { return nil, false })
	wantRoute(t, err, "PublicKeyOfKeyFile->KeyFileAuth.SSHLoginPassphraseCancelled")
}

// opensshBody builds the body of an OpenSSH private key from its parts.
func opensshBody(parts ...[]byte) []byte {
	out := []byte(opensshMagic)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func sshString(s []byte) []byte {
	return append(binary.BigEndian.AppendUint32(nil, uint32(len(s))), s...)
}

func TestAKeyFileThatCannotBeUsedHasItsTag(t *testing.T) {
	_, edPriv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKey(edPriv, "")
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(edPriv)
	blob := signer.PublicKey().Marshal()
	pemOf := func(body []byte) []byte {
		return pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: body})
	}
	none := sshString([]byte("none"))
	cases := []struct {
		name  string
		data  []byte
		route string
	}{
		{"junk", []byte("not a key at all"), "PublicKeyOfKeyFile->KeyFileAuth.SSHLoginKeyFileInvalid"},
		{"another magic", pemOf([]byte("openssh-key-v2\x00")), "PublicKeyOfKeyFile.SSHLoginKeyFileInvalid"},
		{"only the magic", pemOf(opensshBody()), "PublicKeyOfKeyFile.SSHLoginKeyFileInvalid"},
		{"cut after the cipher", pemOf(opensshBody(none)), "PublicKeyOfKeyFile.SSHLoginKeyFileInvalid"},
		{"a string longer than the body", pemOf(opensshBody(none, none, []byte{0xFF, 0xFF, 0xFF, 0xFF})), "PublicKeyOfKeyFile.SSHLoginKeyFileInvalid"},
		{"no key", pemOf(opensshBody(none, none, sshString(nil), []byte{0, 0, 0, 0})), "PublicKeyOfKeyFile.SSHLoginKeyFileInvalid"},
		{"no public key after the count", pemOf(opensshBody(none, none, sshString(nil), []byte{0, 0, 0, 1})), "PublicKeyOfKeyFile.SSHLoginKeyFileInvalid"},
		{"a public key that is not one", pemOf(opensshBody(none, none, sshString(nil), []byte{0, 0, 0, 1}, sshString([]byte("junk")))), "PublicKeyOfKeyFile.SSHLoginKeyFileInvalid"},
		{"larger than any key", []byte(strings.Repeat("a", maxKeyFile+1)), "PublicKeyOfKeyFile.SSHLoginKeyFileInvalid"},
	}
	// a good one built by hand reads as the real one does
	good := pemOf(opensshBody(none, none, sshString(nil), []byte{0, 0, 0, 1}, sshString(blob), sshString([]byte("the private part is never read"))))
	if pub, err := PublicKeyOfKeyFile(keyFile(t, good), nil); err != nil || !SameKey(pub, signer.PublicKey()) {
		t.Fatalf("a key built by hand: %v, %v", pub, err)
	}
	if pub, err := PublicKeyOfKeyFile(keyFile(t, pem.EncodeToMemory(block)), nil); err != nil || !SameKey(pub, signer.PublicKey()) {
		t.Fatalf("the key of ssh: %v, %v", pub, err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub, err := PublicKeyOfKeyFile(keyFile(t, tc.data), nil)
			if pub != nil {
				t.Error("a key came out")
			}
			wantRoute(t, err, tc.route)
		})
	}
	_, err = PublicKeyOfKeyFile(filepath.Join(t.TempDir(), "missing"), nil)
	wantRoute(t, err, "PublicKeyOfKeyFile.SSHLoginKeyFileUnreadable")
	// a folder opens, and cannot be read
	_, err = PublicKeyOfKeyFile(t.TempDir(), nil)
	wantRoute(t, err, "PublicKeyOfKeyFile.SSHLoginKeyFileUnreadable")
}
