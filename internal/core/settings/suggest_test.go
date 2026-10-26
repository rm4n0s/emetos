package settings

import (
	"reflect"
	"testing"
)

// The settings of the suggest widget (step 12.5): the keys and defaults are VS Code's.
func TestSuggestSettingsDefaults(t *testing.T) {
	r := DefaultRegistry()
	tests := []struct {
		key  string
		want any
	}{
		{"editor.quickSuggestions", map[string]any{"other": "offWhenInlineCompletions", "comments": "off", "strings": "off"}},
		{"editor.quickSuggestionsDelay", 10},
		{"editor.suggestOnTriggerCharacters", true},
		{"editor.acceptSuggestionOnEnter", "on"},
		{"editor.acceptSuggestionOnCommitCharacter", true},
		{"editor.suggestSelection", "first"},
		{"editor.snippetSuggestions", "inline"},
		{"editor.wordBasedSuggestions", "offWithInlineSuggestions"},
		{"editor.suggest.insertMode", "insert"},
		{"editor.suggest.filterGraceful", true},
		{"editor.suggest.selectionMode", "always"},
		{"editor.suggest.showIcons", true},
		{"editor.suggest.showDeprecated", true},
		{"editor.suggest.matchOnWordStartOnly", true},
		{"editor.suggest.snippetsPreventQuickSuggestions", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			s, ok := r.Lookup(tt.key)
			if !ok {
				t.Fatal("the setting is not registered")
			}
			if !reflect.DeepEqual(normalizeNumber(s.Default), normalizeNumber(tt.want)) {
				t.Errorf("default = %#v, want %#v", s.Default, tt.want)
			}
			if s.Description == "" {
				t.Error("the setting has no description")
			}
		})
	}
}

func TestEveryKindOfSuggestionHasASetting(t *testing.T) {
	r := DefaultRegistry()
	seen := map[int]string{}
	for _, k := range SuggestKinds {
		s, ok := r.Lookup("editor.suggest.show" + k.Name)
		if !ok {
			t.Errorf("editor.suggest.show%s is not registered", k.Name)
			continue
		}
		if s.Default != true {
			t.Errorf("editor.suggest.show%s defaults to %v, want true", k.Name, s.Default)
		}
		for _, n := range k.Kinds {
			if other, dup := seen[n]; dup {
				t.Errorf("kind %d is in %s and in %s", n, other, k.Name)
			}
			seen[n] = k.Name
		}
	}
	if len(SuggestKinds) != 25 {
		t.Errorf("%d kinds, want the 25 of VS Code", len(SuggestKinds))
	}
}

// normalizeNumber makes the numbers of a default comparable whatever Go type the registration used.
func normalizeNumber(v any) any {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = normalizeNumber(e)
		}
		return out
	}
	return v
}
