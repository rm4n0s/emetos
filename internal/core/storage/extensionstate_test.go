package storage_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/storage"
)

func TestExtensionValuesAreKeptPerExtensionFolderAndKeyAndSurviveAStart(t *testing.T) {
	s, clock, paths := newStore(t, nil)
	ctx := t.Context()
	steps := []struct {
		ext, folder, key, value string
	}{
		{"emetos.python", "/work/a", "python.interpreter", `"/work/a/.venv/bin/python"`},
		{"emetos.python", "/work/b/", "python.interpreter", `"/usr/bin/python3"`},
		{"emetos.python", "", "python.interpreter", `"/usr/bin/python3.12"`},
		{"emetos.python", "/work/a", "python.other", `{"n":1}`},
		{"emetos.rust", "/work/a", "python.interpreter", `"not python's"`},
	}
	for _, st := range steps {
		if err := s.SetExtensionValue(ctx, st.ext, st.folder, st.key, json.RawMessage(st.value)); err != nil {
			t.Fatalf("SetExtensionValue(%v): %v", st, err)
		}
	}
	clock.Advance(storage.DefaultStateDelay)
	again, err := storage.NewStateStore(paths, storage.DefaultProfile, storage.StateStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                    string
		ext, folder, key, value string
		found                   bool
	}{
		{"a folder", "emetos.python", "/work/a", "python.interpreter", `"/work/a/.venv/bin/python"`, true},
		{"the same folder with a slash", "emetos.python", "/work/a/", "python.interpreter", `"/work/a/.venv/bin/python"`, true},
		{"a folder written with a slash", "emetos.python", "/work/b", "python.interpreter", `"/usr/bin/python3"`, true},
		{"a tab without a folder", "emetos.python", "", "python.interpreter", `"/usr/bin/python3.12"`, true},
		{"another key", "emetos.python", "/work/a", "python.other", `{"n":1}`, true},
		{"another extension", "emetos.rust", "/work/a", "python.interpreter", `"not python's"`, true},
		{"a folder with nothing", "emetos.python", "/work/c", "python.interpreter", "", false},
		{"a key with nothing", "emetos.python", "/work/a", "nothing", "", false},
		{"an extension with nothing", "emetos.cpp", "/work/a", "python.interpreter", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := again.ExtensionValue(ctx, tt.ext, tt.folder, tt.key)
			if err != nil {
				t.Fatal(err)
			}
			if ok != tt.found || string(got) != tt.value {
				t.Errorf("ExtensionValue = %s, %v; want %s, %v", got, ok, tt.value, tt.found)
			}
		})
	}
}

func TestAnExtensionValueIsReplacedAndTakenAway(t *testing.T) {
	s, clock, paths := newStore(t, nil)
	ctx := t.Context()
	set := func(v string) {
		t.Helper()
		if err := s.SetExtensionValue(ctx, "emetos.python", "/work/a", "k", json.RawMessage(v)); err != nil {
			t.Fatal(err)
		}
	}
	set(`1`)
	set(`2`)
	if v, ok, _ := s.ExtensionValue(ctx, "emetos.python", "/work/a", "k"); !ok || string(v) != "2" {
		t.Errorf("after replacing: %s, %v", v, ok)
	}
	set(``) // takes it away
	if _, ok, _ := s.ExtensionValue(ctx, "emetos.python", "/work/a", "k"); ok {
		t.Error("the value is still there")
	}
	set(``) // nothing to take away: no change
	clock.Advance(storage.DefaultStateDelay)
	data, err := os.ReadFile(mustGlobalPath(t, paths))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "extensions") {
		t.Errorf("the empty levels were written:\n%s", data)
	}
}

func TestExtensionValuesThatCannotBeKeptFailWithTheirOwnTag(t *testing.T) {
	s, _, _ := newStore(t, nil)
	tests := []struct {
		name                string
		ext, key, value     string
		wantRoute, wantText string
	}{
		{"no extension", "", "k", `1`, "StateStore.SetExtensionValue.ExtensionStateInvalid", "needs an extension"},
		{"no key", "emetos.python", "", `1`, "StateStore.SetExtensionValue.ExtensionStateInvalid", "needs an extension"},
		{"not JSON", "emetos.python", "k", `{`, "StateStore.SetExtensionValue.ExtensionStateInvalid", "not JSON"},
		{"too large", "emetos.python", "k", `"` + strings.Repeat("x", storage.MaxExtensionValue) + `"`, "StateStore.SetExtensionValue.ExtensionStateInvalid", "too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.SetExtensionValue(t.Context(), tt.ext, "/work/a", tt.key, json.RawMessage(tt.value))
			appErr := routeOf(t, err)
			if !appErr.HasRoute(tt.wantRoute) || !strings.Contains(appErr.Message, tt.wantText) {
				t.Errorf("route %s, message %q", appErr.Route(), appErr.Message)
			}
		})
	}
}

func TestExtensionValuesOfAFileAreCleanedWhenRead(t *testing.T) {
	s, _, paths := newStore(t, nil)
	file := mustGlobalPath(t, paths)
	if err := os.MkdirAll(dirOf(file), 0o700); err != nil {
		t.Fatal(err)
	}
	text := `{"version":1,"extensions":{
		"emetos.python":{"/work/a":{"good":"yes","":1,"broken":{}},"relative/folder":{"k":1},"":{"nofolder":true}},
		"":{"/work/a":{"k":1}}}}`
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	g, err := s.LoadGlobal(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]map[string]string{}
	for ext, folders := range g.Extensions {
		got[ext] = map[string]map[string]string{}
		for folder, keys := range folders {
			got[ext][folder] = map[string]string{}
			for k, v := range keys {
				got[ext][folder][k] = string(v)
			}
		}
	}
	if len(got) != 1 || len(got["emetos.python"]) != 2 || len(got["emetos.python"]["/work/a"]) != 2 ||
		got["emetos.python"]["/work/a"]["good"] != `"yes"` || got["emetos.python"]["/work/a"]["broken"] != `{}` || got["emetos.python"][""]["nofolder"] != `true` {
		t.Errorf("after cleaning: %v", got)
	}
}

// mustGlobalPath is the path of the global state file.
func mustGlobalPath(t *testing.T, paths storage.Paths) string {
	t.Helper()
	p, err := paths.GlobalStatePath(storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// dirOf is the folder of a path.
func dirOf(p string) string { return p[:strings.LastIndex(p, "/")] }
