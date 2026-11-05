package tutor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rm4n0s/errors"
)

// fakeKeys is a KeyLabeler with fixed answers.
type fakeKeys struct {
	keys   map[string]string
	titles map[string]string
}

func (f fakeKeys) KeyLabel(id string) string { return f.keys[id] }

func (f fakeKeys) CommandTitle(id string) (string, bool) {
	t, ok := f.titles[id]
	return t, ok
}

// lessons is a small lesson folder under "tutor".
func lessons() fstest.MapFS {
	return fstest.MapFS{
		"tutor/TUTOR.md":            {Data: []byte("Press {{key:a.bound}}, then {{key:a.unbound}}; {{key:a.bound}} again.\n")},
		"tutor/practice/notes.md":   {Data: []byte("{{key:b.other}}\n")},
		"tutor/practice/fruits.txt": {Data: []byte("{{key:a.bound}} stays as it is\n")},
	}
}

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), route)
	}
}

func TestPlaceholdersAreListedOnceAndSorted(t *testing.T) {
	got, err := Placeholders(lessons(), "tutor")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.bound", "a.unbound", "b.other"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPlaceholdersOfAMissingRoot(t *testing.T) {
	_, err := Placeholders(lessons(), "nothing")
	wantRoute(t, err, "Placeholders.TutorLessonsUnreadable")
}

func TestExpansions(t *testing.T) {
	keys := fakeKeys{keys: map[string]string{"a.bound": "Ctrl+D"}, titles: map[string]string{"a.unbound": "Do It"}}
	tests := []struct {
		id, want string
	}{
		{"a.bound", "Ctrl+D"},
		{"a.unbound", `(no key: run "Do It" from the Command Palette)`},
		{"b.unknown", `(no key: run "b.unknown" from the Command Palette)`},
	}
	got := Expansions([]string{"a.bound", "a.unbound", "b.unknown"}, keys)
	for _, tt := range tests {
		if got[tt.id] != tt.want {
			t.Errorf("%s: got %q, want %q", tt.id, got[tt.id], tt.want)
		}
	}
}

func TestFolder(t *testing.T) {
	cache := t.TempDir()
	base := Base(cache)
	tests := []struct {
		name  string
		inUse []string
		want  string
	}{
		{"none open", nil, "tutor"},
		{"the first is open", []string{"tutor"}, "tutor-2"},
		{"the first two are open", []string{"tutor", "tutor-2"}, "tutor-3"},
		{"a later one is open", []string{"tutor-2"}, "tutor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Folder(cache, func(dir string) bool { return slices.Contains(tt.inUse, filepath.Base(dir)) })
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(base, tt.want); got != want {
				t.Errorf("got %s, want %s", got, want)
			}
		})
	}
}

func TestFolderWhenEveryOneIsOpen(t *testing.T) {
	_, err := Folder(t.TempDir(), func(string) bool { return true })
	wantRoute(t, err, "Folder.TutorNoFreeFolder")
}

func TestIsPractice(t *testing.T) {
	cache := "/home/ada/.cache"
	tests := []struct {
		path string
		want bool
	}{
		{"/home/ada/.cache/emetos/tutor", true},
		{"/home/ada/.cache/emetos/tutor-12", true},
		{"/home/ada/.cache/emetos/tutor/TUTOR.md", true},
		{"/home/ada/.cache/emetos/tutor-2/practice/fruits.txt", true},
		{"/home/ada/.cache/emetos", false},
		{"/home/ada/.cache/emetos/tutorial", false},
		{"/home/ada/.cache/emetos/other/tutor", false},
		{"/home/ada/projects/tutor", false},
		{"/home/ada/.cache/tutor", false},
	}
	for _, tt := range tests {
		if got := IsPractice(cache, tt.path); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestCopyMakesAFreshCopyWithTheKeys(t *testing.T) {
	cache := t.TempDir()
	dir := filepath.Join(Base(cache), "tutor")
	exp := map[string]string{"a.bound": "Ctrl+D", "a.unbound": "(no key)", "b.other": "F9"}
	// a first copy, changed by the user, and a file of their own
	if err := Copy(context.Background(), lessons(), "tutor", dir, exp); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, MainFile), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the second copy starts clean
	if err := Copy(context.Background(), lessons(), "tutor", dir, exp); err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	tests := []struct {
		file, want string
	}{
		{MainFile, "Press Ctrl+D, then (no key); Ctrl+D again.\n"},
		{"practice/notes.md", "F9\n"},
		// a file that is not Markdown is copied as it is
		{"practice/fruits.txt", "{{key:a.bound}} stays as it is\n"},
	}
	for _, tt := range tests {
		if got := read(tt.file); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.file, got, tt.want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "mine.txt")); !os.IsNotExist(err) {
		t.Errorf("the user's file is still there: %v", err)
	}
}

func TestCopyFailures(t *testing.T) {
	cache := t.TempDir()
	readOnly := filepath.Join(cache, "ro")
	if err := os.MkdirAll(readOnly, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		dir  string
	}{
		{"not a practice folder", context.Background(), filepath.Join(Base(cache), "projects")},
		{"a folder that cannot be written", context.Background(), filepath.Join(readOnly, "emetos", "tutor")},
		{"canceled", canceled, filepath.Join(Base(cache), "tutor")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if os.Geteuid() == 0 && strings.Contains(tt.name, "written") {
				t.Skip("root writes anywhere")
			}
			err := Copy(tt.ctx, lessons(), "tutor", tt.dir, nil)
			wantRoute(t, err, "Copy.TutorCopyFailed")
			if appErr, ok := errors.FromError(err); ok && appErr.Metadata["folder"] != tt.dir {
				t.Errorf("the folder of the error is %v", appErr.Metadata["folder"])
			}
		})
	}
}
