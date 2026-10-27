package sshlogin

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extensions/internal/statefile"
)

// maxHostKeyFile is the largest host key file that is read; an Ed25519 key in OpenSSH format is under 500 bytes.
const maxHostKeyFile = 64 << 10

// LoadOrCreateHostKey returns the Ed25519 host key kept at path (an OpenSSH private key). When the file does not exist a new
// key is made and written there with mode 0600, in a folder of mode 0700. An existing file that other users can read or
// change, that is not a regular file, that is too large or is not an Ed25519 key in OpenSSH format is refused and never
// replaced. The errors have the tags SSHLoginHostKeyInvalid, SSHLoginHostKeyCreateFailed and those of statefile.Read.
func LoadOrCreateHostKey(path string) (ssh.Signer, error) {
	data, err := statefile.Read(path, maxHostKeyFile, statefile.Private)
	if statefile.Missing(err) {
		return createHostKey(path, rand.Reader)
	}
	if err != nil {
		return nil, err
	}
	raw, err := ssh.ParseRawPrivateKey(data)
	if err != nil {
		return nil, errors.NewErr("SSHLoginHostKeyInvalid", err).SetMetadata("path", path)
	}
	var key ed25519.PrivateKey
	switch k := raw.(type) {
	case *ed25519.PrivateKey:
		key = *k
	case ed25519.PrivateKey:
		key = k
	default:
		return nil, errors.New("SSHLoginHostKeyInvalid", "the host key is not an Ed25519 key", "path", path)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, errors.NewErr("SSHLoginHostKeyInvalid", err).SetMetadata("path", path)
	}
	return signer, nil
}

// createHostKey makes a host key from the source of randomness and writes it to path.
func createHostKey(path string, random io.Reader) (ssh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(random)
	if err != nil {
		return nil, errors.NewErr("SSHLoginHostKeyCreateFailed", err).SetMetadata("path", path)
	}
	block, err := ssh.MarshalPrivateKey(priv, "emetos host key")
	if err != nil {
		return nil, errors.NewErr("SSHLoginHostKeyCreateFailed", err).SetMetadata("path", path)
	}
	if err := statefile.Write(path, pem.EncodeToMemory(block)); err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, errors.NewErr("SSHLoginHostKeyCreateFailed", err).SetMetadata("path", path)
	}
	return signer, nil
}
