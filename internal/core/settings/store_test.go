package settings

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
	"github.com/rm4n0s/errors"
)

type fixture struct {
	t         *testing.T
	paths     storage.Paths
	folder    string
	store     *Store
	userPath  string
	wsPath    string
	listeners []ChangeEvent
}

// newFixture makes a store whose files are in isolated folders (SPEC §15.1). The registry has
// the settings of Emetos and a few of the tests.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	paths := storagetest.Isolate(t)
	userPath, err := UserSettingsPath(paths, storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(t.TempDir(), "project")
	reg := DefaultRegistry()
	reg.MustRegister(
		Setting{Key: "test.app", Types: []Type{Boolean}, Default: false, Scope: ScopeApplication},
		Setting{Key: "test.list", Types: []Type{Array}, ItemTypes: []Type{String}, Default: []string{}},
		Setting{Key: "test.obj", Types: []Type{Object}, Default: map[string]any{"a": true}},
		Setting{Key: "test.size", Types: []Type{Integer}, Default: 3, Minimum: Float(1), Maximum: Float(9)},
		Setting{Key: "test.mode", Types: []Type{String}, Default: "one", Enum: []any{"one", "two"}},
	)
	f := &fixture{t: t, paths: paths, folder: folder, userPath: userPath, wsPath: WorkspaceSettingsPath(folder)}
	f.store = NewStore(reg, StoreOptions{UserPath: userPath, WorkspacePath: f.wsPath})
	f.store.OnChange(func(ev ChangeEvent) { f.listeners = append(f.listeners, ev) })
	return f
}

func (f *fixture) write(path, text string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) read(path string) string {
	f.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f *fixture) reload() ChangeEvent {
	f.t.Helper()
	ev, err := f.store.Reload(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	return ev
}

func TestDefaultRegistry(t *testing.T) {
	r := DefaultRegistry()
	type want struct {
		key      string
		types    []Type
		def      any
		scope    Scope
		category string
	}
	for _, w := range []want{
		{"editor.tabSize", []Type{Number}, 4.0, ScopeLanguageOverridable, "Text Editor"},
		{"editor.insertSpaces", []Type{Boolean}, true, ScopeLanguageOverridable, "Text Editor"},
		{"editor.detectIndentation", []Type{Boolean}, true, ScopeLanguageOverridable, "Text Editor"},
		{"editor.wordSeparators", []Type{String}, "`~!@#$%^&*()-=+[{]}\\|;:'\",.<>/?", ScopeLanguageOverridable, "Text Editor"},
		{"files.associations", []Type{Object}, map[string]any{}, ScopeWindow, "Files"},
		{"files.encoding", []Type{String}, "utf8", ScopeLanguageOverridable, "Files"},
		{"files.eol", []Type{String}, "auto", ScopeLanguageOverridable, "Files"},
		{"files.autoGuessEncoding", []Type{Boolean}, false, ScopeLanguageOverridable, "Files"},
		{"explorer.autoReveal", []Type{Boolean, String}, true, ScopeWindow, "Explorer"},
		{"explorer.openEditors.visible", []Type{Number}, 9.0, ScopeWindow, "Explorer"},
		{"explorer.openEditors.sortOrder", []Type{String}, "editorOrder", ScopeWindow, "Explorer"},
		{"workbench.iconTheme", []Type{String, Null}, "vs-seti", ScopeWindow, "Workbench"},
	} {
		s, ok := r.Lookup(w.key)
		if !ok {
			t.Errorf("%s is not registered", w.key)
			continue
		}
		if !reflect.DeepEqual(s.Types, w.types) || !reflect.DeepEqual(s.Default, w.def) || s.Scope != w.scope || s.Category != w.category || s.Description == "" {
			t.Errorf("%s = %+v", w.key, s)
		}
	}
	tab, _ := r.Lookup("editor.tabSize")
	if *tab.Minimum != 1 || *tab.Maximum != 100 {
		t.Errorf("tab size range %v %v", *tab.Minimum, *tab.Maximum)
	}
	eol, _ := r.Lookup("files.eol")
	assertEqual(t, "eol enum", eol.Enum, []any{"\n", "\r\n", "auto"})
	assertEqual(t, "eol descriptions", len(eol.EnumDescriptions), 3)
	enc, _ := r.Lookup("files.encoding")
	if len(enc.Enum) < 20 || len(enc.Enum) != len(enc.EnumDescriptions) || !inList(enc.Enum, "utf8bom") || !inList(enc.Enum, "windows1252") {
		t.Errorf("encodings %v", enc.Enum)
	}
}

func TestPaths(t *testing.T) {
	paths := storagetest.Isolate(t)
	user, err := UserSettingsPath(paths, storage.DefaultProfile)
	if err != nil || user != filepath.Join(paths.ConfigHome, "emetos", "settings.json") {
		t.Errorf("user path %q, %v", user, err)
	}
	profile, err := UserSettingsPath(paths, "work")
	if err != nil || profile != filepath.Join(paths.ConfigHome, "emetos", "profiles", "work", "settings.json") {
		t.Errorf("profile path %q, %v", profile, err)
	}
	if _, err := UserSettingsPath(paths, "../x"); err == nil {
		t.Error("an unsafe profile name was accepted")
	} else {
		assertRoute(t, err, "ProfilePathUnsafe")
	}
	if got := WorkspaceSettingsPath("/p/q"); got != "/p/q/.emetos/settings.json" {
		t.Errorf("workspace path %q", got)
	}
}

func TestGetDefaults(t *testing.T) {
	f := newFixture(t)
	if got := Get[int](f.store, "editor.tabSize", ""); got != 4 {
		t.Errorf("tab size %d", got)
	}
	if got := Get[bool](f.store, "editor.insertSpaces", "go"); !got {
		t.Error("insert spaces")
	}
	if got := Get[string](f.store, "files.eol", ""); got != "auto" {
		t.Errorf("eol %q", got)
	}
	if got := Get[float64](f.store, "editor.tabSize", ""); got != 4 {
		t.Errorf("tab size %v", got)
	}
	if got := Get[any](f.store, "editor.tabSize", ""); got != 4.0 {
		t.Errorf("tab size %v", got)
	}
}

// The precedence of the layers: defaults < user < workspace, and a language block over the
// plain settings of all of them.
func TestPrecedence(t *testing.T) {
	const key = "editor.tabSize"
	cases := []struct {
		name      string
		user, ws  string
		languages map[string]int // language id -> expected; "" is no language
	}{
		{"defaults", ``, ``, map[string]int{"": 4, "go": 4}},
		{"user", `{"editor.tabSize": 2}`, ``, map[string]int{"": 2, "go": 2}},
		{"workspace over user", `{"editor.tabSize": 2}`, `{"editor.tabSize": 8}`, map[string]int{"": 8, "go": 8}},
		{"nested form", `{"editor": {"tabSize": 3}}`, ``, map[string]int{"": 3}},
		{"user language block", `{"editor.tabSize": 2, "[go]": {"editor.tabSize": 6}}`, ``, map[string]int{"": 2, "go": 6, "json": 2}},
		{"workspace language block over workspace", ``, `{"editor.tabSize": 8, "[go]": {"editor.tabSize": 5}}`, map[string]int{"": 8, "go": 5, "json": 8}},
		{"user language block over workspace plain", `{"[go]": {"editor.tabSize": 3}}`, `{"editor.tabSize": 8}`, map[string]int{"": 8, "go": 3}},
		{"workspace block over user block", `{"[go]": {"editor.tabSize": 3}}`, `{"[go]": {"editor.tabSize": 7}}`, map[string]int{"": 4, "go": 7}},
		{"block of several languages", `{"[go][json]": {"editor.tabSize": 2}}`, ``, map[string]int{"go": 2, "json": 2, "yaml": 4}},
		{"alone beats shared", `{"[go][json]": {"editor.tabSize": 2}, "[go]": {"editor.tabSize": 9}}`, ``, map[string]int{"go": 9, "json": 2}},
		{"dotted key in a block with nested form", `{"[go]": {"editor": {"tabSize": 5}}}`, ``, map[string]int{"go": 5, "": 4}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			if c.user != "" {
				f.write(f.userPath, c.user)
			}
			if c.ws != "" {
				f.write(f.wsPath, c.ws)
			}
			f.reload()
			for lang, want := range c.languages {
				if got := Get[int](f.store, key, lang); got != want {
					t.Errorf("language %q: %d, want %d", lang, got, want)
				}
			}
		})
	}
}

func TestLanguageDefaults(t *testing.T) {
	f := newFixture(t)
	if err := f.store.Registry().RegisterLanguageDefaults("go", map[string]any{"editor.insertSpaces": false, "editor.tabSize": 8}); err != nil {
		t.Fatal(err)
	}
	// Registered after the store was made: it still sees them.
	if Get[bool](f.store, "editor.insertSpaces", "go") || !Get[bool](f.store, "editor.insertSpaces", "") || Get[int](f.store, "editor.tabSize", "go") != 8 {
		t.Error("language defaults")
	}
	// As in VS Code, a language default is over the user's plain setting, not over a block.
	f.write(f.userPath, `{"editor.insertSpaces": true}`)
	f.reload()
	if Get[bool](f.store, "editor.insertSpaces", "go") {
		t.Error("the user's plain setting won over a language default")
	}
	f.write(f.userPath, `{"[go]": {"editor.insertSpaces": true}}`)
	f.reload()
	if !Get[bool](f.store, "editor.insertSpaces", "go") {
		t.Error("the user's block lost to a language default")
	}
	in := f.store.Inspect("editor.insertSpaces", "go")
	if in.Source != SourceUser || !in.DefaultLanguage.Ok || in.DefaultLanguage.Value != false {
		t.Errorf("inspect %+v", in)
	}
}

func TestInvalidValuesFallBackToTheDefault(t *testing.T) {
	f := newFixture(t)
	f.write(f.userPath, `{"test.size": 50, "test.mode": "three", "editor.tabSize": "wide", "test.list": ["a", 1], "editor.insertSpaces": false}`)
	f.write(f.wsPath, `{"test.size": 4}`)
	f.reload()
	if got := Get[int](f.store, "test.size", ""); got != 4 {
		t.Errorf("a valid workspace value over an invalid user one: %d", got)
	}
	if got := Get[string](f.store, "test.mode", ""); got != "one" {
		t.Errorf("mode %q", got)
	}
	if got := Get[int](f.store, "editor.tabSize", ""); got != 4 {
		t.Errorf("tab size %d", got)
	}
	if got := Get[[]string](f.store, "test.list", ""); len(got) != 0 {
		t.Errorf("list %v", got)
	}
	if Get[bool](f.store, "editor.insertSpaces", "") {
		t.Error("a valid value was replaced")
	}
	// The invalid ones are reported.
	reported := map[string]ProblemKind{}
	for _, p := range f.store.Problems() {
		reported[p.Key] = p.Kind
	}
	for _, key := range []string{"test.size", "test.mode", "editor.tabSize", "test.list"} {
		if reported[key] != ProblemInvalidValue {
			t.Errorf("%s is not reported: %v", key, reported)
		}
	}
	// An invalid workspace value falls back to the default, not to the user's value.
	f.write(f.userPath, `{"test.size": 2}`)
	f.write(f.wsPath, `{"test.size": 0}`)
	f.reload()
	if got := Get[int](f.store, "test.size", ""); got != 3 {
		t.Errorf("size %d", got)
	}
}

func TestInvalidValueInALanguageBlock(t *testing.T) {
	f := newFixture(t)
	f.write(f.userPath, `{"test.mode": "two", "[go]": {"test.mode": "nope"}}`)
	f.reload()
	if got := Get[string](f.store, "test.mode", "go"); got != "one" {
		t.Errorf("go: %q", got)
	}
	if got := Get[string](f.store, "test.mode", ""); got != "two" {
		t.Errorf("plain: %q", got)
	}
	found := false
	for _, p := range f.store.Problems() {
		found = found || (p.Kind == ProblemInvalidValue && p.Key == "test.mode")
	}
	if !found {
		t.Errorf("not reported: %+v", f.store.Problems())
	}
}

func TestGetMisuse(t *testing.T) {
	f := newFixture(t)
	if !debugBuild {
		t.Skip("release builds do not panic")
	}
	expectMisuse := func(name string, fn func()) {
		t.Helper()
		defer func() {
			err, _ := recover().(error)
			if err == nil {
				t.Errorf("%s: no panic", name)
				return
			}
			assertRoute(t, err, "SettingMisuse")
		}()
		fn()
	}
	expectMisuse("unregistered", func() { Get[int](f.store, "not.registered", "") })
	expectMisuse("wrong type", func() { Get[string](f.store, "editor.tabSize", "") })
	expectMisuse("wrong type 2", func() { Get[[]string](f.store, "editor.insertSpaces", "") })
}

func TestGetTypes(t *testing.T) {
	f := newFixture(t)
	f.write(f.userPath, `{"test.list": ["a", "b"], "test.obj": {"a": false, "b": 2}, "test.size": 7}`)
	f.reload()
	assertEqual(t, "list", Get[[]string](f.store, "test.list", ""), []string{"a", "b"})
	assertEqual(t, "list as any", Get[[]any](f.store, "test.list", ""), []any{"a", "b"})
	assertEqual(t, "object", Get[map[string]any](f.store, "test.obj", ""), map[string]any{"a": false, "b": 2.0})
	type obj struct {
		A bool `json:"a"`
		B int  `json:"b"`
	}
	assertEqual(t, "struct", Get[obj](f.store, "test.obj", ""), obj{A: false, B: 2})
	assertEqual(t, "int64", Get[int64](f.store, "test.size", ""), int64(7))
	assertEqual(t, "int32", Get[int32](f.store, "test.size", ""), int32(7))
	assertEqual(t, "uint", Get[uint](f.store, "test.size", ""), uint(7))
	assertEqual(t, "float32", Get[float32](f.store, "test.size", ""), float32(7))
	// Objects are merged across layers.
	f.write(f.wsPath, `{"test.obj": {"c": 1}}`)
	f.reload()
	assertEqual(t, "merged object", Get[map[string]any](f.store, "test.obj", ""), map[string]any{"a": false, "b": 2.0, "c": 1.0})
}

func TestInspect(t *testing.T) {
	f := newFixture(t)
	f.write(f.userPath, `{"editor.tabSize": 2, "[go]": {"editor.tabSize": 6}, "test.app": true}`)
	f.write(f.wsPath, `{"editor.tabSize": 8, "test.app": true}`)
	f.reload()
	in := f.store.Inspect("editor.tabSize", "go")
	want := Inspected{
		Default: opt(4.0), User: opt(2.0), Workspace: opt(8.0),
		UserLanguage: opt(6.0), Effective: opt(6.0), Source: SourceUser,
	}
	assertEqual(t, "go", in, want)
	in = f.store.Inspect("editor.tabSize", "")
	assertEqual(t, "no language", in, Inspected{Default: opt(4.0), User: opt(2.0), Workspace: opt(8.0), Effective: opt(8.0), Source: SourceWorkspace})
	in = f.store.Inspect("test.app", "")
	if !in.WorkspaceIgnored || in.Source != SourceUser || in.Effective.Value != true || in.Workspace.Value != true {
		t.Errorf("application setting in a workspace: %+v", in)
	}
	in = f.store.Inspect("files.eol", "")
	if in.Source != SourceDefault || in.Effective.Value != "auto" {
		t.Errorf("default: %+v", in)
	}
	in = f.store.Inspect("unknown.key", "")
	if in.Effective.Ok {
		t.Errorf("unknown key: %+v", in)
	}
}

func TestReloadAndProblems(t *testing.T) {
	f := newFixture(t)
	t.Run("missing files are empty layers", func(t *testing.T) {
		ev := f.reload()
		if !ev.IsEmpty() || len(f.store.Problems()) != 0 {
			t.Errorf("event %+v problems %+v", ev, f.store.Problems())
		}
	})
	t.Run("problems of both files", func(t *testing.T) {
		f.write(f.userPath, `{"editor.tabSize": 2, "mystery.setting": 1, "mystery.nested": {"x": 1}, "a": 1, "a.b": 2, "files": {"eol": "\n"} `)
		f.write(f.wsPath, `{"test.app": true, "editor.insertSpaces": false}`)
		f.reload()
		kinds := map[ProblemKind][]string{}
		for _, p := range f.store.Problems() {
			kinds[p.Kind] = append(kinds[p.Kind], p.Target.String()+":"+p.Key)
		}
		if len(kinds[ProblemSyntax]) == 0 {
			t.Error("the missing brace is not reported")
		}
		assertEqual(t, "unknown", kinds[ProblemUnknownKey], []string{"user:a", "user:mystery.nested.x", "user:mystery.setting"})
		assertEqual(t, "not in workspace", kinds[ProblemNotInWorkspace], []string{"workspace:test.app"})
		if len(kinds[ProblemConflict]) != 1 {
			t.Errorf("conflicts %v", kinds[ProblemConflict])
		}
		// The tolerant parser still gives what it can.
		if Get[int](f.store, "editor.tabSize", "") != 2 || Get[string](f.store, "files.eol", "") != "\n" || Get[bool](f.store, "editor.insertSpaces", "") {
			t.Error("values of a file with errors were dropped")
		}
		for _, p := range f.store.Problems() {
			if p.Kind == ProblemSyntax && (p.Target != TargetUser || p.Message == "" || p.Offset <= 0) {
				t.Errorf("syntax problem %+v", p)
			}
		}
	})
	t.Run("a file that is no object", func(t *testing.T) {
		f.write(f.userPath, `[1, 2]`)
		f.write(f.wsPath, ``)
		f.reload()
		found := false
		for _, p := range f.store.Problems() {
			found = found || p.Kind == ProblemNotObject
		}
		if !found || Get[int](f.store, "editor.tabSize", "") != 4 {
			t.Errorf("problems %+v", f.store.Problems())
		}
	})
	t.Run("unreadable file keeps the old settings", func(t *testing.T) {
		f.write(f.userPath, `{"editor.tabSize": 2}`)
		f.reload()
		if err := os.Remove(f.userPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(f.userPath, 0o755); err != nil { // a folder where the file was
			t.Fatal(err)
		}
		_, err := f.store.Reload(context.Background())
		assertRoute(t, err, "FileIsDirectory")
		if got := Get[int](f.store, "editor.tabSize", ""); got != 2 {
			t.Errorf("tab size %d", got)
		}
	})
	t.Run("a store without a workspace", func(t *testing.T) {
		s := NewStore(DefaultRegistry(), StoreOptions{UserPath: f.userPath})
		if _, err := s.Reload(context.Background()); err == nil {
			t.Log("the user path is a folder in this test")
		}
		s = NewStore(DefaultRegistry(), StoreOptions{})
		if _, err := s.Reload(context.Background()); err != nil {
			t.Error(err)
		}
		assertRoute(t, s.Update(context.Background(), "editor.tabSize", 2, TargetUser), "SettingsNoWorkspace")
	})
}

func TestChangeEvents(t *testing.T) {
	f := newFixture(t)
	t.Run("keys that changed", func(t *testing.T) {
		f.write(f.userPath, `{"editor.tabSize": 2, "files.eol": "\n"}`)
		ev := f.reload()
		assertEqual(t, "keys", ev.Keys, []string{"editor.tabSize", "files.eol"})
		assertEqual(t, "targets", ev.Targets, []Target{TargetUser})
		if len(f.listeners) != 1 {
			t.Fatalf("%d events delivered", len(f.listeners))
		}
		for _, c := range []struct {
			section, lang string
			want          bool
		}{
			{"editor.tabSize", "", true}, {"editor", "", true}, {"files", "", true}, {"files.eol", "go", true},
			{"editor.insertSpaces", "", false}, {"edit", "", false}, {"editor.tabSize.x", "", true}, {"", "", false}, {"search", "", false},
		} {
			if got := ev.Affects(c.section, c.lang); got != c.want {
				t.Errorf("Affects(%q, %q) = %v", c.section, c.lang, got)
			}
		}
	})
	t.Run("a reload that changes nothing says nothing", func(t *testing.T) {
		before := len(f.listeners)
		ev := f.reload()
		if !ev.IsEmpty() || len(ev.Targets) != 0 || len(f.listeners) != before {
			t.Errorf("event %+v", ev)
		}
	})
	t.Run("a value removed", func(t *testing.T) {
		f.write(f.userPath, `{"files.eol": "\n"}`)
		ev := f.reload()
		assertEqual(t, "keys", ev.Keys, []string{"editor.tabSize"})
	})
	t.Run("a change of the workspace", func(t *testing.T) {
		f.write(f.wsPath, `{"editor.insertSpaces": false}`)
		ev := f.reload()
		assertEqual(t, "keys", ev.Keys, []string{"editor.insertSpaces"})
		assertEqual(t, "targets", ev.Targets, []Target{TargetWorkspace})
	})
	t.Run("a value that only an unrelated layer hides is not a change", func(t *testing.T) {
		f.write(f.userPath, `{"files.eol": "\n", "editor.insertSpaces": true}`)
		ev := f.reload()
		if len(ev.Keys) != 0 {
			t.Errorf("keys %v", ev.Keys)
		}
		assertEqual(t, "targets", ev.Targets, []Target{TargetUser})
	})
	t.Run("language blocks", func(t *testing.T) {
		f.write(f.userPath, `{"files.eol": "\n", "editor.insertSpaces": true, "[go]": {"editor.tabSize": 8}}`)
		ev := f.reload()
		if len(ev.Keys) != 0 {
			t.Errorf("base keys %v", ev.Keys)
		}
		assertEqual(t, "languages", ev.Languages, map[string][]string{"go": {"editor.tabSize"}})
		if !ev.Affects("editor.tabSize", "go") || ev.Affects("editor.tabSize", "json") || !ev.Affects("editor", "") || ev.Affects("files", "") {
			t.Errorf("Affects wrong for %+v", ev)
		}
	})
	t.Run("nested keys are compared by leaf", func(t *testing.T) {
		f := newFixture(t)
		f.write(f.userPath, `{"editor": {"tabSize": 4, "insertSpaces": true}}`)
		f.reload()
		f.write(f.userPath, `{"editor": {"tabSize": 4, "insertSpaces": false}}`)
		ev := f.reload()
		assertEqual(t, "keys", ev.Keys, []string{"editor.insertSpaces"})
		if ev.Affects("editor.tabSize", "") {
			t.Error("the tab size did not change")
		}
	})
	t.Run("unsubscribe", func(t *testing.T) {
		g := newFixture(t)
		count := 0
		off := g.store.OnChange(func(ChangeEvent) { count++ })
		g.write(g.userPath, `{"files.eol": "\n"}`)
		g.reload()
		off()
		g.write(g.userPath, `{"files.eol": "\r\n"}`)
		g.reload()
		if count != 1 {
			t.Errorf("%d calls", count)
		}
	})
}

func TestUpdate(t *testing.T) {
	ctx := context.Background()
	t.Run("creates the file and its folder", func(t *testing.T) {
		f := newFixture(t)
		if _, err := os.Stat(filepath.Dir(f.wsPath)); err == nil {
			t.Fatal("the folder exists")
		}
		if err := f.store.Update(ctx, "editor.tabSize", 2, TargetWorkspace); err != nil {
			t.Fatal(err)
		}
		if got := f.read(f.wsPath); got != "{\n    \"editor.tabSize\": 2\n}" {
			t.Errorf("file %q", got)
		}
		if got := Get[int](f.store, "editor.tabSize", ""); got != 2 {
			t.Errorf("value %d", got)
		}
		if len(f.listeners) != 1 || !f.listeners[0].Affects("editor.tabSize", "") {
			t.Errorf("events %+v", f.listeners)
		}
		// Nothing else was left in the folder.
		entries, _ := os.ReadDir(filepath.Dir(f.wsPath))
		if len(entries) != 1 {
			t.Errorf("folder holds %v", entries)
		}
	})
	t.Run("the user file goes in the isolated config folder", func(t *testing.T) {
		f := newFixture(t)
		if err := f.store.Update(ctx, "files.eol", "\n", TargetUser); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(f.userPath, f.paths.ConfigHome) {
			t.Errorf("%s is outside %s", f.userPath, f.paths.ConfigHome)
		}
		if got := f.read(f.userPath); !strings.Contains(got, `"files.eol": "\n"`) {
			t.Errorf("file %q", got)
		}
	})
	t.Run("comments and layout are kept", func(t *testing.T) {
		f := newFixture(t)
		f.write(f.userPath, "{\n  // the font\n  \"editor.tabSize\": 2, // spaces\n\n  \"files.eol\": \"\\n\"\n}\n")
		f.reload()
		if err := f.store.Update(ctx, "editor.tabSize", 8, TargetUser); err != nil {
			t.Fatal(err)
		}
		want := "{\n  // the font\n  \"editor.tabSize\": 8, // spaces\n\n  \"files.eol\": \"\\n\"\n}\n"
		if got := f.read(f.userPath); got != want {
			t.Errorf("file %q, want %q", got, want)
		}
		if err := f.store.Update(ctx, "editor.insertSpaces", false, TargetUser); err != nil {
			t.Fatal(err)
		}
		got := f.read(f.userPath)
		if !strings.Contains(got, "// the font") || !strings.Contains(got, "// spaces") || !strings.Contains(got, `"editor.insertSpaces": false`) {
			t.Errorf("file %q", got)
		}
		// The new text is valid and read back.
		if Get[bool](f.store, "editor.insertSpaces", "") || Get[int](f.store, "editor.tabSize", "") != 8 {
			t.Error("values not reloaded")
		}
	})
	t.Run("a setting written as nested objects is edited there", func(t *testing.T) {
		f := newFixture(t)
		f.write(f.userPath, "{\n  \"editor\": {\n    \"tabSize\": 2\n  }\n}")
		if err := f.store.Update(ctx, "editor.tabSize", 6, TargetUser); err != nil {
			t.Fatal(err)
		}
		if got := f.read(f.userPath); got != "{\n  \"editor\": {\n    \"tabSize\": 6\n  }\n}" {
			t.Errorf("file %q", got)
		}
		if err := f.store.Update(ctx, "editor.insertSpaces", false, TargetUser); err != nil {
			t.Fatal(err)
		}
		if got := f.read(f.userPath); !strings.Contains(got, `"editor.insertSpaces": false`) {
			t.Errorf("file %q", got)
		}
	})
	t.Run("language blocks", func(t *testing.T) {
		f := newFixture(t)
		f.write(f.userPath, "{\n  \"editor.tabSize\": 2\n}")
		if err := f.store.UpdateForLanguage(ctx, "editor.tabSize", "go", 8, TargetUser); err != nil {
			t.Fatal(err)
		}
		want := "{\n  \"editor.tabSize\": 2,\n  \"[go]\": {\n    \"editor.tabSize\": 8\n  }\n}"
		if got := f.read(f.userPath); got != want {
			t.Errorf("file %q, want %q", got, want)
		}
		if Get[int](f.store, "editor.tabSize", "go") != 8 || Get[int](f.store, "editor.tabSize", "") != 2 {
			t.Error("values")
		}
		if err := f.store.UpdateForLanguage(ctx, "editor.tabSize", "go", 3, TargetUser); err != nil {
			t.Fatal(err)
		}
		if Get[int](f.store, "editor.tabSize", "go") != 3 {
			t.Error("block not updated")
		}
		if err := f.store.RemoveForLanguage(ctx, "editor.tabSize", "go", TargetUser); err != nil {
			t.Fatal(err)
		}
		if Get[int](f.store, "editor.tabSize", "go") != 2 {
			t.Error("block value not removed")
		}
	})
	t.Run("remove", func(t *testing.T) {
		f := newFixture(t)
		f.write(f.userPath, "{\n  // keep\n  \"files.eol\": \"\\n\",\n  \"editor.tabSize\": 2\n}")
		if err := f.store.Remove(ctx, "editor.tabSize", TargetUser); err != nil {
			t.Fatal(err)
		}
		if got := f.read(f.userPath); got != "{\n  // keep\n  \"files.eol\": \"\\n\"\n}" {
			t.Errorf("file %q", got)
		}
		if Get[int](f.store, "editor.tabSize", "") != 4 {
			t.Error("default not back")
		}
		// Removing what is not there is fine and writes nothing.
		before := len(f.listeners)
		if err := f.store.Remove(ctx, "editor.insertSpaces", TargetUser); err != nil || len(f.listeners) != before {
			t.Errorf("err %v, events %d", err, len(f.listeners)-before)
		}
	})
	t.Run("setting the same value changes nothing", func(t *testing.T) {
		f := newFixture(t)
		f.write(f.userPath, "{\n  \"editor.tabSize\": 2\n}")
		f.reload()
		info1, _ := os.Stat(f.userPath)
		before := len(f.listeners)
		if err := f.store.Update(ctx, "editor.tabSize", 2, TargetUser); err != nil {
			t.Fatal(err)
		}
		info2, _ := os.Stat(f.userPath)
		if !info2.ModTime().Equal(info1.ModTime()) || len(f.listeners) != before {
			t.Error("the file was rewritten")
		}
	})
	t.Run("the line ending and the BOM of the file are kept", func(t *testing.T) {
		f := newFixture(t)
		f.write(f.userPath, "\xef\xbb\xbf{\r\n  \"editor.tabSize\": 2\r\n}\r\n")
		if err := f.store.Update(ctx, "editor.insertSpaces", false, TargetUser); err != nil {
			t.Fatal(err)
		}
		want := "\xef\xbb\xbf{\r\n  \"editor.tabSize\": 2,\r\n  \"editor.insertSpaces\": false\r\n}\r\n"
		if got := f.read(f.userPath); got != want {
			t.Errorf("file %q, want %q", got, want)
		}
	})
	t.Run("the indentation of the file wins", func(t *testing.T) {
		f := newFixture(t)
		f.write(f.userPath, "{\n\t\"editor.tabSize\": 2\n}")
		if err := f.store.Update(ctx, "files.eol", "\n", TargetUser); err != nil {
			t.Fatal(err)
		}
		if got := f.read(f.userPath); got != "{\n\t\"editor.tabSize\": 2,\n\t\"files.eol\": \"\\n\"\n}" {
			t.Errorf("file %q", got)
		}
		f.write(f.userPath, "{\n   // odd\n   \"editor.tabSize\": 2\n}")
		if err := f.store.Update(ctx, "files.eol", "\n", TargetUser); err != nil {
			t.Fatal(err)
		}
		if got := f.read(f.userPath); !strings.Contains(got, "\n   \"files.eol\"") {
			t.Errorf("file %q", got)
		}
		f.write(f.userPath, "{\n \t\"editor.tabSize\": 2\n}")
		if err := f.store.Update(ctx, "files.eol", "\n", TargetUser); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("the indentation follows editor.tabSize and editor.insertSpaces", func(t *testing.T) {
		f := newFixture(t)
		f.write(f.userPath, `{"editor.tabSize": 2, "editor.insertSpaces": false}`)
		f.reload()
		if err := f.store.Update(ctx, "files.eol", "\n", TargetUser); err != nil {
			t.Fatal(err)
		}
		if got := f.read(f.userPath); !strings.Contains(got, "\n\t\"files.eol\"") {
			t.Errorf("file %q", got)
		}
	})
	t.Run("objects and arrays", func(t *testing.T) {
		f := newFixture(t)
		if err := f.store.Update(ctx, "test.list", []string{"x", "y"}, TargetUser); err != nil {
			t.Fatal(err)
		}
		if err := f.store.Update(ctx, "test.obj", map[string]any{"k": 1}, TargetUser); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, "list", Get[[]string](f.store, "test.list", ""), []string{"x", "y"})
		assertEqual(t, "obj", Get[map[string]any](f.store, "test.obj", ""), map[string]any{"a": true, "k": 1.0})
	})
}

func TestUpdateErrors(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cases := []struct {
		name   string
		update func() error
		route  string
	}{
		{"unregistered", func() error { return f.store.Update(ctx, "not.registered", 1, TargetUser) }, "Store.write.SettingUnregistered"},
		{"invalid value", func() error { return f.store.Update(ctx, "editor.tabSize", 0, TargetUser) }, "Store.write.SettingValueInvalid"},
		{"wrong type", func() error { return f.store.Update(ctx, "editor.insertSpaces", "yes", TargetUser) }, "Store.write.SettingValueInvalid"},
		{"not JSON", func() error { return f.store.Update(ctx, "editor.tabSize", make(chan int), TargetUser) }, "SettingValueNotJSON"},
		{"application setting in the workspace", func() error { return f.store.Update(ctx, "test.app", true, TargetWorkspace) }, "Store.write.SettingScopeInvalid"},
		{"unknown target", func() error { return f.store.Update(ctx, "editor.tabSize", 2, Target(9)) }, "Store.pathOf.SettingsNoWorkspace"},
		{"remove from an unknown target", func() error { return f.store.Remove(ctx, "editor.tabSize", Target(0)) }, "Store.pathOf.SettingsNoWorkspace"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assertRoute(t, c.update(), c.route) })
	}
	t.Run("the application setting goes in the user file", func(t *testing.T) {
		if err := f.store.Update(ctx, "test.app", true, TargetUser); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a file with errors is not written", func(t *testing.T) {
		f.write(f.userPath, `{"editor.tabSize": 2,, }`)
		before := f.read(f.userPath)
		assertRoute(t, f.store.Update(ctx, "files.eol", "\n", TargetUser), "Store.write.SettingsFileInvalid")
		if f.read(f.userPath) != before {
			t.Error("the file was changed")
		}
		f.write(f.userPath, `[1]`)
		assertRoute(t, f.store.Update(ctx, "files.eol", "\n", TargetUser), "Store.write.SettingsFileInvalid")
	})
	t.Run("a file that cannot be read", func(t *testing.T) {
		if err := os.Remove(f.userPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(f.userPath, 0o755); err != nil {
			t.Fatal(err)
		}
		assertRoute(t, f.store.Update(ctx, "files.eol", "\n", TargetUser), "FileIsDirectory")
	})
	t.Run("a folder that cannot be created", func(t *testing.T) {
		g := newFixture(t)
		g.write(filepath.Join(g.folder, "x"), "x")
		if err := os.Chmod(g.folder, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(g.folder, 0o700) })
		if os.Geteuid() == 0 {
			t.Skip("root can write anywhere")
		}
		assertRoute(t, g.store.Update(ctx, "files.eol", "\n", TargetWorkspace), "Store.write.SettingsWriteFailed")
	})
	t.Run("a folder name taken by a file", func(t *testing.T) {
		g := newFixture(t)
		g.write(filepath.Join(g.folder, ".emetos"), "I am a file")
		assertRoute(t, g.store.Update(ctx, "files.eol", "\n", TargetWorkspace), "FileReadFailed")
	})
	t.Run("a cancelled context", func(t *testing.T) {
		g := newFixture(t)
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if err := g.store.Update(cancelled, "files.eol", "\n", TargetUser); err == nil {
			t.Error("no error")
		}
		if _, err := os.Stat(g.userPath); err == nil {
			t.Error("the file was written")
		}
	})
}

func TestStoreIsSafeForConcurrentUse(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 50; j++ {
				Get[int](f.store, "editor.tabSize", "go")
				f.store.Inspect("editor.tabSize", "go")
				f.store.Problems()
			}
		}()
	}
	for j := 0; j < 10; j++ {
		if err := f.store.Update(ctx, "editor.tabSize", j+1, TargetUser); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	if got := Get[int](f.store, "editor.tabSize", ""); got != 10 {
		t.Errorf("tab size %d", got)
	}
}

func TestErrorsPackageIsUsed(t *testing.T) {
	// Every failure of this package carries a tag, so the UI can tell them apart.
	f := newFixture(t)
	err := f.store.Update(context.Background(), "nope", 1, TargetUser)
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "SettingUnregistered" {
		t.Errorf("error %v", err)
	}
}

func TestStoreSetWorkspacePath(t *testing.T) {
	f := newFixture(t)
	other := filepath.Join(t.TempDir(), "other")
	f.write(f.wsPath, `{"test.size": 7}`)
	f.write(WorkspaceSettingsPath(other), `{"test.size": 5}`)
	f.reload()
	size := func() float64 { return Get[float64](f.store, "test.size", "") }
	tests := []struct {
		name string
		path string
		want float64
	}{
		{"the layer of the old folder is replaced", WorkspaceSettingsPath(other), 5},
		{"a folder without a file has the defaults", WorkspaceSettingsPath(filepath.Join(t.TempDir(), "empty")), 3},
		{"no folder drops the layer", "", 3},
		{"the first folder again", f.wsPath, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.listeners = nil
			if _, err := f.store.SetWorkspacePath(context.Background(), tt.path); err != nil {
				t.Fatal(err)
			}
			if got := size(); got != tt.want {
				t.Errorf("test.size = %v, want %v", got, tt.want)
			}
		})
	}
	if err := f.store.Update(context.Background(), "test.size", 4, TargetWorkspace); err != nil {
		t.Fatal(err)
	}
	if got := f.read(f.wsPath); !strings.Contains(got, "4") {
		t.Errorf("the update went to %q", got)
	}
	if _, err := f.store.SetWorkspacePath(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	err := f.store.Update(context.Background(), "test.size", 4, TargetWorkspace)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("SettingsNoWorkspace") {
		t.Errorf("with no folder an update of the workspace fails with SettingsNoWorkspace, got %v", err)
	}
}
