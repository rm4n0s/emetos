package python

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// The three servers over the fake API: which program runs, what it is given, and what the user is told when it is missing.

func TestTheSettingChoosesTheProgramThatRuns(t *testing.T) {
	tests := []struct {
		name    string
		setting any
		program string
		args    []string
		version string
	}{
		{"basedpyright is the default", nil, "basedpyright-langserver", []string{"--stdio"}, "basedpyright 1.40.2"},
		{"basedpyright by name", ServerBasedpyright, "basedpyright-langserver", []string{"--stdio"}, "basedpyright 1.40.2"},
		{"pyright", ServerPyright, "pyright-langserver", []string{"--stdio"}, "pyright 1.1.414"},
		{"pylsp", ServerPylsp, "pylsp", nil, "pylsp 1.15.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := map[string]any{}
			if tt.setting != nil {
				settings[LanguageServerSetting] = tt.setting
			}
			r := newRig(t, rigOptions{servers: []string{"basedpyright", "pyright", "pylsp"}, settings: settings})
			l, err := r.run()
			if err != nil {
				t.Fatal(err)
			}
			if l.Program != filepath.Join(r.bin, tt.program) || !slices.Equal(l.Args, tt.args) {
				t.Errorf("program %s %v, want %s %v", l.Program, l.Args, tt.program, tt.args)
			}
			r.editor("a.py", "python", "x = 1\n")
			if tip := r.item().Tooltip; !strings.Contains(tip, tt.version) {
				t.Errorf("the tooltip lacks the version %q:\n%s", tt.version, tip)
			}
			if it := r.item(); !strings.HasPrefix(it.Text, "$(check) Python 3.11.2") {
				t.Errorf("item = %q", it.Text)
			}
		})
	}
}

func TestTheServerRunsInTheNearestPythonProject(t *testing.T) {
	tests := []struct {
		name string
		// files are written to the folder of the tab, above is a file written to the folder that holds it
		files map[string]string
		above string
		root  func(r *rig) string
	}{
		{"the folder holds pyproject.toml", map[string]string{"pyproject.toml": ""}, "", func(r *rig) string { return r.folder }},
		{"the folder holds pyrightconfig.json", map[string]string{"pyrightconfig.json": "{}"}, "", func(r *rig) string { return r.folder }},
		{"the folder holds setup.py", map[string]string{"setup.py": ""}, "", func(r *rig) string { return r.folder }},
		{"the project is above the folder", nil, "pyproject.toml", func(r *rig) string { return r.base }},
		{"no marker: the folder", nil, "", func(r *rig) string { return r.folder }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{servers: []string{"basedpyright"}, files: tt.files})
			if tt.above != "" {
				writeFile(t, r.base, tt.above, "", 0o644)
			}
			l, err := r.run()
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.root(r); l.Root != want {
				t.Errorf("root = %s, want %s", l.Root, want)
			}
		})
	}
}

func TestTheInterpreterAndTheSettingsThatAreNotDefaultsGoToTheServer(t *testing.T) {
	venv := func(r *rig) string { return filepath.Join(r.folder, ".venv", "bin", "python") }
	prepare := func(t *testing.T, server string, settings map[string]any) (*rig, string) {
		o := rigOptions{servers: []string{server}, settings: settings, files: map[string]string{}}
		r := newRig(t, o)
		fakeEnv(t, filepath.Join(r.folder, ".venv"), "3.12.1", "")
		if err := r.ext.interps.Choose(venv(r)); err != nil {
			t.Fatal(err)
		}
		r.settle()
		return r, venv(r)
	}

	t.Run("basedpyright", func(t *testing.T) {
		r, py := prepare(t, "basedpyright", map[string]any{
			"basedpyright.analysis.typeCheckingMode": "strict", "basedpyright.analysis.diagnosticMode": "openFilesOnly",
			"basedpyright.analysis.inlayHints.genericTypes": true, "python.analysis.typeCheckingMode": "off",
		})
		l, err := r.run()
		if err != nil {
			t.Fatal(err)
		}
		if got, want := configuration(t, l, "python"), `{"pythonPath":"`+py+`"}`; got != want {
			t.Errorf("python = %s, want %s", got, want)
		}
		// only what differs from the default is sent, and the settings of Pyright are not basedpyright's
		if got, want := configuration(t, l, "basedpyright"), `{"analysis":{"inlayHints":{"genericTypes":true},"typeCheckingMode":"strict"}}`; got != want {
			t.Errorf("basedpyright = %s, want %s", got, want)
		}
		if got := configuration(t, l, "pyright"); got != "{}" {
			t.Errorf("pyright = %s", got)
		}
		if got := configuration(t, l, "basedpyright.analysis.typeCheckingMode"); got != `"strict"` {
			t.Errorf("a section below one of ours = %s", got)
		}
		if l.InitializationOptions != nil {
			t.Errorf("initialization options = %v", l.InitializationOptions)
		}
	})

	t.Run("pyright", func(t *testing.T) {
		r, py := prepare(t, "pyright", map[string]any{
			LanguageServerSetting: ServerPyright, "python.analysis.typeCheckingMode": "strict", "python.analysis.extraPaths": []any{"libs"},
			"pyright.disableOrganizeImports": true, "basedpyright.analysis.typeCheckingMode": "off",
		})
		l, err := r.run()
		if err != nil {
			t.Fatal(err)
		}
		if got, want := configuration(t, l, "python"), `{"analysis":{"extraPaths":["libs"],"typeCheckingMode":"strict"},"pythonPath":"`+py+`"}`; got != want {
			t.Errorf("python = %s, want %s", got, want)
		}
		if got, want := configuration(t, l, "pyright"), `{"disableOrganizeImports":true}`; got != want {
			t.Errorf("pyright = %s, want %s", got, want)
		}
		if got := configuration(t, l, "basedpyright"); got != "<not answered>" {
			t.Errorf("basedpyright = %s", got)
		}
	})

	t.Run("pylsp", func(t *testing.T) {
		r, py := prepare(t, "pylsp", map[string]any{
			LanguageServerSetting: ServerPylsp, "pylsp.plugins.pycodestyle.enabled": false, "pylsp.plugins.pylint.enabled": true,
			"pylsp.plugins.pycodestyle.maxLineLength": 100, "pylsp.configurationSources": []any{"flake8"},
		})
		l, err := r.run()
		if err != nil {
			t.Fatal(err)
		}
		want := `{"pylsp":{"configurationSources":["flake8"],"plugins":{"jedi":{"environment":"` + py + `"},"pycodestyle":{"enabled":false,"maxLineLength":100},"pylint":{"enabled":true}}}}`
		if got := options(t, l); got != want {
			t.Errorf("options = %s\nwant      %s", got, want)
		}
	})

	t.Run("pylsp with a Jedi environment the user named", func(t *testing.T) {
		r, _ := prepare(t, "pylsp", map[string]any{LanguageServerSetting: ServerPylsp, "pylsp.plugins.jedi.environment": "/opt/other/bin/python"})
		l, err := r.run()
		if err != nil {
			t.Fatal(err)
		}
		if got, want := options(t, l), `{"pylsp":{"plugins":{"jedi":{"environment":"/opt/other/bin/python"}}}}`; got != want {
			t.Errorf("options = %s, want %s", got, want)
		}
	})

	t.Run("no interpreter at all", func(t *testing.T) {
		r := newRig(t, rigOptions{servers: []string{"basedpyright"}, noPython: true})
		l, err := r.run()
		if err != nil {
			t.Fatal(err)
		}
		if got := configuration(t, l, "python"); got != "{}" {
			t.Errorf("python = %s", got)
		}
	})
}

func TestNoneTurnsTheServerOffAndTellsNobody(t *testing.T) {
	r := newRig(t, rigOptions{servers: []string{"basedpyright"}, settings: map[string]any{LanguageServerSetting: ServerNone}})
	r.editor("a.py", "python", "x = 1\n")
	if _, err := r.run(); err == nil {
		t.Fatal("the server started")
	} else {
		wantRoute(t, err, "Resolve.ServerDisabled")
	}
	it := r.item()
	if !strings.HasPrefix(it.Text, "$(circle-slash) Python 3.11.2") || !strings.HasSuffix(it.Text, ": disabled") {
		t.Errorf("item = %q", it.Text)
	}
	if n := r.notes(); len(n) != 0 {
		t.Errorf("the user was told: %v", n)
	}
	// turning it back on restarts the server
	r.api.SetSetting(LanguageServerSetting, ServerBasedpyright)
	r.settle()
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d", got)
	}
	if _, err := r.run(); err != nil {
		t.Errorf("the server does not start after none was changed: %v", err)
	}
}

func TestAMissingServerIsToldWithItsOwnInstallHelp(t *testing.T) {
	tests := []struct {
		name     string
		server   string
		message  string
		commands []string
	}{
		{"basedpyright", ServerBasedpyright, "basedpyright was not found.", []string{"pipx install basedpyright"}},
		{"pyright", ServerPyright, "pyright was not found.", []string{"npm install -g pyright", "sudo pacman -S pyright"}},
		{"pylsp", ServerPylsp, "pylsp was not found.", []string{"pipx install python-lsp-server", "sudo dnf install python3-lsp-server", "sudo apt install python3-pylsp", "sudo pacman -S python-lsp-server", "sudo zypper install python3-python-lsp-server"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{settings: map[string]any{LanguageServerSetting: tt.server}})
			r.editor("a.py", "python", "x = 1\n")
			if _, err := r.run(); err == nil {
				t.Fatal("the server started")
			}
			notes := r.api.Notes()
			if len(notes) != 1 || notes[0].Message != tt.message {
				t.Fatalf("notes = %+v", notes)
			}
			if !slices.Equal(notes[0].Actions, []string{"How to Install", "Copy Install Command", "Set Path..."}) {
				t.Errorf("buttons = %v", notes[0].Actions)
			}
			if it := r.item(); !strings.Contains(it.Text, tt.server+" not found") {
				t.Errorf("item = %q", it.Text)
			}
			r.api.RunNoteAction(0, "Copy Install Command")
			// the command of the family of the machine this test runs on (the install help of each family is TestEachServerHasItsInstallCommandsPerSystem's)
			if got := r.api.ClipboardTexts(); len(got) != 1 || !slices.Contains(tt.commands, got[0]) {
				t.Errorf("copied %v, want one of %q", got, tt.commands)
			}
		})
	}
}

func TestEachServerHasItsInstallCommandsPerSystem(t *testing.T) {
	cfg := New().(*python).serverConfig()
	want := map[string]map[string]string{
		"basedpyright": {"any": "pipx install basedpyright"},
		"pyright":      {"arch": "sudo pacman -S pyright", "any": "npm install -g pyright"},
		"pylsp": {
			"fedora": "sudo dnf install python3-lsp-server", "debian": "sudo apt install python3-pylsp", "arch": "sudo pacman -S python-lsp-server",
			"suse": "sudo zypper install python3-python-lsp-server", "any": "pipx install python-lsp-server",
		},
	}
	for _, p := range cfg.Programs {
		got := map[string]string{}
		for _, c := range p.Install.Commands {
			got[c.Family] = c.Command
		}
		for family, cmd := range want[p.Name] {
			if got[family] != cmd {
				t.Errorf("%s on %s: %q, want %q", p.Name, family, got[family], cmd)
			}
		}
		if len(got) != len(want[p.Name]) || p.Install.Docs == "" || p.PathSetting != p.Name+".path" {
			t.Errorf("%s: commands %v, docs %q, path setting %q", p.Name, got, p.Install.Docs, p.PathSetting)
		}
	}
}

func TestTheServerInTheInterpretersBinFolderIsFoundAfterPATH(t *testing.T) {
	t.Run("only in the environment", func(t *testing.T) {
		r := newRig(t, rigOptions{})
		py := fakeEnv(t, filepath.Join(r.folder, ".venv"), "3.12.1", "")
		writeFile(t, filepath.Join(r.folder, ".venv"), "bin/basedpyright-langserver", "#!/bin/sh\n", 0o755)
		writeFile(t, filepath.Join(r.folder, ".venv"), "bin/basedpyright", "#!/bin/sh\necho 'basedpyright 9.9.9'\n", 0o755)
		if err := r.ext.interps.Choose(py); err != nil {
			t.Fatal(err)
		}
		r.settle()
		l, err := r.run()
		if err != nil {
			t.Fatal(err)
		}
		if l.Program != filepath.Join(r.folder, ".venv", "bin", "basedpyright-langserver") {
			t.Errorf("program = %s", l.Program)
		}
		r.editor("a.py", "python", "")
		if tip := r.item().Tooltip; !strings.Contains(tip, "basedpyright 9.9.9") {
			t.Errorf("the version was asked of the program in the environment:\n%s", tip)
		}
	})
	t.Run("PATH comes first", func(t *testing.T) {
		r := newRig(t, rigOptions{servers: []string{"basedpyright"}})
		py := fakeEnv(t, filepath.Join(r.folder, ".venv"), "3.12.1", "")
		writeFile(t, filepath.Join(r.folder, ".venv"), "bin/basedpyright-langserver", "#!/bin/sh\n", 0o755)
		if err := r.ext.interps.Choose(py); err != nil {
			t.Fatal(err)
		}
		r.settle()
		l, err := r.run()
		if err != nil {
			t.Fatal(err)
		}
		if l.Program != filepath.Join(r.bin, "basedpyright-langserver") {
			t.Errorf("program = %s", l.Program)
		}
	})
	t.Run("the path setting is the only place", func(t *testing.T) {
		r := newRig(t, rigOptions{servers: []string{"basedpyright"}, settings: map[string]any{BasedpyrightPathSetting: "/nowhere/basedpyright-langserver"}})
		_, err := r.run()
		wantRoute(t, err, "findSet.ServerPathInvalid")
		if n := r.notes(); len(n) != 1 || !strings.Contains(n[0], "basedpyright.path is set to /nowhere/basedpyright-langserver") {
			t.Errorf("notes = %v", n)
		}
	})
}

func TestTheServerRestartsWhenASettingItReadChanges(t *testing.T) {
	r := newRig(t, rigOptions{servers: []string{"basedpyright", "pyright", "pylsp"}})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	changes := []struct {
		key   string
		value any
	}{
		{LanguageServerSetting, ServerPyright},
		{"python.analysis.typeCheckingMode", "strict"},
		{DefaultInterpreterSetting, "/usr/bin/python3"},
	}
	for n, c := range changes {
		r.api.SetSetting(c.key, c.value)
		r.settle()
		if got := r.api.ServerRestarts(ServerID); got != n+1 {
			t.Errorf("after %s: %d restarts, want %d", c.key, got, n+1)
		}
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
	}
	// a setting of another server was read by no start: it restarts nothing
	r.api.SetSetting("pylsp.plugins.pylint.enabled", true)
	r.settle()
	if got := r.api.ServerRestarts(ServerID); got != len(changes) {
		t.Errorf("restarts = %d after a setting of pylsp", got)
	}
}

func TestEveryDeclaredPythonSettingKeepsItsNameAndType(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range settingSpecs() {
		if seen[s.Key] {
			t.Errorf("%s is declared twice", s.Key)
		}
		seen[s.Key] = true
		if s.Description == "" || s.Category == "" || s.Type == "" {
			t.Errorf("%+v lacks a description, category or type", s)
		}
		prefixes := []string{"python.", "basedpyright.", "pyright.", "pylsp."}
		if !slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(s.Key, p) }) {
			t.Errorf("%s has an unexpected prefix", s.Key)
		}
	}
	// the settings that are nested into the section of their server have a section to be cut from
	for _, set := range pyrightSettings() {
		if !strings.HasPrefix(set.key, "python.") && !strings.HasPrefix(set.key, "pyright.") {
			t.Errorf("%s is not a setting of Pyright", set.key)
		}
	}
	for _, set := range basedpyrightSettings() {
		if !strings.HasPrefix(set.key, "basedpyright.") {
			t.Errorf("%s is not a setting of basedpyright", set.key)
		}
	}
	for _, set := range pylspSettings() {
		if !strings.HasPrefix(set.key, "pylsp.") {
			t.Errorf("%s is not a setting of python-lsp-server", set.key)
		}
	}
	if extension.CheckManifest(New().Manifest()) != nil {
		t.Error("the manifest is not valid")
	}
}
