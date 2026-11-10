package sshlogin

import (
	"os"
	"testing"
)

// shortTempDir is a private folder with a short path, for a Unix socket: the address of one holds 104 bytes on macOS, and
// t.TempDir's folder (named after the test, under /var/folders/...) is longer.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ssl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
