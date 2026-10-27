package sshlogin

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func authorizedLine(t *testing.T, k ssh.PublicKey, comment string) string {
	t.Helper()
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))
	if comment != "" {
		line += " " + comment
	}
	return line
}

func TestParseAuthorizedKey(t *testing.T) {
	ed := newKey(t)
	rsa2048, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaPub, _ := ssh.NewPublicKey(&rsa2048.PublicKey)
	rsa1024, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	smallPub, _ := ssh.NewPublicKey(&rsa1024.PublicKey)
	cert := &ssh.Certificate{Key: ed.signer.PublicKey(), CertType: ssh.UserCert, ValidBefore: ssh.CertTimeInfinity}
	if err := cert.SignCert(rand.Reader, newKey(t).signer); err != nil {
		t.Fatal(err)
	}
	edLine := authorizedLine(t, ed.signer.PublicKey(), "")

	tests := []struct {
		name    string
		line    string
		comment string
		route   string
	}{
		{"ed25519 with a comment", edLine + " laptop B (work)", "laptop B (work)", ""},
		{"no comment", edLine, "", ""},
		{"spaces around", "  " + edLine + " me@host \t", "me@host", ""},
		{"rsa 2048", authorizedLine(t, rsaPub, "rsa"), "rsa", ""},
		{"control characters are dropped", edLine + " a\x1b[31mb", "a[31mb", ""},
		{"long comment is cut", edLine + " " + strings.Repeat("é", 300), strings.Repeat("é", MaxCommentRunes), ""},
		{"empty", "  ", "", "ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"comment line", "# ssh-ed25519 AAAA", "", "ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"two lines", edLine + "\n" + edLine, "", "ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"not a key", "ssh-ed25519 not-base64!", "", "ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"options", `command="rm -rf /",no-pty ` + edLine, "", "ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"too long", edLine + " " + strings.Repeat("x", MaxKeyLine), "", "ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"not utf-8", edLine + " \xff\xfe", "", "ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"certificate", authorizedLine(t, cert, ""), "", "ParseAuthorizedKey.SSHLoginKeyUnsupported"},
		{"small rsa", authorizedLine(t, smallPub, ""), "", "ParseAuthorizedKey.SSHLoginKeyUnsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, err := ParseAuthorizedKey(tt.line)
			if tt.route != "" {
				wantRoute(t, err, tt.route)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if k.Comment != tt.comment {
				t.Errorf("comment = %q, want %q", k.Comment, tt.comment)
			}
			again, err := ParseAuthorizedKey(k.Line())
			if err != nil || !k.Equal(again.Key) || again.Comment != k.Comment {
				t.Errorf("Line() = %q does not parse back: %v", k.Line(), err)
			}
		})
	}
}

func TestAllowedKeyNames(t *testing.T) {
	k := newKey(t)
	withComment := k.allowed("laptop")
	if withComment.Name() != "laptop" || withComment.Type() != "ssh-ed25519" || !strings.HasPrefix(withComment.Fingerprint(), "SHA256:") {
		t.Errorf("key = %s %s %s", withComment.Name(), withComment.Type(), withComment.Fingerprint())
	}
	if bare := k.allowed(""); bare.Name() != bare.Fingerprint() {
		t.Errorf("Name of a key with no comment = %q", bare.Name())
	}
	if SameKey(nil, k.signer.PublicKey()) || SameKey(k.signer.PublicKey(), nil) || SameKey(k.signer.PublicKey(), newKey(t).signer.PublicKey()) {
		t.Error("SameKey says different keys are the same")
	}
}

func TestHostKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "emetos")
	path := filepath.Join(dir, "host-key")
	first, err := LoadOrCreateHostKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicKey().Type() != ssh.KeyAlgoED25519 {
		t.Errorf("type = %s", first.PublicKey().Type())
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("host key file: %v, %v", info, err)
	}
	if di, err := os.Stat(dir); err != nil || di.Mode().Perm() != 0o700 {
		t.Errorf("folder: %v, %v", di, err)
	}
	again, err := LoadOrCreateHostKey(path)
	if err != nil || !SameKey(first.PublicKey(), again.PublicKey()) {
		t.Fatalf("the key changed between runs: %v", err)
	}

	// the key is not replaced when the file cannot be used
	before, _ := os.ReadFile(path)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	_, err = LoadOrCreateHostKey(path)
	wantRoute(t, err, "Read.StateFileUnsafeMode")
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("the host key was replaced")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	block, _ := ssh.MarshalPrivateKey(rsaKey, "")
	tests := []struct {
		name    string
		content []byte
		route   string
	}{
		{"junk", []byte("not a key"), "LoadOrCreateHostKey.SSHLoginHostKeyInvalid"},
		{"an RSA key", pemBytes(block), "LoadOrCreateHostKey.SSHLoginHostKeyInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "k")
			if err := os.WriteFile(p, tt.content, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadOrCreateHostKey(p)
			wantRoute(t, err, tt.route)
		})
	}

	// a source of randomness that fails
	_, err = createHostKey(filepath.Join(t.TempDir(), "k"), strings.NewReader(""))
	wantRoute(t, err, "createHostKey.SSHLoginHostKeyCreateFailed")

	// a folder that cannot be made
	_, err = LoadOrCreateHostKey(filepath.Join(readOnlyDir(t), "sub", "key"))
	wantRoute(t, err, "Write.StateFileWriteFailed")
}

func pemBytes(b *pem.Block) []byte { return pem.EncodeToMemory(b) }
