package exthost

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// langFS is the files of a language pack that has the language id for the extension ext, a grammar and a setting default.
func langFS(id, ext string) fstest.MapFS {
	return fstest.MapFS{
		"package.json": {Data: []byte(`{"name": "` + id + `", "contributes": {
			"languages": [{"id": "` + id + `", "extensions": ["` + ext + `"], "configuration": "./c.json"}],
			"grammars": [{"language": "` + id + `", "scopeName": "source.` + id + `", "path": "./g.json"}],
			"configurationDefaults": {"[` + id + `]": {"editor.tabSize": 3}}}}`)},
		"c.json": {Data: []byte(`{"comments": {"lineComment": "#"}}`)},
		"g.json": {Data: []byte(`{"scopeName": "source.` + id + `", "patterns": [{"match": "\\w+", "name": "word"}]}`)},
	}
}

// packManifest is the manifest of a language pack extension.
func packManifest(id string, files fstest.MapFS) extension.Manifest {
	m := manifest(id)
	m.Contributes.Languages = files
	return m
}

// languageRig makes a host over the packs "p.one" (language one, .one) and "p.two" (language two, .two).
func languageRig(t *testing.T) (*rig, *language.Registry) {
	t.Helper()
	langs := language.NewRegistry()
	one, _ := factory(packManifest("p.one", langFS("one", ".one")), nil)
	two, _ := factory(packManifest("p.two", langFS("two", ".two")), nil)
	return newRigLanguages(t, nil, langs, one, two), langs
}

// detects lists what the registry makes of the files.
func detects(r *language.Registry, files ...string) string {
	var out []string
	for _, f := range files {
		out = append(out, r.Detect(f, ""))
	}
	return strings.Join(out, " ")
}

func TestPacksAreRegisteredBeforeTheFirstScope(t *testing.T) {
	r, langs := languageRig(t)
	if got := detects(langs, "a.one", "a.two", "a.txt"); got != "one two plaintext" {
		t.Errorf("before the first scope: %s", got)
	}
	if isPack, registered := r.host.LanguagePack("p.one"); !isPack || !registered {
		t.Errorf("LanguagePack = %v %v", isPack, registered)
	}
	if isPack, _ := r.host.LanguagePack("nobody"); isPack {
		t.Error("an unknown extension is a pack")
	}
	if got := settings.Get[int](settings.NewStore(r.registry, settings.StoreOptions{}), "editor.tabSize", "one"); got != 3 {
		t.Errorf("the pack gave no settings default for its language: %d", got)
	}
	if langs.Code("one") == 0 || langs.Code("one") == langs.Code("two") {
		t.Error("the languages have no distinct numbers")
	}
}

func TestPacksFollowTheScopesThatHoldThem(t *testing.T) {
	r, langs := languageRig(t)
	a := r.newTab("a")
	b := r.newTab("b")
	if got := detects(langs, "x.one", "x.two"); got != "one two" {
		t.Fatalf("two scopes: %s", got)
	}
	// a pack held by two scopes is registered once
	if n := strings.Count(strings.Join(languageIDs(langs), " "), "one"); n != 1 {
		t.Errorf("the language is in the registry %d times", n)
	}
	a.scope.Close()
	if got := detects(langs, "x.one", "x.two"); got != "one two" {
		t.Errorf("the other scope still holds the packs: %s", got)
	}
	b.scope.Close()
	if got := detects(langs, "x.one", "x.two"); got != "plaintext plaintext" {
		t.Errorf("no scope holds the packs: %s", got)
	}
	if langs.HasGrammar("one") || langs.IsRegistered("two") {
		t.Error("the grammar or the language of a pack nobody holds is still there")
	}
	if got := settings.Get[int](settings.NewStore(r.registry, settings.StoreOptions{}), "editor.tabSize", "one"); got != 4 {
		t.Errorf("the settings default of a pack nobody holds is still given: %d", got)
	}
	// the next scope brings them back, with the numbers they had
	code := langs.Code("one")
	r.newTab("c")
	if got := detects(langs, "x.one", "x.two"); got != "one two" || langs.Code("one") != code {
		t.Errorf("a new scope: %s, code %d (was %d)", got, langs.Code("one"), code)
	}
}

func TestDisablingAPackRemovesItsLanguages(t *testing.T) {
	r, langs := languageRig(t)
	a := r.newTab("a")
	version := langs.Version()
	a.disabled = []string{"p.one"}
	a.scope.Refresh()
	if got := detects(langs, "x.one", "x.two"); got != "plaintext two" {
		t.Errorf("one disabled: %s", got)
	}
	if langs.Version() == version {
		t.Error("the version of the registry did not change")
	}
	if isPack, registered := r.host.LanguagePack("p.one"); !isPack || registered {
		t.Errorf("LanguagePack = %v %v", isPack, registered)
	}
	a.disabled = nil
	a.scope.Refresh()
	if got := detects(langs, "x.one", "x.two"); got != "one two" {
		t.Errorf("one enabled again: %s", got)
	}
}

func TestAPackStaysWhileAnotherScopeHasItEnabled(t *testing.T) {
	r, langs := languageRig(t)
	a := r.newTab("a")
	r.newTab("b")
	a.disabled = []string{"p.one"}
	a.scope.Refresh()
	if got := detects(langs, "x.one"); got != "one" {
		t.Errorf("the registry is the process's: a pack another scope has enabled stays: %s", got)
	}
}

func TestAScopeThatStartsWithAPackDisabled(t *testing.T) {
	r, langs := languageRig(t)
	// the window makes the registry follow its settings before it opens a file
	r.host.SyncLanguages([]string{"p.two"})
	if got := detects(langs, "x.one", "x.two"); got != "one plaintext" {
		t.Errorf("SyncLanguages: %s", got)
	}
	a := r.newTab("a", "p.two")
	if got := detects(langs, "x.one", "x.two"); got != "one plaintext" {
		t.Errorf("a scope with the pack disabled: %s", got)
	}
	// a second tab that has it enabled brings it back
	r.newTab("b")
	if got := detects(langs, "x.one", "x.two"); got != "one two" {
		t.Errorf("a scope with the pack enabled: %s", got)
	}
	// and the first tab, where it is disabled, does not hold it
	r.tabs[1].scope.Close()
	if got := detects(langs, "x.two"); got != "plaintext" {
		t.Errorf("the scope that disabled it holds it: %s", got)
	}
	_ = a
}

func TestAPackWithBrokenFilesIsLeftOut(t *testing.T) {
	var logged bytes.Buffer
	langs := language.NewRegistry()
	good, _ := factory(packManifest("p.good", langFS("good", ".good")), nil)
	broken, _ := factory(packManifest("p.broken", fstest.MapFS{}), nil)
	reg := settings.DefaultRegistry()
	host := NewHost(HostOptions{Factories: []extension.Factory{broken, good}, Settings: reg, Languages: langs, Logger: slog.New(slog.NewTextHandler(&logged, nil))})
	if got := detects(langs, "x.good"); got != "good" {
		t.Errorf("the good pack: %s", got)
	}
	if !strings.Contains(logged.String(), "the languages of an extension are left out") || !strings.Contains(logged.String(), "ExtensionUnreadable") {
		t.Errorf("the log does not say why: %s", logged.String())
	}
	if _, registered := host.LanguagePack("p.broken"); registered {
		t.Error("a pack that could not be read is registered")
	}
	// holding and releasing it changes nothing and does not panic
	host.NewScope(ScopeOptions{Info: extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "x"}}).Close()
}

func TestPacksWithoutARegistryAreLeftAlone(t *testing.T) {
	one, _ := factory(packManifest("p.one", langFS("one", ".one")), nil)
	r := newRig(t, one)
	tb := r.newTab("a")
	if isPack, _ := r.host.LanguagePack("p.one"); isPack {
		t.Error("an extension is a language pack in a host that has no registry")
	}
	tb.scope.Close()
}

// languageIDs lists the ids of the languages of a registry.
func languageIDs(r *language.Registry) []string {
	var ids []string
	for _, l := range r.Languages() {
		ids = append(ids, l.ID)
	}
	return ids
}
