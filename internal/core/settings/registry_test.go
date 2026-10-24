package settings

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func assertRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestRegisterErrors(t *testing.T) {
	cases := []struct {
		name    string
		setting Setting
		route   string
	}{
		{"empty key", Setting{Types: []Type{String}}, "Register.SettingKeyInvalid"},
		{"leading dot", Setting{Key: ".a", Types: []Type{String}}, "Register.SettingKeyInvalid"},
		{"trailing dot", Setting{Key: "a.", Types: []Type{String}}, "Register.SettingKeyInvalid"},
		{"empty segment", Setting{Key: "a..b", Types: []Type{String}}, "Register.SettingKeyInvalid"},
		{"language block", Setting{Key: "[go]", Types: []Type{Object}}, "Register.SettingKeyInvalid"},
		{"no type", Setting{Key: "a.b"}, "Register.SettingTypeMissing"},
		{"default of another type", Setting{Key: "a.b", Types: []Type{String}, Default: 5}, "Register.SettingDefaultInvalid"},
		{"default not in the enum", Setting{Key: "a.b", Types: []Type{String}, Default: "x", Enum: []any{"y"}}, "Register.SettingDefaultInvalid"},
		{"default below the minimum", Setting{Key: "a.b", Types: []Type{Number}, Default: 0, Minimum: Float(1)}, "Register.SettingDefaultInvalid"},
		{"default that is no JSON", Setting{Key: "a.b", Types: []Type{Object}, Default: make(chan int)}, "Register.SettingDefaultInvalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertRoute(t, NewRegistry().Register(c.setting), c.route)
		})
	}
	t.Run("duplicate", func(t *testing.T) {
		r := NewRegistry()
		r.MustRegister(Setting{Key: "a.b", Types: []Type{String}})
		assertRoute(t, r.Register(Setting{Key: "a.b", Types: []Type{String}}), "Register.SettingDuplicate")
	})
	t.Run("MustRegister panics", func(t *testing.T) {
		defer func() {
			err, _ := recover().(error)
			assertRoute(t, err, "Register.SettingKeyInvalid")
		}()
		NewRegistry().MustRegister(Setting{Key: ""})
	})
	t.Run("language defaults", func(t *testing.T) {
		r := NewRegistry()
		for _, id := range []string{"", " ", "[go]", "a]b"} {
			assertRoute(t, r.RegisterLanguageDefaults(id, nil), "RegisterLanguageDefaults.SettingLanguageInvalid")
		}
		assertRoute(t, r.RegisterLanguageDefaults("go", map[string]any{"a": make(chan int)}), "RegisterLanguageDefaults.SettingDefaultInvalid")
	})
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	v0 := r.Version()
	r.MustRegister(
		Setting{Key: "z.one", Types: []Type{String}, Category: "Zed"},
		Setting{Key: "a.two", Types: []Type{Number}, Category: "Aye"},
		Setting{Key: "a.three", Types: []Type{Boolean}, Category: "Aye"},
	)
	if r.Version() == v0 {
		t.Error("the version did not change")
	}
	t.Run("All keeps the order of registration", func(t *testing.T) {
		var keys []string
		for _, s := range r.All() {
			keys = append(keys, s.Key)
		}
		assertEqual(t, "keys", keys, []string{"z.one", "a.two", "a.three"})
	})
	t.Run("categories are sorted and listed once", func(t *testing.T) {
		assertEqual(t, "categories", r.Categories(), []string{"Aye", "Zed"})
	})
	t.Run("a setting without a default gets the zero value of its type", func(t *testing.T) {
		r := NewRegistry()
		r.MustRegister(
			Setting{Key: "s", Types: []Type{String}}, Setting{Key: "n", Types: []Type{Number}}, Setting{Key: "i", Types: []Type{Integer}},
			Setting{Key: "b", Types: []Type{Boolean}}, Setting{Key: "a", Types: []Type{Array}}, Setting{Key: "o", Types: []Type{Object}},
			Setting{Key: "z", Types: []Type{Null, String}},
		)
		want := map[string]any{"s": "", "n": 0.0, "i": 0.0, "b": false, "a": []any{}, "o": map[string]any{}, "z": nil}
		for key, w := range want {
			s, _ := r.Lookup(key)
			assertEqual(t, key, s.Default, w)
		}
	})
	t.Run("defaults are stored as JSON values", func(t *testing.T) {
		r := NewRegistry()
		r.MustRegister(Setting{Key: "l", Types: []Type{Array}, Default: []string{"a", "b"}}, Setting{Key: "n", Types: []Type{Integer}, Default: int64(7)})
		s, _ := r.Lookup("l")
		assertEqual(t, "array", s.Default, []any{"a", "b"})
		s, _ = r.Lookup("n")
		assertEqual(t, "int", s.Default, 7.0)
	})
	t.Run("Lookup of an unknown key", func(t *testing.T) {
		if _, ok := r.Lookup("nope"); ok {
			t.Error("found")
		}
	})
}

func TestValidate(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(
		Setting{Key: "str", Types: []Type{String}},
		Setting{Key: "num", Types: []Type{Number}, Minimum: Float(1), Maximum: Float(10), Default: 1},
		Setting{Key: "int", Types: []Type{Integer}},
		Setting{Key: "bool", Types: []Type{Boolean}},
		Setting{Key: "either", Types: []Type{String, Number}},
		Setting{Key: "nullable", Types: []Type{String, Null}},
		Setting{Key: "enum", Types: []Type{String}, Enum: []any{"a", "b"}, Default: "a"},
		Setting{Key: "list", Types: []Type{Array}, ItemTypes: []Type{String}},
		Setting{Key: "enumlist", Types: []Type{Array}, ItemEnum: []any{"x", "y"}},
		Setting{Key: "obj", Types: []Type{Object}},
	)
	cases := []struct {
		key   string
		value any
		ok    bool
	}{
		{"str", "x", true}, {"str", 1, false}, {"str", nil, false}, {"str", true, false},
		{"num", 5, true}, {"num", 5.5, true}, {"num", 0, false}, {"num", 11, false}, {"num", "5", false},
		{"int", 3, true}, {"int", 3.0, true}, {"int", 3.5, false},
		{"bool", false, true}, {"bool", "false", false},
		{"either", "x", true}, {"either", 2, true}, {"either", true, false},
		{"nullable", nil, true}, {"nullable", "s", true}, {"nullable", 1, false},
		{"enum", "a", true}, {"enum", "c", false},
		{"list", []string{"a"}, true}, {"list", []any{"a", 1}, false}, {"list", []any{}, true}, {"list", "a", false},
		{"enumlist", []string{"x", "y"}, true}, {"enumlist", []string{"z"}, false},
		{"obj", map[string]any{"a": 1}, true}, {"obj", []any{}, false},
		{"unregistered", 12345, true},
		{"str", make(chan int), false},
	}
	for _, c := range cases {
		if msg := r.Validate(c.key, c.value); (msg == "") != c.ok {
			t.Errorf("%s = %#v: %q", c.key, c.value, msg)
		}
	}
}

func TestThemeSettingsAreRegistered(t *testing.T) {
	r := DefaultRegistry()
	cases := []struct {
		key  string
		want any
	}{
		{"workbench.colorTheme", "Dark Modern"},
		{"workbench.preferredDarkColorTheme", "Dark Modern"},
		{"workbench.preferredLightColorTheme", "Light Modern"},
		{"window.autoDetectColorScheme", false},
	}
	for _, tc := range cases {
		s, ok := r.Lookup(tc.key)
		if !ok {
			t.Errorf("%s is not registered", tc.key)
			continue
		}
		if s.Default != tc.want || s.Description == "" {
			t.Errorf("%s: default %v, want %v (description %q)", tc.key, s.Default, tc.want, s.Description)
		}
	}
}

func TestTokenizationLineLengthIsRegistered(t *testing.T) {
	s, ok := DefaultRegistry().Lookup("editor.maxTokenizationLineLength")
	if !ok || s.Default != float64(20000) || s.Scope != ScopeLanguageOverridable {
		t.Errorf("editor.maxTokenizationLineLength: %+v, %v", s, ok)
	}
}

func TestBracketSettingsAreRegistered(t *testing.T) {
	cases := []struct {
		key  string
		want any
	}{
		{"editor.matchBrackets", "always"},
		{"editor.bracketPairColorization.enabled", true},
		{"editor.bracketPairColorization.independentColorPoolPerBracketType", false},
	}
	for _, tc := range cases {
		s, ok := DefaultRegistry().Lookup(tc.key)
		if !ok || s.Default != tc.want || s.Scope != ScopeLanguageOverridable || s.Description == "" {
			t.Errorf("%s: %+v, %v", tc.key, s, ok)
		}
	}
	s, _ := DefaultRegistry().Lookup("editor.matchBrackets")
	if len(s.Enum) != 3 || len(s.EnumDescriptions) != 3 {
		t.Errorf("editor.matchBrackets has the values always, near and never: %v", s.Enum)
	}
}

func TestEmmetSettingsAreRegistered(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"emmet.variables", "Variables to be used in Emmet snippets."},
		{"emmet.syntaxProfiles", "Define profile for specified syntax or use your own profile with specific rules."},
	}
	r := DefaultRegistry()
	for _, tc := range cases {
		s, ok := r.Lookup(tc.key)
		if !ok || s.Description != tc.want || len(s.Types) != 1 || s.Types[0] != Object || s.Category != categoryEmmet {
			t.Errorf("%s: %+v, %v", tc.key, s, ok)
			continue
		}
		if m, isMap := s.Default.(map[string]any); !isMap || len(m) != 0 {
			t.Errorf("%s: default %#v, want an empty object", tc.key, s.Default)
		}
		if msg := r.Validate(tc.key, map[string]any{"html": map[string]any{"tagCase": "upper"}}); msg != "" {
			t.Errorf("%s: an object is refused: %s", tc.key, msg)
		}
		if msg := r.Validate(tc.key, "no"); msg == "" {
			t.Errorf("%s: a string is accepted", tc.key)
		}
	}
}

// The settings that step 5.11 adds: their types, defaults, descriptions and scopes are VS Code's.
func TestEmmetExpansionSettingsAreRegistered(t *testing.T) {
	cases := []struct {
		key        string
		typ        Type
		def        any
		scope      Scope
		descPrefix string
		good, bad  any
	}{
		{"emmet.includeLanguages", Object, map[string]any{}, ScopeWindow, "Enable Emmet abbreviations in languages that are not supported by default.",
			map[string]any{"vue-html": "html"}, "html"},
		{"emmet.excludeLanguages", Array, []any{"markdown"}, ScopeWindow, "An array of languages where Emmet abbreviations should not be expanded.",
			[]any{"markdown", "plaintext"}, "markdown"},
		{"emmet.triggerExpansionOnTab", Boolean, false, ScopeLanguageOverridable, "When enabled, Emmet abbreviations are expanded when pressing TAB",
			true, "yes"},
	}
	r := DefaultRegistry()
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			s, ok := r.Lookup(tc.key)
			if !ok {
				t.Fatal("not registered")
			}
			if len(s.Types) != 1 || s.Types[0] != tc.typ || s.Scope != tc.scope || s.Category != categoryEmmet {
				t.Errorf("type, scope or category: %+v", s)
			}
			if !strings.HasPrefix(s.Description, tc.descPrefix) {
				t.Errorf("description %q", s.Description)
			}
			if !reflect.DeepEqual(s.Default, tc.def) {
				t.Errorf("default %#v, want %#v", s.Default, tc.def)
			}
			if msg := r.Validate(tc.key, tc.good); msg != "" {
				t.Errorf("a valid value is refused: %s", msg)
			}
			if msg := r.Validate(tc.key, tc.bad); msg == "" {
				t.Errorf("%#v is accepted", tc.bad)
			}
		})
	}
	// the items of the list of excluded languages are strings
	if msg := r.Validate("emmet.excludeLanguages", []any{1}); msg == "" {
		t.Error("a number in emmet.excludeLanguages is accepted")
	}
	// emmet.showExpandedAbbreviation is not registered (SPEC 9.10.2)
	if _, ok := r.Lookup("emmet.showExpandedAbbreviation"); ok {
		t.Error("emmet.showExpandedAbbreviation must not be registered")
	}
}

func TestMenuBarVisibilityIsRegistered(t *testing.T) {
	r := DefaultRegistry()
	s, ok := r.Lookup("window.menuBarVisibility")
	if !ok || s.Default != "classic" || s.Scope != ScopeApplication || s.Category != categoryWindow || s.Description == "" {
		t.Fatalf("window.menuBarVisibility: %+v, %v", s, ok)
	}
	if len(s.Enum) != 4 || len(s.EnumDescriptions) != 4 {
		t.Errorf("the values are classic, visible, toggle and hidden: %v", s.Enum)
	}
	cases := []struct {
		value   any
		invalid bool
	}{
		{"classic", false}, {"visible", false}, {"toggle", false}, {"hidden", false},
		{"compact", true}, {"", true}, {true, true}, {nil, true},
	}
	for _, tc := range cases {
		if got := r.Validate("window.menuBarVisibility", tc.value) != ""; got != tc.invalid {
			t.Errorf("Validate(%v): invalid = %v, want %v", tc.value, got, tc.invalid)
		}
	}
}

func TestLayoutSettingsAreRegistered(t *testing.T) {
	r := DefaultRegistry()
	cases := []struct {
		key     string
		def     any
		enum    []any
		valid   []any
		invalid []any
	}{
		{"workbench.sideBar.location", "left", []any{"left", "right"}, []any{"left", "right"}, []any{"top", "", 1}},
		{"workbench.activityBar.location", "default", []any{"default", "top", "bottom", "hidden"}, []any{"default", "top", "bottom", "hidden"}, []any{"left", false}},
		{"workbench.statusBar.visible", true, nil, []any{true, false}, []any{"yes", 1}},
		{"workbench.panel.defaultLocation", "bottom", []any{"left", "bottom", "top", "right"}, []any{"left", "bottom", "top", "right"}, []any{"middle"}},
		{"workbench.panel.opensMaximized", "preserve", []any{"always", "never", "preserve"}, []any{"always", "never", "preserve"}, []any{"sometimes", true}},
		{"window.title", DefaultWindowTitle, nil, []any{"${activeEditorShort}", ""}, []any{1, true}},
		{"window.titleSeparator", " - ", nil, []any{" | ", ""}, []any{1}},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			s, ok := r.Lookup(tc.key)
			if !ok {
				t.Fatal("not registered")
			}
			if s.Default != tc.def || s.Description == "" || s.Scope != ScopeWindow {
				t.Errorf("default %v scope %v description %q", s.Default, s.Scope, s.Description)
			}
			if len(s.Enum) != len(tc.enum) {
				t.Errorf("enum %v, want %v", s.Enum, tc.enum)
			}
			for _, v := range tc.valid {
				if msg := r.Validate(tc.key, v); msg != "" {
					t.Errorf("%v is refused: %s", v, msg)
				}
			}
			for _, v := range tc.invalid {
				if r.Validate(tc.key, v) == "" {
					t.Errorf("%v is accepted", v)
				}
			}
		})
	}
	// the default title is VS Code's template for Linux
	if DefaultWindowTitle != "${dirty}${activeEditorShort}${separator}${rootName}${separator}${profileName}${separator}${appName}" {
		t.Errorf("DefaultWindowTitle = %q", DefaultWindowTitle)
	}
}

func TestQuickInputSettingsAreRegistered(t *testing.T) {
	cases := []struct {
		key  string
		want any
	}{
		{"workbench.commandPalette.history", float64(50)},
		{"workbench.commandPalette.preserveInput", false},
		{"workbench.quickOpen.closeOnFocusLost", true},
	}
	for _, tc := range cases {
		s, ok := DefaultRegistry().Lookup(tc.key)
		if !ok || s.Default != tc.want || s.Description == "" {
			t.Errorf("%s: %+v, %v", tc.key, s, ok)
		}
	}
}

func TestHotExitIsRegistered(t *testing.T) {
	r := DefaultRegistry()
	s, ok := r.Lookup("files.hotExit")
	if !ok || s.Default != "onExit" || s.Scope != ScopeApplication || s.Category != categoryFiles || s.Description == "" {
		t.Fatalf("files.hotExit: %+v, %v", s, ok)
	}
	if len(s.Enum) != 3 || len(s.EnumDescriptions) != 3 {
		t.Errorf("the values are off, onExit and onExitAndWindowClose: %v", s.Enum)
	}
	for _, tc := range []struct {
		value   any
		invalid bool
	}{
		{"off", false}, {"onExit", false}, {"onExitAndWindowClose", false},
		{"always", true}, {"", true}, {true, true}, {nil, true},
	} {
		if got := r.Validate("files.hotExit", tc.value) != ""; got != tc.invalid {
			t.Errorf("Validate(%v): invalid = %v, want %v", tc.value, got, tc.invalid)
		}
	}
	// window.confirmBeforeClose is not registered (SPEC §10.10)
	if _, ok := r.Lookup("window.confirmBeforeClose"); ok {
		t.Error("window.confirmBeforeClose must not be registered")
	}
}
