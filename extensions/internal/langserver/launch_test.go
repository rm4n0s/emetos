package langserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestResolveFindsChecksAndCustomizes(t *testing.T) {
	base := t.TempDir()
	bin := filepath.Join(base, "bin")
	program(t, bin, "ra", "rust-analyzer 1.78.0 (abc)")
	project := tree(t, "crate/Cargo.toml", "crate/src/main.rs", "h/")
	cfg := Config{
		ID: "ra", Name: "rust-analyzer", Languages: []string{"rust"},
		Programs: []Program{{
			Name: "rust-analyzer", Exe: "ra", Args: []string{"--stdio"}, PathSetting: "ra.path",
			Version: VersionCheck{Args: []string{"--version"}, Min: "1.70"},
		}},
		RootMarkers: []string{"Cargo.toml"},
		Customize: func(l *Launch, env Env) error {
			l.Args = append(l.Args, "--log="+env.Settings.String("ra.log"))
			l.Env = []string{"RA_LOG=1"}
			l.InitializationOptions = map[string]any{"cargo": map[string]any{"features": env.Settings.Strings("ra.features")}, "version": l.Version}
			l.Configuration = map[string]any{"rust-analyzer": map[string]any{"check": map[string]any{"command": "clippy"}}}
			return nil
		},
	}
	settings := settingsOf(map[string]any{"ra.log": "debug", "ra.features": []any{"a", "b"}})
	env := Env{Folder: filepath.Join(project, "crate", "src"), Home: filepath.Join(project, "h"), Path: bin, Settings: settings}
	l, err := Resolve(context.Background(), cfg, env)
	if err != nil {
		t.Fatal(err)
	}
	if l.Found.Path != filepath.Join(bin, "ra") || l.Found.Source != FromPath || l.Version != "1.78.0" || l.VersionErr != nil {
		t.Errorf("found %+v, version %q (%v)", l.Found, l.Version, l.VersionErr)
	}
	if l.Root != filepath.Join(project, "crate") {
		t.Errorf("root = %s", l.Root)
	}
	if !slices.Equal(l.Args, []string{"--stdio", "--log=debug"}) || !slices.Equal(l.Env, []string{"RA_LOG=1"}) {
		t.Errorf("args %v, env %v", l.Args, l.Env)
	}
	if cfg.Programs[0].Args[0] != "--stdio" || len(cfg.Programs[0].Args) != 1 {
		t.Errorf("the arguments of the config were changed: %v", cfg.Programs[0].Args)
	}
	// the host's view of it
	sl := l.serverLaunch()
	if sl.Program != l.Found.Path || sl.Root != l.Root || !slices.Equal(sl.Args, l.Args) {
		t.Errorf("serverLaunch = %+v", sl)
	}
	raw, _ := json.Marshal(sl.InitializationOptions)
	if string(raw) != `{"cargo":{"features":["a","b"]},"version":"1.78.0"}` {
		t.Errorf("initializationOptions = %s", raw)
	}
	for section, want := range map[string]string{
		"rust-analyzer":               `{"check":{"command":"clippy"}}`,
		"rust-analyzer.check":         `{"command":"clippy"}`,
		"rust-analyzer.check.command": `"clippy"`,
	} {
		got, ok := sl.Configuration(section)
		if !ok || string(got) != want {
			t.Errorf("Configuration(%q) = %s, %v; want %s", section, got, ok, want)
		}
	}
	for _, section := range []string{"", "editor.tabSize", "rust-analyzer.nothing", "rust-analyzer.check.command.deeper", "rust"} {
		if got, ok := sl.Configuration(section); ok {
			t.Errorf("Configuration(%q) = %s, want the settings to answer", section, got)
		}
	}
	// the settings the launch read are remembered, even those that were not there
	read := settings.Read()
	for _, key := range []string{"ra.path", "ra.log", "ra.features"} {
		if _, ok := read[key]; !ok {
			t.Errorf("%s was not remembered: %v", key, read)
		}
	}
}

func TestResolveSelectsAmongThePrograms(t *testing.T) {
	base := t.TempDir()
	bin := filepath.Join(base, "bin")
	program(t, bin, "basedpyright-langserver", "1.0.0")
	program(t, bin, "pylsp", "pylsp v1.2.3")
	cfg := Config{
		ID: "python", Name: "Python", Languages: []string{"python"},
		Programs: []Program{
			{Name: "basedpyright", Exe: "basedpyright-langserver", PathSetting: "basedpyright.path", Args: []string{"--stdio"}},
			{Name: "pyright", Exe: "pyright-langserver", PathSetting: "pyright.path", Args: []string{"--stdio"}},
			{Name: "pylsp", PathSetting: "pylsp.path", Version: VersionCheck{Args: []string{"--version"}}},
		},
		Select: func(s *Settings) int {
			switch s.String("python.languageServer") {
			case "pyright":
				return 1
			case "pylsp":
				return 2
			case "bogus":
				return 17
			}
			return 0
		},
	}
	tests := []struct {
		name    string
		choice  string
		program string
		version string
		route   string
	}{
		{"the default", "", "basedpyright", "", ""},
		{"pylsp, with its version", "pylsp", "pylsp", "1.2.3", ""},
		{"pyright is not installed", "pyright", "", "", "Resolve->Find.ServerNotFound"},
		{"an index out of range is the first", "bogus", "basedpyright", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := settingsOf(map[string]any{"python.languageServer": tt.choice})
			l, err := Resolve(context.Background(), cfg, Env{Folder: base, Home: base, Path: bin, Settings: s})
			if tt.route != "" {
				wantRoute(t, err, tt.route)
				if metadata(t, err, "program") != "pyright" {
					t.Errorf("the failure is about %q", metadata(t, err, "program"))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if l.Program.Name != tt.program || l.Version != tt.version {
				t.Errorf("program %s, version %q", l.Program.Name, l.Version)
			}
			if _, ok := s.Read()["python.languageServer"]; !ok {
				t.Error("the choice was not remembered")
			}
		})
	}
}

func TestResolveFailures(t *testing.T) {
	base := t.TempDir()
	bin := filepath.Join(base, "bin")
	program(t, bin, "old", "tool 1.2.0")
	program(t, bin, "silent", "no version at all")
	writeFile(t, bin, "broken", "#!/bin/sh\nexit 3\n", 0o755)
	mk := func(name string, vc VersionCheck, customize func(*Launch, Env) error) Config {
		return Config{ID: "x", Name: name, Languages: []string{"x"}, Programs: []Program{{Name: name, Version: vc}}, Customize: customize}
	}
	env := Env{Folder: base, Home: base, Path: bin, Settings: settingsOf(nil)}
	ours := errors.New("OurTag", "ours")
	tests := []struct {
		name  string
		cfg   Config
		route string
	}{
		{"not found", mk("nothing-here", VersionCheck{}, nil), "Resolve->Find.ServerNotFound"},
		{"too old", mk("old", VersionCheck{Args: []string{"-v"}, Min: "1.10"}, nil), "Resolve.ServerTooOld"},
		{"Customize fails with an error of ours", mk("old", VersionCheck{}, func(*Launch, Env) error { return ours }), "OurTag"},
		{"Customize fails with another error", mk("old", VersionCheck{}, func(*Launch, Env) error { return context.DeadlineExceeded }), "Resolve.ServerCustomizeFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolve(context.Background(), tt.cfg, env)
			if err == nil {
				t.Fatal("Resolve did not fail")
			}
			wantRoute(t, err, tt.route)
		})
	}

	// an unreadable version is no failure, even with a minimum
	for _, name := range []string{"silent", "broken"} {
		l, err := Resolve(context.Background(), mk(name, VersionCheck{Args: []string{"-v"}, Min: "9"}, nil), env)
		if err != nil || l.Version != "" || l.VersionErr == nil {
			t.Errorf("%s: Resolve = %+v, %v", name, l, err)
		}
	}

	// a context that ended while the version was asked
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Resolve(ctx, mk("old", VersionCheck{Args: []string{"-v"}}, nil), env)
	wantRoute(t, err, "Resolve.ServerLaunchCancelled")
}

func TestResolveWithNoSettings(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bin")
	program(t, bin, "tool", "1.0")
	cfg := Config{ID: "x", Name: "tool", Languages: []string{"x"}, Programs: []Program{{Name: "tool"}}}
	l, err := Resolve(context.Background(), cfg, Env{Path: bin})
	if err != nil || l.Found.Source != FromPath || l.Root != "" {
		t.Errorf("Resolve = %+v, %v", l, err)
	}
}

func TestConfigSpecAndSettings(t *testing.T) {
	cfg := Config{
		ID: "python", Name: "Python", Languages: []string{"python"}, Category: "Python", TraceKey: "python.trace.server",
		ConfigurationSection: "python", WatchedFiles: []string{"**/*.py"},
		Programs: []Program{
			{Name: "basedpyright", Exe: "basedpyright-langserver", Args: []string{"--stdio"}, PathSetting: "basedpyright.path", After: []string{"~/.local/bin"}},
			{Name: "pyright", PathSetting: "pyright.path"},
			{Name: "pylsp", PathSetting: "basedpyright.path"},
			{Name: "none"},
		},
	}
	sp := cfg.Spec()
	if sp.ID != "python" || sp.Name != "Python" || sp.Program != "basedpyright-langserver" || !sp.OwnUI || sp.PathSetting != "" ||
		sp.TraceKey != "python.trace.server" || sp.Category != "Python" || sp.ConfigurationSection != "python" || len(sp.Args) != 1 {
		t.Errorf("Spec = %+v", sp)
	}
	specs := cfg.SettingSpecs()
	var keys []string
	for _, s := range specs {
		keys = append(keys, s.Key)
		if s.Type != "string" || s.Scope != "machine" || s.Default != "" || s.Description == "" {
			t.Errorf("setting = %+v", s)
		}
	}
	if !slices.Equal(keys, []string{"basedpyright.path", "pyright.path"}) {
		t.Errorf("setting keys = %v", keys)
	}
	if specs[0].Description != "The path of the basedpyright program. When empty, basedpyright is looked for in PATH, then ~/.local/bin." {
		t.Errorf("description = %q", specs[0].Description)
	}
}
