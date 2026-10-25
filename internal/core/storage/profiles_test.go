package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
)

// profileFixture is the profiles of an isolated set of folders, with the state store of the default profile.
type profileFixture struct {
	paths    storage.Paths
	state    *storage.StateStore
	clock    *storagetest.Clock
	profiles *storage.Profiles
}

func newProfileFixture(t *testing.T, mutate func(*storage.ProfilesOptions)) *profileFixture {
	t.Helper()
	state, clock, paths := newStore(t, nil)
	opts := storage.ProfilesOptions{State: state}
	if mutate != nil {
		mutate(&opts)
	}
	return &profileFixture{paths: paths, state: state, clock: clock, profiles: storage.NewProfiles(paths, opts)}
}

// put writes a file below the configuration folder of a profile.
func (f *profileFixture) put(t *testing.T, profile, rel, text string) string {
	t.Helper()
	dir, err := f.paths.ConfigDir(profile)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// files lists the files below the configuration folder of a profile, as slash-separated relative paths with
// their text.
func (f *profileFixture) files(t *testing.T, profile string) map[string]string {
	t.Helper()
	dir, err := f.paths.ConfigDir(profile)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			if rel == "profiles" {
				return filepath.SkipDir
			}
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *profileFixture) list(t *testing.T) []string {
	t.Helper()
	list, err := f.profiles.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestValidProfileName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"a", true}, {"Work", true}, {"Work 2", true}, {"a-b_c", true}, {"Ünïcode", true}, {"日本語", true},
		{"0", true}, {"a b c", true}, {"x" + strings.Repeat("y", 63), true},
		{"", false}, {" ", false}, {" a", false}, {"a ", false}, {"a/b", false}, {"a\\b", false}, {"a.b", false},
		{".", false}, {"..", false}, {"tab\t", false}, {"new\nline", false}, {"emoji😀", false}, {"a\x00b", false},
		{"x" + strings.Repeat("y", 64), false}, {"a:b", false}, {"~", false}, {"a,b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := storage.ValidProfileName(tt.name); got != tt.want {
				t.Errorf("ValidProfileName(%q) = %v, want %v", tt.name, got, tt.want)
			}
			err := storage.ValidateName(tt.name)
			if (err == nil) != tt.want {
				t.Errorf("ValidateName(%q) = %v, want ok %v", tt.name, err, tt.want)
			}
			if err != nil && !routeOf(t, err).HasRoute("ProfileNameInvalid") {
				t.Errorf("unexpected failure path: %s", routeOf(t, err).Route())
			}
		})
	}
}

func TestProfileDisplayName(t *testing.T) {
	if got := storage.ProfileDisplayName(storage.DefaultProfile); got != "Default" {
		t.Errorf("the default profile is shown as %q", got)
	}
	if got := storage.ProfileDisplayName("Work"); got != "Work" {
		t.Errorf("Work is shown as %q", got)
	}
}

func TestListHasTheDefaultFirstAndTheRestSortedWithoutRegardToCase(t *testing.T) {
	f := newProfileFixture(t, nil)
	if got := f.list(t); !slices.Equal(got, []string{storage.DefaultProfile}) {
		t.Fatalf("list of a fresh install: %q", got)
	}
	for _, name := range []string{"zeta", "Beta", "alpha", "Gamma", "beta 2", "_x"} {
		if _, err := f.profiles.Create(t.Context(), name, storage.CreateEmpty); err != nil {
			t.Fatal(err)
		}
	}
	// what is not a profile: a file, a folder with a name that is not valid, a leftover of a failed creation
	root := filepath.Join(f.paths.ConfigHome, "emetos", "profiles")
	if err := os.WriteFile(filepath.Join(root, "afile"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bad.name", ".new-123", "Default", "default"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{storage.DefaultProfile, "_x", "alpha", "Beta", "beta 2", "Gamma", "zeta"}
	if got := f.list(t); !slices.Equal(got, want) {
		t.Errorf("list = %q, want %q", got, want)
	}
}

func TestLookupMatchesWithoutRegardToCaseAndTheDefaultHasTwoNames(t *testing.T) {
	f := newProfileFixture(t, nil)
	if _, err := f.profiles.Create(t.Context(), "Work", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"the empty name is the default", "", storage.DefaultProfile, true},
		{"Default", "Default", storage.DefaultProfile, true},
		{"default in lower case", "default", storage.DefaultProfile, true},
		{"DEFAULT", "DEFAULT", storage.DefaultProfile, true},
		{"exact", "Work", "Work", true},
		{"lower case gives the stored name", "work", "Work", true},
		{"upper case gives the stored name", "WORK", "Work", true},
		{"unknown", "Play", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := f.profiles.Lookup(t.Context(), tt.in)
			if err != nil || ok != tt.ok || got != tt.want {
				t.Errorf("Lookup(%q) = %q, %v, %v; want %q, %v", tt.in, got, ok, err, tt.want, tt.ok)
			}
			if f.profiles.Exists(t.Context(), tt.in) != tt.ok {
				t.Errorf("Exists(%q) = %v", tt.in, !tt.ok)
			}
		})
	}
	if path, err := f.profiles.Path("Work"); err != nil || path != filepath.Join(f.paths.ConfigHome, "emetos", "profiles", "Work") {
		t.Errorf("Path(Work) = %q, %v", path, err)
	}
	if path, err := f.profiles.Path("default"); err != nil || path != filepath.Join(f.paths.ConfigHome, "emetos") {
		t.Errorf("Path(default) = %q, %v", path, err)
	}
	if _, err := f.profiles.Path("../x"); err == nil || !routeOf(t, err).HasRoute("ProfilePathUnsafe") {
		t.Errorf("Path of an unsafe name: %v", err)
	}
}

func TestCreateRefusesNamesThatAreNotValidOrAreTaken(t *testing.T) {
	f := newProfileFixture(t, nil)
	if _, err := f.profiles.Create(t.Context(), "Work", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   string
		from storage.CreateSource
		tag  string
	}{
		{"empty", "", storage.CreateEmpty, "ProfileNameInvalid"},
		{"a slash", "a/b", storage.CreateEmpty, "ProfileNameInvalid"},
		{"leading space", " a", storage.CreateEmpty, "ProfileNameInvalid"},
		{"too long", strings.Repeat("a", 65), storage.CreateEmpty, "ProfileNameInvalid"},
		{"the default", "Default", storage.CreateEmpty, "ProfileNameReserved"},
		{"the default in lower case", "default", storage.CreateEmpty, "ProfileNameReserved"},
		{"the default in capitals", "DEFAULT", storage.CreateEmpty, "ProfileNameReserved"},
		{"the same name", "Work", storage.CreateEmpty, "ProfileExists"},
		{"the same name in lower case", "work", storage.CreateEmpty, "ProfileExists"},
		{"the same name in capitals", "WORK", storage.CreateEmpty, "ProfileExists"},
		{"a source that is not there", "New", storage.CreateFrom("Nope"), "ProfileNotFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.profiles.Create(t.Context(), tt.in, tt.from)
			if err == nil {
				t.Fatal("expected an error")
			}
			if appErr := routeOf(t, err); !appErr.HasRoute(tt.tag) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	if got := f.list(t); !slices.Equal(got, []string{storage.DefaultProfile, "Work"}) {
		t.Errorf("the refused names made profiles: %q", got)
	}
}

func TestCreateEmptyMakesAnEmptyFolder(t *testing.T) {
	f := newProfileFixture(t, nil)
	f.put(t, storage.DefaultProfile, "settings.json", `{"a":1}`)
	name, err := f.profiles.Create(t.Context(), "Blank", storage.CreateEmpty)
	if err != nil || name != "Blank" {
		t.Fatalf("Create = %q, %v", name, err)
	}
	if got := f.files(t, "Blank"); len(got) != 0 {
		t.Errorf("an empty profile has files: %v", got)
	}
	dir, _ := f.paths.ConfigDir("Blank")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("the folder of the profile: %v", err)
	}
}

func TestCreateFromACopiesTheFilesOfAProfileAndNothingElse(t *testing.T) {
	f := newProfileFixture(t, nil)
	src := "Source"
	if _, err := f.profiles.Create(t.Context(), src, storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	f.put(t, src, "settings.json", `{"editor.fontSize": 22}`)
	f.put(t, src, "keybindings.json", `[{"key":"ctrl+j","command":"x"}]`)
	f.put(t, src, "snippets/go.json", `{"a":{"prefix":"a","body":"b"}}`)
	f.put(t, src, "snippets/nested/more.code-snippets", `{}`)
	// what is not part of the profile
	f.put(t, src, "themes/mine.json", `{}`)
	f.put(t, src, "unrelated.txt", `x`)
	f.put(t, src, "state.json", `{}`)
	dir, _ := f.paths.ConfigDir(src)
	if err := os.Symlink("/etc/hostname", filepath.Join(dir, "snippets", "link.json")); err != nil {
		t.Fatal(err)
	}
	// the state of the source and its backups are not copied either
	stateDir, _ := f.paths.StateDir(src)
	if err := os.MkdirAll(filepath.Join(stateDir, "windows"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "windows", "0123456789abcdef.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"settings.json":                      `{"editor.fontSize": 22}`,
		"keybindings.json":                   `[{"key":"ctrl+j","command":"x"}]`,
		"snippets/go.json":                   `{"a":{"prefix":"a","body":"b"}}`,
		"snippets/nested/more.code-snippets": `{}`,
	}
	tests := []struct {
		name string
		from storage.CreateSource
	}{
		{"from a profile by name", storage.CreateFrom(src)},
		{"from a profile by name in lower case", storage.CreateFrom("source")},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := []string{"Copy1", "Copy2"}[i]
			if _, err := f.profiles.Create(t.Context(), name, tt.from); err != nil {
				t.Fatal(err)
			}
			got := f.files(t, name)
			if len(got) != len(want) {
				t.Errorf("files %v, want %v", got, want)
			}
			for k, v := range want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
			if _, err := os.Stat(filepath.Join(f.paths.StateHome, "emetos", "profiles", name)); !os.IsNotExist(err) {
				t.Errorf("the new profile has a state folder: %v", err)
			}
		})
	}
	// the source is untouched
	if got := f.files(t, src); len(got) != 8 {
		t.Errorf("the source has %d files: %v", len(got), got)
	}
}

func TestCreateFromTheDefaultProfileLeavesTheProfilesAndThemesOut(t *testing.T) {
	f := newProfileFixture(t, nil)
	f.put(t, storage.DefaultProfile, "settings.json", `{"workbench.colorTheme": "Light Modern"}`)
	f.put(t, storage.DefaultProfile, "snippets/js.json", `{}`)
	f.put(t, storage.DefaultProfile, "themes/mine.json", `{}`)
	if _, err := f.profiles.Create(t.Context(), "Other", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	for i, from := range []string{"", "Default", "default"} {
		name := []string{"FromEmptyName", "FromDefault", "FromLower"}[i]
		if _, err := f.profiles.Create(t.Context(), name, storage.CreateFrom(from)); err != nil {
			t.Fatal(err)
		}
		got := f.files(t, name)
		if len(got) != 2 || got["settings.json"] == "" || got["snippets/js.json"] != "{}" {
			t.Errorf("copy from %q: %v", from, got)
		}
		dir, _ := f.paths.ConfigDir(name)
		if _, err := os.Stat(filepath.Join(dir, "profiles")); !os.IsNotExist(err) {
			t.Errorf("the copy holds the profiles folder: %v", err)
		}
	}
}

func TestCreateLeavesNoHalfMadeProfile(t *testing.T) {
	failing := errors.New("InjectedFailure", "the disk is full")
	tests := []struct {
		name   string
		mutate func(*storage.ProfilesOptions)
		tag    string
	}{
		{"a copy that fails", func(o *storage.ProfilesOptions) {
			n := 0
			o.CopyFile = func(src, dst string) error {
				if n++; n == 2 {
					return failing
				}
				return os.WriteFile(dst, nil, 0o644)
			}
		}, "ProfileCopyFailed"},
		{"a rename into place that fails", func(o *storage.ProfilesOptions) {
			o.Rename = func(oldPath, newPath string) error { return failing }
		}, "ProfileCreateFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newProfileFixture(t, tt.mutate)
			f.put(t, storage.DefaultProfile, "settings.json", `{}`)
			f.put(t, storage.DefaultProfile, "keybindings.json", `[]`)
			_, err := f.profiles.Create(t.Context(), "New", storage.CreateFrom(storage.DefaultProfile))
			if err == nil {
				t.Fatal("expected an error")
			}
			if appErr := routeOf(t, err); !appErr.HasRoute("Profiles.Create") || !appErr.HasRoute(tt.tag) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if got := f.list(t); !slices.Equal(got, []string{storage.DefaultProfile}) {
				t.Errorf("the failed creation left a profile: %q", got)
			}
			entries, rerr := os.ReadDir(filepath.Join(f.paths.ConfigHome, "emetos", "profiles"))
			if rerr != nil {
				t.Fatal(rerr)
			}
			if len(entries) != 0 {
				var names []string
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Errorf("the failed creation left %v in the profiles folder", names)
			}
		})
	}
}

func TestCreateWithACancelledContext(t *testing.T) {
	f := newProfileFixture(t, nil)
	f.put(t, storage.DefaultProfile, "settings.json", `{}`)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := f.profiles.Create(ctx, "New", storage.CreateFrom(""))
	if err == nil {
		t.Fatal("expected an error")
	}
	if f.profiles.Exists(t.Context(), "New") {
		t.Error("a cancelled creation made the profile")
	}
}

func TestEnsureMakesTheProfileOfAnUnknownName(t *testing.T) {
	f := newProfileFixture(t, nil)
	if _, err := f.profiles.Create(t.Context(), "Work", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		in          string
		want        string
		wantCreated bool
		tag         string
	}{
		{"a profile that exists", "work", "Work", false, ""},
		{"the default", "Default", storage.DefaultProfile, false, ""},
		{"an unknown name is made empty", "Fresh", "Fresh", true, ""},
		{"the same name again is not made twice", "fresh", "Fresh", false, ""},
		{"an invalid name", "a/b", "", false, "ProfileNameInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, created, err := f.profiles.Ensure(t.Context(), tt.in)
			if tt.tag != "" {
				if err == nil || !routeOf(t, err).HasRoute("Profiles.Ensure") || !routeOf(t, err).HasRoute(tt.tag) {
					t.Fatalf("Ensure(%q) = %v", tt.in, err)
				}
				return
			}
			if err != nil || got != tt.want || created != tt.wantCreated {
				t.Errorf("Ensure(%q) = %q, %v, %v; want %q, %v", tt.in, got, created, err, tt.want, tt.wantCreated)
			}
		})
	}
}

func TestRename(t *testing.T) {
	f := newProfileFixture(t, nil)
	for _, name := range []string{"Work", "Play"} {
		if _, err := f.profiles.Create(t.Context(), name, storage.CreateEmpty); err != nil {
			t.Fatal(err)
		}
	}
	f.put(t, "Work", "settings.json", `{"a":1}`)
	f.put(t, "Work", "snippets/go.json", `{}`)
	stateDir, _ := f.paths.StateDir("Work")
	if err := os.MkdirAll(filepath.Join(stateDir, "windows"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "windows", "0123456789abcdef.json"), []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := f.profiles.SetLastProfile(ctx, "/work/a", "Work"); err != nil {
		t.Fatal(err)
	}
	if err := f.profiles.SetLastProfile(ctx, "/work/b", "Play"); err != nil {
		t.Fatal(err)
	}
	err := f.state.UpdateGlobal(ctx, func(g *storage.GlobalState) bool {
		g.WorkspaceTabs = []storage.WorkspaceTab{
			{ID: "0123456789abcdef", Folder: "/work/a", Profile: "Work"},
			{ID: "fedcba9876543210", Folder: "/work/b", Profile: "Play"},
			{ID: "aaaaaaaaaaaaaaaa"},
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := f.profiles.Rename(ctx, "work", "Job"); err != nil {
		t.Fatal(err)
	}
	if got := f.list(t); !slices.Equal(got, []string{storage.DefaultProfile, "Job", "Play"}) {
		t.Errorf("list after the rename: %q", got)
	}
	got := f.files(t, "Job")
	if got["settings.json"] != `{"a":1}` || got["snippets/go.json"] != `{}` || len(got) != 2 {
		t.Errorf("the renamed profile has %v", got)
	}
	newState, _ := f.paths.StateDir("Job")
	if _, err := os.Stat(filepath.Join(newState, "windows", "0123456789abcdef.json")); err != nil {
		t.Errorf("the state of the profile did not move: %v", err)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Errorf("the old state folder is still there: %v", err)
	}
	// the records were written (the rename flushes), and the other profile's are as they were
	again, err := storage.NewStateStore(f.paths, storage.DefaultProfile, storage.StateStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	g, err := again.LoadGlobal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if g.LastProfiles["/work/a"] != "Job" || g.LastProfiles["/work/b"] != "Play" || len(g.LastProfiles) != 2 {
		t.Errorf("last profiles: %v", g.LastProfiles)
	}
	profiles := []string{g.WorkspaceTabs[0].Profile, g.WorkspaceTabs[1].Profile, g.WorkspaceTabs[2].Profile}
	if !slices.Equal(profiles, []string{"Job", "Play", ""}) {
		t.Errorf("profiles of the open tabs: %q", profiles)
	}
	if p, ok, err := f.profiles.LastProfile(ctx, "/work/a"); err != nil || !ok || p != "Job" {
		t.Errorf("LastProfile = %q, %v, %v", p, ok, err)
	}
}

func TestRenameChangesOnlyTheCapitalization(t *testing.T) {
	f := newProfileFixture(t, nil)
	if _, err := f.profiles.Create(t.Context(), "work", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	if err := f.profiles.Rename(t.Context(), "work", "Work"); err != nil {
		t.Fatal(err)
	}
	if got := f.list(t); !slices.Equal(got, []string{storage.DefaultProfile, "Work"}) {
		t.Errorf("list: %q", got)
	}
	if err := f.profiles.Rename(t.Context(), "Work", "Work"); err != nil {
		t.Errorf("renaming a profile to its own name: %v", err)
	}
}

func TestRenameFailures(t *testing.T) {
	failing := errors.New("InjectedFailure", "no")
	f := newProfileFixture(t, nil)
	for _, name := range []string{"Work", "Play"} {
		if _, err := f.profiles.Create(t.Context(), name, storage.CreateEmpty); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name     string
		from, to string
		tag      string
	}{
		{"the default cannot be renamed", "Default", "Other", "ProfileDefaultImmutable"},
		{"the empty name is the default", "", "Other", "ProfileDefaultImmutable"},
		{"a profile that is not there", "Nope", "Other", "ProfileNotFound"},
		{"an invalid new name", "Work", "a/b", "ProfileNameInvalid"},
		{"the new name is the default", "Work", "Default", "ProfileNameReserved"},
		{"the new name is another profile in other capitals", "Work", "PLAY", "ProfileExists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.profiles.Rename(t.Context(), tt.from, tt.to)
			if err == nil {
				t.Fatal("expected an error")
			}
			if appErr := routeOf(t, err); !appErr.HasRoute("Profiles.Rename") || !appErr.HasRoute(tt.tag) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	if got := f.list(t); !slices.Equal(got, []string{storage.DefaultProfile, "Play", "Work"}) {
		t.Errorf("list after the failures: %q", got)
	}

	t.Run("a move that fails", func(t *testing.T) {
		g := newProfileFixture(t, func(o *storage.ProfilesOptions) {
			o.Rename = func(oldPath, newPath string) error { return failing }
		})
		if err := os.MkdirAll(filepath.Join(g.paths.ConfigHome, "emetos", "profiles", "Work"), 0o755); err != nil {
			t.Fatal(err)
		}
		err := g.profiles.Rename(t.Context(), "Work", "Job")
		if err == nil || !routeOf(t, err).HasRoute("Profiles.Rename") || !routeOf(t, err).HasRoute("ProfileRenameFailed") {
			t.Fatalf("Rename = %v", err)
		}
		if got := g.list(t); !slices.Equal(got, []string{storage.DefaultProfile, "Work"}) {
			t.Errorf("list: %q", got)
		}
	})
	t.Run("the move of the state fails and the configuration goes back", func(t *testing.T) {
		calls := 0
		g := newProfileFixture(t, func(o *storage.ProfilesOptions) {
			o.Rename = func(oldPath, newPath string) error {
				if calls++; calls == 2 {
					return failing
				}
				return os.Rename(oldPath, newPath)
			}
		})
		if err := os.MkdirAll(filepath.Join(g.paths.ConfigHome, "emetos", "profiles", "Work"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(g.paths.StateHome, "emetos", "profiles", "Work"), 0o700); err != nil {
			t.Fatal(err)
		}
		err := g.profiles.Rename(t.Context(), "Work", "Job")
		if err == nil || !routeOf(t, err).HasRoute("ProfileRenameFailed") {
			t.Fatalf("Rename = %v", err)
		}
		if got := g.list(t); !slices.Equal(got, []string{storage.DefaultProfile, "Work"}) {
			t.Errorf("the configuration did not go back: %q", got)
		}
	})
}

func TestDelete(t *testing.T) {
	f := newProfileFixture(t, nil)
	for _, name := range []string{"Work", "Play"} {
		if _, err := f.profiles.Create(t.Context(), name, storage.CreateEmpty); err != nil {
			t.Fatal(err)
		}
	}
	f.put(t, "Work", "settings.json", `{}`)
	stateDir, _ := f.paths.StateDir("Work")
	if err := os.MkdirAll(filepath.Join(stateDir, "windows"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	for folder, profile := range map[string]string{"/a": "Work", "/b": "Play", "/c": "Work"} {
		if err := f.profiles.SetLastProfile(ctx, folder, profile); err != nil {
			t.Fatal(err)
		}
	}
	err := f.state.UpdateGlobal(ctx, func(g *storage.GlobalState) bool {
		g.WorkspaceTabs = []storage.WorkspaceTab{{ID: "0123456789abcdef", Folder: "/a", Profile: "Work"}, {ID: "fedcba9876543210", Folder: "/b", Profile: "Play"}}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.profiles.Delete(ctx, "WORK"); err != nil {
		t.Fatal(err)
	}
	if got := f.list(t); !slices.Equal(got, []string{storage.DefaultProfile, "Play"}) {
		t.Errorf("list after the delete: %q", got)
	}
	dir, _ := f.paths.ConfigDir("Work")
	for _, p := range []string{dir, stateDir} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", p, err)
		}
	}
	g, err := f.state.LoadGlobal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.LastProfiles) != 1 || g.LastProfiles["/b"] != "Play" {
		t.Errorf("last profiles after the delete: %v", g.LastProfiles)
	}
	if g.WorkspaceTabs[0].Profile != "" || g.WorkspaceTabs[1].Profile != "Play" {
		t.Errorf("profiles of the tabs after the delete: %+v", g.WorkspaceTabs)
	}
	// the record of a deleted profile reads as no record
	if _, ok, err := f.profiles.LastProfile(ctx, "/a"); ok || err != nil {
		t.Errorf("LastProfile(/a) = %v, %v", ok, err)
	}
}

func TestDeleteFailures(t *testing.T) {
	f := newProfileFixture(t, nil)
	if _, err := f.profiles.Create(t.Context(), "Work", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, profile, tag string
	}{
		{"the default", "Default", "ProfileDefaultImmutable"},
		{"the default by its empty name", "", "ProfileDefaultImmutable"},
		{"the default in lower case", "default", "ProfileDefaultImmutable"},
		{"a profile that is not there", "Nope", "ProfileNotFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.profiles.Delete(t.Context(), tt.profile)
			if err == nil || !routeOf(t, err).HasRoute("Profiles.Delete") || !routeOf(t, err).HasRoute(tt.tag) {
				t.Errorf("Delete(%q) = %v", tt.profile, err)
			}
		})
	}
	t.Run("a removal that fails", func(t *testing.T) {
		g := newProfileFixture(t, func(o *storage.ProfilesOptions) {
			o.RemoveAll = func(string) error { return errors.New("InjectedFailure", "busy") }
		})
		if err := os.MkdirAll(filepath.Join(g.paths.ConfigHome, "emetos", "profiles", "Work"), 0o755); err != nil {
			t.Fatal(err)
		}
		err := g.profiles.Delete(t.Context(), "Work")
		if err == nil || !routeOf(t, err).HasRoute("Profiles.Delete") || !routeOf(t, err).HasRoute("ProfileDeleteFailed") {
			t.Fatalf("Delete = %v", err)
		}
		if !g.profiles.Exists(t.Context(), "Work") {
			t.Error("the profile is gone although the removal failed")
		}
	})
}

func TestLastProfilePerFolder(t *testing.T) {
	f := newProfileFixture(t, nil)
	ctx := t.Context()
	for _, name := range []string{"Work", "Play"} {
		if _, err := f.profiles.Create(ctx, name, storage.CreateEmpty); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok, err := f.profiles.LastProfile(ctx, "/home/u/project"); ok || err != nil {
		t.Fatalf("a folder with no record: %v, %v", ok, err)
	}
	if err := f.profiles.SetLastProfile(ctx, "/home/u/project/", "work"); err != nil {
		t.Fatal(err)
	}
	if err := f.profiles.SetLastProfile(ctx, "/home/u/other", "Play"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		folder string
		want   string
		ok     bool
	}{
		{"the record is by the cleaned path", "/home/u/project", "Work", true},
		{"a dot segment is the same folder", "/home/u/./project", "Work", true},
		{"another folder", "/home/u/other", "Play", true},
		{"a folder with no record", "/home/u/none", "", false},
		{"a relative path has none", "project", "", false},
	}
	check := func(t *testing.T, p *storage.Profiles) {
		for _, tt := range tests {
			got, ok, err := p.LastProfile(ctx, tt.folder)
			if err != nil || ok != tt.ok || got != tt.want {
				t.Errorf("%s: LastProfile(%q) = %q, %v, %v; want %q, %v", tt.name, tt.folder, got, ok, err, tt.want, tt.ok)
			}
		}
	}
	check(t, f.profiles)
	// the records are written to the state file and read back by another store
	f.clock.Advance(storage.DefaultStateDelay)
	again, err := storage.NewStateStore(f.paths, storage.DefaultProfile, storage.StateStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("read back", func(t *testing.T) {
		check(t, storage.NewProfiles(f.paths, storage.ProfilesOptions{State: again}))
	})

	// the default profile removes the record; a record of a profile that is gone reads as none
	if err := f.profiles.SetLastProfile(ctx, "/home/u/project", "Default"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := f.profiles.LastProfile(ctx, "/home/u/project"); ok {
		t.Error("the default profile left a record")
	}
	dir, _ := f.paths.ConfigDir("Play")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := f.profiles.LastProfile(ctx, "/home/u/other"); ok {
		t.Error("a profile that is gone is still the last one")
	}

	for _, tt := range []struct {
		name, folder, profile, tag string
	}{
		{"a folder that is not absolute", "rel/dir", "Work", "ProfileFolderInvalid"},
		{"a profile that is not there", "/x", "Nope", "ProfileNotFound"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := f.profiles.SetLastProfile(ctx, tt.folder, tt.profile)
			if err == nil || !routeOf(t, err).HasRoute("Profiles.SetLastProfile") || !routeOf(t, err).HasRoute(tt.tag) {
				t.Errorf("SetLastProfile = %v", err)
			}
		})
	}
}

func TestProfilesWithoutAStateStoreKeepNoRecords(t *testing.T) {
	paths := storagetest.Isolate(t)
	p := storage.NewProfiles(paths, storage.ProfilesOptions{})
	if _, err := p.Create(t.Context(), "Work", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	if err := p.SetLastProfile(t.Context(), "/a", "Work"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := p.LastProfile(t.Context(), "/a"); ok || err != nil {
		t.Errorf("LastProfile = %v, %v", ok, err)
	}
	if err := p.Rename(t.Context(), "Work", "Job"); err != nil {
		t.Fatal(err)
	}
	if err := p.Delete(t.Context(), "Job"); err != nil {
		t.Fatal(err)
	}
}

func TestListFailure(t *testing.T) {
	paths := storagetest.Isolate(t)
	// the profiles folder is a file
	if err := os.MkdirAll(filepath.Join(paths.ConfigHome, "emetos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ConfigHome, "emetos", "profiles"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p := storage.NewProfiles(paths, storage.ProfilesOptions{})
	_, err := p.List(t.Context())
	if err == nil || !routeOf(t, err).HasRoute("Profiles.List.ProfileListFailed") {
		t.Fatalf("List = %v", err)
	}
	if _, _, err := p.Lookup(t.Context(), "x"); err == nil || !routeOf(t, err).HasRoute("ProfileListFailed") {
		t.Errorf("Lookup = %v", err)
	}
	if p.Exists(t.Context(), "x") {
		t.Error("Exists is true although the list fails")
	}
	if _, err := p.Create(t.Context(), "x", storage.CreateEmpty); err == nil {
		t.Error("Create succeeded although the profiles folder is a file")
	}
}

func TestTheStateOfProfilesIsAdditive(t *testing.T) {
	paths := storagetest.Isolate(t)
	dir := filepath.Join(paths.StateHome, "emetos")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		text         string
		wantProfiles map[string]string
		wantWindow   string
	}{
		{"a file from before profiles", `{"version":1,"openWindows":[{"id":"0123456789abcdef","folder":"/a"}],"recent":{}}`, nil, ""},
		{"with the records", `{"version":1,"lastProfiles":{"/a":"Work","/b":"Play"},"openWindows":[{"id":"0123456789abcdef","folder":"/a","profile":"Work"}],"recent":{}}`,
			map[string]string{"/a": "Work", "/b": "Play"}, "Work"},
		{"records that cannot be used are dropped", `{"version":1,"lastProfiles":{"rel":"Work","/a":"bad/name","/b":"Ok","/c":""},"openWindows":[{"id":"0123456789abcdef","profile":"a/b"}],"recent":{}}`,
			map[string]string{"/b": "Ok"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(tt.text), 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := storage.NewStateStore(paths, storage.DefaultProfile, storage.StateStoreOptions{})
			if err != nil {
				t.Fatal(err)
			}
			g, err := s.LoadGlobal(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(g.LastProfiles) != len(tt.wantProfiles) {
				t.Errorf("last profiles %v, want %v", g.LastProfiles, tt.wantProfiles)
			}
			for k, v := range tt.wantProfiles {
				if g.LastProfiles[k] != v {
					t.Errorf("last profile of %s = %q, want %q", k, g.LastProfiles[k], v)
				}
			}
			if len(g.WorkspaceTabs) != 1 || g.WorkspaceTabs[0].Profile != tt.wantWindow {
				t.Errorf("tabs %+v, want profile %q", g.WorkspaceTabs, tt.wantWindow)
			}
			// what the caller changes in a copy does not reach the kept state
			if g.LastProfiles != nil {
				g.LastProfiles["/zzz"] = "X"
				if again, _ := s.LoadGlobal(t.Context()); again.LastProfiles["/zzz"] != "" {
					t.Error("LoadGlobal shares the map with the kept state")
				}
			}
		})
	}
}
