package fileio

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func mustWrite(t *testing.T, path, text string, opts WriteOptions) FileStat {
	t.Helper()
	stat, err := Write(context.Background(), path, text, opts)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	return stat
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// Every encoding writes what Read reads back.
func TestWriteThenReadEveryEncoding(t *testing.T) {
	for _, e := range Encodings() {
		t.Run(e.ID, func(t *testing.T) {
			text := "line one é\nline two\n"
			if e.enc != nil {
				text = sampleFor(t, e)
			}
			path := filepath.Join(t.TempDir(), "f")
			mustWrite(t, path, text, WriteOptions{Encoding: e.ID, BOM: strings.HasPrefix(e.ID, "utf16")})
			res, err := Read(context.Background(), path, ReadOptions{Encoding: e.ID})
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			wantEnc := e.ID
			if res.Text != text || res.Encoding != wantEnc {
				t.Errorf("Read = %q as %s, want %q as %s", res.Text, res.Encoding, text, wantEnc)
			}
			wantBOM := e.ID == UTF8BOM || strings.HasPrefix(e.ID, "utf16")
			if res.HasBOM != wantBOM {
				t.Errorf("HasBOM = %v, want %v", res.HasBOM, wantBOM)
			}
		})
	}
}

func TestWriteBytes(t *testing.T) {
	tests := []struct {
		name string
		text string
		opts WriteOptions
		want string
	}{
		{"default is utf8 with lf", "a\nb\n", WriteOptions{}, "a\nb\n"},
		{"crlf", "a\nb\n", WriteOptions{LineEnding: CRLF}, "a\r\nb\r\n"},
		{"utf8 with a mark", "é", WriteOptions{Encoding: UTF8BOM}, "\xEF\xBB\xBF\xc3\xa9"},
		{"utf16 le with a mark", "a\n", WriteOptions{Encoding: UTF16LE, BOM: true}, "\xFF\xFEa\x00\n\x00"},
		{"utf16 be with a mark", "a", WriteOptions{Encoding: UTF16BE, BOM: true}, "\xFE\xFF\x00a"},
		{"utf16 le without a mark", "a", WriteOptions{Encoding: UTF16LE}, "a\x00"},
		{"utf16 with crlf", "a\n", WriteOptions{Encoding: UTF16LE, LineEnding: CRLF}, "a\x00\r\x00\n\x00"},
		{"the mark flag does nothing for utf8", "a", WriteOptions{BOM: true}, "a"},
		{"windows 1252", "café €", WriteOptions{Encoding: "windows1252"}, "caf\xe9 \x80"},
		{"shift jis", "日本", WriteOptions{Encoding: "shiftjis"}, "\x93\xfa\x96\x7b"},
		{"empty", "", WriteOptions{Encoding: UTF8BOM}, "\xEF\xBB\xBF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f")
			mustWrite(t, path, tt.text, tt.opts)
			if got := string(readBytes(t, path)); got != tt.want {
				t.Errorf("bytes = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteKeepsLineEndingOnRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	mustWrite(t, path, "a\nb\n", WriteOptions{LineEnding: CRLF})
	res, err := Read(context.Background(), path, ReadOptions{})
	if err != nil || res.Text != "a\nb\n" || res.LineEnding != CRLF {
		t.Errorf("Read = %q, %q, %v", res.Text, res.LineEnding, err)
	}
}

func TestWriteLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	mustWrite(t, path, "one", WriteOptions{})
	mustWrite(t, path, "two", WriteOptions{})
	if got := names(t, dir); len(got) != 1 || got[0] != "f" {
		t.Errorf("folder holds %v", got)
	}
	if got := string(readBytes(t, path)); got != "two" {
		t.Errorf("content = %q", got)
	}
}

func TestWriteModes(t *testing.T) {
	dir := t.TempDir()
	for _, mode := range []os.FileMode{0o600, 0o640, 0o755} {
		path := filepath.Join(dir, mode.String())
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		stat := mustWrite(t, path, "new", WriteOptions{})
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode || stat.Mode.Perm() != mode {
			t.Errorf("mode %v became %v (stat says %v)", mode, info.Mode().Perm(), stat.Mode.Perm())
		}
	}
	created := filepath.Join(dir, "created")
	mustWrite(t, created, "x", WriteOptions{})
	if info, _ := os.Stat(created); info.Mode().Perm() != 0o644 {
		t.Errorf("a new file has mode %v, want 0644", info.Mode().Perm())
	}
}

func TestWriteReturnsTheStat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	stat := mustWrite(t, path, "hello", WriteOptions{})
	info, _ := os.Stat(path)
	if stat.Size != 5 || !stat.ModTime.Equal(info.ModTime()) || stat.Mode != info.Mode() {
		t.Errorf("stat = %+v, file says %v %v %v", stat, info.Size(), info.ModTime(), info.Mode())
	}
}

func TestWriteReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("old"), 0o444); err != nil {
		t.Fatal(err)
	}
	_, err := Write(context.Background(), path, "new", WriteOptions{})
	wantTag(t, err, "Write", "FileReadOnly")
	if got := string(readBytes(t, path)); got != "old" {
		t.Errorf("a refused save changed the file to %q", got)
	}
	mustWrite(t, path, "new", WriteOptions{Overwrite: true})
	info, _ := os.Stat(path)
	if string(readBytes(t, path)) != "new" || info.Mode().Perm() != 0o644 {
		t.Errorf("overwrite left %q with mode %v", readBytes(t, path), info.Mode().Perm())
	}
}

func TestWriteFollowsLinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, link, "new", WriteOptions{})
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v %v", info, err)
	}
	if got := string(readBytes(t, target)); got != "new" {
		t.Errorf("target = %q", got)
	}
}

func TestWriteInPlaceWhenFolderIsNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: folder permissions don't apply")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	mustWrite(t, path, "new", WriteOptions{})
	if got := string(readBytes(t, path)); got != "new" {
		t.Errorf("content = %q", got)
	}
	// A new file in that folder can't be made.
	_, err := Write(context.Background(), filepath.Join(dir, "other"), "x", WriteOptions{})
	wantTag(t, err, "Write", "FilePermissionDenied")
}

func TestWriteInPlaceCreatesWhenAllowed(t *testing.T) {
	// The in-place path for a file that doesn't exist yet, reached by making the temporary file fail.
	dir := t.TempDir()
	path := filepath.Join(dir, "new")
	fs := &faultFS{createTempErr: &os.PathError{Op: "open", Path: dir, Err: syscall.EACCES}}
	mustWrite(t, path, "hello", WriteOptions{fs: fs})
	if got := string(readBytes(t, path)); got != "hello" {
		t.Errorf("content = %q", got)
	}
	// And when that fails too, the error says why.
	fs.openErr = &os.PathError{Op: "open", Path: path, Err: syscall.EACCES}
	_, err := Write(context.Background(), filepath.Join(dir, "other"), "x", WriteOptions{fs: fs})
	wantTag(t, err, "Write", "FilePermissionDenied")
	fs.openErr = errNoSpace
	_, err = Write(context.Background(), filepath.Join(dir, "other"), "x", WriteOptions{fs: fs})
	wantTag(t, err, "Write", "FileWriteFailed")
}

func TestWriteInPlaceFailuresAfterOpening(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	permission := &os.PathError{Op: "open", Path: dir, Err: syscall.EACCES}
	for name, fs := range map[string]*faultFS{
		"write": {createTempErr: permission, writeErr: errNoSpace},
		"sync":  {createTempErr: permission, syncErr: errNoSpace},
	} {
		_, err := Write(context.Background(), path, "hello", WriteOptions{fs: fs})
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		wantTag(t, err, "writeInPlace", "FileWriteFailed")
	}
}

// A failure at any point of an atomic save leaves the original as it was and no
// temporary file behind.
func TestWriteFailureLeavesTheOriginal(t *testing.T) {
	failures := map[string]*faultFS{
		"write":      {writeErr: errNoSpace},
		"sync":       {syncErr: errNoSpace},
		"rename":     {renameErr: &os.LinkError{Op: "rename", Err: syscall.EIO}},
		"createtemp": {createTempErr: errNoSpace},
	}
	for name, fs := range failures {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "f")
			if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Write(context.Background(), path, "replacement that is longer", WriteOptions{fs: fs})
			wantTag(t, err, "Write", "FileWriteFailed")
			if got := string(readBytes(t, path)); got != "original" {
				t.Errorf("the original became %q", got)
			}
			if got := names(t, dir); len(got) != 1 {
				t.Errorf("folder holds %v", got)
			}
		})
	}
}

func TestWriteErrors(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	existing := filepath.Join(dir, "existing")
	if err := os.WriteFile(existing, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		ctx  context.Context
		path string
		text string
		opts WriteOptions
		tag  string
	}{
		{"folder", context.Background(), dir, "x", WriteOptions{}, "FileIsDirectory"},
		{"pipe", context.Background(), fifo, "x", WriteOptions{}, "FileNotRegular"},
		{"missing folder", context.Background(), filepath.Join(dir, "no", "such", "f"), "x", WriteOptions{}, "FileNotFound"},
		{"unknown encoding", context.Background(), existing, "x", WriteOptions{Encoding: "klingon"}, "EncodingUnknown"},
		{"character the encoding lacks", context.Background(), existing, "日本", WriteOptions{Encoding: "windows1252"}, "EncodingCannotRepresent"},
		{"character the encoding lacks on line 2", context.Background(), existing, "ok\n日", WriteOptions{Encoding: "iso88591"}, "EncodingCannotRepresent"},
		{"cancelled", cancelled, existing, "x", WriteOptions{}, "WriteCancelled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Write(tt.ctx, tt.path, tt.text, tt.opts)
			wantTag(t, err, "Write", tt.tag)
		})
	}
	if got := string(readBytes(t, existing)); got != "keep" {
		t.Errorf("a refused save changed the file to %q", got)
	}
}

func TestWriteBadPathStat(t *testing.T) {
	_, err := Write(context.Background(), "a\x00b", "x", WriteOptions{})
	wantTag(t, err, "Write", "FileWriteFailed")
}

func TestWriteInCurrentFolder(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	mustWrite(t, "relative.txt", "hi", WriteOptions{})
	if got := string(readBytes(t, filepath.Join(dir, "relative.txt"))); got != "hi" {
		t.Errorf("content = %q", got)
	}
}

func TestWriteIfUnchanged(t *testing.T) {
	tests := []struct {
		name   string
		expect func(cur FileStat) *FileStat
		tag    string
	}{
		{"same stat", func(cur FileStat) *FileStat { return &cur }, ""},
		{"no expectation", func(FileStat) *FileStat { return nil }, ""},
		{"another modification time", func(cur FileStat) *FileStat { cur.ModTime = cur.ModTime.Add(time.Second); return &cur }, "FileModifiedSince"},
		{"another size", func(cur FileStat) *FileStat { cur.Size++; return &cur }, "FileModifiedSince"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f")
			cur := mustWrite(t, path, "first", WriteOptions{})
			_, err := Write(context.Background(), path, "second", WriteOptions{IfUnchanged: tt.expect(cur)})
			if tt.tag == "" {
				if err != nil {
					t.Fatalf("Write: %v", err)
				}
				if got := string(readBytes(t, path)); got != "second" {
					t.Errorf("content = %q", got)
				}
				return
			}
			wantTag(t, err, "prepareTarget", tt.tag)
			if got := string(readBytes(t, path)); got != "first" {
				t.Errorf("a refused write changed the file to %q", got)
			}
		})
	}
}

func TestWriteIfUnchangedLetsAMissingFileBeWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gone")
	stat := FileStat{ModTime: time.Unix(1, 0), Size: 3}
	mustWrite(t, path, "back", WriteOptions{IfUnchanged: &stat})
	if got := string(readBytes(t, path)); got != "back" {
		t.Errorf("content = %q", got)
	}
}

func TestWriteFailIfExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	mustWrite(t, path, "new", WriteOptions{FailIfExists: true}) // nothing there: written
	_, err := Write(context.Background(), path, "again", WriteOptions{FailIfExists: true})
	wantTag(t, err, "prepareTarget", "FileExists")
	if got := string(readBytes(t, path)); got != "new" {
		t.Errorf("a refused write changed the file to %q", got)
	}
}
