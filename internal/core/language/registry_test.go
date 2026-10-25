package language

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rm4n0s/errors"
)

// ext makes an extension from its package.json and its files.
func ext(packageJSON string, files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{"package.json": {Data: []byte(packageJSON)}}
	for name, content := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return fsys
}

// wantTag checks that err is an error of the package with the tag.
func wantTag(t *testing.T, err error, tag string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(tag) {
		t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tag)
	}
}

func TestParseManifest(t *testing.T) {
	m, err := ParseManifest([]byte(`{
		"name": "x",
		"contributes": {
			"languages": [
				{"id": "a", "aliases": ["A", "a"], "extensions": [".a"], "filenames": ["Afile"], "filenamePatterns": ["*.aa"], "mimetypes": ["text/a"], "firstLine": "^#a", "configuration": "./c.json"},
				{"id": ""},
				{"aliases": ["no id"]},
				{"id": "b", "extensions": "not an array"},
				{"id": "c", "firstLine": 3},
				7
			],
			"grammars": [
				{"language": "a", "scopeName": "source.a", "path": "./a.json", "embeddedLanguages": {"x": "b"}, "tokenTypes": {"y": "string"}, "injectTo": ["source.z"]},
				{"scopeName": "source.only", "path": "./o.json"},
				{"scopeName": "no path"},
				{"language": 3, "scopeName": "s", "path": "p"},
				{"scopeName": "s", "path": "p", "embeddedLanguages": []},
				{"scopeName": "s", "path": "p", "tokenTypes": 1},
				{"scopeName": "s", "path": "p", "injectTo": "x"},
				"text"
			]
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "x" || len(m.Languages) != 1 || len(m.Grammars) != 2 {
		t.Fatalf("manifest: %+v", m)
	}
	want := LanguageContribution{ID: "a", Aliases: []string{"A", "a"}, Extensions: []string{".a"}, Filenames: []string{"Afile"},
		FilenamePatterns: []string{"*.aa"}, MIMETypes: []string{"text/a"}, FirstLine: "^#a", Configuration: "./c.json"}
	if !reflect.DeepEqual(m.Languages[0], want) {
		t.Errorf("language: %+v", m.Languages[0])
	}
	g := m.Grammars[0]
	if g.Language != "a" || g.ScopeName != "source.a" || g.Path != "./a.json" || g.EmbeddedLanguages["x"] != "b" || g.TokenTypes["y"] != "string" || g.InjectTo[0] != "source.z" {
		t.Errorf("grammar: %+v", g)
	}
	if m.Grammars[1].Language != "" {
		t.Errorf("a grammar that is only included has no language: %+v", m.Grammars[1])
	}
	tags := map[string]int{}
	for _, p := range m.Problems {
		appErr, _ := errors.FromError(p)
		tags[appErr.Tag]++
	}
	if tags["LanguageContributionInvalid"] != 5 || tags["GrammarContributionInvalid"] != 6 {
		t.Errorf("problems: %v", tags)
	}
	for _, p := range m.Problems {
		wantTag(t, p, "ContributionInvalid")
	}
	_, err = ParseManifest([]byte(`{`))
	wantTag(t, err, "ParseManifest.ManifestInvalid")
}

func TestRegistryMergesLanguages(t *testing.T) {
	r := NewRegistry()
	if err := r.AddExtension(ext(`{"name": "one", "contributes": {"languages": [
		{"id": "go", "aliases": ["Go"], "extensions": [".go"], "configuration": "./c.json"},
		{"id": "json", "extensions": [".json"]},
		{"id": "nameless"}]}}`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := r.AddExtension(ext(`{"name": "two", "contributes": {"languages": [
		{"id": "go", "aliases": ["Golang", "go"], "extensions": [".golang"], "mimetypes": ["text/x-go-source"]},
		{"id": "json", "extensions": [".tsbuildinfo"], "aliases": []},
		{"id": "nameless", "aliases": [""]}]}}`, nil)); err != nil {
		t.Fatal(err)
	}
	g, ok := r.Language("go")
	if !ok || g.Name != "Golang" {
		t.Errorf("the name is the first alias of the last contribution that has aliases: %+v", g)
	}
	if want := []string{".go", ".golang"}; !reflect.DeepEqual(g.Extensions, want) {
		t.Errorf("extensions = %v, want %v", g.Extensions, want)
	}
	if want := []string{"text/x-go", "text/x-go-source"}; !reflect.DeepEqual(g.MIMETypes, want) {
		t.Errorf("MIME types = %v, want %v", g.MIMETypes, want)
	}
	j, _ := r.Language("json")
	if j.Name != "json" || !reflect.DeepEqual(j.Extensions, []string{".json", ".tsbuildinfo"}) {
		t.Errorf("json: %+v", j)
	}
	if n, _ := r.Language("nameless"); n.Name != "nameless" {
		t.Errorf("a language without aliases is named by its id: %+v", n)
	}
	if _, ok := r.Language("nope"); ok || r.IsRegistered("nope") || !r.IsRegistered("go") {
		t.Error("IsRegistered")
	}
	// The extension that has a configuration puts its extensions first.
	r2 := NewRegistry()
	_ = r2.AddExtension(ext(`{"contributes": {"languages": [{"id": "x", "extensions": [".b"]}]}}`, nil))
	_ = r2.AddExtension(ext(`{"contributes": {"languages": [{"id": "x", "extensions": [".a"], "configuration": "./c.json"}]}}`, nil))
	if x, _ := r2.Language("x"); !reflect.DeepEqual(x.Extensions, []string{".a", ".b"}) {
		t.Errorf("extensions = %v", x.Extensions)
	}
}

func TestRegistryLookups(t *testing.T) {
	r := NewRegistry()
	_ = r.AddExtension(ext(`{"contributes": {"languages": [
		{"id": "go", "aliases": ["Go", "golang"], "mimetypes": ["text/x-go"]}, {"id": "ts", "aliases": ["TypeScript"]}]}}`, nil))
	for name, want := range map[string]string{"go": "go", "GO": "go", "Golang": "go", "typescript": "ts", "plain text": PlainTextID, "text": PlainTextID} {
		if id, ok := r.ByName(name); !ok || id != want {
			t.Errorf("ByName(%q) = %q, %v; want %q", name, id, ok, want)
		}
	}
	if _, ok := r.ByName("nope"); ok {
		t.Error("ByName of a name that is not there")
	}
	if id, ok := r.ByMIMEType("text/x-go"); !ok || id != "go" {
		t.Errorf("ByMIMEType = %q, %v", id, ok)
	}
	if id, ok := r.ByMIMEType("text/x-ts"); !ok || id != "ts" {
		t.Errorf("the default MIME type is text/x-id: %q, %v", id, ok)
	}
	if _, ok := r.ByMIMEType("nope"); ok {
		t.Error("ByMIMEType of a type that is not there")
	}
	// Codes: 0 is no language, 1 plain text, then in order.
	if r.Code(PlainTextID) != 1 || r.Code("go") != 2 || r.Code("ts") != 3 || r.Code("nope") != 0 {
		t.Errorf("codes: %d %d %d %d", r.Code(PlainTextID), r.Code("go"), r.Code("ts"), r.Code("nope"))
	}
	if r.IDOfCode(2) != "go" || r.IDOfCode(0) != "" || r.IDOfCode(99) != "" || r.IDOfCode(-1) != "" {
		t.Error("IDOfCode")
	}
	var ids []string
	for _, l := range r.Languages() {
		ids = append(ids, l.ID)
	}
	if strings.Join(ids, " ") != "plaintext go ts" {
		t.Errorf("Languages() = %v", ids)
	}
}

func TestAddExtensionErrors(t *testing.T) {
	r := NewRegistry()
	wantTag(t, r.AddExtension(fstest.MapFS{}), "Registry.AddExtension.ExtensionUnreadable")
	wantTag(t, r.AddExtension(ext(`[`, nil)), "Registry.AddExtension->ParseManifest.ManifestInvalid")
	if err := r.AddExtension(ext(`{"contributes": {"languages": [{"id": ""}]}}`, nil)); err != nil {
		t.Fatal(err)
	}
	if p := r.Problems(); len(p) != 1 {
		t.Fatalf("problems = %v", p)
	} else {
		wantTag(t, p[0], "LanguageContributionInvalid")
	}
}

// errorsFrom gives the tag of an error of the package.
func errorsFrom(err error) (string, bool) {
	appErr, ok := errors.FromError(err)
	if !ok {
		return "", false
	}
	return appErr.Tag, true
}

// pack makes a small extension that contributes the language id for the extension ext, with a grammar and settings defaults.
func pack(id, ext string) fstest.MapFS {
	return fstest.MapFS{
		"package.json": {Data: []byte(`{"name": "` + id + `", "contributes": {
			"languages": [{"id": "` + id + `", "extensions": ["` + ext + `"], "aliases": ["` + strings.ToUpper(id) + `"], "configuration": "./c.json"}],
			"grammars": [{"language": "` + id + `", "scopeName": "source.` + id + `", "path": "./g.json"}],
			"configurationDefaults": {"[` + id + `]": {"editor.tabSize": 3}}}}`)},
		"c.json": {Data: []byte(`{"comments": {"lineComment": "#"}}`)},
		"g.json": {Data: []byte(`{"scopeName": "source.` + id + `", "patterns": [{"match": "\\w+", "name": "word"}]}`)},
	}
}

func TestRemoveExtensionTakesTheLanguageAway(t *testing.T) {
	r := NewRegistry()
	if err := r.AddExtension(pack("fixed", ".fx")); err != nil {
		t.Fatal(err)
	}
	m, err := r.AddNamedExtension("one", pack("one", ".one"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Languages) != 1 || m.Languages[0].ID != "one" || m.ConfigurationDefaults["one"]["editor.tabSize"] != float64(3) {
		t.Fatalf("manifest = %+v", m)
	}
	if _, err := r.AddNamedExtension("two", pack("two", ".two")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFileAssociations(map[string]any{"*.mine": "two"}); len(err) != 0 {
		t.Fatal(err)
	}
	version := r.Version()
	codeTwo := r.Code("two")
	if r.Detect("a.one", "") != "one" || !r.HasGrammar("one") || r.Configuration("one").Comments == nil {
		t.Fatal("the extension is not in the registry")
	}

	if !r.RemoveExtension("one") {
		t.Fatal("RemoveExtension of an extension that is there reported false")
	}
	if r.Version() == version {
		t.Error("Version did not change")
	}
	if r.Detect("a.one", "") != PlainTextID || r.IsRegistered("one") || r.HasGrammar("one") || r.Grammar("source.one") != nil {
		t.Error("the language of the removed extension is still known")
	}
	if _, ok := r.ByName("ONE"); ok {
		t.Error("the alias of the removed language is still known")
	}
	if _, has := r.ConfigurationDefaults()["one"]; has {
		t.Error("the settings defaults of the removed extension are still given")
	}
	// the others stay, with the same numbers, and so do the associations of the user
	if r.Detect("a.fx", "") != "fixed" || r.Detect("a.two", "") != "two" || r.Detect("x.mine", "") != "two" || r.Code("two") != codeTwo {
		t.Errorf("the other extensions changed: %q %q %q, code %d (was %d)", r.Detect("a.fx", ""), r.Detect("a.two", ""), r.Detect("x.mine", ""), r.Code("two"), codeTwo)
	}
	if r.Detect("a.txt", "") != PlainTextID || !r.IsRegistered(PlainTextID) {
		t.Error("plain text is gone")
	}
	if r.RemoveExtension("one") || r.RemoveExtension("") || r.RemoveExtension("fixed") {
		t.Error("RemoveExtension of an extension that is not there, or that cannot be removed, reported true")
	}

	// it comes back with the number it had
	codeOne := r.Code("one")
	if codeOne == 0 || r.IDOfCode(codeOne) != "one" {
		t.Fatalf("the number of the removed language was not kept: %d", codeOne)
	}
	if _, err := r.AddNamedExtension("one", pack("one", ".one")); err != nil {
		t.Fatal(err)
	}
	if r.Detect("a.one", "") != "one" || r.Code("one") != codeOne || r.Grammar("source.one") == nil {
		t.Error("the extension did not come back")
	}
}

func TestAddNamedExtensionErrors(t *testing.T) {
	r := NewRegistry()
	_, err := r.AddNamedExtension("", pack("x", ".x"))
	wantTag(t, err, "Registry.AddNamedExtension.ExtensionKeyEmpty")
	_, err = r.AddNamedExtension("k", fstest.MapFS{})
	wantTag(t, err, "Registry.AddNamedExtension.ExtensionUnreadable")
	_, err = r.AddNamedExtension("k", ext(`[`, nil))
	wantTag(t, err, "Registry.AddNamedExtension->ParseManifest.ManifestInvalid")
	if _, err = r.AddNamedExtension("k", pack("x", ".x")); err != nil {
		t.Fatal(err)
	}
	_, err = r.AddNamedExtension("k", pack("y", ".y"))
	wantTag(t, err, "Registry.AddNamedExtension.ExtensionKeyTaken")
	if r.IsRegistered("y") {
		t.Error("an extension that was refused was added")
	}
}
