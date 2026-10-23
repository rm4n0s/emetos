package language

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestConfigurationDefaultsOfAnExtension(t *testing.T) {
	data := []byte(`{
		"name": "ext",
		"contributes": {
			"configurationDefaults": {
				"[go]": {"editor.insertSpaces": false},
				"[json][jsonc]": {"editor.tabSize": 2, "editor.quickSuggestions": {"strings": true}},
				"editor.fontSize": 20,
				"[broken": {"a": 1},
				"[]": {"a": 1},
				"[go]x": {"a": 1}
			}
		}
	}`)
	m, err := ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]any{
		"go":    {"editor.insertSpaces": false},
		"json":  {"editor.tabSize": float64(2), "editor.quickSuggestions": map[string]any{"strings": true}},
		"jsonc": {"editor.tabSize": float64(2), "editor.quickSuggestions": map[string]any{"strings": true}},
	}
	if !reflect.DeepEqual(m.ConfigurationDefaults, want) {
		t.Errorf("defaults = %v\nwant %v", m.ConfigurationDefaults, want)
	}
	// A block that is not an object is a problem.
	m, err = ParseManifest([]byte(`{"name": "ext", "contributes": {"configurationDefaults": {"[go]": 3}}}`))
	if err != nil || len(m.Problems) != 1 || len(m.ConfigurationDefaults) != 0 {
		t.Fatalf("problems = %v, defaults = %v, err = %v", m.Problems, m.ConfigurationDefaults, err)
	}
	if appErr, ok := errors.FromError(m.Problems[0]); !ok || !appErr.HasRoute("ConfigurationDefaultsInvalid") {
		t.Errorf("problem: %v", m.Problems[0])
	}
	if m, _ := ParseManifest([]byte(`{"name": "ext"}`)); m.ConfigurationDefaults != nil {
		t.Error("no defaults when there are none")
	}
}

func TestBundledLanguagesGiveSettingsDefaults(t *testing.T) {
	r, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	got := r.ConfigurationDefaults()
	if v := got["go"]["editor.insertSpaces"]; v != false {
		t.Errorf("go: %v", got["go"])
	}
	for _, id := range []string{"yaml", "dockercompose"} {
		if got[id]["editor.tabSize"] != float64(2) || got[id]["editor.insertSpaces"] != true {
			t.Errorf("%s: %v", id, got[id])
		}
	}
	if got["makefile"]["editor.insertSpaces"] != false {
		t.Errorf("makefile: %v", got["makefile"])
	}
	// The result is a copy.
	got["go"]["editor.insertSpaces"] = true
	if r.ConfigurationDefaults()["go"]["editor.insertSpaces"] != false {
		t.Error("ConfigurationDefaults gives copies")
	}
}
