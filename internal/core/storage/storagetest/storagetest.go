// Package storagetest isolates tests from the real user folders (SPEC §15.1).
package storagetest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/storage"
)

// Isolate points HOME and every XDG variable at fresh folders and returns the
// Paths that result, so a test never reads or writes the user's real
// configuration, state, cache, trash or socket. The folders are removed and the
// variables restored when the test ends. A test that calls Isolate can't run in
// parallel (it uses t.Setenv).
//
// The runtime folder has a short path (under the system temporary folder, not
// t.TempDir), because the path of a Unix socket is limited to about 100 bytes.
func Isolate(t testing.TB) storage.Paths {
	t.Helper()
	base := t.TempDir()
	dir := func(name string) string {
		d := filepath.Join(base, name)
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("storagetest.Isolate: %v", err)
		}
		return d
	}
	runtime, err := os.MkdirTemp("", "emr")
	if err != nil {
		t.Fatalf("storagetest.Isolate: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(runtime); err != nil {
			t.Errorf("storagetest.Isolate: removing %s: %v", runtime, err)
		}
	})
	t.Setenv("HOME", dir("home"))
	t.Setenv("XDG_CONFIG_HOME", dir("config"))
	t.Setenv("XDG_STATE_HOME", dir("state"))
	t.Setenv("XDG_CACHE_HOME", dir("cache"))
	t.Setenv("XDG_DATA_HOME", dir("data"))
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	paths, err := storage.ResolvePaths()
	if err != nil {
		t.Fatalf("storagetest.Isolate: %v", err)
	}
	return paths
}
