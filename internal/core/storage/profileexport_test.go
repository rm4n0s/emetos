package storage_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/storage"
)

func TestValidSnippetFileName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"go.json", true},
		{"my snippets.code-snippets", true},
		{"a.b.json", true},
		{".hidden.json", true},
		{"ünï.json", true},
		{"", false},
		{".", false},
		{"..", false},
		{".json", false},
		{".code-snippets", false},
		{"../evil.json", false},
		{"..\\evil.json", false},
		{"/abs.json", false},
		{"a/b.json", false},
		{"a\\b.json", false},
		{"nul\x00.json", false},
		{"new\nline.json", false},
		{"tab\t.json", false},
		{"del\x7f.json", false},
		{"go.txt", false},
		{"go.JSON", false},
		{"go", false},
		{"go.json.bak", false},
		{"bad\xffutf.json", false},
		{strings.Repeat("a", 251) + ".json", false},
		{strings.Repeat("a", 250) + ".json", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := storage.ValidSnippetFileName(tt.name); got != tt.want {
				t.Errorf("ValidSnippetFileName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestParseProfileExport(t *testing.T) {
	huge := `{"name":"Big","settings":"` + strings.Repeat("a", storage.MaxProfileExportSize) + `"}`
	tests := []struct {
		name string
		text string
		want storage.ProfileExport
		tag  string
	}{
		{"all keys", `{"name":"Work","settings":"{}","keybindings":"[]","snippets":{"go.json":"{}","all.code-snippets":"x"}}`,
			storage.ProfileExport{Name: "Work", Settings: "{}", Keybindings: "[]", Snippets: map[string]string{"go.json": "{}", "all.code-snippets": "x"}}, ""},
		{"only a name", `{"name":"Work"}`, storage.ProfileExport{Name: "Work", Snippets: map[string]string{}}, ""},
		{"nulls", `{"name":"Work","settings":null,"keybindings":null,"snippets":null}`, storage.ProfileExport{Name: "Work", Snippets: map[string]string{}}, ""},
		{"a null snippet", `{"name":"Work","snippets":{"go.json":null}}`, storage.ProfileExport{Name: "Work", Snippets: map[string]string{"go.json": ""}}, ""},
		{"unknown keys are ignored", `{"name":"Work","extensions":[1,2],"version":3}`, storage.ProfileExport{Name: "Work", Snippets: map[string]string{}}, ""},
		{"not JSON", `not json`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"empty", ``, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"an array", `[]`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"null", `null`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"text after the value", `{"name":"Work"} {}`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"no name", `{"settings":"{}"}`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"a name of the wrong type", `{"name":3}`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"an empty name", `{"name":""}`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"an invalid name", `{"name":"a/b"}`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"a name with a space at the end", `{"name":"Work "}`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"settings of the wrong type", `{"name":"Work","settings":{}}`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"keybindings of the wrong type", `{"name":"Work","keybindings":[]}`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"snippets of the wrong type", `{"name":"Work","snippets":[]}`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"a snippet of the wrong type", `{"name":"Work","snippets":{"go.json":3}}`, storage.ProfileExport{}, "ProfileImportInvalid"},
		{"a snippet file up a folder", `{"name":"Work","snippets":{"../evil.json":"x"}}`, storage.ProfileExport{}, "ProfileImportSnippetInvalid"},
		{"an absolute snippet file", `{"name":"Work","snippets":{"/abs.json":"x"}}`, storage.ProfileExport{}, "ProfileImportSnippetInvalid"},
		{"a snippet file in a folder", `{"name":"Work","snippets":{"a/b.json":"x"}}`, storage.ProfileExport{}, "ProfileImportSnippetInvalid"},
		{"a dot", `{"name":"Work","snippets":{".":"x"}}`, storage.ProfileExport{}, "ProfileImportSnippetInvalid"},
		{"two dots", `{"name":"Work","snippets":{"..":"x"}}`, storage.ProfileExport{}, "ProfileImportSnippetInvalid"},
		{"an empty file name", `{"name":"Work","snippets":{"":"x"}}`, storage.ProfileExport{}, "ProfileImportSnippetInvalid"},
		{"a NUL in a file name", `{"name":"Work","snippets":{"a\u0000.json":"x"}}`, storage.ProfileExport{}, "ProfileImportSnippetInvalid"},
		{"a wrong extension", `{"name":"Work","snippets":{"a.sh":"x"}}`, storage.ProfileExport{}, "ProfileImportSnippetInvalid"},
		{"one bad file among good ones", `{"name":"Work","snippets":{"ok.json":"x","../bad.json":"y"}}`, storage.ProfileExport{}, "ProfileImportSnippetInvalid"},
		{"larger than the limit", huge, storage.ProfileExport{}, "ProfileImportTooLarge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := storage.ParseProfileExport([]byte(tt.text))
			if tt.tag != "" {
				if err == nil {
					t.Fatalf("expected an error, got %+v", got)
				}
				if appErr := routeOf(t, err); !appErr.HasRoute("ParseProfileExport") || !appErr.HasRoute(tt.tag) {
					t.Errorf("unexpected failure path: %s", appErr.Route())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != tt.want.Name || got.Settings != tt.want.Settings || got.Keybindings != tt.want.Keybindings ||
				len(got.Snippets) != len(tt.want.Snippets) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for k, v := range tt.want.Snippets {
				if got.Snippets[k] != v {
					t.Errorf("snippet %q = %q, want %q", k, got.Snippets[k], v)
				}
			}
		})
	}
}

func TestMarshalIsTheFormatOfTheSpec(t *testing.T) {
	e := storage.ProfileExport{Name: "Work", Settings: "{\n\t\"a\": \"<b>\"\n}\n", Snippets: map[string]string{"z.json": "1", "a.json": "2"}}
	data, err := e.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, data)
	}
	keys := make([]string, 0, 4)
	for k := range generic {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"keybindings", "name", "settings", "snippets"}) {
		t.Errorf("keys %v", keys)
	}
	if generic["keybindings"] != "" || generic["settings"] != e.Settings {
		t.Errorf("content %v", generic)
	}
	// the keys are in the order of the spec, the snippets sorted, the text has no HTML escapes and ends in a line break
	text := string(data)
	order := []string{`"name"`, `"settings"`, `"keybindings"`, `"snippets"`, `"a.json"`, `"z.json"`}
	last := -1
	for _, k := range order {
		i := strings.Index(text, k)
		if i <= last {
			t.Errorf("%s is out of order in\n%s", k, text)
		}
		last = i
	}
	if strings.Contains(text, `\u003c`) || !strings.HasSuffix(text, "}\n") {
		t.Errorf("text: %s", text)
	}
	// no snippets is an empty object, not null
	data, err = storage.ProfileExport{Name: "Work"}.Marshal()
	if err != nil || !strings.Contains(string(data), `"snippets": {}`) {
		t.Errorf("empty export: %s, %v", data, err)
	}
}

func TestExportOfAProfile(t *testing.T) {
	f := newProfileFixture(t, nil)
	if _, err := f.profiles.Create(t.Context(), "Work", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	f.put(t, "Work", "settings.json", `{"editor.fontSize": 20} // mine`+"\n")
	f.put(t, "Work", "keybindings.json", `[{"key":"ctrl+1","command":"x"}]`)
	f.put(t, "Work", "snippets/go.json", `{"a":{}}`)
	f.put(t, "Work", "snippets/all.code-snippets", `{"b":{}}`)
	f.put(t, "Work", "snippets/notes.txt", `not a snippet file`)
	f.put(t, "Work", "snippets/nested/inner.json", `{}`)
	if err := os.Symlink(filepath.Join(f.paths.ConfigHome, "elsewhere.json"), filepath.Join(f.paths.ConfigHome, "emetos", "profiles", "Work", "snippets", "link.json")); err != nil {
		t.Fatal(err)
	}
	f.put(t, storage.DefaultProfile, "settings.json", `{"editor.tabSize": 2}`)

	tests := []struct {
		name    string
		profile string
		want    storage.ProfileExport
	}{
		{"a profile with every file", "work", storage.ProfileExport{
			Name: "Work", Settings: `{"editor.fontSize": 20} // mine` + "\n", Keybindings: `[{"key":"ctrl+1","command":"x"}]`,
			Snippets: map[string]string{"go.json": `{"a":{}}`, "all.code-snippets": `{"b":{}}`}}},
		{"the default profile with missing files", "", storage.ProfileExport{Name: "Default", Settings: `{"editor.tabSize": 2}`, Snippets: map[string]string{}}},
		{"the default profile by its name", "default", storage.ProfileExport{Name: "Default", Settings: `{"editor.tabSize": 2}`, Snippets: map[string]string{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.profiles.Export(t.Context(), tt.profile)
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != tt.want.Name || got.Settings != tt.want.Settings || got.Keybindings != tt.want.Keybindings || len(got.Snippets) != len(tt.want.Snippets) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for k, v := range tt.want.Snippets {
				if got.Snippets[k] != v {
					t.Errorf("snippet %q = %q", k, got.Snippets[k])
				}
			}
		})
	}
}

func TestExportFailures(t *testing.T) {
	f := newProfileFixture(t, nil)
	if _, err := f.profiles.Create(t.Context(), "Work", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		setup   func(t *testing.T)
		ctx     context.Context
		profile string
		tag     string
	}{
		{"no such profile", func(*testing.T) {}, t.Context(), "Nope", "ProfileNotFound"},
		{"a file that is not UTF-8", func(t *testing.T) { f.put(t, "Work", "settings.json", "{\"a\": \"\xff\"}") }, t.Context(), "Work", "ProfileExportUnsupported"},
		{"a snippet that is not UTF-8", func(t *testing.T) {
			f.put(t, "Work", "settings.json", "{}")
			f.put(t, "Work", "snippets/bad.json", "\xc3\x28")
		}, t.Context(), "Work", "ProfileExportUnsupported"},
		{"a file past the limit", func(t *testing.T) {
			f.put(t, "Work", "snippets/bad.json", "{}")
			f.put(t, "Work", "keybindings.json", strings.Repeat("a", storage.MaxProfileExportSize+1))
		}, t.Context(), "Work", "ProfileExportTooLarge"},
		{"files together past the limit", func(t *testing.T) {
			f.put(t, "Work", "keybindings.json", strings.Repeat("a", storage.MaxProfileExportSize/2+1))
			f.put(t, "Work", "settings.json", strings.Repeat("a", storage.MaxProfileExportSize/2+1))
		}, t.Context(), "Work", "ProfileExportTooLarge"},
		{"a file that cannot be read", func(t *testing.T) {
			// a folder where the file should be
			dir, _ := f.paths.ConfigDir("Work")
			for _, name := range []string{"settings.json", "keybindings.json"} {
				_ = os.Remove(filepath.Join(dir, name))
			}
			if err := os.Mkdir(filepath.Join(dir, "settings.json"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, t.Context(), "Work", "ProfileExportFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			_, err := f.profiles.Export(tt.ctx, tt.profile)
			if err == nil {
				t.Fatal("expected an error")
			}
			if tt.tag == "ProfileNotFound" {
				if appErr := routeOf(t, err); !appErr.HasRoute(tt.tag) {
					t.Errorf("unexpected failure path: %s", appErr.Route())
				}
				return
			}
			if appErr := routeOf(t, err); !appErr.HasRoute("Profiles.Export") || !appErr.HasRoute(tt.tag) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestMarshalPastTheLimit(t *testing.T) {
	_, err := storage.ProfileExport{Name: "Work", Settings: strings.Repeat("a", storage.MaxProfileExportSize)}.Marshal()
	if err == nil || !routeOf(t, err).HasRoute("ProfileExportTooLarge") {
		t.Errorf("err = %v", err)
	}
}

func TestImportMakesTheProfileFromTheExport(t *testing.T) {
	f := newProfileFixture(t, nil)
	e := storage.ProfileExport{
		Name: "Other", Settings: "{ // comment\n}\n", Keybindings: "[]",
		Snippets: map[string]string{"go.json": `{"a":1}`, "all.code-snippets": `{"b":2}`},
	}
	name, err := f.profiles.Import(t.Context(), e, "Imported")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Imported" || !f.profiles.Exists(t.Context(), "imported") {
		t.Fatalf("name %q", name)
	}
	want := map[string]string{
		"settings.json": e.Settings, "keybindings.json": e.Keybindings,
		"snippets/go.json": `{"a":1}`, "snippets/all.code-snippets": `{"b":2}`,
	}
	got := f.files(t, "Imported")
	if len(got) != len(want) {
		t.Fatalf("files %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	// empty content makes no file and no folder
	if _, err := f.profiles.Import(t.Context(), storage.ProfileExport{Name: "x"}, "Bare"); err != nil {
		t.Fatal(err)
	}
	if got := f.files(t, "Bare"); len(got) != 0 {
		t.Errorf("an empty export made %v", got)
	}
	dir, _ := f.paths.ConfigDir("Imported")
	info, err := os.Stat(filepath.Join(dir, "settings.json"))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("settings.json: %v, %v", info, err)
	}
}

func TestImportFailures(t *testing.T) {
	failing := errors.New("InjectedFailure", "the disk is full")
	good := storage.ProfileExport{Name: "Work", Settings: "{}", Keybindings: "[]", Snippets: map[string]string{"a.json": "1", "b.json": "2"}}
	tests := []struct {
		name   string
		mutate func(*storage.ProfilesOptions)
		export storage.ProfileExport
		as     string
		tag    string
	}{
		{"an invalid name", nil, good, "a/b", "ProfileNameInvalid"},
		{"an empty name", nil, good, "", "ProfileNameInvalid"},
		{"the name of the default profile", nil, good, "default", "ProfileNameReserved"},
		{"a name that exists", nil, good, "EXISTING", "ProfileExists"},
		{"a snippet that goes up a folder", nil, storage.ProfileExport{Snippets: map[string]string{"../evil.json": "x"}}, "New", "ProfileImportSnippetInvalid"},
		{"an absolute snippet", nil, storage.ProfileExport{Snippets: map[string]string{"/tmp/evil.json": "x"}}, "New", "ProfileImportSnippetInvalid"},
		{"a snippet in a folder", nil, storage.ProfileExport{Snippets: map[string]string{"a/b.json": "x"}}, "New", "ProfileImportSnippetInvalid"},
		{"a snippet with the wrong extension", nil, storage.ProfileExport{Snippets: map[string]string{"a.sh": "x"}}, "New", "ProfileImportSnippetInvalid"},
		{"a write that fails", func(o *storage.ProfilesOptions) {
			n := 0
			o.WriteFile = func(path string, data []byte, perm os.FileMode) error {
				if n++; n == 3 {
					return failing
				}
				return os.WriteFile(path, data, perm)
			}
		}, good, "New", "ProfileImportFailed"},
		{"a rename into place that fails", func(o *storage.ProfilesOptions) {
			o.Rename = func(oldPath, newPath string) error { return failing }
		}, good, "New", "ProfileImportFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newProfileFixture(t, nil)
			if _, err := f.profiles.Create(t.Context(), "Existing", storage.CreateEmpty); err != nil {
				t.Fatal(err)
			}
			profiles := f.profiles
			if tt.mutate != nil {
				opts := storage.ProfilesOptions{}
				tt.mutate(&opts)
				profiles = storage.NewProfiles(f.paths, opts)
			}
			_, err := profiles.Import(t.Context(), tt.export, tt.as)
			if err == nil {
				t.Fatal("expected an error")
			}
			if appErr := routeOf(t, err); !appErr.HasRoute("Profiles.Import") || !appErr.HasRoute(tt.tag) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			// nothing is left but what was there before
			entries, rerr := os.ReadDir(filepath.Join(f.paths.ConfigHome, "emetos", "profiles"))
			if rerr != nil {
				t.Fatal(rerr)
			}
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			if !slices.Equal(names, []string{"Existing"}) {
				t.Errorf("the profiles folder holds %v", names)
			}
		})
	}
}

func TestImportNeverWritesOutsideTheSnippetsFolder(t *testing.T) {
	f := newProfileFixture(t, nil)
	outside := filepath.Join(f.paths.ConfigHome, "emetos", "profiles", "evil.json")
	for _, file := range []string{"../evil.json", "../../evil.json", outside, "..\\evil.json", "sub/../../evil.json"} {
		_, err := f.profiles.Import(t.Context(), storage.ProfileExport{Snippets: map[string]string{file: "x"}}, "New")
		if err == nil {
			t.Errorf("%q was accepted", file)
		}
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Errorf("a file was written outside: %v", err)
	}
	if f.profiles.Exists(t.Context(), "New") {
		t.Error("a profile was made")
	}
}

func TestExportThenImportRoundTrips(t *testing.T) {
	f := newProfileFixture(t, nil)
	if _, err := f.profiles.Create(t.Context(), "Work", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	f.put(t, "Work", "settings.json", "{\r\n\t// crlf and a tab\r\n\t\"a\": \"é 世界\"\r\n}")
	f.put(t, "Work", "keybindings.json", "\xef\xbb\xbf[]\n")
	f.put(t, "Work", "snippets/go.json", `{"a": "<b> & \"c\""}`)
	f.put(t, "Work", "snippets/all.code-snippets", "")
	path := filepath.Join(t.TempDir(), "work.json")

	e, err := f.profiles.Export(t.Context(), "Work")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.WriteProfileExport(t.Context(), path, e); err != nil {
		t.Fatal(err)
	}
	read, err := storage.ReadProfileExport(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if read.Name != "Work" {
		t.Errorf("name %q", read.Name)
	}
	if _, err := f.profiles.Import(t.Context(), read, "Copy"); err != nil {
		t.Fatal(err)
	}
	a, b := f.files(t, "Work"), f.files(t, "Copy")
	if len(a) != len(b) || len(a) != 4 {
		t.Fatalf("files %v and %v", a, b)
	}
	for k, v := range a {
		if b[k] != v {
			t.Errorf("%s: %q became %q", k, v, b[k])
		}
	}
}

func TestWriteProfileExport(t *testing.T) {
	dir := t.TempDir()
	e := storage.ProfileExport{Name: "Work", Settings: "{}"}

	path := filepath.Join(dir, "new.json")
	if err := storage.WriteProfileExport(t.Context(), path, e); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("a new file: %v, %v", info, err)
	}
	// a file that exists keeps its permissions and is replaced
	if err := os.WriteFile(filepath.Join(dir, "old.json"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := storage.WriteProfileExport(t.Context(), filepath.Join(dir, "old.json"), e); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dir, "old.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("an old file: %v, %v", info, err)
	}
	got, err := storage.ReadProfileExport(t.Context(), filepath.Join(dir, "old.json"))
	if err != nil || got.Settings != "{}" {
		t.Errorf("read back: %+v, %v", got, err)
	}
	// no temporary file stays
	entries, _ := os.ReadDir(dir)
	for _, en := range entries {
		if strings.HasSuffix(en.Name(), ".tmp") {
			t.Errorf("left %s", en.Name())
		}
	}
}

func TestWriteAndReadProfileExportFailures(t *testing.T) {
	dir := t.TempDir()
	big := storage.ProfileExport{Name: "Work", Settings: strings.Repeat("a", storage.MaxProfileExportSize)}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	bigFile := filepath.Join(dir, "big.json")
	if err := os.WriteFile(bigFile, []byte(strings.Repeat(" ", storage.MaxProfileExportSize+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		run  func() error
		fn   string
		tag  string
	}{
		{"write: too large", func() error { return storage.WriteProfileExport(t.Context(), filepath.Join(dir, "a.json"), big) }, "WriteProfileExport", "ProfileExportTooLarge"},
		{"write: a folder that is not there", func() error {
			return storage.WriteProfileExport(t.Context(), filepath.Join(dir, "no", "a.json"), storage.ProfileExport{Name: "W"})
		}, "WriteProfileExport", "ProfileExportWriteFailed"},
		{"write: a folder in the way", func() error {
			if err := os.Mkdir(filepath.Join(dir, "d.json"), 0o755); err != nil {
				return err
			}
			return storage.WriteProfileExport(t.Context(), filepath.Join(dir, "d.json"), storage.ProfileExport{Name: "W"})
		}, "WriteProfileExport", "ProfileExportWriteFailed"},
		{"write: cancelled", func() error {
			return storage.WriteProfileExport(cancelled, filepath.Join(dir, "c.json"), storage.ProfileExport{Name: "W"})
		}, "WriteProfileExport", "ProfileExportWriteFailed"},
		{"read: no such file", func() error {
			_, err := storage.ReadProfileExport(t.Context(), filepath.Join(dir, "no.json"))
			return err
		}, "ReadProfileExport", "ProfileImportReadFailed"},
		{"read: cancelled", func() error { _, err := storage.ReadProfileExport(cancelled, bigFile); return err }, "ReadProfileExport", "ProfileImportReadFailed"},
		{"read: too large", func() error { _, err := storage.ReadProfileExport(t.Context(), bigFile); return err }, "ReadProfileExport", "ProfileImportTooLarge"},
		{"read: a folder", func() error { _, err := storage.ReadProfileExport(t.Context(), dir); return err }, "ReadProfileExport", "ProfileImportReadFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("expected an error")
			}
			if appErr := routeOf(t, err); !appErr.HasRoute(tt.fn) || !appErr.HasRoute(tt.tag) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	entries, _ := os.ReadDir(dir)
	for _, en := range entries {
		if strings.HasSuffix(en.Name(), ".tmp") {
			t.Errorf("a failed write left %s", en.Name())
		}
	}
}

func TestFreeName(t *testing.T) {
	f := newProfileFixture(t, nil)
	for _, n := range []string{"Work", "Work 2", "work 3"} {
		if _, err := f.profiles.Create(t.Context(), n, storage.CreateEmpty); err != nil {
			t.Fatal(err)
		}
	}
	long := strings.Repeat("x", storage.MaxProfileNameLength)
	if _, err := f.profiles.Create(t.Context(), long, storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		base, want string
	}{
		{"Fresh", "Fresh"},
		{"Work", "Work 4"},
		{"WORK", "WORK 4"},
		{"Default", "Default 2"},
		{"default", "default 2"},
		{"a/b", "a-b"},
		{"My (profile)", "My -profile-"},
		{"  padded  ", "padded"},
		{long, strings.Repeat("x", storage.MaxProfileNameLength-2) + " 2"},
		{strings.Repeat("y", 70), strings.Repeat("y", storage.MaxProfileNameLength)},
	}
	for _, tt := range tests {
		t.Run(tt.base, func(t *testing.T) {
			got, err := f.profiles.FreeName(t.Context(), tt.base)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want || !storage.ValidProfileName(got) {
				t.Errorf("FreeName(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
	for _, base := range []string{"", "   "} {
		if _, err := f.profiles.FreeName(t.Context(), base); err == nil || !routeOf(t, err).HasRoute("ProfileNameInvalid") {
			t.Errorf("FreeName(%q): %v", base, err)
		}
	}
}
