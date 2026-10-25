package settings

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"
)

// Type is a JSON type a setting's value can have.
type Type string

// The JSON types.
const (
	// String is a JSON string.
	String Type = "string"
	// Number is any JSON number.
	Number Type = "number"
	// Integer is a JSON number without a fraction.
	Integer Type = "integer"
	// Boolean is true or false.
	Boolean Type = "boolean"
	// Array is a JSON array.
	Array Type = "array"
	// Object is a JSON object.
	Object Type = "object"
	// Null is null.
	Null Type = "null"
)

// Scope says where a setting can be set and whether a language can override it
// (VS Code's ConfigurationScope).
type Scope int

const (
	// ScopeWindow is the default: a setting for the window, from the user and the workspace.
	ScopeWindow Scope = iota
	// ScopeApplication is a setting of the user only: a workspace cannot set it.
	ScopeApplication
	// ScopeMachine is a setting of the machine: a workspace cannot set it.
	ScopeMachine
	// ScopeResource is a setting that can differ per file, from the user and the workspace.
	ScopeResource
	// ScopeLanguageOverridable is a resource setting that a [language] block can set (the
	// settings UI lists it for each language).
	ScopeLanguageOverridable
)

// inWorkspace tells whether a workspace's settings.json may set a setting of this scope.
func (s Scope) inWorkspace() bool {
	return s == ScopeWindow || s == ScopeResource || s == ScopeLanguageOverridable
}

// Setting describes one registered setting (SPEC §11.1).
type Setting struct {
	// Key is the dotted name, such as "editor.tabSize".
	Key string
	// Types are the JSON types the value may have; the first is the main one.
	Types []Type
	// Default is the value when nothing sets it. It is a Go value that encoding/json
	// writes as a value of Types (an int is fine; it is stored as a float64).
	Default any
	// Description tells what the setting does (Markdown may be used).
	Description string
	// Enum lists the only values allowed, when it is not empty.
	Enum []any
	// EnumDescriptions describe the values of Enum, in the same order.
	EnumDescriptions []string
	// Minimum and Maximum bound a number, when not nil.
	Minimum, Maximum *float64
	// ItemTypes are the types an element of an array may have, when not empty.
	ItemTypes []Type
	// ItemEnum lists the only values an element of an array may have, when not empty.
	ItemEnum []any
	// Scope is where the setting may be set.
	Scope Scope
	// Category is the group of the settings UI, such as "Text Editor".
	Category string
	// Deprecated is a message that says what to use instead; empty for a setting in use.
	Deprecated string
}

// Float returns a pointer to v, for Minimum and Maximum.
func Float(v float64) *float64 { return &v }

// Registry holds the settings, with their defaults (SPEC §11.1). It is safe for
// concurrent use.
type Registry struct {
	mu       sync.RWMutex
	settings map[string]*Setting
	order    []string
	// languageDefaults are the defaults of language blocks: language id -> key -> value.
	languageDefaults map[string]map[string]any
	languageOrder    []string
	version          int
}

// NewRegistry makes an empty registry; DefaultRegistry makes one with the settings Emetos has.
func NewRegistry() *Registry {
	return &Registry{settings: map[string]*Setting{}, languageDefaults: map[string]map[string]any{}}
}

// Register adds a setting. It fails with the tags SettingKeyInvalid (an empty key, a key
// with an empty segment or one that looks like a language block), SettingDuplicate,
// SettingTypeMissing and SettingDefaultInvalid (the default does not satisfy the setting's
// own types, enum and range).
func (r *Registry) Register(s Setting) error {
	if s.Key == "" || strings.Contains(s.Key, "..") || strings.HasPrefix(s.Key, ".") || strings.HasSuffix(s.Key, ".") || overrideKey.MatchString(s.Key) {
		return errors.New("SettingKeyInvalid", "the key of a setting is not a dotted name", "key", s.Key)
	}
	if len(s.Types) == 0 {
		return errors.New("SettingTypeMissing", "a setting needs a type", "key", s.Key)
	}
	if s.Default == nil && !hasType(s.Types, Null) {
		s.Default = zeroValue(s.Types[0])
	}
	def, err := normalize(s.Default)
	if err != nil {
		return err
	}
	s.Default = def
	s.Enum = normalizeAll(s.Enum)
	s.ItemEnum = normalizeAll(s.ItemEnum)
	if msg := s.validate(s.Default); msg != "" {
		return errors.New("SettingDefaultInvalid", "the default of the setting is not a valid value: "+msg, "key", s.Key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.settings[s.Key]; ok {
		return errors.New("SettingDuplicate", "the setting is already registered", "key", s.Key)
	}
	r.settings[s.Key] = &s
	r.order = append(r.order, s.Key)
	r.version++
	return nil
}

// Unregister removes a setting and reports whether it was registered. It is how the settings of an extension go when the
// extension is deactivated (SPEC §10.15); the settings of the editor are never removed. A value the user wrote for the key
// stays in their file.
func (r *Registry) Unregister(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.settings[key]; !ok {
		return false
	}
	delete(r.settings, key)
	r.order = slices.DeleteFunc(r.order, func(k string) bool { return k == key })
	r.version++
	return true
}

// MustRegister is Register for settings written in the code: it panics on an error.
func (r *Registry) MustRegister(settings ...Setting) {
	for _, s := range settings {
		if err := r.Register(s); err != nil {
			panic(err)
		}
	}
}

// RegisterLanguageDefaults sets the defaults a language has for settings (what a language
// contributes with configurationDefaults, such as "editor.insertSpaces": false for Go).
// Calling it again for a language merges the values, the later winning.
func (r *Registry) RegisterLanguageDefaults(languageID string, values map[string]any) error {
	if strings.TrimSpace(languageID) == "" || strings.ContainsAny(languageID, "[]") {
		return errors.New("SettingLanguageInvalid", "the language id is empty or has brackets", "language", languageID)
	}
	norm := map[string]any{}
	for k, v := range values {
		nv, err := normalize(v)
		if err != nil {
			return err
		}
		norm[k] = nv
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.languageDefaults[languageID]; !ok {
		r.languageDefaults[languageID] = map[string]any{}
		r.languageOrder = append(r.languageOrder, languageID)
	}
	for k, v := range norm {
		r.languageDefaults[languageID][k] = v
	}
	r.version++
	return nil
}

// UnregisterLanguageDefaults takes back the defaults that RegisterLanguageDefaults gave a language for the keys, and
// reports whether there was anything to take. It is how the defaults of a language pack go when the pack is turned off
// (SPEC §10.15). A value the user wrote stays in their file. A language with no default left is forgotten.
func (r *Registry) UnregisterLanguageDefaults(languageID string, keys []string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	values, ok := r.languageDefaults[languageID]
	if !ok {
		return false
	}
	removed := false
	for _, k := range keys {
		if _, has := values[k]; has {
			delete(values, k)
			removed = true
		}
	}
	if len(values) == 0 {
		delete(r.languageDefaults, languageID)
		r.languageOrder = slices.DeleteFunc(r.languageOrder, func(id string) bool { return id == languageID })
	}
	if removed {
		r.version++
	}
	return removed
}

// Lookup returns the setting with the key.
func (r *Registry) Lookup(key string) (Setting, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.settings[key]
	if !ok {
		return Setting{}, false
	}
	return *s, true
}

// All returns the settings in the order they were registered.
func (r *Registry) All() []Setting {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Setting, len(r.order))
	for i, k := range r.order {
		out[i] = *r.settings[k]
	}
	return out
}

// Categories returns the categories of the settings, sorted.
func (r *Registry) Categories() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]bool{}
	var out []string
	for _, s := range r.settings {
		if s.Category != "" && !seen[s.Category] {
			seen[s.Category] = true
			out = append(out, s.Category)
		}
	}
	sort.Strings(out)
	return out
}

// Version changes whenever something is registered.
func (r *Registry) Version() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.version
}

// Validate tells whether value is allowed for the setting with the key; the message
// says why not, and is empty for a valid value. A key that is not registered has no
// rules.
func (r *Registry) Validate(key string, value any) string {
	s, ok := r.Lookup(key)
	if !ok {
		return ""
	}
	v, err := normalize(value)
	if err != nil {
		return err.Error()
	}
	return s.validate(v)
}

// normalize turns a Go value into what the parser gives: nil, bool, float64, string,
// []any and map[string]any.
func normalize(v any) (any, error) {
	switch x := v.(type) {
	case nil, bool, float64, string:
		return v, nil
	case int:
		return float64(x), nil
	case int64:
		return float64(x), nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, errors.NewErr("SettingValueNotJSON", err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.NewErr("SettingValueNotJSON", err)
	}
	return out, nil
}

func normalizeAll(list []any) []any {
	out := make([]any, len(list))
	for i, v := range list {
		nv, err := normalize(v)
		if err != nil {
			nv = v
		}
		out[i] = nv
	}
	return out
}

func typeOf(v any) Type {
	switch v.(type) {
	case nil:
		return Null
	case bool:
		return Boolean
	case float64:
		return Number
	case string:
		return String
	case []any:
		return Array
	case map[string]any:
		return Object
	}
	return ""
}

func hasType(types []Type, t Type) bool {
	for _, x := range types {
		if x == t || (t == Integer && x == Number) {
			return true
		}
	}
	return false
}

// typeAllowed tells whether v has one of the types.
func typeAllowed(types []Type, v any) bool {
	t := typeOf(v)
	if t == Number {
		f := v.(float64)
		if hasType(types, Number) {
			return true
		}
		return hasType(types, Integer) && f == math.Trunc(f) && !math.IsInf(f, 0)
	}
	return hasType(types, t)
}

func inList(list []any, v any) bool {
	for _, x := range list {
		if reflect.DeepEqual(x, v) {
			return true
		}
	}
	return false
}

// validate checks a normalized value against the setting; it returns "" when it is valid.
func (s *Setting) validate(v any) string {
	if !typeAllowed(s.Types, v) {
		return "expected " + typeNames(s.Types)
	}
	if len(s.Enum) > 0 && !inList(s.Enum, v) {
		return "not one of the allowed values"
	}
	if f, ok := v.(float64); ok {
		if s.Minimum != nil && f < *s.Minimum {
			return "less than " + strconv.FormatFloat(*s.Minimum, 'g', -1, 64)
		}
		if s.Maximum != nil && f > *s.Maximum {
			return "more than " + strconv.FormatFloat(*s.Maximum, 'g', -1, 64)
		}
	}
	if list, ok := v.([]any); ok {
		for i, item := range list {
			if len(s.ItemTypes) > 0 && !typeAllowed(s.ItemTypes, item) {
				return "item " + strconv.Itoa(i) + ": expected " + typeNames(s.ItemTypes)
			}
			if len(s.ItemEnum) > 0 && !inList(s.ItemEnum, item) {
				return "item " + strconv.Itoa(i) + " is not one of the allowed values"
			}
		}
	}
	return ""
}

func typeNames(types []Type) string {
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = string(t)
	}
	return strings.Join(names, " or ")
}

// zeroValue is the default of a setting that gives none: what VS Code uses for the type.
func zeroValue(t Type) any {
	switch t {
	case String:
		return ""
	case Number, Integer:
		return 0.0
	case Boolean:
		return false
	case Array:
		return []any{}
	case Object:
		return map[string]any{}
	}
	return nil
}
