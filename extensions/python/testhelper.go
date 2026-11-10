package python

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"github.com/rm4n0s/emetos/extensions/internal/cachehome"
	"os"
	"path/filepath"

	"github.com/rm4n0s/errors"
)

// The helper scripts of the Testing provider (SPEC §10.32 "The providers", Python): a pytest plugin and a unittest runner that print the
// discovery and the results as JSON lines. They are embedded in the extension and written to Emetos's cache folder when a run needs them.

//go:embed helpers/emetos_pytest.py helpers/emetos_unittest.py
var helperFiles embed.FS

// The names of the helper scripts. The pytest plugin is passed with -p, so its name without .py is the module name.
const (
	pytestPlugin    = "emetos_pytest"
	unittestScript  = "emetos_unittest.py"
	helpersFolder   = "python-tests"
	helperFileMode  = 0o600
	helperFolderDir = 0o700
)

// helperDir writes the helper scripts below the cache folder of Emetos, in a folder named after their content (so that two versions of
// Emetos never share a file and a script is never replaced while a run reads it), and returns the folder. A cache folder that cannot be
// found or written fails with PythonTestHelperWriteFailed.
func helperDir() (string, error) {
	cache, ok := cachehome.Dir()
	if !ok {
		return "", errors.New("PythonTestHelperWriteFailed", "the cache folder cannot be found: neither XDG_CACHE_HOME nor HOME is usable")
	}
	names := []string{pytestPlugin + ".py", unittestScript}
	data := make([][]byte, len(names))
	sum := sha256.New()
	for i, name := range names {
		b, err := helperFiles.ReadFile("helpers/" + name)
		if err != nil {
			return "", errors.NewErr("PythonTestHelperWriteFailed", err).SetMetadata("file", name)
		}
		data[i] = b
		sum.Write([]byte(name))
		sum.Write(b)
	}
	dir := filepath.Join(cache, "emetos", helpersFolder, hex.EncodeToString(sum.Sum(nil))[:12])
	if err := os.MkdirAll(dir, helperFolderDir); err != nil {
		return "", errors.NewErr("PythonTestHelperWriteFailed", err).SetMetadata("dir", dir)
	}
	for i, name := range names {
		path := filepath.Join(dir, name)
		if have, err := os.ReadFile(path); err == nil && string(have) == string(data[i]) {
			continue
		}
		tmp, err := os.CreateTemp(dir, name+".*")
		if err != nil {
			return "", errors.NewErr("PythonTestHelperWriteFailed", err).SetMetadata("dir", dir)
		}
		_, werr := tmp.Write(data[i])
		cerr := tmp.Close()
		if werr == nil {
			werr = cerr
		}
		if werr == nil {
			werr = os.Chmod(tmp.Name(), helperFileMode)
		}
		if werr == nil {
			werr = os.Rename(tmp.Name(), path)
		}
		if werr != nil {
			_ = os.Remove(tmp.Name())
			return "", errors.NewErr("PythonTestHelperWriteFailed", werr).SetMetadata("file", path)
		}
	}
	return dir, nil
}
