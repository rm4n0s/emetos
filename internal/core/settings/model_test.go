package settings

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/jsonc"
)

// J parses JSON written in a test into the values the settings use.
func J(t testing.TB, text string) map[string]any {
	t.Helper()
	v, errs := jsonc.Parse(text, jsonc.ParseOptions{})
	if len(errs) != 0 {
		t.Fatalf("bad JSON %q: %v", text, errs)
	}
	m, _ := v.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// model builds a Model of JSON text for contents and the blocks of overrides.
func model(t testing.TB, contents string, keys []string, overrides ...Override) *Model {
	t.Helper()
	return NewModel(J(t, contents), keys, overrides)
}

func block(t testing.TB, contents string, keys []string, ids ...string) Override {
	t.Helper()
	return Override{Identifiers: ids, Keys: keys, Contents: J(t, contents)}
}

func assertEqual(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got %#v\nwant %#v", name, got, want)
	}
}

func opt(v any) Optional { return Optional{Value: v, Ok: true} }

// Ported from VS Code: src/vs/platform/configuration/test/common/configurationModels.test.ts
// ('ConfigurationModel': setValue and removeValue).
func TestVSCodeModelSetAndRemove(t *testing.T) {
	cases := []struct {
		name         string
		contents     string
		keys         []string
		key          string
		value        any
		wantContents string
		wantKeys     []string
	}{
		{"setValue for a key that has no sections and not defined", `{"a":{"b":1}}`, []string{"a.b"}, "f", 1.0, `{"a":{"b":1},"f":1}`, []string{"a.b", "f"}},
		{"setValue for a key that has no sections and defined", `{"a":{"b":1},"f":1}`, []string{"a.b", "f"}, "f", 3.0, `{"a":{"b":1},"f":3}`, []string{"a.b", "f"}},
		{"setValue for a key that has sections and not defined", `{"a":{"b":1},"f":1}`, []string{"a.b", "f"}, "b.c", 1.0, `{"a":{"b":1},"f":1,"b":{"c":1}}`, []string{"a.b", "f", "b.c"}},
		{"setValue for a key that has sections and defined", `{"a":{"b":1},"b":{"c":1},"f":1}`, []string{"a.b", "b.c", "f"}, "b.c", 3.0, `{"a":{"b":1},"b":{"c":3},"f":1}`, []string{"a.b", "b.c", "f"}},
		{"setValue for a key that has sections and sub section not defined", `{"a":{"b":1},"f":1}`, []string{"a.b", "f"}, "a.c", 1.0, `{"a":{"b":1,"c":1},"f":1}`, []string{"a.b", "f", "a.c"}},
		{"setValue for a key that has sections and sub section defined", `{"a":{"b":1,"c":1},"f":1}`, []string{"a.b", "a.c", "f"}, "a.c", 3.0, `{"a":{"b":1,"c":3},"f":1}`, []string{"a.b", "a.c", "f"}},
		{"setValue for a key that has sections and last section is added", `{"a":{"b":{}},"f":1}`, []string{"a.b", "f"}, "a.b.c", 1.0, `{"a":{"b":{"c":1}},"f":1}`, []string{"a.b", "f", "a.b.c"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := model(t, c.contents, c.keys)
			m.SetValue(c.key, c.value)
			assertEqual(t, "contents", m.Contents(), J(t, c.wantContents))
			assertEqual(t, "keys", m.Keys(), c.wantKeys)
		})
	}
	removals := []struct {
		name         string
		contents     string
		keys         []string
		key          string
		wantContents string
		wantKeys     []string
	}{
		{"removeValue: remove a non existing key", `{"a":{"b":2}}`, []string{"a.b"}, "a.b.c", `{"a":{"b":2}}`, []string{"a.b"}},
		{"removeValue: remove a single segmented key", `{"a":1}`, []string{"a"}, "a", `{}`, []string{}},
		{"removeValue: remove a multi segmented key", `{"a":{"b":1}}`, []string{"a.b"}, "a.b", `{}`, []string{}},
	}
	for _, c := range removals {
		t.Run(c.name, func(t *testing.T) {
			m := model(t, c.contents, c.keys)
			m.RemoveValue(c.key)
			assertEqual(t, "contents", m.Contents(), J(t, c.wantContents))
			assertEqual(t, "keys", append([]string{}, m.Keys()...), c.wantKeys)
		})
	}
}

// Ported from VS Code: configurationModels.test.ts ('get overriding configuration …', 'Test override …').
func TestVSCodeModelOverride(t *testing.T) {
	cases := []struct {
		name      string
		contents  string
		overrides []Override
		id        string
		want      string
	}{
		{"for an existing identifier", `{"a":{"b":1},"f":1}`, []Override{block(t, `{"a":{"d":1}}`, []string{"a"}, "c")}, "c", `{"a":{"b":1,"d":1},"f":1}`},
		{"for an identifier that does not exist", `{"a":{"b":1},"f":1}`, []Override{block(t, `{"a":{"d":1}}`, []string{"a"}, "c")}, "xyz", `{"a":{"b":1},"f":1}`},
		{"when one of the keys does not exist in base", `{"a":{"b":1},"f":1}`, []Override{block(t, `{"a":{"d":1},"g":1}`, []string{"a", "g"}, "c")}, "c", `{"a":{"b":1,"d":1},"f":1,"g":1}`},
		{"when one of the keys in base is not an object", `{"a":{"b":1},"f":1}`, []Override{block(t, `{"a":{"d":1},"f":{"g":1}}`, []string{"a", "f"}, "c")}, "c", `{"a":{"b":1,"d":1},"f":{"g":1}}`},
		{"when one of the keys in the override is not an object", `{"a":{"b":1},"f":{"g":1}}`, []Override{block(t, `{"a":{"d":1},"f":1}`, []string{"a", "f"}, "c")}, "c", `{"a":{"b":1,"d":1},"f":1}`},
		{"when the override is an empty object", `{"a":{"b":1},"f":{"g":1}}`, []Override{block(t, `{}`, nil, "c")}, "c", `{"a":{"b":1},"f":{"g":1}}`},
		{"Test override gives all content merged with overrides", `{"a":1,"c":1}`, []Override{block(t, `{"a":2}`, []string{"a"}, "b")}, "b", `{"a":2,"c":1}`},
		{"a falsy override value counts", `{"a":1,"c":1}`, []Override{block(t, `{"a":false}`, []string{"a"}, "b")}, "b", `{"a":false,"c":1}`},
		{"an array is replaced, not merged", `{"a":[1,2,3]}`, []Override{block(t, `{"a":[9]}`, []string{"a"}, "b")}, "b", `{"a":[9]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := NewModel(J(t, c.contents), nil, c.overrides)
			assertEqual(t, "contents", m.Override(c.id).Contents(), J(t, c.want))
			// The model itself is not changed.
			assertEqual(t, "base", m.Contents(), J(t, c.contents))
		})
	}

	t.Run("Test override when an override has multiple identifiers", func(t *testing.T) {
		m := model(t, `{"a":1,"c":1}`, []string{"a", "c"}, block(t, `{"a":2}`, []string{"a"}, "x", "y"))
		for _, id := range []string{"x", "y"} {
			actual := m.Override(id)
			assertEqual(t, id+" contents", actual.Contents(), J(t, `{"a":2,"c":1}`))
			assertEqual(t, id+" keys", actual.Keys(), []string{"a", "c"})
			assertEqual(t, id+" block keys", m.KeysForOverride(id), []string{"a"})
		}
	})
	t.Run("Test override when an identifier is defined in multiple overrides", func(t *testing.T) {
		m := model(t, `{"a":1,"c":1}`, []string{"a", "c"},
			block(t, `{"a":3,"b":1}`, []string{"a", "b"}, "x"), block(t, `{"a":2}`, []string{"a"}, "x", "y"))
		actual := m.Override("x")
		assertEqual(t, "contents", actual.Contents(), J(t, `{"a":3,"c":1,"b":1}`))
		assertEqual(t, "keys", actual.Keys(), []string{"a", "c"})
		assertEqual(t, "block keys", m.KeysForOverride("x"), []string{"a", "b"})
	})
	t.Run("the model is cached and a change clears the cache", func(t *testing.T) {
		m := model(t, `{"a":1}`, []string{"a"}, block(t, `{"a":2}`, []string{"a"}, "b"))
		first := m.Override("b")
		if m.Override("b") != first {
			t.Error("not cached")
		}
		m.SetValue("a", 5.0)
		assertEqual(t, "after SetValue", m.Override("b").Contents(), J(t, `{"a":2}`))
		m.SetValue("z", 1.0)
		assertEqual(t, "new key", m.Override("b").Contents(), J(t, `{"a":2,"z":1}`))
	})
}

func TestVSCodeModelMerge(t *testing.T) {
	t.Run("simple merge", func(t *testing.T) {
		result := model(t, `{"a":1,"b":2}`, []string{"a", "b"}).Merge(model(t, `{"a":3,"c":4}`, []string{"a", "c"}))
		assertEqual(t, "contents", result.Contents(), J(t, `{"a":3,"b":2,"c":4}`))
		assertEqual(t, "keys", result.Keys(), []string{"a", "b", "c"})
	})
	t.Run("recursive merge", func(t *testing.T) {
		result := model(t, `{"a":{"b":1}}`, []string{"a.b"}).Merge(model(t, `{"a":{"b":2}}`, []string{"a.b"}))
		assertEqual(t, "contents", result.Contents(), J(t, `{"a":{"b":2}}`))
		v, _ := result.GetValue("a")
		assertEqual(t, "a", v, J(t, `{"b":2}`))
		assertEqual(t, "keys", result.Keys(), []string{"a.b"})
	})
	t.Run("simple merge overrides", func(t *testing.T) {
		base := model(t, `{"a":{"b":1}}`, []string{"a.b"}, block(t, `{"a":2}`, []string{"a"}, "c"))
		add := model(t, `{"a":{"b":2}}`, []string{"a.b"}, block(t, `{"b":2}`, []string{"b"}, "c"))
		result := base.Merge(add)
		assertEqual(t, "contents", result.Contents(), J(t, `{"a":{"b":2}}`))
		assertEqual(t, "overrides", result.Overrides(), []Override{block(t, `{"a":2,"b":2}`, []string{"a", "b"}, "c")})
		assertEqual(t, "override", result.Override("c").Contents(), J(t, `{"a":2,"b":2}`))
		assertEqual(t, "keys", result.Keys(), []string{"a.b"})
	})
	t.Run("recursive merge overrides", func(t *testing.T) {
		base := model(t, `{"a":{"b":1},"f":1}`, []string{"a.b", "f"}, block(t, `{"a":{"d":1}}`, []string{"a"}, "c"))
		add := model(t, `{"a":{"b":2}}`, []string{"a.b"}, block(t, `{"a":{"e":2}}`, []string{"a"}, "c"))
		result := base.Merge(add)
		assertEqual(t, "contents", result.Contents(), J(t, `{"a":{"b":2},"f":1}`))
		assertEqual(t, "overrides", result.Overrides(), []Override{block(t, `{"a":{"d":1,"e":2}}`, []string{"a"}, "c")})
		assertEqual(t, "override", result.Override("c").Contents(), J(t, `{"a":{"b":2,"d":1,"e":2},"f":1}`))
		assertEqual(t, "keys", result.Keys(), []string{"a.b", "f"})
	})
	t.Run("Test merge when configuration models have multiple identifiers", func(t *testing.T) {
		base := model(t, `{"a":1,"c":1}`, []string{"a", "c"}, block(t, `{"c":1}`, []string{"c"}, "y"), block(t, `{"a":2}`, []string{"a"}, "x", "y"))
		target := model(t, `{"a":2,"b":1}`, []string{"a", "b"}, block(t, `{"a":3,"b":2}`, []string{"a", "b"}, "x"), block(t, `{"b":3}`, []string{"b"}, "x", "y"))
		actual := base.Merge(target)
		assertEqual(t, "contents", actual.Contents(), J(t, `{"a":2,"c":1,"b":1}`))
		assertEqual(t, "keys", actual.Keys(), []string{"a", "c", "b"})
		assertEqual(t, "overrides", actual.Overrides(), []Override{
			block(t, `{"c":1}`, []string{"c"}, "y"),
			block(t, `{"a":2,"b":3}`, []string{"a", "b"}, "x", "y"),
			block(t, `{"a":3,"b":2}`, []string{"a", "b"}, "x"),
		})
		assertEqual(t, "identifiers", actual.OverrideIdentifiers(), []string{"y", "x"})
	})
	t.Run("the merge does not change its arguments", func(t *testing.T) {
		base := model(t, `{"a":{"b":1}}`, []string{"a.b"})
		add := model(t, `{"a":{"c":2}}`, []string{"a.c"})
		result := base.Merge(add)
		result.SetValue("a.d", 3.0)
		assertEqual(t, "base", base.Contents(), J(t, `{"a":{"b":1}}`))
		assertEqual(t, "add", add.Contents(), J(t, `{"a":{"c":2}}`))
	})
}

func TestVSCodeModelValues(t *testing.T) {
	t.Run("Test contents while getting an existing property", func(t *testing.T) {
		v, ok := model(t, `{"a":1}`, nil).GetValue("a")
		assertEqual(t, "a", []any{v, ok}, []any{1.0, true})
		v, _ = model(t, `{"a":{"b":1}}`, nil).GetValue("a")
		assertEqual(t, "a", v, J(t, `{"b":1}`))
	})
	t.Run("Test contents are undefined for non existing properties", func(t *testing.T) {
		m := model(t, `{"awesome":true}`, nil)
		for _, key := range []string{"unknownproperty", "awesome.x", "a.b.c"} {
			if v, ok := m.GetValue(key); ok {
				t.Errorf("%s = %v", key, v)
			}
		}
		if v, ok := m.GetValue(""); !ok || !reflect.DeepEqual(v, m.Contents()) {
			t.Errorf("the whole tree: %v %v", v, ok)
		}
	})
	t.Run("a null value is a value", func(t *testing.T) {
		if v, ok := model(t, `{"a":null}`, nil).GetValue("a"); !ok || v != nil {
			t.Errorf("%v %v", v, ok)
		}
	})
	t.Run("the override value", func(t *testing.T) {
		m := model(t, `{"a":1}`, nil, block(t, `{"a":2,"b":{"c":3}}`, []string{"a", "b.c"}, "x"))
		if v, ok := m.GetOverrideValue("a", "x"); !ok || v != 2.0 {
			t.Errorf("a: %v %v", v, ok)
		}
		if v, ok := m.GetOverrideValue("b.c", "x"); !ok || v != 3.0 {
			t.Errorf("b.c: %v %v", v, ok)
		}
		if v, ok := m.GetOverrideValue("", "x"); !ok || !reflect.DeepEqual(v, J(t, `{"a":2,"b":{"c":3}}`)) {
			t.Errorf("all: %v %v", v, ok)
		}
		if _, ok := m.GetOverrideValue("a", "y"); ok {
			t.Error("a value for a language without a block")
		}
	})
}

func TestVSCodeModelInspect(t *testing.T) {
	rawSame := func() *Model {
		return model(t, `{"a":1,"c":1}`, []string{"a", "c"}, block(t, `{"a":2,"b":1}`, []string{"a"}, "x", "y"))
	}
	type insp struct {
		value, override, merged Optional
		overrides               []OverrideValue
	}
	check := func(t *testing.T, name string, got Inspection, want insp) {
		t.Helper()
		assertEqual(t, name+" value", got.Value, want.value)
		assertEqual(t, name+" override", got.Override, want.override)
		assertEqual(t, name+" merged", got.Merged, want.merged)
		assertEqual(t, name+" overrides", got.Overrides, want.overrides)
	}
	xy := []string{"x", "y"}
	t.Run("inspect when raw is same", func(t *testing.T) {
		m := rawSame()
		check(t, "a", m.Inspect("a", ""), insp{opt(1.0), Optional{}, opt(1.0), []OverrideValue{{xy, 2.0}}})
		check(t, "a in x", m.Inspect("a", "x"), insp{opt(1.0), opt(2.0), opt(2.0), []OverrideValue{{xy, 2.0}}})
		check(t, "b in x", m.Inspect("b", "x"), insp{Optional{}, opt(1.0), opt(1.0), []OverrideValue{{xy, 1.0}}})
		check(t, "d", m.Inspect("d", ""), insp{})
	})
	t.Run("inspect when raw is not same", func(t *testing.T) {
		m := model(t, `{"a":1,"c":1}`, []string{"a", "c"}, block(t, `{"a":2}`, []string{"a"}, "x", "y"))
		m.raw = []rawSource{{dict: rawFromMap(J(t, `{"a":1,"b":2,"c":1,"d":3,"[x][y]":{"a":2,"b":1}}`))}}
		check(t, "a", m.Inspect("a", ""), insp{opt(1.0), Optional{}, opt(1.0), []OverrideValue{{xy, 2.0}}})
		check(t, "a in x", m.Inspect("a", "x"), insp{opt(1.0), opt(2.0), opt(2.0), []OverrideValue{{xy, 2.0}}})
		check(t, "b in x", m.Inspect("b", "x"), insp{opt(2.0), opt(1.0), opt(1.0), []OverrideValue{{xy, 1.0}}})
		check(t, "d", m.Inspect("d", ""), insp{opt(3.0), Optional{}, opt(3.0), nil})
		check(t, "e", m.Inspect("e", ""), insp{})
	})
	t.Run("inspect in merged configuration when raw is same", func(t *testing.T) {
		m := model(t, `{"a":1}`, []string{"a"}, block(t, `{"a":2}`, []string{"a"}, "x", "y")).Merge(model(t, `{"b":3}`, []string{"b"}))
		check(t, "a", m.Inspect("a", ""), insp{opt(1.0), Optional{}, opt(1.0), []OverrideValue{{xy, 2.0}}})
		check(t, "a in x", m.Inspect("a", "x"), insp{opt(1.0), opt(2.0), opt(2.0), []OverrideValue{{xy, 2.0}}})
		check(t, "b", m.Inspect("b", ""), insp{opt(3.0), Optional{}, opt(3.0), nil})
		check(t, "b in y", m.Inspect("b", "y"), insp{opt(3.0), Optional{}, opt(3.0), nil})
		check(t, "c", m.Inspect("c", ""), insp{})
	})
	t.Run("inspect in merged configuration when raw is not same for one model", func(t *testing.T) {
		target1 := model(t, `{"a":1}`, []string{"a"}, block(t, `{"a":2}`, []string{"a"}, "x", "y"))
		target1.raw = []rawSource{{dict: rawFromMap(J(t, `{"a":1,"b":2,"c":3,"[x][y]":{"a":2,"b":4}}`))}}
		m := target1.Merge(model(t, `{"b":3}`, []string{"b"}))
		check(t, "a", m.Inspect("a", ""), insp{opt(1.0), Optional{}, opt(1.0), []OverrideValue{{xy, 2.0}}})
		check(t, "a in x", m.Inspect("a", "x"), insp{opt(1.0), opt(2.0), opt(2.0), []OverrideValue{{xy, 2.0}}})
		check(t, "b", m.Inspect("b", ""), insp{opt(3.0), Optional{}, opt(3.0), []OverrideValue{{xy, 4.0}}})
		check(t, "b in y", m.Inspect("b", "y"), insp{opt(3.0), opt(4.0), opt(4.0), []OverrideValue{{xy, 4.0}}})
		check(t, "c", m.Inspect("c", ""), insp{opt(3.0), Optional{}, opt(3.0), nil})
	})
	t.Run("inspect: return all overrides", func(t *testing.T) {
		m := model(t, `{"a":1,"c":1}`, []string{"a", "c"},
			block(t, `{"a":2,"b":1}`, []string{"a", "b"}, "x", "y"), block(t, `{"a":3}`, []string{"a"}, "x"), block(t, `{"b":3}`, []string{"b"}, "y"))
		assertEqual(t, "overrides", m.Inspect("a", "").Overrides, []OverrideValue{{xy, 2.0}, {[]string{"x"}, 3.0}})
	})
	t.Run("inspect when no overrides", func(t *testing.T) {
		if got := model(t, `{"a":1,"c":1}`, []string{"a", "c"}).Inspect("a", "").Overrides; got != nil {
			t.Errorf("overrides %v", got)
		}
	})
}

func TestModelLanguageBlocksAreUpdated(t *testing.T) {
	t.Run("SetValue of a block key makes its override", func(t *testing.T) {
		m := model(t, `{"a":1}`, []string{"a"})
		m.SetValue("[go]", J(t, `{"a":2,"b.c":3}`))
		assertEqual(t, "overrides", m.Overrides(), []Override{block(t, `{"a":2,"b":{"c":3}}`, []string{"a", "b.c"}, "go")})
		assertEqual(t, "go", m.Override("go").Contents()["a"], 2.0)
		// Setting it again replaces the block.
		m.SetValue("[go]", J(t, `{"a":5}`))
		assertEqual(t, "overrides", m.Overrides(), []Override{block(t, `{"a":5}`, []string{"a"}, "go")})
		assertEqual(t, "keys", m.Keys(), []string{"a", "[go]"})
	})
	t.Run("AddValue puts a key at the end again", func(t *testing.T) {
		m := model(t, `{"a":1,"b":2}`, []string{"a", "b"})
		m.AddValue("a", 3.0)
		assertEqual(t, "keys", m.Keys(), []string{"a", "b", "a"})
		assertEqual(t, "contents", m.Contents(), J(t, `{"a":3,"b":2}`))
	})
	t.Run("RemoveValue of a block key removes the block", func(t *testing.T) {
		m := model(t, `{"a":1}`, []string{"a"})
		m.SetValue("[go]", J(t, `{"a":2}`))
		m.SetValue("[json]", J(t, `{"a":3}`))
		m.RemoveValue("[go]")
		assertEqual(t, "overrides", m.Overrides(), []Override{block(t, `{"a":3}`, []string{"a"}, "json")})
		assertEqual(t, "keys", m.Keys(), []string{"a", "[json]"})
		assertEqual(t, "go", m.Override("go"), m)
	})
	t.Run("a conflict is reported and the value ignored", func(t *testing.T) {
		m := model(t, `{"a":"text"}`, []string{"a"})
		m.SetValue("a.b", 1.0)
		assertEqual(t, "contents", m.Contents(), J(t, `{"a":"text"}`))
	})
}

func TestStoreKeys(t *testing.T) {
	f := newFixture(t)
	f.write(f.userPath, `{"files.eol": "\n", "editor": {"tabSize": 2}, "[go]": {"editor.tabSize": 3}}`)
	f.reload()
	assertEqual(t, "user", f.store.Keys(TargetUser), []string{"editor.tabSize", "files.eol"})
	assertEqual(t, "workspace", f.store.Keys(TargetWorkspace), []string(nil))
}
