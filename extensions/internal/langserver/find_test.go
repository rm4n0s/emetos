package langserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindLooksInTheOrderOfTheSpec(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	folder := filepath.Join(base, "work", "app")
	pathA, pathB := filepath.Join(base, "pathA"), filepath.Join(base, "pathB")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	mk := func(rel string) string { return program(t, base, rel, "x 1.0.0") }
	inHome := mk("home/.cargo/bin/ra")
	inPathA := mk("pathA/ra")
	inPathB := mk("pathB/ra")
	inBefore := mk("work/app/node_modules/.bin/ra")
	setAbs := mk("tools/custom-ra")
	setRel := mk("work/app/tools/rel-ra")
	setHome := mk("home/bin/home-ra")
	setNamed := mk("pathB/named-ra")
	notExec := writeFile(t, base, "pathA/plain", "text", 0o644)
	_ = notExec
	plainDir := filepath.Join(base, "adir")
	if err := os.MkdirAll(plainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "pathA", "linked")
	if err := os.Symlink(setAbs, link); err != nil {
		t.Fatal(err)
	}

	prog := Program{Name: "ra", PathSetting: "pack.path", Before: []string{"${workspaceFolder}/node_modules/.bin"}, After: []string{"~/.cargo/bin"}}
	tests := []struct {
		name    string
		prog    Program
		setting string
		path    string
		want    string
		source  string
		route   string
	}{
		{"the setting wins over everything", prog, setAbs, pathA, setAbs, FromSetting, ""},
		{"a setting relative to the folder", prog, "tools/rel-ra", pathA, setRel, FromSetting, ""},
		{"a setting in the home folder", prog, "~/bin/home-ra", pathA, setHome, FromSetting, ""},
		{"a setting with a variable", prog, "${userHome}/bin/home-ra", pathA, setHome, FromSetting, ""},
		{"a setting that is a name looked for in PATH", prog, "named-ra", pathA + ":" + pathB, setNamed, FromSetting, ""},
		{"a setting that is a link", prog, link, "", link, FromSetting, ""},
		{"a setting that does not exist", prog, filepath.Join(base, "nowhere"), pathA, "", "", "Find->findSet.ServerPathInvalid"},
		{"a setting that is a folder", prog, plainDir, pathA, "", "", "Find->findSet.ServerPathInvalid"},
		{"a setting that is not executable", prog, filepath.Join(base, "pathA", "plain"), pathA, "", "", "Find->findSet.ServerPathInvalid"},
		{"a setting that is a name nobody has", prog, "no-such-ra", pathA, "", "", "Find->findSet.ServerPathInvalid"},
		{"the folder before PATH", prog, "", pathA, inBefore, FromBefore, ""},
		{"the first folder of PATH", Program{Name: "ra", After: prog.After}, "", pathA + ":" + pathB, inPathA, FromPath, ""},
		{"the second folder of PATH", Program{Name: "ra"}, "", filepath.Join(base, "empty") + ":" + pathB, inPathB, FromPath, ""},
		{"empty entries of PATH are not the current folder", Program{Name: "ra"}, "", ":" + pathB + ":", inPathB, FromPath, ""},
		{"the place of the installer after PATH", Program{Name: "ra", After: []string{"~/.cargo/bin"}}, "", filepath.Join(base, "empty"), inHome, FromAfter, ""},
		{"PATH before the place of the installer", Program{Name: "ra", After: []string{"~/.cargo/bin"}}, "", pathA, inPathA, FromPath, ""},
		{"a file that is not executable is skipped", Program{Name: "plain"}, "", pathA, "", "", "Find.ServerNotFound"},
		{"nothing anywhere", Program{Name: "no-such-ra", After: []string{"~/.cargo/bin"}}, "", pathA, "", "", "Find.ServerNotFound"},
		{"another file name than the display name", Program{Name: "Rust Analyzer", Exe: "ra"}, "", pathA, inPathA, FromPath, ""},
		{"a folder from the hook before the others", Program{Name: "ra", Before: prog.Before, Dirs: func(Env) ([]string, []string) { return []string{"${workspaceFolder}/../../pathB"}, nil }}, "", pathA, inPathB, FromBefore, ""},
		{"a folder from the hook after PATH", Program{Name: "ra", Dirs: func(Env) ([]string, []string) { return nil, []string{"~/.cargo/bin"} }}, "", filepath.Join(base, "empty"), inHome, FromAfter, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]any{}
			if tt.setting != "" {
				values["pack.path"] = tt.setting
			}
			env := Env{Folder: folder, Home: home, Path: tt.path, Settings: settingsOf(values)}
			got, err := Find(tt.prog, env)
			if tt.route != "" {
				if err == nil {
					t.Fatalf("Find = %+v, want the failure %s", got, tt.route)
				}
				wantRoute(t, err, tt.route)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Path != tt.want || got.Source != tt.source {
				t.Errorf("Find = %+v, want %s from %s", got, tt.want, tt.source)
			}
		})
	}
}

func TestFindNotFoundListsThePlacesItLookedAt(t *testing.T) {
	base := t.TempDir()
	env := Env{Folder: filepath.Join(base, "w"), Home: filepath.Join(base, "h"), Path: "/p1:/p2", Settings: settingsOf(nil)}
	_, err := Find(Program{Name: "ra", Before: []string{"${workspaceFolder}/b"}, After: []string{"~/a"}}, env)
	wantRoute(t, err, "Find.ServerNotFound")
	msg := err.Error()
	for _, want := range []string{"ra was not found"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	want := strings.Join([]string{filepath.Join(base, "w", "b"), "/p1", "/p2", filepath.Join(base, "h", "a")}, ":")
	if got := metadata(t, err, "searched"); got != want {
		t.Errorf("searched = %q, want %q", got, want)
	}
}

func TestFindUsesTheProgramsOwnSearch(t *testing.T) {
	called := false
	prog := Program{Name: "jdtls", Find: func(env Env) (Found, error) {
		called = true
		return Found{Path: env.Folder + "/launcher.jar", Source: FromSetting}, nil
	}}
	got, err := Find(prog, Env{Folder: "/w"})
	if err != nil || !called || got.Path != "/w/launcher.jar" {
		t.Errorf("Find = %+v, %v (called %v)", got, err, called)
	}
}

func TestExpand(t *testing.T) {
	env := Env{Folder: "/w/app", Home: "/home/u", Environ: func(n string) string { return map[string]string{"TOOLS": "/opt/tools"}[n] }}
	tests := []struct{ in, want string }{
		{"~", "/home/u"},
		{"~/bin", "/home/u/bin"},
		{"~other/bin", "~other/bin"},
		{"${userHome}/x", "/home/u/x"},
		{"${workspaceFolder}/node_modules/.bin", "/w/app/node_modules/.bin"},
		{"${env:TOOLS}/bin", "/opt/tools/bin"},
		{"${env:NOPE}/bin", "/bin"},
		{"${env:UNCLOSED", "${env:UNCLOSED"},
		{"/plain/path", "/plain/path"},
	}
	for _, tt := range tests {
		if got := env.Expand(tt.in); got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := env.absolute("tools/x"); got != "/w/app/tools/x" {
		t.Errorf("absolute(relative) = %q", got)
	}
}

func TestCheckPath(t *testing.T) {
	base := t.TempDir()
	exe := program(t, base, "bin/ok", "x")
	plain := writeFile(t, base, "bin/plain", "x", 0o644)
	env := Env{Folder: base, Home: base}
	tests := []struct {
		name, text, want string
	}{
		{"empty clears the setting", "  ", ""},
		{"a program", exe, ""},
		{"a name looked for in PATH", "clangd-18", ""},
		{"relative to the folder", "bin/ok", ""},
		{"missing", filepath.Join(base, "nope"), filepath.Join(base, "nope") + " does not exist"},
		{"a folder", filepath.Join(base, "bin"), filepath.Join(base, "bin") + " is a folder"},
		{"not executable", plain, plain + " is not executable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckPath(env, tt.text); got != tt.want {
				t.Errorf("CheckPath = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSettingsRemembersWhatItRead(t *testing.T) {
	s := settingsOf(map[string]any{"a": "  text ", "b": true, "c": 2.5, "d": []any{"x", 1.0, "y"}, "e": map[string]any{"k": "v"}})
	if s.String("a") != "text" || !s.Bool("b") || s.Float("c") != 2.5 || len(s.Strings("d")) != 2 || s.Object("e")["k"] != "v" {
		t.Error("a value was read wrongly")
	}
	if s.String("b") != "" || s.Bool("a") || s.Float("a") != 0 || s.Strings("a") != nil || s.Object("a") != nil || s.String("missing") != "" {
		t.Error("a value of another type was not empty")
	}
	if _, ok := s.Value("missing"); ok {
		t.Error("a missing setting exists")
	}
	read := s.Read()
	if read["a"] != `"  text "` || read["missing"] != "absent" || len(s.Keys()) != 6 {
		t.Errorf("read = %v, keys %v", read, s.Keys())
	}
	if Fingerprint(make(chan int), true) != "unencodable" {
		t.Error("an unencodable value has a fingerprint")
	}
}
