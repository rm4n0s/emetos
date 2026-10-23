package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

// appDir is the folder of Emetos inside each XDG base folder.
const appDir = "emetos"

// DefaultProfile is the name of the default profile: its folders are the plain
// emetos/ folders, not one under profiles/ (SPEC §10.12).
const DefaultProfile = ""

// Paths holds the XDG base folders (SPEC §10.10). Every path Emetos reads or
// writes is computed from them, so tests and proofs redirect them with the XDG
// environment variables (see storagetest.Isolate).
type Paths struct {
	// ConfigHome is $XDG_CONFIG_HOME, else ~/.config.
	ConfigHome string
	// StateHome is $XDG_STATE_HOME, else ~/.local/state.
	StateHome string
	// CacheHome is $XDG_CACHE_HOME, else ~/.cache.
	CacheHome string
	// DataHome is $XDG_DATA_HOME, else ~/.local/share.
	DataHome string
	// RuntimeDir is $XDG_RUNTIME_DIR. It is empty when the variable is unset or not
	// absolute: the XDG specification gives it no default.
	RuntimeDir string
}

// ResolvePaths computes Paths from the process environment.
func ResolvePaths() (Paths, error) {
	return PathsFromEnv(os.Getenv)
}

// PathsFromEnv computes Paths from getenv. As the XDG Base Directory
// Specification says, a variable that is empty or holds a relative path is
// ignored. The defaults come from $HOME; when one is needed and $HOME is not an
// absolute path, the error has the tag HomeDirUnknown.
func PathsFromEnv(getenv func(string) string) (Paths, error) {
	pick := func(variable, fallback string) (string, error) {
		if v := getenv(variable); filepath.IsAbs(v) {
			return filepath.Clean(v), nil
		}
		home := getenv("HOME")
		if !filepath.IsAbs(home) {
			return "", errors.New("HomeDirUnknown", "$HOME is not an absolute path, so the default folder is unknown",
				"variable", variable, "home", home)
		}
		return filepath.Join(home, fallback), nil
	}
	var p Paths
	var err error
	if p.ConfigHome, err = pick("XDG_CONFIG_HOME", ".config"); err != nil {
		return Paths{}, err
	}
	if p.StateHome, err = pick("XDG_STATE_HOME", filepath.Join(".local", "state")); err != nil {
		return Paths{}, err
	}
	if p.CacheHome, err = pick("XDG_CACHE_HOME", ".cache"); err != nil {
		return Paths{}, err
	}
	if p.DataHome, err = pick("XDG_DATA_HOME", filepath.Join(".local", "share")); err != nil {
		return Paths{}, err
	}
	if v := getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(v) {
		p.RuntimeDir = filepath.Clean(v)
	}
	return p, nil
}

// ConfigDir is the folder of a profile's settings.json, keybindings.json and
// snippets: $XDG_CONFIG_HOME/emetos for the default profile, else
// $XDG_CONFIG_HOME/emetos/profiles/<profile>.
func (p Paths) ConfigDir(profile string) (string, error) {
	return profileDir(p.ConfigHome, profile)
}

// ThemesDir is the folder of the user's color themes, $XDG_CONFIG_HOME/emetos/themes (SPEC §8.4).
// The themes are shared by the profiles.
func (p Paths) ThemesDir() string {
	return filepath.Join(p.ConfigHome, appDir, "themes")
}

// StateDir is the folder of a profile's UI state: $XDG_STATE_HOME/emetos for the
// default profile, else $XDG_STATE_HOME/emetos/profiles/<profile>.
func (p Paths) StateDir(profile string) (string, error) {
	return profileDir(p.StateHome, profile)
}

// CacheDir is $XDG_CACHE_HOME/emetos.
func (p Paths) CacheDir() string {
	return filepath.Join(p.CacheHome, appDir)
}

// LogsDir is $XDG_STATE_HOME/emetos/logs (SPEC §13).
func (p Paths) LogsDir() string {
	return filepath.Join(p.StateHome, appDir, "logs")
}

// BackupsDir is $XDG_STATE_HOME/emetos/backups, with one folder per window
// inside (SPEC §10.10).
func (p Paths) BackupsDir() string {
	return filepath.Join(p.StateHome, appDir, "backups")
}

// TrashDir is the freedesktop.org home trash, $XDG_DATA_HOME/Trash (SPEC §6.3).
func (p Paths) TrashDir() string {
	return filepath.Join(p.DataHome, "Trash")
}

// SocketPath is the socket of the running instance, $XDG_RUNTIME_DIR/emetos-<uid>.sock
// (SPEC §10.11). It is empty when RuntimeDir is.
func (p Paths) SocketPath(uid int) string {
	if p.RuntimeDir == "" {
		return ""
	}
	return filepath.Join(p.RuntimeDir, fmt.Sprintf("emetos-%d.sock", uid))
}

// profileDir joins the Emetos folder of base with the profile's folder. A profile
// name that is not a single folder name has the tag ProfilePathUnsafe; the rules
// for names (SPEC §10.12) are checked where profiles are created.
func profileDir(base, profile string) (string, error) {
	root := filepath.Join(base, appDir)
	if profile == DefaultProfile {
		return root, nil
	}
	if profile == "." || profile == ".." || strings.ContainsAny(profile, "/\\\x00") {
		return "", errors.New("ProfilePathUnsafe", "the profile name is not a single folder name", "profile", profile)
	}
	return filepath.Join(root, "profiles", profile), nil
}
