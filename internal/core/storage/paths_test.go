package storage

import (
	"testing"

	"github.com/rm4n0s/errors"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestPathsFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Paths
	}{
		{"every variable set", map[string]string{
			"XDG_CONFIG_HOME": "/c", "XDG_STATE_HOME": "/s", "XDG_CACHE_HOME": "/k",
			"XDG_DATA_HOME": "/d", "XDG_RUNTIME_DIR": "/r", "HOME": "/home/u",
		}, Paths{"/c", "/s", "/k", "/d", "/r"}},
		{"defaults from HOME", map[string]string{"HOME": "/home/u"},
			Paths{"/home/u/.config", "/home/u/.local/state", "/home/u/.cache", "/home/u/.local/share", ""}},
		{"relative variables are ignored", map[string]string{
			"XDG_CONFIG_HOME": "rel/c", "XDG_STATE_HOME": "", "XDG_CACHE_HOME": "./k",
			"XDG_DATA_HOME": "d", "XDG_RUNTIME_DIR": "run", "HOME": "/home/u",
		}, Paths{"/home/u/.config", "/home/u/.local/state", "/home/u/.cache", "/home/u/.local/share", ""}},
		{"variables are cleaned", map[string]string{
			"XDG_CONFIG_HOME": "/c/", "XDG_STATE_HOME": "/s//x", "XDG_CACHE_HOME": "/k/./y",
			"XDG_DATA_HOME": "/d/../e", "XDG_RUNTIME_DIR": "/r/", "HOME": "/home/u",
		}, Paths{"/c", "/s/x", "/k/y", "/e", "/r"}},
		{"no HOME needed when every folder is set", map[string]string{
			"XDG_CONFIG_HOME": "/c", "XDG_STATE_HOME": "/s", "XDG_CACHE_HOME": "/k", "XDG_DATA_HOME": "/d",
		}, Paths{"/c", "/s", "/k", "/d", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PathsFromEnv(env(tt.env))
			if err != nil {
				t.Fatalf("PathsFromEnv: %v", err)
			}
			if got != tt.want {
				t.Errorf("Paths = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPathsFromEnvHomeUnknown(t *testing.T) {
	for _, home := range []string{"", "relative/home"} {
		_, err := PathsFromEnv(env(map[string]string{"HOME": home, "XDG_CONFIG_HOME": "/c"}))
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("HOME=%q: expected *errors.Error, got %T", home, err)
		}
		if !appErr.HasRoute("PathsFromEnv") || !appErr.HasRoute("HomeDirUnknown") {
			t.Errorf("HOME=%q: unexpected failure path: %s", home, appErr.Route())
		}
	}
}

func TestResolvePaths(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	t.Setenv("XDG_CONFIG_HOME", "/conf")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	got, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{"/conf", "/home/u/.local/state", "/home/u/.cache", "/home/u/.local/share", "/run/user/1000"}
	if got != want {
		t.Errorf("Paths = %+v, want %+v", got, want)
	}
}

func TestPathMethods(t *testing.T) {
	p := Paths{"/c", "/s", "/k", "/d", "/r"}
	tests := []struct {
		name string
		got  func() (string, error)
		want string
	}{
		{"config default profile", func() (string, error) { return p.ConfigDir(DefaultProfile) }, "/c/emetos"},
		{"config named profile", func() (string, error) { return p.ConfigDir("work") }, "/c/emetos/profiles/work"},
		{"state default profile", func() (string, error) { return p.StateDir("") }, "/s/emetos"},
		{"state named profile", func() (string, error) { return p.StateDir("my work_1") }, "/s/emetos/profiles/my work_1"},
		{"cache", func() (string, error) { return p.CacheDir(), nil }, "/k/emetos"},
		{"themes", func() (string, error) { return p.ThemesDir(), nil }, "/c/emetos/themes"},
		{"logs", func() (string, error) { return p.LogsDir(), nil }, "/s/emetos/logs"},
		{"backups", func() (string, error) { return p.BackupsDir(), nil }, "/s/emetos/backups"},
		{"trash", func() (string, error) { return p.TrashDir(), nil }, "/d/Trash"},
		{"socket", func() (string, error) { return p.SocketPath(1000), nil }, "/r/emetos-1000.sock"},
		{"socket without a runtime dir", func() (string, error) { return Paths{}.SocketPath(1000), nil }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.got()
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProfilePathUnsafe(t *testing.T) {
	p := Paths{ConfigHome: "/c", StateHome: "/s"}
	for _, name := range []string{".", "..", "../x", "a/b", `a\b`, "/abs", "nul\x00"} {
		for _, call := range []struct {
			fn    string
			route string
			get   func(string) (string, error)
		}{
			{"ConfigDir", "Paths.ConfigDir", p.ConfigDir},
			{"StateDir", "Paths.StateDir", p.StateDir},
		} {
			_, err := call.get(name)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("%s(%q): expected *errors.Error, got %T", call.fn, name, err)
			}
			if !appErr.HasRoute(call.route) || !appErr.HasRoute("profileDir") || !appErr.HasRoute("ProfilePathUnsafe") {
				t.Errorf("%s(%q): unexpected failure path: %s", call.fn, name, appErr.Route())
			}
		}
	}
}
