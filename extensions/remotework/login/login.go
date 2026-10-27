package login

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

// ChannelType is the one channel type of the Remote Work server.
const ChannelType = "emetos-remotework"

// The sizes and limits of the conversation.
const (
	// Version is the version of the request and the grant.
	Version = 1
	// TokenSize is the size of the session token in bytes (256 bits) and FingerprintSize the size of a SHA-256 fingerprint.
	TokenSize       = 32
	FingerprintSize = 32
	// MaxWorkspaceName is the longest workspace name of a request and a grant, and MaxWorkspaces the most names a grant lists.
	MaxWorkspaceName = 200
	MaxWorkspaces    = 16
	// MaxGrant is the largest grant the client reads.
	MaxGrant = 8 << 10
	// GrantTimeout is how long the client waits for the grant.
	GrantTimeout = 15 * time.Second
)

// The statuses of a grant.
const (
	// StatusOK: the session is granted; the token (and perhaps a certificate fingerprint) are in the grant.
	StatusOK = "ok"
	// StatusNone: no workspace of the host has Remote Work on.
	StatusNone = "none"
	// StatusAmbiguous: several workspaces have it on and the request named none; the grant lists their names.
	StatusAmbiguous = "ambiguous"
	// StatusUnknown: the request named a workspace that does not have Remote Work on; the grant lists the names that do.
	StatusUnknown = "unknown"
)

// Request is what the client asks for when it opens the channel.
type Request struct {
	// Workspace is the name of the workspace; empty asks for the only one.
	Workspace string
}

// Encode is the data of the channel open message: the version, then the name. It fails with RemoteWorkRequestInvalid for a
// name that is too long or not UTF-8.
func (r Request) Encode() ([]byte, error) {
	if len(r.Workspace) > MaxWorkspaceName || !utf8.ValidString(r.Workspace) {
		return nil, errors.New("RemoteWorkRequestInvalid", "the workspace name is too long or not UTF-8", "length", len(r.Workspace))
	}
	return append([]byte{Version}, r.Workspace...), nil
}

// ParseRequest reads the data of a channel open message. It fails with RemoteWorkRequestInvalid.
func ParseRequest(b []byte) (Request, error) {
	switch {
	case len(b) == 0:
		return Request{}, errors.New("RemoteWorkRequestInvalid", "the request is empty")
	case b[0] != Version:
		return Request{}, errors.New("RemoteWorkRequestInvalid", "the request is of another version", "version", int(b[0]))
	case len(b)-1 > MaxWorkspaceName || !utf8.Valid(b[1:]):
		return Request{}, errors.New("RemoteWorkRequestInvalid", "the workspace name is too long or not UTF-8", "length", len(b)-1)
	}
	return Request{Workspace: string(b[1:])}, nil
}

// Grant is the answer of the host.
type Grant struct {
	// Status is one of the Status constants.
	Status string `json:"status"`
	// Workspace is the name of the workspace granted (StatusOK only).
	Workspace string `json:"workspace,omitempty"`
	// Token is the session token, TokenSize random bytes (StatusOK only). It is kept in memory on both sides.
	Token []byte `json:"token,omitempty"`
	// CertSHA256 is the SHA-256 fingerprint of the QUIC certificate of the host, FingerprintSize bytes, or empty when the host
	// offers no QUIC (the session then runs over the SSH channel).
	CertSHA256 []byte `json:"certSha256,omitempty"`
	// Workspaces are the names of the workspaces that have Remote Work on (StatusAmbiguous and StatusUnknown).
	Workspaces []string `json:"workspaces,omitempty"`
}

// grantWire is the JSON of a grant with its version.
type grantWire struct {
	Version int `json:"version"`
	Grant
}

// check validates a grant; the host checks what it writes and the client what it reads.
func (g Grant) check() error {
	if len(g.Workspaces) > MaxWorkspaces {
		return errors.New("RemoteWorkGrantInvalid", "the grant lists too many workspaces", "count", len(g.Workspaces))
	}
	for _, w := range g.Workspaces {
		if len(w) > MaxWorkspaceName || !utf8.ValidString(w) {
			return errors.New("RemoteWorkGrantInvalid", "a workspace name of the grant is too long or not UTF-8")
		}
	}
	if len(g.Workspace) > MaxWorkspaceName || !utf8.ValidString(g.Workspace) {
		return errors.New("RemoteWorkGrantInvalid", "the workspace name of the grant is too long or not UTF-8")
	}
	switch g.Status {
	case StatusOK:
		if len(g.Token) != TokenSize || g.Workspace == "" || (len(g.CertSHA256) != 0 && len(g.CertSHA256) != FingerprintSize) {
			return errors.New("RemoteWorkGrantInvalid", "a granted session needs a workspace, a token of 32 bytes and a certificate fingerprint of 0 or 32 bytes",
				"token", len(g.Token), "cert", len(g.CertSHA256))
		}
	case StatusNone, StatusAmbiguous, StatusUnknown:
		if len(g.Token) != 0 || len(g.CertSHA256) != 0 || g.Workspace != "" {
			return errors.New("RemoteWorkGrantInvalid", "a refusal carries no token, certificate or workspace", "status", g.Status)
		}
	default:
		return errors.New("RemoteWorkGrantInvalid", "the status of the grant is not known", "status", g.Status)
	}
	return nil
}

// WriteGrant writes the grant: its length and its JSON. It fails with RemoteWorkGrantInvalid for a grant that ReadGrant would
// refuse and RemoteWorkGrantWriteFailed.
func WriteGrant(w io.Writer, g Grant) error {
	if err := g.check(); err != nil {
		return err
	}
	body, err := json.Marshal(grantWire{Version: Version, Grant: g})
	if err != nil {
		return errors.NewErr("RemoteWorkGrantInvalid", err)
	}
	if len(body) > MaxGrant {
		return errors.New("RemoteWorkGrantInvalid", "the grant is too large", "size", len(body))
	}
	msg := binary.BigEndian.AppendUint32(make([]byte, 0, 4+len(body)), uint32(len(body)))
	msg = append(msg, body...)
	if _, err := w.Write(msg); err != nil {
		return errors.NewErr("RemoteWorkGrantWriteFailed", err)
	}
	return nil
}

// ReadGrant reads a grant. The length is checked against MaxGrant before the body is read, the JSON must have no field this
// version does not know and nothing after it, and the grant must pass its checks. It fails with RemoteWorkGrantInvalid and
// RemoteWorkGrantReadFailed.
func ReadGrant(r io.Reader) (Grant, error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return Grant{}, errors.NewErr("RemoteWorkGrantReadFailed", err)
	}
	n := binary.BigEndian.Uint32(head[:])
	if n == 0 || n > MaxGrant {
		return Grant{}, errors.New("RemoteWorkGrantInvalid", "the size of the grant is not allowed", "size", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return Grant{}, errors.NewErr("RemoteWorkGrantReadFailed", err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var w grantWire
	if err := dec.Decode(&w); err != nil {
		return Grant{}, errors.NewErr("RemoteWorkGrantInvalid", err)
	}
	if dec.More() {
		return Grant{}, errors.New("RemoteWorkGrantInvalid", "there is more than one value in the grant")
	}
	if w.Version != Version {
		return Grant{}, errors.New("RemoteWorkGrantInvalid", "the grant is of another version", "version", w.Version)
	}
	if err := w.check(); err != nil {
		return Grant{}, err
	}
	return w.Grant, nil
}

// ConnectOptions are what Connect needs.
type ConnectOptions struct {
	// Address is host:port of the Remote Work server.
	Address string
	// Workspace is the name of the workspace to use; empty for the only one.
	Workspace string
	// Auth is the key to log in with, Hosts the store of pinned host keys and ConfirmHost the question on first use
	// (sshlogin.DialOptions).
	Auth        *sshlogin.Auth
	Hosts       sshlogin.HostKeys
	ConfirmHost sshlogin.ConfirmHostFunc
	// HandshakeTimeout bounds the connection and the login (sshlogin default); GrantTimeout the wait for the grant
	// (GrantTimeout when zero).
	HandshakeTimeout time.Duration
	GrantTimeout     time.Duration
}

// Connection is a login that was granted a session.
type Connection struct {
	// Client is the SSH connection and Channel the channel of the session, which carries the messages of package protocol when
	// the session runs over SSH.
	Client  *sshlogin.Client
	Channel ssh.Channel
	// Grant is the answer of the host: the workspace, the token and the certificate fingerprint.
	Grant Grant
}

// Close ends the connection.
func (c *Connection) Close() error { return c.Client.Close() }

// Connect logs in to the host, opens the channel and reads the grant. A host that refuses to grant a session (nothing is on,
// several are and none was named, the name is not on) gives RemoteWorkLoginDenied, whose metadata has the status and the names
// of the workspaces that are on. The other errors are those of sshlogin.Dial and OpenChannel, RemoteWorkRequestInvalid and
// those of ReadGrant.
func Connect(ctx context.Context, o ConnectOptions) (*Connection, error) {
	req, err := Request{Workspace: o.Workspace}.Encode()
	if err != nil {
		return nil, err
	}
	client, err := sshlogin.Dial(ctx, sshlogin.DialOptions{Address: o.Address, Auth: o.Auth, Hosts: o.Hosts, ConfirmHost: o.ConfirmHost, HandshakeTimeout: o.HandshakeTimeout})
	if err != nil {
		return nil, err
	}
	ch, err := client.OpenChannel(ChannelType, req)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	timeout := o.GrantTimeout
	if timeout <= 0 {
		timeout = GrantTimeout
	}
	// the channel has no deadline: a host that never answers, or a context that ends, closes the connection under the read
	timer := time.AfterFunc(timeout, func() { _ = client.Close() })
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	grant, err := ReadGrant(ch)
	// a timer or a context that fired while the grant arrived has closed the connection: the grant is not used
	timerStopped, ctxStopped := timer.Stop(), stop()
	if !timerStopped || !ctxStopped {
		if err == nil {
			err = errors.New("RemoteWorkGrantReadFailed", "the host took too long to answer, or the login was cancelled")
		}
	}
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	if grant.Status != StatusOK {
		_ = client.Close()
		return nil, errors.New("RemoteWorkLoginDenied", "the host did not grant a session: "+deniedWhy(grant.Status),
			"status", grant.Status, "workspaces", grant.Workspaces)
	}
	return &Connection{Client: client, Channel: ch, Grant: grant}, nil
}

// deniedWhy says in words why a status was not granted.
func deniedWhy(status string) string {
	switch status {
	case StatusNone:
		return "Remote Work is not turned on for any workspace"
	case StatusAmbiguous:
		return "several workspaces have Remote Work on; choose one"
	case StatusUnknown:
		return "that workspace does not have Remote Work on"
	}
	return status
}
