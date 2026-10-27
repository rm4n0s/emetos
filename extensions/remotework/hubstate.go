package remotework

import (
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

// SessionInfo is a live session as the page shows it.
type SessionInfo struct {
	// Key is the name of the key (its comment, or its fingerprint) and Fingerprint its fingerprint.
	Key, Fingerprint string
	// Remote is the address of the client and Started when the session began.
	Remote  string
	Started time.Time
	session *Session
}

// Snapshot is what the page shows for one workspace.
type Snapshot struct {
	// Config is the configuration (a copy) and Keys its keys, parsed.
	Config Config
	Keys   []sshlogin.AllowedKey
	// On says that the workspace has Remote Work on.
	On bool
	// Listening says that the SSH listener is open, and Addr where.
	Listening bool
	Addr      string
	// ListenErr is why the listener could not be opened, LoadErr why remote-work.json is not used and HostKeyErr why the host key
	// cannot be read; empty when all is well.
	ListenErr, LoadErr, HostKeyErr string
	// HostFingerprint is the fingerprint of the host key (SHA256:...), or empty while there is none.
	HostFingerprint string
	// Sessions are the live sessions of the workspace.
	Sessions []SessionInfo
}

// Snapshot reads the state for the workspace with the folder. It makes nothing: the host key is made by EnsureHostKey.
func (h *Hub) Snapshot(folder string) Snapshot {
	folder = filepath.Clean(folder)
	h.mu.Lock()
	h.load()
	snap := Snapshot{Config: h.cfg.clone(), Keys: slices.Clone(h.keys), On: folder != "." && h.cfg.On(folder), Listening: h.srv != nil}
	if h.addr != nil {
		snap.Addr = h.addr.String()
	}
	if h.listenErr != nil {
		snap.ListenErr = h.listenErr.Error()
	}
	if h.loadErr != nil {
		snap.LoadErr = h.loadErr.Error()
	}
	signer, dir := h.hostKey, h.dir
	for s := range h.sessions {
		if s.folder == folder {
			snap.Sessions = append(snap.Sessions, SessionInfo{Key: s.Key.Name(), Fingerprint: s.Key.Fingerprint(), Remote: s.Remote, Started: s.Started, session: s})
		}
	}
	h.mu.Unlock()
	slices.SortFunc(snap.Sessions, func(a, b SessionInfo) int { return a.Started.Compare(b.Started) })
	if _, err := os.Stat(filepath.Join(dir, hostKeyFileName)); signer == nil && dir != "" && err == nil {
		var keyErr error
		if signer, keyErr = h.HostKey(); keyErr != nil {
			snap.HostKeyErr = keyErr.Error()
		}
	}
	if signer != nil {
		snap.HostFingerprint = ssh.FingerprintSHA256(signer.PublicKey())
	}
	return snap
}

// EnsureHostKey makes the host key if there is none yet and returns its fingerprint. The page calls it when it opens, so that the
// user can read the fingerprint before the first client asks for it.
func (h *Hub) EnsureHostKey() (string, error) {
	h.mu.Lock()
	h.load()
	unusable := h.loadErr != nil
	h.mu.Unlock()
	if unusable {
		return "", h.unusable()
	}
	s, err := h.HostKey()
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(s.PublicKey()), nil
}

// unusable is the error of a change while remote-work.json cannot be used.
func (h *Hub) unusable() error {
	h.mu.Lock()
	cause := ""
	if h.loadErr != nil {
		cause = h.loadErr.Error()
	}
	h.mu.Unlock()
	return errors.New("RemoteWorkConfigUnusable", "remote-work.json cannot be used, so nothing is changed until it is fixed: "+cause)
}

// mutate changes the configuration and writes it; nothing is kept when the file cannot be written.
func (h *Hub) mutate(change func(c *Config) error) error {
	h.mu.Lock()
	h.load()
	if h.loadErr != nil {
		h.mu.Unlock()
		return h.unusable()
	}
	next := h.cfg.clone()
	if err := change(&next); err != nil {
		h.mu.Unlock()
		return err
	}
	if err := saveConfig(h.dir, next); err != nil {
		h.mu.Unlock()
		return err
	}
	h.cfg, h.keys = next, next.AllowedKeys()
	h.mu.Unlock()
	return nil
}

// SetWorkspace turns Remote Work on or off for the workspace with the folder. Turning it on makes the host key if it is missing
// and opens the listener; turning it off ends the sessions of the workspace and closes the listener when no workspace is left.
func (h *Hub) SetWorkspace(folder string, on bool) error {
	folder = filepath.Clean(folder)
	if on {
		if _, err := h.EnsureHostKey(); err != nil {
			return err
		}
	}
	err := h.mutate(func(c *Config) error {
		if !filepath.IsAbs(folder) {
			return errors.New("RemoteWorkSettingInvalid", "only a workspace with a folder can have Remote Work on", "folder", folder)
		}
		has := slices.Contains(c.Workspaces, folder)
		switch {
		case on && !has:
			c.Workspaces = append(c.Workspaces, folder)
			slices.Sort(c.Workspaces)
		case !on && has:
			c.Workspaces = slices.DeleteFunc(c.Workspaces, func(w string) bool { return w == folder })
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !on {
		h.endWhere(func(s *Session) bool { return s.folder == folder }, reasonOff, "Remote Work was turned off for the workspace")
	}
	h.reconcile()
	h.changed()
	return nil
}

// SetPort sets the port of the listener (the listener moves to it at once, and the sessions end). It fails with
// RemoteWorkSettingInvalid.
func (h *Hub) SetPort(port int) error {
	if err := CheckPort(port); err != nil {
		return err
	}
	if err := h.mutate(func(c *Config) error { c.Port = port; return nil }); err != nil {
		return err
	}
	h.reconcile()
	h.changed()
	return nil
}

// SetIdleMinutes sets the idle timeout of the sessions that start or have traffic from now on. It fails with
// RemoteWorkSettingInvalid.
func (h *Hub) SetIdleMinutes(minutes int) error {
	if err := CheckIdle(minutes); err != nil {
		return err
	}
	if err := h.mutate(func(c *Config) error { c.IdleMinutes = minutes; return nil }); err != nil {
		return err
	}
	h.changed()
	return nil
}

// AddKey adds the key of an authorized_keys line to the list. It fails with the tags of sshlogin.ParseAuthorizedKey and with
// RemoteWorkKeyDuplicate and RemoteWorkTooManyKeys.
func (h *Hub) AddKey(line string) (sshlogin.AllowedKey, error) {
	key, err := sshlogin.ParseAuthorizedKey(line)
	if err != nil {
		return sshlogin.AllowedKey{}, err
	}
	err = h.mutate(func(c *Config) error {
		for _, e := range c.Keys {
			if k, err := sshlogin.ParseAuthorizedKey(e.Line); err == nil && k.Equal(key.Key) {
				return errors.New("RemoteWorkKeyDuplicate", "this key is in the list already", "fingerprint", key.Fingerprint())
			}
		}
		if len(c.Keys) >= MaxKeys {
			return errors.New("RemoteWorkTooManyKeys", "the list holds at most 256 keys", "keys", len(c.Keys))
		}
		c.Keys = append(c.Keys, KeyEntry{Line: key.Line(), Added: h.opts.Now().UTC().Format(time.RFC3339)})
		return nil
	})
	if err != nil {
		return sshlogin.AllowedKey{}, err
	}
	h.changed()
	return key, nil
}

// RemoveKey takes the key with the fingerprint out of the list and ends the sessions it logged in. It fails with
// RemoteWorkKeyUnknown.
func (h *Hub) RemoveKey(fingerprint string) error {
	err := h.mutate(func(c *Config) error {
		n := len(c.Keys)
		c.Keys = slices.DeleteFunc(c.Keys, func(e KeyEntry) bool {
			k, err := sshlogin.ParseAuthorizedKey(e.Line)
			return err == nil && k.Fingerprint() == fingerprint
		})
		if len(c.Keys) == n {
			return errors.New("RemoteWorkKeyUnknown", "there is no such key in the list", "fingerprint", fingerprint)
		}
		return nil
	})
	if err != nil {
		return err
	}
	h.endWhere(func(s *Session) bool { return s.Key.Fingerprint() == fingerprint }, reasonHost, "the key was removed from the list")
	h.changed()
	return nil
}

// endWhere ends the sessions that match.
func (h *Hub) endWhere(match func(*Session) bool, reason Reason, text string) {
	h.mu.Lock()
	var ended []*Session
	for s := range h.sessions {
		if match(s) {
			ended = append(ended, s)
		}
	}
	h.mu.Unlock()
	for _, s := range ended {
		s.End(reason, text)
	}
}

// Disconnect ends a session of the page's list, as the banner's button does.
func (s SessionInfo) Disconnect() {
	if s.session != nil {
		s.session.End(reasonHost, "the user of the host pressed Disconnect")
	}
}
