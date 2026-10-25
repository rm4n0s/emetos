package packfs

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestEveryPackIsThere(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	list, err := All()
	if err != nil {
		t.Fatal(err)
	}
	names := Names()
	if len(list) != len(names) || len(names) < 20 {
		t.Fatalf("%d file systems for %d names", len(list), len(names))
	}
	for i, fsys := range list {
		if _, err := fs.Stat(fsys, "package.json"); err != nil {
			t.Errorf("%s: %v", names[i], err)
		}
	}
	// every folder of extensions/ that has assets is listed, so a new language pack is not forgotten
	entries, err := os.ReadDir(filepath.Join(root, "extensions"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(root, "extensions", e.Name(), "assets", "package.json")); err == nil && !slices.Contains(names, e.Name()) {
			t.Errorf("extensions/%s has assets/package.json and is not in Names", e.Name())
		}
	}
	// the slice is a copy
	names[0] = "changed"
	if Names()[0] == "changed" {
		t.Error("Names gave the slice it keeps")
	}
}

func TestFailures(t *testing.T) {
	cases := []struct {
		name  string
		run   func() error
		route string
	}{
		{"a pack that is not there", func() error { _, err := Open("nothing"); return err }, "Open.PackNotFound"},
		{"no repository above the folder", func() error {
			t.Chdir(t.TempDir())
			_, err := Root()
			return err
		}, "Root.RepoRootNotFound"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
