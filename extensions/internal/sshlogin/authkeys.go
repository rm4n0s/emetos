package sshlogin

import (
	"crypto/rsa"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// MaxKeyLine is the longest authorized_keys line ParseAuthorizedKey takes, and MaxCommentRunes the longest comment it keeps.
const (
	MaxKeyLine      = 8 << 10
	MaxCommentRunes = 128
)

// AllowedKey is a public key that may log in: the key and the comment of its authorized_keys line.
type AllowedKey struct {
	// Key is the public key.
	Key ssh.PublicKey
	// Comment is the comment of the line, without control characters and cut to MaxCommentRunes; may be empty.
	Comment string
}

// Fingerprint is the SHA-256 fingerprint of the key in the form of ssh-keygen (SHA256:base64).
func (k AllowedKey) Fingerprint() string { return ssh.FingerprintSHA256(k.Key) }

// Type is the name of the key's algorithm (ssh-ed25519).
func (k AllowedKey) Type() string { return k.Key.Type() }

// Line is the authorized_keys line of the key: type, base64 and the comment when there is one.
func (k AllowedKey) Line() string {
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k.Key)))
	if k.Comment != "" {
		line += " " + k.Comment
	}
	return line
}

// Name is what a banner or a notification calls the key: its comment, or its fingerprint when it has none.
func (k AllowedKey) Name() string {
	if k.Comment != "" {
		return k.Comment
	}
	return k.Fingerprint()
}

// Equal tells whether the keys are the same key (their wire encodings are equal).
func (k AllowedKey) Equal(other ssh.PublicKey) bool { return SameKey(k.Key, other) }

// SameKey compares two public keys by their wire encoding, which is how a login is matched against the list.
func SameKey(a, b ssh.PublicKey) bool {
	if a == nil || b == nil {
		return false
	}
	return string(a.Marshal()) == string(b.Marshal())
}

// allowedTypes are the key types a login accepts. DSA keys and certificates are not among them.
var allowedTypes = map[string]bool{
	ssh.KeyAlgoED25519: true, ssh.KeyAlgoECDSA256: true, ssh.KeyAlgoECDSA384: true, ssh.KeyAlgoECDSA521: true,
	ssh.KeyAlgoRSA: true, ssh.KeyAlgoSKED25519: true, ssh.KeyAlgoSKECDSA256: true,
}

// MinRSABits is the smallest RSA key a login accepts.
const MinRSABits = 2048

// ParseAuthorizedKey reads one line of an authorized_keys file: a public key and an optional comment. A line with options
// (command=, from=, restrict, ...) is refused, because the options would not be applied; so are several lines, an empty or
// comment line, a certificate, a type that is not Ed25519, ECDSA, RSA of at least 2048 bits or a security key, and a line
// longer than MaxKeyLine. The errors have the tags SSHLoginKeyLineInvalid and SSHLoginKeyUnsupported.
func ParseAuthorizedKey(line string) (AllowedKey, error) {
	line = strings.TrimSpace(line)
	switch {
	case line == "":
		return AllowedKey{}, errors.New("SSHLoginKeyLineInvalid", "the line is empty")
	case len(line) > MaxKeyLine:
		return AllowedKey{}, errors.New("SSHLoginKeyLineInvalid", "the line is too long to be a public key", "length", len(line))
	case strings.ContainsAny(line, "\r\n"):
		return AllowedKey{}, errors.New("SSHLoginKeyLineInvalid", "add one key at a time: the text has several lines")
	case strings.HasPrefix(line, "#"):
		return AllowedKey{}, errors.New("SSHLoginKeyLineInvalid", "the line is a comment")
	case !utf8.ValidString(line):
		return AllowedKey{}, errors.New("SSHLoginKeyLineInvalid", "the line is not UTF-8 text")
	}
	key, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return AllowedKey{}, errors.NewErr("SSHLoginKeyLineInvalid", err).SetMessage("this is not a public key in the form of a line of an authorized_keys file (" + err.Error() + ")")
	}
	if len(options) > 0 {
		return AllowedKey{}, errors.New("SSHLoginKeyLineInvalid", "the options of an authorized_keys line (command=, from=, restrict) are not applied here; remove them")
	}
	if len(strings.TrimSpace(string(rest))) > 0 {
		return AllowedKey{}, errors.New("SSHLoginKeyLineInvalid", "add one key at a time: the text has more than one key")
	}
	if _, isCert := key.(*ssh.Certificate); isCert || !allowedTypes[key.Type()] {
		return AllowedKey{}, errors.New("SSHLoginKeyUnsupported", "only Ed25519, ECDSA, RSA and security keys are accepted, not certificates or other types", "type", key.Type())
	}
	if key.Type() == ssh.KeyAlgoRSA {
		if ck, ok := key.(ssh.CryptoPublicKey); ok {
			if pub, ok := ck.CryptoPublicKey().(*rsa.PublicKey); ok && pub.N.BitLen() < MinRSABits {
				return AllowedKey{}, errors.New("SSHLoginKeyUnsupported", "an RSA key needs at least 2048 bits", "bits", pub.N.BitLen())
			}
		}
	}
	return AllowedKey{Key: key, Comment: cleanComment(comment)}, nil
}

// cleanComment drops the control characters of a comment (it is shown in banners and notifications) and cuts it.
func cleanComment(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > MaxCommentRunes {
		s = string(r[:MaxCommentRunes])
	}
	return s
}
