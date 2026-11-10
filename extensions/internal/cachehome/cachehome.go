// Package cachehome finds the folder that holds the caches of the programs of the user. It is $XDG_CACHE_HOME when that is set,
// and $HOME/.cache otherwise, on every system: os.UserCacheDir does the same on Linux but answers ~/Library/Caches on macOS and
// ignores XDG_CACHE_HOME there, which the tests set to keep Emetos away from the folders of the user, and which Emetos's own
// storage (internal/core/storage) honors on every system.
package cachehome

import (
	"os"
	"path/filepath"
)

// Dir is the cache folder. ok is false when it cannot be known: XDG_CACHE_HOME is not an absolute path, or it is unset and HOME
// is not set.
func Dir() (dir string, ok bool) {
	if dir = os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return dir, filepath.IsAbs(dir)
	}
	home := os.Getenv("HOME")
	if home == "" {
		return "", false
	}
	return filepath.Join(home, ".cache"), true
}
