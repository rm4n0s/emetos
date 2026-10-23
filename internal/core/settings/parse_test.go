package settings

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/jsonc"
)

const parserTestKey = "ConfigurationModelParserTest.windowSetting"

func parserRegistry(t testing.TB) *Registry {
	r := NewRegistry()
	if err := r.Register(Setting{Key: parserTestKey, Types: []Type{String}, Default: "isSet"}); err != nil {
		t.Fatal(err)
	}
	return r
}

// Ported from VS Code: configurationModels.test.ts ('ConfigurationModelParser').
func TestVSCodeModelParser(t *testing.T) {
	r := parserRegistry(t)
	overrides := func(text string) []Override { return r.ParseModel(text, ParseOptions{}).Model.Overrides() }

	t.Run("parse configuration model with single override identifier", func(t *testing.T) {
		assertEqual(t, "overrides", overrides(`{"[x]": {"a": 1}}`), []Override{block(t, `{"a":1}`, []string{"a"}, "x")})
	})
	t.Run("parse configuration model with multiple override identifiers", func(t *testing.T) {
		assertEqual(t, "overrides", overrides(`{"[x][y]": {"a": 1}}`), []Override{block(t, `{"a":1}`, []string{"a"}, "x", "y")})
	})
	t.Run("parse configuration model with multiple duplicate override identifiers", func(t *testing.T) {
		assertEqual(t, "overrides", overrides(`{"[x][y][x][z]": {"a": 1}}`), []Override{block(t, `{"a":1}`, []string{"a"}, "x", "y", "z")})
	})
	t.Run("parse configuration model with exclude option", func(t *testing.T) {
		m := r.ParseModel(`{"a": 1, "b": 2}`, ParseOptions{Exclude: []string{"a"}}).Model
		if v, ok := m.GetValue("a"); ok {
			t.Errorf("a = %v", v)
		}
		if v, _ := m.GetValue("b"); v != 2.0 {
			t.Errorf("b = %v", v)
		}
	})
	t.Run("parse configuration model with exclude option even included", func(t *testing.T) {
		m := r.ParseModel(`{"a": 1, "b": 2}`, ParseOptions{Exclude: []string{"a"}, Include: []string{"a"}}).Model
		if _, ok := m.GetValue("a"); ok {
			t.Error("a is there")
		}
		if v, _ := m.GetValue("b"); v != 2.0 {
			t.Errorf("b = %v", v)
		}
	})
	t.Run("parse configuration model with scopes filter", func(t *testing.T) {
		m := r.ParseModel(`{"`+parserTestKey+`": "1"}`, ParseOptions{Scopes: []Scope{ScopeApplication}}).Model
		if v, ok := m.GetValue(parserTestKey); ok {
			t.Errorf("value %v", v)
		}
	})
	t.Run("parse configuration model with include option", func(t *testing.T) {
		m := r.ParseModel(`{"`+parserTestKey+`": "1"}`, ParseOptions{Include: []string{parserTestKey}, Scopes: []Scope{ScopeApplication}}).Model
		if v, _ := m.GetValue(parserTestKey); v != "1" {
			t.Errorf("value %v", v)
		}
	})
	t.Run("parse configuration model with invalid setting key", func(t *testing.T) {
		res := r.ParseModel(`{"a": null, "a.b.c": {"c": 1}}`, ParseOptions{})
		if v, ok := res.Model.GetValue("a"); !ok || v != nil {
			t.Errorf("a = %v %v", v, ok)
		}
		for _, key := range []string{"a.b", "a.b.c"} {
			if v, ok := res.Model.GetValue(key); ok {
				t.Errorf("%s = %v", key, v)
			}
		}
		if len(res.Conflicts) != 1 {
			t.Errorf("conflicts %v", res.Conflicts)
		}
	})
	t.Run("a key called __proto__ is an ordinary key", func(t *testing.T) {
		res := r.ParseModel(`{ "__proto__": { "editor.fontSize": 100 }, "excluded": true }`, ParseOptions{Exclude: []string{"excluded"}})
		v, _ := res.Model.GetValue("__proto__")
		assertEqual(t, "__proto__", v, J(t, `{"editor.fontSize": 100}`))
		if v, ok := res.Model.GetValue("editor.fontSize"); ok {
			t.Errorf("editor.fontSize = %v", v)
		}
	})
	t.Run("parse configuration model ignores unregistered keys when skipping them", func(t *testing.T) {
		m := r.ParseModel(`{"constructor": true, "toString": true, "`+parserTestKey+`": "x"}`, ParseOptions{SkipUnregistered: true}).Model
		for _, key := range []string{"constructor", "toString"} {
			if _, ok := m.GetValue(key); ok {
				t.Errorf("%s is there", key)
			}
		}
		if v, _ := m.GetValue(parserTestKey); v != "x" {
			t.Errorf("value %v", v)
		}
	})
}

// Ported from VS Code: configurationModels.test.ts ('CustomConfigurationModel').
func TestVSCodeCustomModel(t *testing.T) {
	r := NewRegistry()
	parse := func(text string) *Model { return r.ParseModel(text, ParseOptions{}).Model }

	t.Run("simple merge using models", func(t *testing.T) {
		result := parse(`{"a":1,"b":2}`).Merge(parse(`{"a":3,"c":4}`))
		assertEqual(t, "contents", result.Contents(), J(t, `{"a":3,"b":2,"c":4}`))
	})
	t.Run("simple merge with an undefined contents", func(t *testing.T) {
		assertEqual(t, "empty added", parse(`{"a":1,"b":2}`).Merge(parse("")).Contents(), J(t, `{"a":1,"b":2}`))
		assertEqual(t, "empty base", parse("").Merge(parse(`{"a":1,"b":2}`)).Contents(), J(t, `{"a":1,"b":2}`))
		assertEqual(t, "both empty", parse("").Merge(parse("")).Contents(), map[string]any{})
	})
	t.Run("Recursive merge using config models", func(t *testing.T) {
		assertEqual(t, "contents", parse(`{"a":{"b":1}}`).Merge(parse(`{"a":{"b":2}}`)).Contents(), J(t, `{"a":{"b":2}}`))
	})
	t.Run("Test configWithOverrides gives all content merged with overrides", func(t *testing.T) {
		m := parse(`{"a":1,"c":1,"[b]":{"a":2}}`)
		assertEqual(t, "contents", m.Override("b").Contents(), J(t, `{"a":2,"c":1,"[b]":{"a":2}}`))
	})
	t.Run("Test configWithOverrides gives empty contents", func(t *testing.T) {
		assertEqual(t, "contents", parse("").Override("b").Contents(), map[string]any{})
	})
	t.Run("Test update with empty data", func(t *testing.T) {
		for _, text := range []string{"", "  ", "// just a comment", "{}"} {
			m := parse(text)
			assertEqual(t, text+" contents", m.Contents(), map[string]any{})
			assertEqual(t, text+" keys", len(m.Keys()), 0)
		}
	})
	t.Run("Test empty property is not ignored", func(t *testing.T) {
		m := parse(`{"": 1}`)
		assertEqual(t, "contents", m.Contents(), map[string]any{"": 1.0})
		assertEqual(t, "keys", m.Keys(), []string{""})
	})
}

func TestParseModel(t *testing.T) {
	r := parserRegistry(t)
	t.Run("keys keep the order of the file and a repeated key keeps its first place", func(t *testing.T) {
		m := r.ParseModel(`{"z": 1, "a": 2, "z": 3, "m.n": 4}`, ParseOptions{}).Model
		assertEqual(t, "keys", m.Keys(), []string{"z", "a", "m.n"})
		assertEqual(t, "contents", m.Contents(), J(t, `{"z":3,"a":2,"m":{"n":4}}`))
	})
	t.Run("a key written before its parent loses to it, in file order", func(t *testing.T) {
		res := r.ParseModel(`{"a.b": 1, "a": 2}`, ParseOptions{})
		assertEqual(t, "contents", res.Model.Contents(), J(t, `{"a":2}`))
		res = r.ParseModel(`{"a": 2, "a.b": 1}`, ParseOptions{})
		assertEqual(t, "contents", res.Model.Contents(), J(t, `{"a":2}`))
		if len(res.Conflicts) != 1 {
			t.Errorf("conflicts %v", res.Conflicts)
		}
	})
	t.Run("the block keys keep the order of the file", func(t *testing.T) {
		m := r.ParseModel(`{"[go]": {"z.a": 1, "b": 2}}`, ParseOptions{}).Model
		assertEqual(t, "keys", m.Overrides()[0].Keys, []string{"z.a", "b"})
		assertEqual(t, "contents", m.Overrides()[0].Contents, J(t, `{"z":{"a":1},"b":2}`))
	})
	t.Run("syntax errors are listed and the rest is used", func(t *testing.T) {
		res := r.ParseModel(`{"a": 1, "b": }`, ParseOptions{})
		if len(res.Errors) == 0 {
			t.Error("no errors")
		}
		if v, _ := res.Model.GetValue("a"); v != 1.0 {
			t.Errorf("a = %v", v)
		}
		if res.Errors[0].Code != jsonc.ValueExpected {
			t.Errorf("error %v", res.Errors[0])
		}
	})
	t.Run("comments and trailing commas are fine", func(t *testing.T) {
		res := r.ParseModel("{\n // c\n \"a\": 1, /* d */\n}", ParseOptions{})
		if len(res.Errors) != 0 {
			t.Errorf("errors %v", res.Errors)
		}
	})
	t.Run("a file that is no object is empty and says so", func(t *testing.T) {
		for _, text := range []string{`[1, 2]`, `5`, `"x"`} {
			res := r.ParseModel(text, ParseOptions{})
			if !res.NotObject || !res.Model.IsEmpty() {
				t.Errorf("%s: %+v", text, res)
			}
		}
	})
	t.Run("nested settings are looked into when filtering", func(t *testing.T) {
		r := NewRegistry()
		r.MustRegister(
			Setting{Key: "a.app", Types: []Type{Boolean}, Scope: ScopeApplication},
			Setting{Key: "a.win", Types: []Type{Boolean}},
			Setting{Key: "obj", Types: []Type{Object}, Scope: ScopeApplication},
		)
		opts := ParseOptions{Scopes: []Scope{ScopeWindow}}
		m := r.ParseModel(`{"a": {"app": true, "win": true, "other": 1}, "obj": {"x": 1}, "unknown": {"y": 2}, "e": {}}`, opts).Model
		assertEqual(t, "contents", m.Contents(), J(t, `{"a":{"win":true,"other":1},"unknown":{"y":2},"e":{}}`))
		// The unfiltered settings are still there to inspect.
		in := m.Inspect("a.app", "")
		assertEqual(t, "app", in.Value, opt(true))
		in = m.Inspect("obj", "")
		assertEqual(t, "obj", in.Value, opt(J(t, `{"x":1}`)))
	})
	t.Run("language blocks are filtered like the rest", func(t *testing.T) {
		r := NewRegistry()
		r.MustRegister(Setting{Key: "a.app", Types: []Type{Boolean}, Scope: ScopeApplication}, Setting{Key: "a.win", Types: []Type{Boolean}})
		m := r.ParseModel(`{"[go]": {"a.app": true, "a.win": true}}`, ParseOptions{Scopes: []Scope{ScopeWindow}}).Model
		assertEqual(t, "overrides", m.Overrides(), []Override{block(t, `{"a":{"win":true}}`, []string{"a.win"}, "go")})
	})
	t.Run("ParseMap", func(t *testing.T) {
		m := r.ParseMap(map[string]any{"b": 1.0, "a.c": 2.0}, ParseOptions{}).Model
		assertEqual(t, "contents", m.Contents(), J(t, `{"a":{"c":2},"b":1}`))
	})
	t.Run("override keys", func(t *testing.T) {
		assertEqual(t, "ids", OverrideIdentifiers("[x][ y ][x]"), []string{"x", "y"})
		assertEqual(t, "no block", OverrideIdentifiers("editor"), []string(nil))
		assertEqual(t, "no block 2", OverrideIdentifiers("[x]y"), []string(nil))
		assertEqual(t, "make", OverrideKey("go", "json"), "[go][json]")
		if !IsOverrideKey("[go]") || IsOverrideKey("go") || IsOverrideKey("[]") {
			t.Error("IsOverrideKey")
		}
	})
}
