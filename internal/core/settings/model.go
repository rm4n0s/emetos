package settings

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// overrideKey matches the key of a language block: "[go]" or "[go][json]".
var overrideKey = regexp.MustCompile(`^(\[([^\]]+)\])+$`)

var overrideIdentifier = regexp.MustCompile(`\[([^\]]+)\]`)

// IsOverrideKey tells whether a key names a language block, such as "[go]".
func IsOverrideKey(key string) bool { return overrideKey.MatchString(key) }

// OverrideIdentifiers returns the language ids in the key of a block, without
// repeats: "[x][y][x]" gives x and y. A key that is no block gives none.
//
// Ported from VS Code: src/vs/platform/configuration/common/configurationRegistry.ts (overrideIdentifiersFromKey).
func OverrideIdentifiers(key string) []string {
	var out []string
	if !overrideKey.MatchString(key) {
		return out
	}
	for _, m := range overrideIdentifier.FindAllStringSubmatch(key, -1) {
		if id := strings.TrimSpace(m[1]); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// OverrideKey makes the key of a block for language ids: "[go]" or "[go][json]".
func OverrideKey(ids ...string) string {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString("[" + id + "]")
	}
	return b.String()
}

// Override is a language block of a settings file: the settings for the languages
// of Identifiers.
type Override struct {
	// Identifiers are the language ids of the block.
	Identifiers []string
	// Keys are the names of the settings as they are written in the block.
	Keys []string
	// Contents are the settings as a tree, "editor.tabSize" being {"editor": {"tabSize": …}}.
	Contents map[string]any
}

func (o Override) clone() Override {
	return Override{Identifiers: slices.Clone(o.Identifiers), Keys: slices.Clone(o.Keys), Contents: deepClone(o.Contents).(map[string]any)}
}

// rawSource is where a Model's unfiltered settings come from: a parsed dictionary
// or another model.
type rawSource struct {
	dict  *rawInput
	model *Model
}

// Model is the settings of one file (or of several merged), as a tree of values with
// the language blocks apart. Values are what the JSON parser gives: nil, bool, float64,
// string, []any and map[string]any. A Model is read by many goroutines; SetValue,
// AddValue and RemoveValue change it and must not run at the same time as anything else.
//
// Ported from VS Code: src/vs/platform/configuration/common/configurationModels.ts (ConfigurationModel).
type Model struct {
	contents  map[string]any
	keys      []string
	overrides []Override
	raw       []rawSource

	mu       sync.Mutex
	rawCache *Model
	cache    map[string]*Model
}

// NewModel makes a model of a tree of values, the keys as they were written and the
// language blocks. The model owns the arguments.
func NewModel(contents map[string]any, keys []string, overrides []Override) *Model {
	if contents == nil {
		contents = map[string]any{}
	}
	return &Model{contents: contents, keys: keys, overrides: overrides}
}

// Contents is the tree of values. Do not change it.
func (m *Model) Contents() map[string]any { return m.contents }

// Keys are the names of the settings as they were written, in order.
func (m *Model) Keys() []string { return m.keys }

// Overrides are the language blocks.
func (m *Model) Overrides() []Override { return m.overrides }

// IsEmpty tells whether the model has no settings at all.
func (m *Model) IsEmpty() bool {
	return len(m.keys) == 0 && len(m.contents) == 0 && len(m.overrides) == 0
}

// GetValue returns the value of a dotted section, or false when there is none. An empty
// section is the whole tree.
func (m *Model) GetValue(section string) (any, bool) {
	if section == "" {
		return m.contents, true
	}
	return valueAt(m.contents, section)
}

// valueAt walks a dotted path in a tree.
func valueAt(tree map[string]any, path string) (any, bool) {
	var cur any = tree
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = obj[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// Optional is a value that may be absent: nil is a value (JSON null) when Ok is set.
type Optional struct {
	Value any
	Ok    bool
}

// OverrideValue is the value a language block gives.
type OverrideValue struct {
	Identifiers []string
	Value       any
}

// Inspection says where a value comes from.
type Inspection struct {
	// Value is the value of the model without the language blocks.
	Value Optional
	// Override is the value the blocks for the language give, when a language was asked about.
	Override Optional
	// Merged is the value for the language, the blocks over the rest.
	Merged Optional
	// Overrides are the values of all the blocks that set the section.
	Overrides []OverrideValue
}

// Inspect looks at a section: its value, its value in the language blocks of the
// language (when not empty), and in every block. It looks at the unfiltered settings
// when the model was parsed with some left out.
func (m *Model) Inspect(section, languageID string) Inspection {
	raw := m.rawConfiguration()
	var in Inspection
	in.Value = optionalOf(raw.GetValue(section))
	if languageID != "" {
		in.Override = optionalOf(raw.GetOverrideValue(section, languageID))
		in.Merged = optionalOf(raw.Override(languageID).GetValue(section))
	} else {
		in.Merged = in.Value
	}
	for _, o := range raw.overrides {
		if v, ok := NewModel(o.Contents, o.Keys, nil).GetValue(section); ok {
			in.Overrides = append(in.Overrides, OverrideValue{Identifiers: o.Identifiers, Value: v})
		}
	}
	return in
}

func optionalOf(v any, ok bool) Optional { return Optional{Value: v, Ok: ok} }

// rawConfiguration is the model of the unfiltered settings.
func (m *Model) rawConfiguration() *Model {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rawCache != nil {
		return m.rawCache
	}
	if m.raw == nil {
		m.rawCache = m
		return m
	}
	var result *Model
	for _, src := range m.raw {
		cur := src.model
		if cur == nil {
			cur = modelFromRaw(src.dict)
		}
		switch {
		case result == nil:
			result = cur
		case cur != result:
			result = result.Merge(cur)
		}
	}
	m.rawCache = result
	return result
}

// GetOverrideValue returns the value the blocks for a language give a section.
func (m *Model) GetOverrideValue(section, languageID string) (any, bool) {
	c := m.contentsForOverride(languageID)
	if c == nil {
		return nil, false
	}
	if section == "" {
		return c, true
	}
	return valueAt(c, section)
}

// KeysForOverride returns the names written in the blocks that include the language.
func (m *Model) KeysForOverride(languageID string) []string {
	var keys []string
	for _, o := range m.overrides {
		if slices.Contains(o.Identifiers, languageID) {
			for _, k := range o.Keys {
				if !slices.Contains(keys, k) {
					keys = append(keys, k)
				}
			}
		}
	}
	return keys
}

// OverrideIdentifiers returns the language ids of all the blocks, without repeats.
func (m *Model) OverrideIdentifiers() []string {
	var out []string
	for _, o := range m.overrides {
		for _, id := range o.Identifiers {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	return out
}

// Override returns the model for a language: the settings with the blocks of the
// language over them. A language without blocks gets the model itself.
func (m *Model) Override(languageID string) *Model {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cached, ok := m.cache[languageID]; ok {
		return cached
	}
	o := m.overrideModel(languageID)
	if m.cache == nil {
		m.cache = map[string]*Model{}
	}
	m.cache[languageID] = o
	return o
}

func (m *Model) overrideModel(languageID string) *Model {
	oc := m.contentsForOverride(languageID)
	if len(oc) == 0 {
		return m
	}
	contents := map[string]any{}
	for key, base := range m.contents {
		contents[key] = base
	}
	for key, over := range oc {
		base, has := contents[key]
		bm, baseIsMap := base.(map[string]any)
		om, overIsMap := over.(map[string]any)
		if has && baseIsMap && overIsMap {
			merged := deepClone(bm).(map[string]any)
			mergeContents(merged, om)
			contents[key] = merged
		} else {
			contents[key] = deepClone(over)
		}
	}
	return &Model{contents: contents, keys: m.keys, overrides: m.overrides}
}

// contentsForOverride merges the blocks that name the language: first those that name it
// among others, then the one that names it alone.
func (m *Model) contentsForOverride(languageID string) map[string]any {
	var alone map[string]any
	var contents map[string]any
	merge := func(c map[string]any) {
		if c == nil {
			return
		}
		if contents == nil {
			contents = deepClone(c).(map[string]any)
		} else {
			mergeContents(contents, c)
		}
	}
	for _, o := range m.overrides {
		if len(o.Identifiers) == 1 && o.Identifiers[0] == languageID {
			alone = o.Contents
		} else if slices.Contains(o.Identifiers, languageID) {
			merge(o.Contents)
		}
	}
	merge(alone)
	return contents
}

// Merge returns a model with the settings of the others over its own, values of objects
// merged one by one. The language blocks with the same ids are merged too.
func (m *Model) Merge(others ...*Model) *Model {
	contents := deepClone(m.contents).(map[string]any)
	overrides := make([]Override, len(m.overrides))
	for i, o := range m.overrides {
		overrides[i] = o.clone()
	}
	keys := slices.Clone(m.keys)
	raws := m.rawSources()
	for _, other := range others {
		raws = append(raws, other.rawSources()...)
		if other.IsEmpty() {
			continue
		}
		mergeContents(contents, other.contents)
		for _, oo := range other.overrides {
			found := false
			for i := range overrides {
				if slices.Equal(overrides[i].Identifiers, oo.Identifiers) {
					mergeContents(overrides[i].Contents, oo.Contents)
					for _, k := range oo.Keys {
						if !slices.Contains(overrides[i].Keys, k) {
							overrides[i].Keys = append(overrides[i].Keys, k)
						}
					}
					found = true
					break
				}
			}
			if !found {
				overrides = append(overrides, oo.clone())
			}
		}
		for _, k := range other.keys {
			if !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}
	result := &Model{contents: contents, keys: keys, overrides: overrides}
	for _, r := range raws {
		if r.dict != nil {
			result.raw = raws
			break
		}
	}
	return result
}

// rawSources are the sources of the model's unfiltered settings: its own, or itself.
func (m *Model) rawSources() []rawSource {
	if m.raw != nil {
		return slices.Clone(m.raw)
	}
	return []rawSource{{model: m}}
}

// SetValue sets a dotted key, keeping its place among the keys when it is there already.
func (m *Model) SetValue(key string, value any) { m.updateValue(key, value, false) }

// AddValue sets a dotted key and puts it at the end of the keys, even if it is there.
func (m *Model) AddValue(key string, value any) { m.updateValue(key, value, true) }

// RemoveValue removes a dotted key; nothing happens when it is not one of the keys.
func (m *Model) RemoveValue(key string) {
	i := slices.Index(m.keys, key)
	if i < 0 {
		return
	}
	m.changed()
	m.keys = slices.Delete(m.keys, i, i+1)
	removeFromTree(m.contents, key)
	if overrideKey.MatchString(key) {
		ids := OverrideIdentifiers(key)
		if j := slices.IndexFunc(m.overrides, func(o Override) bool { return slices.Equal(o.Identifiers, ids) }); j >= 0 {
			m.overrides = slices.Delete(m.overrides, j, j+1)
		}
	}
}

func (m *Model) changed() {
	m.mu.Lock()
	m.cache, m.rawCache = nil, nil
	m.mu.Unlock()
}

func (m *Model) updateValue(key string, value any, add bool) {
	m.changed()
	addToTree(m.contents, key, value, nil)
	add = add || !slices.Contains(m.keys, key)
	if add {
		m.keys = append(m.keys, key)
	}
	if overrideKey.MatchString(key) {
		block, _ := m.contents[key].(map[string]any)
		ids := OverrideIdentifiers(key)
		o := Override{Identifiers: ids, Keys: sortedKeys(block), Contents: toTree(block, nil)}
		if i := slices.IndexFunc(m.overrides, func(x Override) bool { return slices.Equal(x.Identifiers, ids) }); i >= 0 {
			m.overrides[i] = o
		} else {
			m.overrides = append(m.overrides, o)
		}
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// deepClone copies a tree of values.
func deepClone(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = deepClone(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = deepClone(e)
		}
		return out
	}
	return v
}

// mergeContents puts the values of target over source; objects are merged and
// everything else replaced.
func mergeContents(source, target map[string]any) {
	for key, tv := range target {
		if sv, ok := source[key].(map[string]any); ok {
			if tm, ok := tv.(map[string]any); ok {
				mergeContents(sv, tm)
				continue
			}
		}
		source[key] = deepClone(tv)
	}
}

// addToTree puts a value at a dotted key of a tree. A key that goes through a value
// that is not an object is ignored and reported.
//
// Ported from VS Code: src/vs/platform/configuration/common/configuration.ts (addToValueTree).
func addToTree(root map[string]any, key string, value any, report func(string)) {
	segments := strings.Split(key, ".")
	last := segments[len(segments)-1]
	cur := root
	for i, s := range segments[:len(segments)-1] {
		switch next := cur[s].(type) {
		case nil:
			if _, has := cur[s]; has {
				if report != nil {
					report("Ignoring " + key + " as " + strings.Join(segments[:i+1], ".") + " is null")
				}
				return
			}
			n := map[string]any{}
			cur[s] = n
			cur = n
		case map[string]any:
			cur = next
		default:
			if report != nil {
				report("Ignoring " + key + " as " + strings.Join(segments[:i+1], ".") + " is not an object")
			}
			return
		}
	}
	cur[last] = value
}

// removeFromTree removes a dotted key, and the objects it leaves empty.
//
// Ported from VS Code: src/vs/platform/configuration/common/configuration.ts (removeFromValueTree).
func removeFromTree(tree map[string]any, key string) {
	removeSegments(tree, strings.Split(key, "."))
}

func removeSegments(tree map[string]any, segments []string) {
	first := segments[0]
	if len(segments) == 1 {
		delete(tree, first)
		return
	}
	if child, ok := tree[first].(map[string]any); ok {
		removeSegments(child, segments[1:])
		if len(child) == 0 {
			delete(tree, first)
		}
	}
}

// toTree makes a tree of properties whose names may be dotted.
func toTree(props map[string]any, report func(string)) map[string]any {
	return toTreeOrdered(props, sortedKeys(props), report)
}

func toTreeOrdered(props map[string]any, order []string, report func(string)) map[string]any {
	root := map[string]any{}
	for _, k := range order {
		addToTree(root, k, props[k], report)
	}
	return root
}

// equalValues compares two values of settings.
func equalValues(a, b any) bool { return reflect.DeepEqual(a, b) }
