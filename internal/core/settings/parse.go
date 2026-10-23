package settings

import (
	"slices"
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/jsonc"
)

// rawInput is the top-level object of a settings file with its keys in the order they
// are written, which decides how conflicting keys resolve.
type rawInput struct {
	order  []string
	values map[string]any
	// blockOrder is the order of the keys inside each language block.
	blockOrder map[string][]string
}

// rawFromMap makes a rawInput of a map; its keys are taken in alphabetical order.
func rawFromMap(values map[string]any) *rawInput {
	r := &rawInput{order: sortedKeys(values), values: values, blockOrder: map[string][]string{}}
	return r
}

// rawFromTree reads the top-level properties of an object node.
func rawFromTree(root *jsonc.Node) *rawInput {
	r := &rawInput{values: map[string]any{}, blockOrder: map[string][]string{}}
	for _, prop := range root.Children {
		if len(prop.Children) == 0 {
			continue
		}
		name, _ := prop.Children[0].Value.(string)
		if len(prop.Children) < 2 {
			continue
		}
		if _, seen := r.values[name]; !seen {
			r.order = append(r.order, name)
		}
		value := prop.Children[1]
		r.values[name] = jsonc.NodeValue(value)
		if overrideKey.MatchString(name) && value.Type == jsonc.ObjectNode {
			r.blockOrder[name] = propertyNames(value)
		}
	}
	return r
}

func propertyNames(object *jsonc.Node) []string {
	var names []string
	for _, prop := range object.Children {
		if len(prop.Children) < 2 {
			continue
		}
		name, _ := prop.Children[0].Value.(string)
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// ParseOptions leave settings out when a file is parsed.
type ParseOptions struct {
	// SkipUnregistered leaves out the settings that are not registered.
	SkipUnregistered bool
	// Scopes, when not nil, leaves out the settings whose scope is not in the list.
	Scopes []Scope
	// Include keeps the listed keys whatever their scope; Exclude drops them, and wins.
	Include, Exclude []string
}

// ParseResult is a parsed settings file.
type ParseResult struct {
	// Model has the settings that were kept.
	Model *Model
	// Errors are the syntax errors; the parser goes on after them.
	Errors []jsonc.ParseError
	// Conflicts say which keys were ignored because another key made their place a
	// value that is not an object ("a.b" after "a": 1).
	Conflicts []string
	// NotObject is true when the file holds a value that is not an object.
	NotObject bool
}

// ParseModel parses the text of a settings file. A text with nothing in it gives an empty
// model.
//
// Ported from VS Code: src/vs/platform/configuration/common/configurationModels.ts (ConfigurationModelParser).
func (r *Registry) ParseModel(text string, opts ParseOptions) ParseResult {
	root, errs := jsonc.ParseTree(text, jsonc.ParseOptions{AllowEmptyContent: true})
	res := ParseResult{Errors: errs}
	var raw *rawInput
	switch {
	case root == nil:
		raw = &rawInput{values: map[string]any{}, blockOrder: map[string][]string{}}
	case root.Type != jsonc.ObjectNode:
		res.NotObject = true
		raw = &rawInput{values: map[string]any{}, blockOrder: map[string][]string{}}
	default:
		raw = rawFromTree(root)
	}
	res.Model = r.parseRaw(raw, opts, &res.Conflicts)
	return res
}

// ParseMap makes a model of settings given as a map, as ParseModel does of a file.
func (r *Registry) ParseMap(values map[string]any, opts ParseOptions) ParseResult {
	var res ParseResult
	res.Model = r.parseRaw(rawFromMap(values), opts, &res.Conflicts)
	return res
}

func (r *Registry) parseRaw(raw *rawInput, opts ParseOptions, conflicts *[]string) *Model {
	report := func(msg string) { *conflicts = append(*conflicts, msg) }
	filtered, excluded := r.filter(raw, opts)
	m := &Model{
		contents: toTreeOrdered(filtered.values, filtered.order, report),
		keys:     slices.Clone(filtered.order),
	}
	for _, key := range filtered.order {
		if !overrideKey.MatchString(key) {
			continue
		}
		block, _ := filtered.values[key].(map[string]any)
		order := filtered.blockOrder[key]
		if order == nil {
			order = sortedKeys(block)
		}
		m.overrides = append(m.overrides, Override{
			Identifiers: OverrideIdentifiers(key),
			Keys:        slices.Clone(order),
			Contents:    toTreeOrdered(block, order, report),
		})
	}
	if excluded {
		m.raw = []rawSource{{dict: raw}}
	}
	return m
}

// modelFromRaw parses a dictionary without leaving anything out.
func modelFromRaw(raw *rawInput) *Model {
	return NewRegistry().parseRaw(raw, ParseOptions{}, new([]string))
}

// filter drops what the options leave out and tells whether it dropped anything.
func (r *Registry) filter(raw *rawInput, opts ParseOptions) (*rawInput, bool) {
	if opts.Scopes == nil && !opts.SkipUnregistered && len(opts.Exclude) == 0 {
		return raw, false
	}
	out := &rawInput{values: map[string]any{}, blockOrder: map[string][]string{}}
	dropped := false
	for _, key := range raw.order {
		value := raw.values[key]
		if overrideKey.MatchString(key) {
			block, ok := value.(map[string]any)
			if !ok {
				out.order = append(out.order, key)
				out.values[key] = value
				continue
			}
			order := raw.blockOrder[key]
			if order == nil {
				order = sortedKeys(block)
			}
			kept := map[string]any{}
			var keptOrder []string
			for _, k := range order {
				v, included, d := r.filterProperty(k, block[k], opts)
				dropped = dropped || d
				if included {
					kept[k] = v
					keptOrder = append(keptOrder, k)
				} else {
					dropped = true
				}
			}
			out.order = append(out.order, key)
			out.values[key] = kept
			out.blockOrder[key] = keptOrder
			continue
		}
		v, included, d := r.filterProperty(key, value, opts)
		dropped = dropped || d
		if included {
			out.order = append(out.order, key)
			out.values[key] = v
		} else {
			dropped = true
		}
	}
	return out, dropped
}

// filterProperty decides whether the property at a dotted path is kept. A value that is
// an object and not registered itself is looked into, key by key.
func (r *Registry) filterProperty(path string, value any, opts ParseOptions) (kept any, included, dropped bool) {
	setting, registered := r.Lookup(path)
	if registered {
		return value, shouldInclude(path, &setting, opts), false
	}
	obj, isObject := value.(map[string]any)
	if !isObject || len(obj) == 0 {
		return value, shouldInclude(path, nil, opts), false
	}
	// If the whole object is excluded, do not look into it.
	if slices.Contains(opts.Exclude, path) {
		return value, false, false
	}
	nested := map[string]any{}
	hasAny := false
	for _, k := range sortedKeys(obj) {
		v, inc, d := r.filterProperty(path+"."+k, obj[k], opts)
		dropped = dropped || d
		if inc {
			nested[k] = v
			hasAny = true
		} else {
			dropped = true
		}
	}
	return nested, hasAny, dropped
}

func shouldInclude(key string, s *Setting, opts ParseOptions) bool {
	if slices.Contains(opts.Exclude, key) {
		return false
	}
	if slices.Contains(opts.Include, key) {
		return true
	}
	if opts.SkipUnregistered && s == nil {
		return false
	}
	if s == nil || opts.Scopes == nil {
		return true
	}
	return slices.Contains(opts.Scopes, s.Scope)
}

// flatten lists the dotted keys of the leaves of a tree, the registered settings being
// leaves whatever their value. Language blocks are left out.
func (r *Registry) flatten(tree map[string]any) []string {
	var keys []string
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for _, k := range sortedKeys(m) {
			if prefix == "" && overrideKey.MatchString(k) {
				continue
			}
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			child, isObject := m[k].(map[string]any)
			if _, registered := r.Lookup(path); registered || !isObject || len(child) == 0 {
				keys = append(keys, path)
				continue
			}
			walk(path, child)
		}
	}
	walk("", tree)
	sort.Strings(keys)
	return keys
}

// section reports whether key is the dotted section or inside it.
func inSection(key, section string) bool {
	return key == section || strings.HasPrefix(key, section+".")
}
