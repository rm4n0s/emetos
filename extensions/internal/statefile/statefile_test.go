package statefile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want %s", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("error %T (%v) is not an *errors.Error", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("route = %s, want %s", appErr.Route(), route)
	}
}

func TestWriteAndRead(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	path := filepath.Join(dir, "state.json")
	if err := Write(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("two")); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path, 100, Writable)
	if err != nil || string(got) != "two" {
		t.Fatalf("Read = %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, %v; want 0600", info, err)
	}
	if di, err := os.Stat(dir); err != nil || di.Mode().Perm() != 0o700 {
		t.Errorf("folder mode = %v, %v; want 0700", di, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("the folder has %d entries after two writes, want only the file", len(entries))
	}
	if _, err := Read(path, 100, Private); err != nil {
		t.Errorf("a file of mode 0600 failed the Private check: %v", err)
	}
}

func TestReadErrors(t *testing.T) {
	dir := t.TempDir()
	file := func(name, content string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	target := file("target", "x", 0o600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscallMkfifo(fifo); err != nil {
		t.Skipf("no fifo: %v", err)
	}
	tests := []struct {
		name   string
		path   string
		limit  int64
		forbid os.FileMode
		route  string
	}{
		{"missing", filepath.Join(dir, "none"), 10, Writable, "Read.StateFileReadFailed"},
		{"a link", link, 10, Writable, "Read.StateFileReadFailed"},
		{"a folder", dir, 10, Writable, "Read.StateFileNotRegular"},
		{"a fifo", fifo, 10, Writable, "Read.StateFileNotRegular"},
		{"group writable", file("gw", "x", 0o620), 10, Writable, "Read.StateFileUnsafeMode"},
		{"world writable", file("ww", "x", 0o602), 10, Writable, "Read.StateFileUnsafeMode"},
		{"group readable key", file("gr", "x", 0o640), 10, Private, "Read.StateFileUnsafeMode"},
		{"world readable key", file("wr", "x", 0o604), 10, Private, "Read.StateFileUnsafeMode"},
		{"too large", file("big", strings.Repeat("x", 11), 0o600), 10, Writable, "Read.StateFileTooLarge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read(tt.path, tt.limit, tt.forbid)
			wantRoute(t, err, tt.route)
		})
	}
	// readable by others is fine for a state file
	if _, err := Read(file("readable", "x", 0o644), 10, Writable); err != nil {
		t.Errorf("a state file readable by others was refused: %v", err)
	}
	_, err := Read(filepath.Join(dir, "none"), 10, Writable)
	if !Missing(err) {
		t.Errorf("Missing(%v) = false", err)
	}
	if _, err := Read(file("rd", "x", 0o600), 10, Writable); Missing(err) {
		t.Error("Missing is true for no error")
	}
}

func TestWriteErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores folder permissions")
	}
	ro := t.TempDir()
	if err := os.Chmod(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	wantRoute(t, Write(filepath.Join(ro, "sub", "f"), nil), "Write.StateFileWriteFailed")
	wantRoute(t, Write(filepath.Join(ro, "f"), nil), "Write.StateFileWriteFailed")
	// a folder where the file should be: the rename fails and no temporary file stays
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "f"), 0o700); err != nil {
		t.Fatal(err)
	}
	wantRoute(t, Write(filepath.Join(dir, "f"), []byte("x")), "Write.StateFileWriteFailed")
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the failed write left %d entries", len(entries))
	}
}
