package langserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"
)

// The tests make their own folders, programs and environment: nothing here reads the home folder or the PATH of the machine.

// writeFile writes a file (and its folders) under dir and returns its path.
func writeFile(t *testing.T, dir, rel, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil { // the umask must not change the mode
		t.Fatal(err)
	}
	return p
}

// program writes an executable shell script that prints out (with the shell's own echo: the tests set PATH to one folder).
func program(t *testing.T, dir, rel, out string) string {
	t.Helper()
	return writeFile(t, dir, rel, "#!/bin/sh\necho '"+out+"'\n", 0o755)
}

// settingsOf is a settings reader over a map.
func settingsOf(m map[string]any) *Settings {
	return NewSettings(func(key string) (any, bool) {
		v, ok := m[key]
		return v, ok
	})
}

// wantRoute checks the route and the tag of a failure.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

// metadata is a metadata value of an error as text.
func metadata(t *testing.T, err error, key string) string {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	s, _ := appErr.Metadata[key].(string)
	return s
}
