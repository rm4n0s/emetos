package sshlogin

import (
	"io"
	"os"

	"github.com/rm4n0s/errors"
)

// maxPublicKeyFile is the largest .pub file that is read.
const maxPublicKeyFile = 16 << 10

// ReadPublicKeyFile reads the text of a public key file (the "Choose .pub File" of Remote Work and Relay): a small regular
// file. The line still has to be parsed with ParseAuthorizedKey, which refuses what is not a single public key. The errors have
// the tags SSHLoginPubFileUnreadable (the cause is kept) and SSHLoginPubFileInvalid (not a regular file, or larger than 16 KiB).
func ReadPublicKeyFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", errors.NewErr("SSHLoginPubFileUnreadable", err).SetMetadata("path", path)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", errors.NewErr("SSHLoginPubFileUnreadable", err).SetMetadata("path", path)
	}
	if !info.Mode().IsRegular() || info.Size() > maxPublicKeyFile {
		return "", errors.New("SSHLoginPubFileInvalid", "a public key file is a small regular file", "path", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPublicKeyFile))
	if err != nil {
		return "", errors.NewErr("SSHLoginPubFileUnreadable", err).SetMetadata("path", path)
	}
	return string(data), nil
}
