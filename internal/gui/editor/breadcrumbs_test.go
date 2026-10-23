package editor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestBreadcrumbItems(t *testing.T) {
	type item = BreadcrumbItem
	cases := []struct {
		name       string
		file, root string
		mode       string
		want       []item
	}{
		{"an absolute path", "/home/me/src/main.go", "", "on", []item{
			{"home", "/home", true}, {"me", "/home/me", true}, {"src", "/home/me/src", true}, {"main.go", "/home/me/src/main.go", false},
		}},
		{"a file at the top of the file system", "/main.go", "", "on", []item{{"main.go", "/main.go", false}}},
		{"relative to the root", "/home/me/proj/src/main.go", "/home/me/proj", "on", []item{
			{"proj", "/home/me/proj", true}, {"src", "/home/me/proj/src", true}, {"main.go", "/home/me/proj/src/main.go", false},
		}},
		{"a file directly in the root", "/home/me/proj/main.go", "/home/me/proj/", "on", []item{
			{"proj", "/home/me/proj", true}, {"main.go", "/home/me/proj/main.go", false},
		}},
		{"a file outside the root keeps the whole path", "/etc/hosts", "/home/me/proj", "on", []item{
			{"etc", "/etc", true}, {"hosts", "/etc/hosts", false},
		}},
		{"a sibling folder with the root's name as a prefix", "/home/me/proj2/a.go", "/home/me/proj", "on", []item{
			{"home", "/home", true}, {"me", "/home/me", true}, {"proj2", "/home/me/proj2", true}, {"a.go", "/home/me/proj2/a.go", false},
		}},
		{"the root itself is not a file in it", "/home/me/proj", "/home/me/proj", "on", []item{
			{"home", "/home", true}, {"me", "/home/me", true}, {"proj", "/home/me/proj", false},
		}},
		{"an unclean path", "/home//me/./a/../b.go", "", "on", []item{
			{"home", "/home", true}, {"me", "/home/me", true}, {"b.go", "/home/me/b.go", false},
		}},
		{"only the last", "/home/me/src/main.go", "", "last", []item{{"main.go", "/home/me/src/main.go", false}}},
		{"only the last, with a root", "/home/me/proj/src/main.go", "/home/me/proj", "last", []item{{"main.go", "/home/me/proj/src/main.go", false}}},
		{"off", "/home/me/main.go", "", "off", nil},
		{"an unknown mode", "/home/me/main.go", "", "sometimes", nil},
		{"no path", "", "", "on", nil},
		{"a relative path", "src/main.go", "", "on", nil},
		{"a relative root is ignored", "/a/b.go", "rel", "on", []item{{"a", "/a", true}, {"b.go", "/a/b.go", false}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BreadcrumbItems(tc.file, tc.root, tc.mode)
			if !slices.Equal(got, tc.want) {
				t.Errorf("BreadcrumbItems(%q, %q, %q)\n got %v\nwant %v", tc.file, tc.root, tc.mode, got, tc.want)
			}
		})
	}
}

func TestCompareNames(t *testing.T) {
	cases := []struct {
		a, b string
		want int // the sign
	}{
		{"a", "b", -1},
		{"B", "a", 1},
		{"a", "A", 1}, // only the case differs: by bytes
		{"file2", "file10", -1},
		{"file10", "file2", 1},
		{"file02", "file2", -1}, // the same number: by bytes
		{"file2", "file2", 0},
		{"a", "ab", -1},
		{"ab", "a", 1},
		{"x1y", "x1z", -1},
		{"a9", "a10", -1},
		{"é", "e", 1},
		{"", "a", -1},
	}
	for _, tc := range cases {
		got := compareNames(tc.a, tc.b)
		if (got < 0) != (tc.want < 0) || (got > 0) != (tc.want > 0) {
			t.Errorf("compareNames(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// writeTree makes a folder with files and folders (names ending in / are folders).
func writeTree(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		p := filepath.Join(dir, n)
		if n[len(n)-1] == '/' {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestReadSiblings(t *testing.T) {
	dir := writeTree(t, "b.go", "A.go", "zeta/", "alpha/", "file10.txt", "file2.txt", "Beta/")
	if err := os.Symlink(filepath.Join(dir, "alpha"), filepath.Join(dir, "link-to-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "b.go"), filepath.Join(dir, "link-to-file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSiblings(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
		if e.Path != filepath.Join(dir, e.Name) {
			t.Errorf("%s: path %s", e.Name, e.Path)
		}
	}
	// folders first (a link to a folder is one), then files; each in natural order ignoring case
	want := []string{"alpha", "Beta", "link-to-dir", "zeta", "A.go", "b.go", "broken", "file2.txt", "file10.txt", "link-to-file"}
	if !slices.Equal(names, want) {
		t.Errorf("order\n got %q\nwant %q", names, want)
	}
	for _, e := range got {
		wantDir := slices.Contains([]string{"alpha", "Beta", "link-to-dir", "zeta"}, e.Name)
		if e.Dir != wantDir {
			t.Errorf("%s: Dir = %v", e.Name, e.Dir)
		}
	}
}

func TestReadSiblingsErrors(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name  string
		ctx   context.Context
		dir   string
		route string
	}{
		{"a folder that does not exist", context.Background(), filepath.Join(t.TempDir(), "nope"), "ReadSiblings.BreadcrumbsReadDirFailed"},
		{"a file is not a folder", context.Background(), writeTree(t, "f") + "/f", "ReadSiblings.BreadcrumbsReadDirFailed"},
		{"a cancelled context", cancelled, t.TempDir(), "ReadSiblings.BreadcrumbsReadCancelled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadSiblings(tc.ctx, tc.dir)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if appErr.Metadata["dir"] != tc.dir {
				t.Errorf("metadata %v", appErr.Metadata)
			}
		})
	}
}

func TestBreadcrumbsOptionsFromSettings(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
		want  BreadcrumbsOptions
	}{
		{"the defaults", nil, BreadcrumbsOptions{Enabled: true, FilePath: "on", Icons: true}},
		{"off", map[string]any{"breadcrumbs.enabled": false}, BreadcrumbsOptions{FilePath: "on", Icons: true}},
		{"last, no icons", map[string]any{"breadcrumbs.filePath": "last", "breadcrumbs.icons": false}, BreadcrumbsOptions{Enabled: true, FilePath: "last"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BreadcrumbsOptionsFromSettings(newSettings(t, tc.extra)); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
