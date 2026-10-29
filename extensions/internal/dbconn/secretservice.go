package dbconn

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/rm4n0s/errors"
)

// The names of the Secret Service API (https://specifications.freedesktop.org/secret-service/latest/).
const (
	ssBusName        = "org.freedesktop.secrets"
	ssServicePath    = dbus.ObjectPath("/org/freedesktop/secrets")
	ssServiceIface   = "org.freedesktop.Secret.Service"
	ssCollectionIf   = "org.freedesktop.Secret.Collection"
	ssItemIface      = "org.freedesktop.Secret.Item"
	ssPromptIface    = "org.freedesktop.Secret.Prompt"
	ssSessionIface   = "org.freedesktop.Secret.Session"
	ssNoObject       = dbus.ObjectPath("/")
	ssItemLabel      = "org.freedesktop.Secret.Item.Label"
	ssItemAttributes = "org.freedesktop.Secret.Item.Attributes"
	ssPropLocked     = "Locked"
)

// The attributes every secret of Emetos is stored with, besides the ID of the connection: the keyring's own tools and
// Emetos itself find the secrets by them.
const (
	attrApplication = "application"
	attrKind        = "kind"
	attrConnection  = "connection"
	// applicationName and secretKind are their values.
	applicationName = "emetos"
	secretKind      = "database-connection"
)

// DefaultPromptTimeout is how long SecretService waits for the user to answer the prompt that unlocks the keyring.
const DefaultPromptTimeout = 2 * time.Minute

// secretStruct is the Secret of the API: a struct (oayays) of the session, the parameters (the IV), the value and its type.
type secretStruct struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

// SecretService is the Keyring of the desktop: the Secret Service API on the session bus, which GNOME Keyring, KWallet and
// KeePassXC answer. Every call blocks until the service answers (and, for a locked keyring, until the user answers the prompt of
// the service), so it is called from a goroutine, never from the UI thread. The secrets cross the bus encrypted with the
// service's key exchange (AES-128-CBC over a Diffie-Hellman key) when the service has it, and as plain text, as the API allows,
// when it does not.
//
// The zero value does not work: make one with NewSecretService.
type SecretService struct {
	// Dial opens a connection to the bus; the default connects to the session bus of the user. Each operation opens a
	// connection of its own and closes it, so nothing is shared between calls. A test passes a Dial to a private bus: under
	// `go test` the default refuses, so that no test reaches the user's own keyring.
	Dial func(ctx context.Context) (*dbus.Conn, error)
	// PromptTimeout is how long the call waits for the user at a prompt; DefaultPromptTimeout when zero.
	PromptTimeout time.Duration
	// PromptWindow is the window id handed to the service's prompt (an X11 window id, "" when there is none).
	PromptWindow string
	// NoEncryption makes the client offer only the plain algorithm: for the tests of that path.
	NoEncryption bool
}

var _ Keyring = (*SecretService)(nil)

// NewSecretService makes the keyring of the user's session bus.
func NewSecretService() *SecretService { return &SecretService{} }

// ssConn is one operation: its connection to the bus and its session with the service.
type ssConn struct {
	s       *SecretService
	conn    *dbus.Conn
	session dbus.ObjectPath
	// key is the AES key of the session, nil for the plain algorithm.
	key []byte
}

// openConn connects to the bus and opens a session with the service. The errors have the tags KeyringTestBusRefused,
// KeyringBusUnavailable, KeyringServiceFailed, KeyringSessionFailed and KeyringCryptoFailed.
func (s *SecretService) openConn(ctx context.Context) (*ssConn, error) {
	var conn *dbus.Conn
	var err error
	switch {
	case s.Dial != nil:
		conn, err = s.Dial(ctx)
	case testing.Testing():
		return nil, errors.New("KeyringTestBusRefused", "a test must not talk to the session bus of the user: give the SecretService a Dial to a private bus")
	default:
		conn, err = dbus.ConnectSessionBus(dbus.WithContext(ctx))
	}
	if err != nil {
		return nil, errors.NewErr("KeyringBusUnavailable", err)
	}
	c := &ssConn{s: s, conn: conn}
	if err := c.openSession(ctx); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

// close ends the session and the connection. A failure is only logged: the operation is over, and the service drops a session
// whose connection went away.
func (c *ssConn) close() {
	if c.session != "" {
		if err := c.conn.Object(ssBusName, c.session).Call(ssSessionIface+".Close", 0).Err; err != nil {
			slog.Debug("the Secret Service session did not close", "err", err)
		}
	}
	if err := c.conn.Close(); err != nil {
		slog.Debug("the connection to the bus did not close", "err", err)
	}
}

// call calls a method of an object of the service and stores the results in out. The error has the tag KeyringServiceFailed.
func (c *ssConn) call(ctx context.Context, path dbus.ObjectPath, method string, out []any, args ...any) error {
	call := c.conn.Object(ssBusName, path).CallWithContext(ctx, method, 0, args...)
	if call.Err != nil {
		return errors.NewErr("KeyringServiceFailed", call.Err).SetMetadata("method", method)
	}
	if len(out) > 0 {
		if err := call.Store(out...); err != nil {
			return errors.NewErr("KeyringServiceFailed", err).SetMetadata("method", method, "step", "results")
		}
	}
	return nil
}

// openSession negotiates the session: the encrypted one when the service has it, the plain one otherwise.
func (c *ssConn) openSession(ctx context.Context) error {
	if !c.s.NoEncryption {
		pair, err := newDHKeyPair()
		if err != nil {
			return err
		}
		var out dbus.Variant
		var path dbus.ObjectPath
		if err := c.call(ctx, ssServicePath, ssServiceIface+".OpenSession", []any{&out, &path}, algorithmDH, dbus.MakeVariant(pair.public)); err == nil {
			server, ok := out.Value().([]byte)
			if !ok {
				return errors.New("KeyringSessionFailed", "the service answered the key exchange with something that is not a key")
			}
			key, err := pair.sessionKey(server)
			if err != nil {
				return err
			}
			c.session, c.key = path, key
			return nil
		}
		// The service may not know the algorithm; the plain session below tells whether it answers at all.
	}
	var out dbus.Variant
	var path dbus.ObjectPath
	if err := c.call(ctx, ssServicePath, ssServiceIface+".OpenSession", []any{&out, &path}, algorithmPlain, dbus.MakeVariant("")); err != nil {
		return err
	}
	c.session = path
	return nil
}

// defaultCollection is the collection of the alias "default" (the login keyring). The error has the tag KeyringNoCollection.
func (c *ssConn) defaultCollection(ctx context.Context) (dbus.ObjectPath, error) {
	var path dbus.ObjectPath
	if err := c.call(ctx, ssServicePath, ssServiceIface+".ReadAlias", []any{&path}, "default"); err != nil {
		return "", err
	}
	if path == "" || path == ssNoObject {
		return "", errors.New("KeyringNoCollection", "the keyring has no default collection to keep the password in")
	}
	return path, nil
}

// isLocked reads the Locked property of a collection or an item.
func (c *ssConn) isLocked(ctx context.Context, path dbus.ObjectPath, iface string) (bool, error) {
	var v dbus.Variant
	if err := c.call(ctx, path, "org.freedesktop.DBus.Properties.Get", []any{&v}, iface, ssPropLocked); err != nil {
		return false, err
	}
	locked, _ := v.Value().(bool)
	return locked, nil
}

// unlock unlocks the objects; when the service wants the user to say yes, it shows its prompt and the call waits for the
// answer. The errors have the tags KeyringServiceFailed, KeyringLocked, KeyringPromptFailed.
func (c *ssConn) unlock(ctx context.Context, objects ...dbus.ObjectPath) error {
	if len(objects) == 0 {
		return nil
	}
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := c.call(ctx, ssServicePath, ssServiceIface+".Unlock", []any{&unlocked, &prompt}, objects); err != nil {
		return err
	}
	if prompt == ssNoObject || prompt == "" {
		return nil
	}
	dismissed, err := c.runPrompt(ctx, prompt)
	if err != nil {
		return err
	}
	if dismissed {
		return errors.New("KeyringLocked", "the keyring stayed locked: the prompt was dismissed")
	}
	return nil
}

// runPrompt shows the prompt of the service and waits for its Completed signal; it reports whether the user dismissed it. The
// errors have the tags KeyringServiceFailed and KeyringPromptFailed (the wait ended before an answer).
func (c *ssConn) runPrompt(ctx context.Context, prompt dbus.ObjectPath) (dismissed bool, err error) {
	timeout := c.s.PromptTimeout
	if timeout <= 0 {
		timeout = DefaultPromptTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	signals := make(chan *dbus.Signal, 8)
	c.conn.Signal(signals)
	defer c.conn.RemoveSignal(signals)
	if err := c.conn.AddMatchSignalContext(ctx, dbus.WithMatchObjectPath(prompt), dbus.WithMatchInterface(ssPromptIface), dbus.WithMatchMember("Completed")); err != nil {
		return false, errors.NewErr("KeyringServiceFailed", err).SetMetadata("method", "AddMatch")
	}
	if err := c.call(ctx, prompt, ssPromptIface+".Prompt", nil, c.s.PromptWindow); err != nil {
		return false, err
	}
	for {
		select {
		case <-ctx.Done():
			// ask the service to take the prompt away; the user may be looking at it
			dismiss := c.conn.Object(ssBusName, prompt).Call(ssPromptIface+".Dismiss", 0)
			if dismiss.Err != nil {
				slog.Debug("the prompt of the keyring did not dismiss", "err", dismiss.Err)
			}
			return false, errors.NewErr("KeyringPromptFailed", ctx.Err())
		case sig := <-signals:
			if sig == nil || sig.Path != prompt || sig.Name != ssPromptIface+".Completed" || len(sig.Body) < 1 {
				continue
			}
			dismissed, _ := sig.Body[0].(bool)
			return dismissed, nil
		}
	}
}

// attributes are the attributes the secret of a connection is stored and searched with.
func attributes(connectionID string) map[string]string {
	return map[string]string{attrApplication: applicationName, attrKind: secretKind, attrConnection: connectionID}
}

// search finds the items of the connection: the unlocked ones and the locked ones.
func (c *ssConn) search(ctx context.Context, connectionID string) (unlocked, locked []dbus.ObjectPath, err error) {
	if err := c.call(ctx, ssServicePath, ssServiceIface+".SearchItems", []any{&unlocked, &locked}, attributes(connectionID)); err != nil {
		return nil, nil, err
	}
	return unlocked, locked, nil
}

// Store implements Keyring. The errors have the tags KeyringTestBusRefused, KeyringBusUnavailable, KeyringServiceFailed,
// KeyringSessionFailed, KeyringCryptoFailed, KeyringNoCollection, KeyringLocked and KeyringPromptFailed.
func (s *SecretService) Store(ctx context.Context, connectionID, label, secret string) error {
	c, err := s.openConn(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	coll, err := c.defaultCollection(ctx)
	if err != nil {
		return err
	}
	locked, err := c.isLocked(ctx, coll, ssCollectionIf)
	if err != nil {
		return err
	}
	if locked {
		if err := c.unlock(ctx, coll); err != nil {
			return err
		}
	}
	sec := secretStruct{Session: c.session, ContentType: "text/plain; charset=utf8"}
	if c.key != nil {
		iv, ct, err := encryptSecret(c.key, []byte(secret))
		if err != nil {
			return err
		}
		sec.Parameters, sec.Value = iv, ct
	} else {
		sec.Parameters, sec.Value = []byte{}, []byte(secret)
	}
	props := map[string]dbus.Variant{
		ssItemLabel:      dbus.MakeVariant(label),
		ssItemAttributes: dbus.MakeVariant(attributes(connectionID)),
	}
	var item, prompt dbus.ObjectPath
	if err := c.call(ctx, coll, ssCollectionIf+".CreateItem", []any{&item, &prompt}, props, sec, true); err != nil {
		return err
	}
	if prompt != ssNoObject && prompt != "" {
		dismissed, err := c.runPrompt(ctx, prompt)
		if err != nil {
			return err
		}
		if dismissed {
			return errors.New("KeyringLocked", "the keyring did not keep the password: the prompt was dismissed")
		}
	}
	return nil
}

// Lookup implements Keyring. The errors are those of Store, and KeyringSecretInvalid for a secret the service sent in a form
// that cannot be read.
func (s *SecretService) Lookup(ctx context.Context, connectionID string) (string, bool, error) {
	c, err := s.openConn(ctx)
	if err != nil {
		return "", false, err
	}
	defer c.close()
	unlocked, locked, err := c.search(ctx, connectionID)
	if err != nil {
		return "", false, err
	}
	if len(unlocked) == 0 && len(locked) > 0 {
		if err := c.unlock(ctx, locked...); err != nil {
			return "", false, err
		}
		unlocked = locked
	}
	if len(unlocked) == 0 {
		return "", false, nil
	}
	var sec secretStruct
	if err := c.call(ctx, unlocked[0], ssItemIface+".GetSecret", []any{&sec}, c.session); err != nil {
		return "", false, err
	}
	if c.key == nil {
		return string(sec.Value), true, nil
	}
	plain, err := decryptSecret(c.key, sec.Parameters, sec.Value)
	if err != nil {
		return "", false, err
	}
	return string(plain), true, nil
}

// Delete implements Keyring. The errors are those of Store.
func (s *SecretService) Delete(ctx context.Context, connectionID string) error {
	c, err := s.openConn(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	unlocked, locked, err := c.search(ctx, connectionID)
	if err != nil {
		return err
	}
	if len(locked) > 0 {
		if err := c.unlock(ctx, locked...); err != nil {
			return err
		}
	}
	for _, item := range append(unlocked, locked...) {
		var prompt dbus.ObjectPath
		if err := c.call(ctx, item, ssItemIface+".Delete", []any{&prompt}); err != nil {
			return err
		}
		if prompt != ssNoObject && prompt != "" {
			dismissed, err := c.runPrompt(ctx, prompt)
			if err != nil {
				return err
			}
			if dismissed {
				return errors.New("KeyringLocked", "the keyring did not delete the password: the prompt was dismissed")
			}
		}
	}
	return nil
}
