package dbconn

import (
	"context"
	"sync"

	"github.com/rm4n0s/errors"
)

// Source is where a password came from.
type Source int

// The sources of a password.
const (
	// SourceNone: there is no password to use (the user is asked, or the connection needs none).
	SourceNone Source = iota
	// SourceEnv: the environment variable the connection names.
	SourceEnv
	// SourceMemory: what the user typed in this session and Emetos keeps until the connection is closed.
	SourceMemory
	// SourceKeyring: the desktop's keyring.
	SourceKeyring
)

// String names the source.
func (s Source) String() string {
	return [...]string{"none", "environment", "memory", "keyring"}[s]
}

// Resolved is a password and where it came from.
type Resolved struct {
	Password string
	Source   Source
}

// Saved says where Save put a password.
type Saved struct {
	// In is SourceKeyring when the keyring kept the password, SourceMemory when it is held in memory for the session only (the user
	// did not ask to save it, or no keyring answered).
	In Source
	// KeyringErr is why the keyring did not keep a password that the user asked to save (nil otherwise): the caller tells the user.
	KeyringErr error
}

// Passwords decides where the password of a connection comes from and where it goes (D24): never to a file of Emetos. It is safe
// for use from several goroutines. The methods that talk to the keyring block until it answers, so they are called from a
// goroutine, not from the UI thread.
type Passwords struct {
	keyring Keyring
	lookup  LookupEnv

	mu     sync.Mutex
	memory map[string]string
}

// NewPasswords makes a Passwords over a keyring and the function that reads the environment (os.LookupEnv). A nil keyring is a
// desktop without one: every password stays in memory.
func NewPasswords(k Keyring, lookup LookupEnv) *Passwords {
	return &Passwords{keyring: k, lookup: lookup, memory: map[string]string{}}
}

// Label is the label a password has in the keyring: "Emetos: <connection name>".
func Label(c Connection) string { return "Emetos: " + c.Name }

// Resolve finds the password of the connection: the environment variable it names, else what the user typed in this session, else
// the keyring when the connection says its password is saved there. Source is SourceNone when there is nothing. The errors are
// ConnectionEnvUnset (the variable is not set) and those of the keyring, which the caller handles by asking the user.
func (p *Passwords) Resolve(ctx context.Context, c Connection) (Resolved, error) {
	if c.PasswordEnv != "" {
		pw, err := ExpandEnv(c.PasswordEnv, p.lookup)
		if err != nil {
			return Resolved{}, err
		}
		return Resolved{Password: pw, Source: SourceEnv}, nil
	}
	p.mu.Lock()
	pw, ok := p.memory[c.ID]
	p.mu.Unlock()
	if ok {
		return Resolved{Password: pw, Source: SourceMemory}, nil
	}
	if c.SavePassword && p.keyring != nil {
		pw, found, err := p.keyring.Lookup(ctx, c.ID)
		if err != nil {
			return Resolved{}, err
		}
		if found {
			return Resolved{Password: pw, Source: SourceKeyring}, nil
		}
	}
	return Resolved{}, nil
}

// Save keeps the password of a connection: in the keyring when the connection asks for it, and in memory when it does not or no
// keyring answers (Saved.KeyringErr says why). It never writes the password anywhere else: there is no file it could go to.
func (p *Passwords) Save(ctx context.Context, c Connection, password string) Saved {
	if !c.SavePassword || p.keyring == nil {
		p.Remember(c.ID, password)
		var res Saved
		res.In = SourceMemory
		if c.SavePassword { // asked to save, and there is no keyring at all
			res.KeyringErr = errors.New("KeyringMissing", "this desktop has no keyring to keep the password in", "id", c.ID)
		}
		return res
	}
	if err := p.keyring.Store(ctx, c.ID, Label(c), password); err != nil {
		p.Remember(c.ID, password)
		return Saved{In: SourceMemory, KeyringErr: err}
	}
	p.Forget(c.ID)
	return Saved{In: SourceKeyring}
}

// Remember holds the password of a connection in memory until Forget.
func (p *Passwords) Remember(connectionID, password string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.memory[connectionID] = password
}

// Forget drops the password held in memory for a connection (when it is closed).
func (p *Passwords) Forget(connectionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.memory, connectionID)
}

// Remembered tells whether a password of the connection is held in memory.
func (p *Passwords) Remembered(connectionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.memory[connectionID]
	return ok
}

// Delete forgets the password of a connection and removes it from the keyring (the connection is deleted, or no longer saves
// its password). The error is the keyring's; the memory is cleared in any case.
func (p *Passwords) Delete(ctx context.Context, connectionID string) error {
	p.Forget(connectionID)
	if p.keyring == nil {
		return nil
	}
	return p.keyring.Delete(ctx, connectionID)
}
