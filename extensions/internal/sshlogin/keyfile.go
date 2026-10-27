package sshlogin

import (
	"bytes"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"io"
	"os"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// maxKeyFile is the largest private key file that is read (an RSA 4096 key in OpenSSH format is about 3.4 KB).
const maxKeyFile = 64 << 10

// MaxPassphraseTries is how many passphrases KeyFileAuth asks for before it gives up.
const MaxPassphraseTries = 3

// PassphraseFunc asks for the passphrase of a key file. attempt counts from 1 (so the dialog can say the last one was
// wrong). ok is false when the user cancelled. The function owns the bytes it returns until KeyFileAuth is done with them:
// KeyFileAuth overwrites them as soon as the key is parsed, and neither stores nor logs them.
type PassphraseFunc func(attempt int) (passphrase []byte, ok bool)

// Auth is how a client proves who it is: the signers of a key file, or the keys of an ssh-agent. Close it after the
// connection is made (an agent's socket stays open until then).
type Auth struct {
	signers []ssh.Signer
	closers []io.Closer
	// PublicKey is the public half of the first key, to show to the user (Show My Public Key as QR Code) or to list.
	PublicKey ssh.PublicKey
}

// Close releases the agent connection of the Auth, if it has one.
func (a *Auth) Close() error {
	var first error
	for _, c := range a.closers {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	a.closers = nil
	return first
}

// SignerAuth is the Auth of signers already in memory (a key made by a test).
func SignerAuth(signers ...ssh.Signer) *Auth {
	a := &Auth{signers: signers}
	if len(signers) > 0 {
		a.PublicKey = signers[0].PublicKey()
	}
	return a
}

// KeyFileAuth reads the private key at path (OpenSSH or PEM format, at most 64 KiB) and returns the Auth for it. A key with a
// passphrase asks ask for it, up to MaxPassphraseTries times. The errors have the tags SSHLoginKeyFileUnreadable,
// SSHLoginKeyFileInvalid, SSHLoginPassphraseRequired (the key has a passphrase and ask is nil), SSHLoginPassphraseCancelled
// and SSHLoginWrongPassphrase.
func KeyFileAuth(path string, ask PassphraseFunc) (*Auth, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.NewErr("SSHLoginKeyFileUnreadable", err).SetMetadata("path", path)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxKeyFile+1))
	if err != nil {
		return nil, errors.NewErr("SSHLoginKeyFileUnreadable", err).SetMetadata("path", path)
	}
	defer wipe(data)
	if len(data) > maxKeyFile {
		return nil, errors.New("SSHLoginKeyFileInvalid", "the key file is too large to be a private key", "path", path)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err == nil {
		return SignerAuth(signer), nil
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, errors.NewErr("SSHLoginKeyFileInvalid", err).SetMetadata("path", path)
	}
	if ask == nil {
		return nil, errors.New("SSHLoginPassphraseRequired", "the key is protected by a passphrase", "path", path)
	}
	for attempt := 1; attempt <= MaxPassphraseTries; attempt++ {
		pass, ok := ask(attempt)
		if !ok {
			wipe(pass)
			return nil, errors.New("SSHLoginPassphraseCancelled", "the passphrase was not given", "path", path)
		}
		signer, err = ssh.ParsePrivateKeyWithPassphrase(data, pass)
		wipe(pass)
		if err == nil {
			return SignerAuth(signer), nil
		}
		if !errors.Is(err, x509.IncorrectPasswordError) {
			return nil, errors.NewErr("SSHLoginKeyFileInvalid", err).SetMetadata("path", path)
		}
	}
	return nil, errors.New("SSHLoginWrongPassphrase", "the passphrase is wrong", "path", path, "tries", MaxPassphraseTries)
}

// wipe overwrites secret bytes.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// PublicKeyOfKeyFile reads the public half of the private key at path, for the user to show (Show My Public Key as QR Code). A
// key in OpenSSH format keeps its public key in the clear in front of the private part, so the public key is read from there and
// no passphrase is needed, whether the key has one or not; the private part of the file is not parsed. Another format (PEM) has
// no public part to read: the key is parsed as KeyFileAuth parses it, asking ask for the passphrase when it has one, and only
// the public key is kept. The file is read with the limits of KeyFileAuth (at most 64 KiB) and its bytes are overwritten when
// done. It fails with the tags of KeyFileAuth (SSHLoginKeyFileUnreadable, SSHLoginKeyFileInvalid, SSHLoginPassphraseRequired,
// SSHLoginPassphraseCancelled, SSHLoginWrongPassphrase).
func PublicKeyOfKeyFile(path string, ask PassphraseFunc) (ssh.PublicKey, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.NewErr("SSHLoginKeyFileUnreadable", err).SetMetadata("path", path)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxKeyFile+1))
	if err != nil {
		return nil, errors.NewErr("SSHLoginKeyFileUnreadable", err).SetMetadata("path", path)
	}
	defer wipe(data)
	if len(data) > maxKeyFile {
		return nil, errors.New("SSHLoginKeyFileInvalid", "the key file is too large to be a private key", "path", path)
	}
	if block, _ := pem.Decode(data); block != nil && block.Type == "OPENSSH PRIVATE KEY" {
		defer wipe(block.Bytes)
		pub, problem := openSSHPublicKey(block.Bytes)
		if problem != "" {
			return nil, errors.New("SSHLoginKeyFileInvalid", problem, "path", path)
		}
		return pub, nil
	}
	auth, err := KeyFileAuth(path, ask)
	if err != nil {
		return nil, err
	}
	defer func() { _ = auth.Close() }()
	return auth.PublicKey, nil
}

// opensshMagic starts the body of an OpenSSH private key (PROTOCOL.key of OpenSSH): the magic, then the cipher name, the kdf
// name, the kdf options, the number of keys, the public key of each, and the private part.
const opensshMagic = "openssh-key-v1\x00"

// openSSHPublicKey returns the first public key of the body of an OpenSSH private key, reading nothing of the private part; when
// the body is not one it returns what is wrong with it.
func openSSHPublicKey(body []byte) (key ssh.PublicKey, problem string) {
	if !bytes.HasPrefix(body, []byte(opensshMagic)) {
		return nil, "this is not an OpenSSH private key"
	}
	rest := body[len(opensshMagic):]
	str := func() ([]byte, bool) {
		if len(rest) < 4 {
			return nil, false
		}
		n := binary.BigEndian.Uint32(rest)
		if uint64(n) > uint64(len(rest)-4) {
			return nil, false
		}
		out := rest[4 : 4+n]
		rest = rest[4+n:]
		return out, true
	}
	for range 3 { // the cipher, the kdf and its options
		if _, ok := str(); !ok {
			return nil, "the OpenSSH private key is cut short"
		}
	}
	if len(rest) < 4 || binary.BigEndian.Uint32(rest) == 0 {
		return nil, "the OpenSSH private key holds no key"
	}
	rest = rest[4:]
	blob, ok := str()
	if !ok {
		return nil, "the OpenSSH private key is cut short"
	}
	// the key parsed keeps pieces of the blob, and the blob is part of the file that the caller overwrites
	pub, err := ssh.ParsePublicKey(bytes.Clone(blob))
	if err != nil {
		return nil, "the public key in the OpenSSH private key cannot be read: " + err.Error()
	}
	return pub, ""
}
