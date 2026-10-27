package remotework

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
	"github.com/rm4n0s/emetos/extensions/internal/statefile"
)

// The defaults and limits of the settings of the page.
const (
	// DefaultPort is the port of the SSH listener (and of the UDP port of step 15.3), and DefaultIdleMinutes the idle timeout.
	DefaultPort        = 47123
	DefaultIdleMinutes = 30
	// MinIdleMinutes and MaxIdleMinutes bound the idle timeout; there is no way to turn it off.
	MinIdleMinutes = 1
	MaxIdleMinutes = 24 * 60
	// MaxKeys is the most keys the list holds and MaxWorkspaces the most workspaces the file names.
	MaxKeys       = 256
	MaxWorkspaces = 256
)

// configFileVersion is the version of remote-work.json, and the file names in the state folder.
const (
	configFileVersion = 1
	configFileName    = "remote-work.json"
	hostKeyFileName   = "remote-work-host-key"
	hostsFileName     = "remote-work-hosts.json"
	maxConfigFile     = 1 << 20
)

// Config is the content of remote-work.json: what the page sets.
type Config struct {
	// Version is the version of the file.
	Version int `json:"version"`
	// Port is the TCP port of the listener.
	Port int `json:"port"`
	// IdleMinutes is the idle timeout of a session in minutes.
	IdleMinutes int `json:"idleMinutes"`
	// Workspaces are the folders of the workspaces that have Remote Work on.
	Workspaces []string `json:"workspaces"`
	// Keys are the allowed keys.
	Keys []KeyEntry `json:"keys"`
}

// KeyEntry is an allowed key in the file: its authorized_keys line and when it was added.
type KeyEntry struct {
	// Line is the authorized_keys line (type, base64 and comment).
	Line string `json:"line"`
	// Added is when the key was added, RFC 3339.
	Added string `json:"added"`
}

// DefaultConfig is the configuration of a machine that never used Remote Work: off everywhere.
func DefaultConfig() Config {
	return Config{Version: configFileVersion, Port: DefaultPort, IdleMinutes: DefaultIdleMinutes, Workspaces: []string{}, Keys: []KeyEntry{}}
}

// IdleTimeout is the idle timeout as a duration.
func (c Config) IdleTimeout() time.Duration { return time.Duration(c.IdleMinutes) * time.Minute }

// On tells whether the workspace with the folder has Remote Work on.
func (c Config) On(folder string) bool { return slices.Contains(c.Workspaces, folder) }

// AllowedKeys parses the keys of the list; the file was checked when it was read, so an entry that does not parse is left out.
func (c Config) AllowedKeys() []sshlogin.AllowedKey {
	out := make([]sshlogin.AllowedKey, 0, len(c.Keys))
	for _, e := range c.Keys {
		if k, err := sshlogin.ParseAuthorizedKey(e.Line); err == nil {
			out = append(out, k)
		}
	}
	return out
}

// clone copies the slices, so a caller cannot change the hub's own.
func (c Config) clone() Config {
	c.Workspaces = slices.Clone(c.Workspaces)
	c.Keys = slices.Clone(c.Keys)
	return c
}

// CheckPort and CheckIdle validate what the page sets. They fail with RemoteWorkSettingInvalid.
func CheckPort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("RemoteWorkSettingInvalid", "the port is a number from 1 to 65535", "port", port)
	}
	return nil
}

// CheckIdle validates an idle timeout in minutes.
func CheckIdle(minutes int) error {
	if minutes < MinIdleMinutes || minutes > MaxIdleMinutes {
		return errors.New("RemoteWorkSettingInvalid", "the idle timeout is from 1 minute to 24 hours", "minutes", minutes)
	}
	return nil
}

// check validates a whole file; a file with anything wrong in it is not used at all, so a damaged file never lets in a key the
// user did not list.
func (c Config) check() error {
	if c.Version != configFileVersion {
		return errors.New("RemoteWorkConfigInvalid", "the file has a version this Emetos does not know", "version", c.Version)
	}
	if err := CheckPort(c.Port); err != nil {
		return errors.New("RemoteWorkConfigInvalid", "the port in the file is not valid", "port", c.Port)
	}
	if err := CheckIdle(c.IdleMinutes); err != nil {
		return errors.New("RemoteWorkConfigInvalid", "the idle timeout in the file is not valid", "minutes", c.IdleMinutes)
	}
	if len(c.Workspaces) > MaxWorkspaces || len(c.Keys) > MaxKeys {
		return errors.New("RemoteWorkConfigInvalid", "the file lists too many workspaces or keys", "workspaces", len(c.Workspaces), "keys", len(c.Keys))
	}
	for _, w := range c.Workspaces {
		if !filepath.IsAbs(w) || filepath.Clean(w) != w {
			return errors.New("RemoteWorkConfigInvalid", "a workspace in the file is not a clean absolute path", "workspace", w)
		}
	}
	seen := map[string]bool{}
	for _, e := range c.Keys {
		k, err := sshlogin.ParseAuthorizedKey(e.Line)
		if err != nil {
			return errors.New("RemoteWorkConfigInvalid", "a key in the file is not valid: "+err.Error(), "line", e.Line)
		}
		if seen[k.Fingerprint()] {
			return errors.New("RemoteWorkConfigInvalid", "a key is listed twice", "fingerprint", k.Fingerprint())
		}
		seen[k.Fingerprint()] = true
	}
	return nil
}

// loadConfig reads remote-work.json from the state folder. A file that does not exist is the default configuration. A file
// that other users can write, is not JSON, or has anything wrong in it is refused with the tags of statefile.Read and
// RemoteWorkConfigInvalid, and Remote Work stays off until the user fixes it.
func loadConfig(dir string) (Config, error) {
	path := filepath.Join(dir, configFileName)
	data, err := statefile.Read(path, maxConfigFile, statefile.Writable)
	if statefile.Missing(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, errors.NewErr("RemoteWorkConfigInvalid", err).SetMetadata("path", path)
	}
	if err := c.check(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// saveConfig writes remote-work.json atomically with mode 0600. It fails with RemoteWorkConfigInvalid for a configuration the
// file could not be read back from, and with StateFileWriteFailed.
func saveConfig(dir string, c Config) error {
	c.Version = configFileVersion
	if c.Workspaces == nil {
		c.Workspaces = []string{}
	}
	if c.Keys == nil {
		c.Keys = []KeyEntry{}
	}
	if err := c.check(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return errors.NewErr("RemoteWorkConfigInvalid", err)
	}
	return statefile.Write(filepath.Join(dir, configFileName), append(data, '\n'))
}
