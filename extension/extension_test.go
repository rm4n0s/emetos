package extension

import (
	"encoding/json"
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
		{"the id of Emetos", Manifest{ID: "emetos", Name: "N"}, "CheckManifest.ExtensionIDReserved"},
		{"an id under which Emetos publishes", Manifest{ID: "emetos.document", Name: "N"}, "CheckManifest.ExtensionIDReserved"},
		{"declared topics", Manifest{ID: "a", Name: "N", Events: EventDecl{
			Publishes: []EventSpec{{Topic: "a.x", Fast: true}, {Topic: "shared.y"}}, Handles: []string{"shared.req"}, Subscribes: []string{"emetos.document.*", "**"},
		}}, ""},
		{"a published topic that is malformed", Manifest{ID: "a", Name: "N", Events: EventDecl{Publishes: []EventSpec{{Topic: "a..x"}}}}, "checkEventDecl->CheckTopic.EventTopicInvalid"},
		{"a published topic with a star", Manifest{ID: "a", Name: "N", Events: EventDecl{Publishes: []EventSpec{{Topic: "a.*"}}}}, "checkEventDecl->CheckTopic.EventTopicInvalid"},
		{"a handled topic that is malformed", Manifest{ID: "a", Name: "N", Events: EventDecl{Handles: []string{""}}}, "checkEventDecl->CheckTopic.EventTopicInvalid"},
		{"a pattern that is malformed", Manifest{ID: "a", Name: "N", Events: EventDecl{Subscribes: []string{"a.**.b"}}}, "checkEventDecl->CheckPattern.EventPatternInvalid"},
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

func TestTopicsAndPatterns(t *testing.T) {
	topics := []struct {
		topic string
		ok    bool
	}{
		{"a", true}, {"markdown.preview.open", true}, {"emetos.editor.activeChanged", true}, {"a-b_c.d9", true},
		{"", false}, {"a..b", false}, {".a", false}, {"a.", false}, {"a b", false}, {"a.*", false}, {"a/b", false},
	}
	for _, tt := range topics {
		if got := CheckTopic(tt.topic) == nil; got != tt.ok {
			t.Errorf("CheckTopic(%q) ok = %v, want %v", tt.topic, got, tt.ok)
		}
	}
	patterns := []struct {
		pattern string
		ok      bool
	}{
		{"a.b", true}, {"*", true}, {"**", true}, {"a.*", true}, {"*.b.*", true}, {"a.**", true},
		{"", false}, {"a.**.b", false}, {"a*", false}, {"a.***", false}, {"a..b", false},
	}
	for _, tt := range patterns {
		if got := CheckPattern(tt.pattern) == nil; got != tt.ok {
			t.Errorf("CheckPattern(%q) ok = %v, want %v", tt.pattern, got, tt.ok)
		}
	}
	matches := []struct {
		pattern, topic string
		want           bool
	}{
		{"a.b", "a.b", true}, {"a.b", "a.c", false}, {"a.b", "a.b.c", false}, {"a.b.c", "a.b", false},
		{"a.*", "a.b", true}, {"a.*", "a", false}, {"a.*", "a.b.c", false}, {"*.b", "a.b", true},
		{"a.**", "a.b", true}, {"a.**", "a.b.c", true}, {"a.**", "a", false}, {"**", "a", true}, {"**", "a.b.c", true},
		{"emetos.document.*", "emetos.document.changed", true}, {"emetos.document.*", "emetos.editor.activeChanged", false},
	}
	for _, tt := range matches {
		if got := MatchTopic(tt.pattern, tt.topic); got != tt.want {
			t.Errorf("MatchTopic(%q, %q) = %v, want %v", tt.pattern, tt.topic, got, tt.want)
		}
	}
}

func TestEventValues(t *testing.T) {
	ev := Event{Topic: "a.t", Data: json.RawMessage(`{"n":3}`)}
	type payload struct{ N int }
	if v, err := EventValue[payload](ev); err != nil || v.N != 3 {
		t.Errorf("EventValue = %+v (%v)", v, err)
	}
	if vs, err := EventValues[payload](ev); err != nil || len(vs) != 1 || vs[0].N != 3 {
		t.Errorf("EventValues of a single event = %+v (%v)", vs, err)
	}
	batch := Event{Topic: "a.t", Data: json.RawMessage(`{"n":2}`), Batch: []json.RawMessage{json.RawMessage(`{"n":1}`), json.RawMessage(`{"n":2}`)}}
	if vs, err := EventValues[payload](batch); err != nil || len(vs) != 2 || vs[0].N != 1 || vs[1].N != 2 {
		t.Errorf("EventValues of a batch = %+v (%v)", vs, err)
	}
	bad := Event{Topic: "a.t", Data: json.RawMessage(`"text"`), Batch: []json.RawMessage{json.RawMessage(`"text"`)}}
	_, err := EventValue[payload](bad)
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("Decode.EventValueInvalid") {
		t.Errorf("Decode of a value of another shape: %v", err)
	}
	_, err = EventValues[payload](bad)
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("EventValues[...].EventValueInvalid") {
		t.Errorf("EventValues of a value of another shape: %v", err)
	}
}
