package sshlogin

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// DefaultUser is the user name a client logs in with: the server ignores it and the keys decide.
const DefaultUser = "emetos"

// FirstUse is what the user is shown the first time a client meets a host: the fingerprint to compare with the one the
// host's page shows.
type FirstUse struct {
	// Address is host:port as the user typed it.
	Address string
	// Type is the algorithm of the host key and Fingerprint its SHA-256 fingerprint (SHA256:base64).
	Type        string
	Fingerprint string
}

// ConfirmHostFunc asks the user whether to trust a host on first use. It runs on the goroutine that dials, with no
// connection open, and may take as long as the user needs. true pins the key.
type ConfirmHostFunc func(ctx context.Context, host FirstUse) (bool, error)

// HostKeys is what a client needs of its store of pinned host keys: *HostStore, or a fake in a test.
type HostKeys interface {
	// Check compares the key a server showed with the one pinned for the address.
	Check(addr string, key ssh.PublicKey) (Verdict, Pinned, error)
	// Pin trusts the key for the address from now on.
	Pin(addr string, key ssh.PublicKey) error
}

// DialOptions are what Dial needs.
type DialOptions struct {
	// Address is host:port.
	Address string
	// User is the user name; DefaultUser when empty.
	User string
	// Auth proves who the client is.
	Auth *Auth
	// Hosts pins the host keys.
	Hosts HostKeys
	// ConfirmHost is asked on first use; a nil function refuses an unknown host.
	ConfirmHost ConfirmHostFunc
	// HandshakeTimeout is how long the connection and the login may take, the question to the user not counted (15 s).
	HandshakeTimeout time.Duration
}

// Client is a connection that logged in.
type Client struct {
	conn ssh.Conn
	// HostFingerprint is the fingerprint of the host key the connection was made to.
	HostFingerprint string
}

// Dial connects to a host and logs in with the key of the Auth. The key of the host must be the one pinned for the address. On
// first use (nothing pinned) the connection is dropped before any login, the user is asked through ConfirmHost, the key is
// pinned when the answer is yes, and the connection is made again, so the user may take as long as needed and the host never
// waits for a person. A key that differs from the pinned one is refused with SSHLoginHostKeyChanged, whose metadata has both
// fingerprints; nothing is sent to such a host. The errors have the tags SSHLoginAddressInvalid, SSHLoginDialInvalid,
// SSHLoginHostKeyChanged, SSHLoginHostKeyRejected, SSHLoginHostKeyUnknown, SSHLoginConfirmFailed, SSHLoginAuthFailed and
// SSHLoginConnectFailed, and those of HostStore.
func Dial(ctx context.Context, o DialOptions) (*Client, error) {
	if o.Auth == nil || o.Hosts == nil {
		return nil, errors.New("SSHLoginDialInvalid", "a login needs the key to log in with and the store of pinned host keys")
	}
	addr, err := NormalizeAddress(o.Address)
	if err != nil {
		return nil, err
	}
	o.Address = addr
	c, unknown, err := dial(ctx, o)
	if err != nil || unknown == nil {
		return c, err
	}
	if o.ConfirmHost == nil {
		return nil, errors.New("SSHLoginHostKeyRejected", "the host is not known and nothing can ask whether to trust it", "address", addr, "fingerprint", ssh.FingerprintSHA256(unknown))
	}
	ok, err := o.ConfirmHost(ctx, FirstUse{Address: addr, Type: unknown.Type(), Fingerprint: ssh.FingerprintSHA256(unknown)})
	if err != nil {
		return nil, errors.NewErr("SSHLoginConfirmFailed", err).SetMetadata("address", addr)
	}
	if !ok {
		return nil, errors.New("SSHLoginHostKeyRejected", "the host key was not trusted", "address", addr, "fingerprint", ssh.FingerprintSHA256(unknown))
	}
	if err := o.Hosts.Pin(addr, unknown); err != nil {
		return nil, err
	}
	c, unknown, err = dial(ctx, o)
	if err != nil {
		return nil, err
	}
	if unknown != nil {
		return nil, errors.New("SSHLoginHostKeyUnknown", "the host key was pinned but is not found", "address", addr)
	}
	return c, nil
}

// errUnknownHost stops the handshake at the host key of a host that is not pinned.
var errUnknownHost = &errors.Error{Tag: "SSHLoginHostKeyUnknown", Message: "the host is not known"}

// dial makes one connection. When the host is not pinned it returns its key and no client (the connection ended at the host key).
func dial(ctx context.Context, o DialOptions) (*Client, ssh.PublicKey, error) {
	timeout := o.HandshakeTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	user := o.User
	if user == "" {
		user = DefaultUser
	}
	var hostErr error
	var unknown ssh.PublicKey
	var fingerprint string
	cfg := &ssh.ClientConfig{
		User:              user,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(o.Auth.signers...)},
		ClientVersion:     "SSH-2.0-Emetos",
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			verdict, pinned, err := o.Hosts.Check(o.Address, key)
			switch {
			case err != nil:
				hostErr = err
			case verdict == HostChanged:
				hostErr = errors.New("SSHLoginHostKeyChanged", "the key of the host is not the one that was trusted: someone may be answering in its place, or the host was set up again",
					"address", o.Address, "pinned", pinned.Fingerprint, "presented", ssh.FingerprintSHA256(key))
			case verdict == HostUnknown:
				// the connection ends here, and Dial asks the user; the sentinel only stops the handshake
				unknown = key
				hostErr = errUnknownHost
			default:
				fingerprint = pinned.Fingerprint
				return nil
			}
			return hostErr
		},
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", o.Address)
	if err != nil {
		return nil, nil, errors.NewErr("SSHLoginConnectFailed", err).SetMetadata("address", o.Address)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		stop()
		_ = conn.Close()
		return nil, nil, errors.NewErr("SSHLoginConnectFailed", err).SetMetadata("address", o.Address)
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, o.Address, cfg)
	stop()
	if err != nil {
		_ = conn.Close()
		switch {
		case unknown != nil:
			return nil, unknown, nil
		case hostErr != nil:
			return nil, nil, hostErr
		case strings.Contains(err.Error(), "unable to authenticate"):
			return nil, nil, errors.NewErr("SSHLoginAuthFailed", err).SetMetadata("address", o.Address)
		}
		return nil, nil, errors.NewErr("SSHLoginConnectFailed", err).SetMetadata("address", o.Address)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = sc.Close()
		return nil, nil, errors.NewErr("SSHLoginConnectFailed", err).SetMetadata("address", o.Address)
	}
	go ssh.DiscardRequests(reqs)
	go func() {
		for nc := range chans {
			_ = nc.Reject(ssh.Prohibited, "a client offers no channels")
		}
	}()
	return &Client{conn: sc, HostFingerprint: fingerprint}, nil, nil
}

// OpenChannel opens the channel of the type, with data as its open message. The server's refusal has the tag
// SSHLoginChannelRefused. The requests of the server on the channel are discarded.
func (c *Client) OpenChannel(channelType string, data []byte) (ssh.Channel, error) {
	ch, reqs, err := c.conn.OpenChannel(channelType, data)
	if err != nil {
		return nil, errors.NewErr("SSHLoginChannelRefused", err).SetMetadata("type", channelType)
	}
	go ssh.DiscardRequests(reqs)
	return ch, nil
}

// Close ends the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Wait returns when the connection has ended.
func (c *Client) Wait() error { return c.conn.Wait() }
