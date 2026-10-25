package extension

import (
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestCheckManifest(t *testing.T) {
	ok := Manifest{ID: "emetos.markdown", Name: "Markdown", ActivationEvents: []string{OnStartup, OnLanguage("markdown"), OnFile("**/*.md"), OnCommand("a.b"), OnView("v"), OnWindowTab("k")}}
	tests := []struct {
		name  string
		m     Manifest
		route string // empty: valid
	}{
		{"every kind of event", ok, ""},
		{"no events", Manifest{ID: "sqltools", Name: "SQL"}, ""},
		{"an id with hyphens and digits", Manifest{ID: "my-ext2.sub", Name: "N"}, ""},
		{"an empty id", Manifest{Name: "N"}, "CheckManifest.ExtensionIDInvalid"},
		{"capitals", Manifest{ID: "Emetos", Name: "N"}, "CheckManifest.ExtensionIDInvalid"},
		{"a segment that starts with a digit", Manifest{ID: "a.1b", Name: "N"}, "CheckManifest.ExtensionIDInvalid"},
		{"a trailing dot", Manifest{ID: "a.", Name: "N"}, "CheckManifest.ExtensionIDInvalid"},
		{"no name", Manifest{ID: "a", Name: "  "}, "CheckManifest.ExtensionNameMissing"},
		{"an unknown event", Manifest{ID: "a", Name: "N", ActivationEvents: []string{"onTuesday"}}, "CheckManifest.ExtensionEventInvalid"},
		{"an event with no argument", Manifest{ID: "a", Name: "N", ActivationEvents: []string{"onCommand:"}}, "CheckManifest.ExtensionEventInvalid"},
		{"a bare event that needs an argument", Manifest{ID: "a", Name: "N", ActivationEvents: []string{"onFile"}}, "CheckManifest.ExtensionEventInvalid"},
		{"onStartup with an argument", Manifest{ID: "a", Name: "N", ActivationEvents: []string{"onStartup:x"}}, "CheckManifest.ExtensionEventInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckManifest(tt.m)
			if tt.route == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			appErr, isErr := errors.FromError(err)
			if !isErr {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestEventHelpers(t *testing.T) {
	tests := []struct {
		event     string
		name, arg string
	}{
		{OnStartup, "onStartup", ""},
		{OnLanguage("go"), "onLanguage", "go"},
		{OnFile("a:b/**"), "onFile", "a:b/**"},
		{OnCommand("x.y"), "onCommand", "x.y"},
		{OnView("v"), "onView", "v"},
		{OnWindowTab("scratch"), "onWindowTab", "scratch"},
	}
	for _, tt := range tests {
		t.Run(tt.event, func(t *testing.T) {
			name, arg := SplitEvent(tt.event)
			if name != tt.name || arg != tt.arg {
				t.Errorf("SplitEvent = %q, %q; want %q, %q", name, arg, tt.name, tt.arg)
			}
		})
	}
}

// valueSettings is a Settings with fixed values.
type valueSettings struct{ values map[string]any }

func (v valueSettings) Register(SettingSpec) error { return nil }
func (v valueSettings) Get(key string) (any, error) {
	if x, ok := v.values[key]; ok {
		return x, nil
	}
	return nil, errors.New("Unknown", "no such setting", "key", key)
}
func (v valueSettings) OnChange(func()) func() { return func() {} }

func TestSettingValue(t *testing.T) {
	s := valueSettings{values: map[string]any{
		"str": "text", "num": float64(3), "flag": true, "list": []any{"a", "b"}, "obj": map[string]any{"name": "n", "size": float64(2)},
	}}
	type object struct {
		Name string `json:"name"`
		Size int    `json:"size"`
	}
	if got, err := SettingValue[string](s, "str"); err != nil || got != "text" {
		t.Errorf("string = %q, %v", got, err)
	}
	if got, err := SettingValue[int](s, "num"); err != nil || got != 3 {
		t.Errorf("int = %d, %v", got, err)
	}
	if got, err := SettingValue[float64](s, "num"); err != nil || got != 3 {
		t.Errorf("float64 = %v, %v", got, err)
	}
	if got, err := SettingValue[bool](s, "flag"); err != nil || !got {
		t.Errorf("bool = %v, %v", got, err)
	}
	if got, err := SettingValue[[]string](s, "list"); err != nil || !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("[]string = %v, %v", got, err)
	}
	if got, err := SettingValue[object](s, "obj"); err != nil || got != (object{"n", 2}) {
		t.Errorf("struct = %+v, %v", got, err)
	}
	if _, err := SettingValue[bool](s, "str"); err == nil {
		t.Errorf("a string was read as a bool")
	}
	if _, err := SettingValue[string](s, "missing"); err == nil {
		t.Errorf("a missing setting was read")
	}
}
