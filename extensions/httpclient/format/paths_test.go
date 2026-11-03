package format

import (
	"context"
	"github.com/rm4n0s/errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("no symbolic links: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "data"), filepath.Join(root, "inside")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		path   string
		tag    string
		inside bool
	}{
		{"relative", "data/x.bin", "", true},
		{"relative that does not exist", "new/deeper/x.bin", "", true},
		{"dot dot inside", "data/../data/x", "", true},
		{"absolute inside", filepath.Join(root, "data", "x"), "", true},
		{"link inside", "inside/x", "", true},
		{"dot dot out", "../x", "HttpPathOutsideWorkspace", false},
		{"dot dot out through a folder", "data/../../x", "HttpPathOutsideWorkspace", false},
		{"absolute outside", filepath.Join(outside, "x"), "HttpPathOutsideWorkspace", false},
		{"link out", "escape/x", "HttpPathOutsideWorkspace", false},
		{"link out, file missing", "escape/new/x", "HttpPathOutsideWorkspace", false},
		{"empty", "", "HttpPathInvalid", false},
		{"nul", "a\x00b", "HttpPathInvalid", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePath(root, tt.path)
			if tt.tag == "" {
				if err != nil {
					t.Fatalf("ResolvePath: %v", err)
				}
				if !strings.HasPrefix(got, root) {
					t.Errorf("ResolvePath = %q, not under %q", got, root)
				}
				return
			}
			wantRoute(t, err, "ResolvePath")
			wantRoute(t, err, tt.tag)
		})
	}
}

func TestCheckPaths(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name string
		req  Request
		tag  string
	}{
		{"no body", Request{}, ""},
		{"binary inside", Request{Body: &Body{Type: BodyBinary, Path: "files/a.bin"}}, ""},
		{"binary outside", Request{Body: &Body{Type: BodyBinary, Path: "../a.bin"}}, "HttpPathOutsideWorkspace"},
		{"binary with a variable is checked when sent", Request{Body: &Body{Type: BodyBinary, Path: "{{dir}}/a.bin"}}, ""},
		{"multipart file outside", Request{Body: &Body{Type: BodyMultipart, Fields: []Pair{{Name: "a", Value: "x"}, {Name: "f", File: "/etc/hostname"}}}}, "HttpPathOutsideWorkspace"},
		{"multipart file inside", Request{Body: &Body{Type: BodyMultipart, Fields: []Pair{{Name: "f", File: "a/b"}}}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckPaths(root, tt.req)
			if tt.tag == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			wantRoute(t, err, tt.tag)
		})
	}
}

func TestFileIO(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "x.request.jsonc")
	if err := WriteFile(ctx, path, "{}\n"); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(ctx, path)
	if err != nil || got != "{}\n" {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", st.Mode().Perm())
	}
	// the mode of a file that exists is kept, and no temporary file stays
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(ctx, path, "[]\n"); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("the folder has %d entries, want 1", len(entries))
	}

	tests := []struct {
		name string
		do   func() error
		tag  string
	}{
		{"missing file", func() error { _, err := ReadFile(ctx, filepath.Join(dir, "none")); return err }, "HttpFileReadFailed"},
		{"a folder", func() error { _, err := ReadFile(ctx, dir); return err }, "HttpFileReadFailed"},
		{"not text", func() error {
			p := filepath.Join(dir, "bin")
			if err := os.WriteFile(p, []byte{0xff, 0xfe}, 0o644); err != nil {
				return err
			}
			_, err := ReadFile(ctx, p)
			return err
		}, "HttpFileNotText"},
		{"too large", func() error {
			p := filepath.Join(dir, "big")
			if err := os.WriteFile(p, make([]byte, MaxFileSize+1), 0o644); err != nil {
				return err
			}
			_, err := ReadFile(ctx, p)
			return err
		}, "HttpFileTooLarge"},
		{"write under a file", func() error { return WriteFile(ctx, filepath.Join(path, "x"), "") }, "HttpFileWriteFailed"},
		{"write over a folder", func() error { return WriteFile(ctx, dir, "") }, "HttpFileWriteFailed"},
		{"read cancelled", func() error {
			c, cancel := context.WithCancel(ctx)
			cancel()
			_, err := ReadFile(c, path)
			return err
		}, "HttpCancelled"},
		{"write cancelled", func() error {
			c, cancel := context.WithCancel(ctx)
			cancel()
			return WriteFile(c, path, "")
		}, "HttpCancelled"},
		{"unencodable value", func() error {
			_, err := Marshal(Request{Checks: []Check{{Subject: "body", Op: "exists", Value: make(chan int)}}})
			return err
		}, "HttpFileEncodeFailed"},
		{"unencodable value in an update", func() error {
			_, err := Update(`{"emetosHttp": 1}`, Request{Checks: []Check{{Subject: "body", Op: "exists", Value: make(chan int)}}})
			return err
		}, "HttpFileEncodeFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantRoute(t, tt.do(), tt.tag)
		})
	}
	_, err = ReadFile(ctx, filepath.Join(dir, "none"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing file is not errors.Is fs.ErrNotExist: %v", err)
	}
}
