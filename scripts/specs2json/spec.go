package main

import (
	"bytes"
	"encoding/json"
	"slices"
)

// The JSON form of a completion spec (SPEC §10.25 "Terminal IntelliSense"): the data of a Fig spec that Emetos uses, with the
// fields that are code (generators, post-processing, spec loaders) and the ones it does not use left out. extensions/terminal
// reads this form (suggest_spec.go).

// Names is one name or several (an option's long and short names); the JSON is a string when there is one.
type Names []string

// MarshalJSON writes one name as a string and several as an array.
func (n Names) MarshalJSON() ([]byte, error) {
	if len(n) == 1 {
		return json.Marshal(n[0])
	}
	return json.Marshal([]string(n))
}

// UnmarshalJSON reads a string or an array of strings (the test of the round trip, and any tool that reads the files).
func (n *Names) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*n = Names{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*n = many
	return nil
}

// Spec is a command or a subcommand.
type Spec struct {
	Name        Names     `json:"name"`
	Description string    `json:"description,omitempty"`
	Subcommands []*Spec   `json:"subcommands,omitempty"`
	Options     []*Option `json:"options,omitempty"`
	Args        []*Arg    `json:"args,omitempty"`
	Hidden      bool      `json:"hidden,omitempty"`
	Deprecated  bool      `json:"deprecated,omitempty"`
}

// Option is a flag of a command.
type Option struct {
	Name              Names  `json:"name"`
	Description       string `json:"description,omitempty"`
	Args              []*Arg `json:"args,omitempty"`
	Persistent        bool   `json:"persistent,omitempty"`
	Repeatable        bool   `json:"repeatable,omitempty"`
	RequiresSeparator bool   `json:"requiresSeparator,omitempty"`
	Hidden            bool   `json:"hidden,omitempty"`
	Deprecated        bool   `json:"deprecated,omitempty"`
}

// Arg is an argument of a command or an option.
type Arg struct {
	Name        string       `json:"name,omitempty"`
	Description string       `json:"description,omitempty"`
	Optional    bool         `json:"optional,omitempty"`
	Variadic    bool         `json:"variadic,omitempty"`
	Command     bool         `json:"command,omitempty"`
	Templates   []string     `json:"templates,omitempty"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`
}

// Suggestion is a value an argument may take.
type Suggestion struct {
	Name        Names  `json:"name"`
	Description string `json:"description,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
}

// specOf converts a parsed value to a spec; nil when it is not one.
func specOf(v any) *Spec {
	o, ok := v.(*object)
	if !ok {
		return nil
	}
	names := namesOf(o)
	if len(names) == 0 {
		return nil
	}
	s := &Spec{Name: names, Description: stringOf(o, "description"), Hidden: boolOf(o, "hidden"), Deprecated: deprecatedOf(o)}
	for _, x := range arrayOf(o, "subcommands") {
		if sub := specOf(x); sub != nil {
			s.Subcommands = append(s.Subcommands, sub)
		}
	}
	for _, x := range arrayOf(o, "options") {
		if opt := optionOf(x); opt != nil {
			s.Options = append(s.Options, opt)
		}
	}
	s.Args = argsOf(o)
	return s
}

func optionOf(v any) *Option {
	o, ok := v.(*object)
	if !ok {
		return nil
	}
	names := namesOf(o)
	if len(names) == 0 {
		return nil
	}
	opt := &Option{
		Name: names, Description: stringOf(o, "description"), Args: argsOf(o), Persistent: boolOf(o, "isPersistent"),
		Hidden: boolOf(o, "hidden"), Deprecated: deprecatedOf(o),
	}
	switch r := o.vals["isRepeatable"].(type) {
	case bool:
		opt.Repeatable = r
	case float64:
		opt.Repeatable = r != 1
	}
	switch r := o.vals["requiresSeparator"].(type) {
	case bool:
		opt.RequiresSeparator = r
	case string:
		opt.RequiresSeparator = r != ""
	}
	return opt
}

// argsOf reads the args of a spec or an option: one arg or an array of them.
func argsOf(o *object) []*Arg {
	v, ok := o.get("args")
	if !ok {
		return nil
	}
	var list []any
	switch a := v.(type) {
	case []any:
		list = a
	case *object:
		list = []any{a}
	}
	var out []*Arg
	for _, x := range list {
		if ao, ok := x.(*object); ok {
			out = append(out, argOf(ao))
		}
	}
	return out
}

func argOf(o *object) *Arg {
	a := &Arg{
		Name: stringOf(o, "name"), Description: stringOf(o, "description"), Optional: boolOf(o, "isOptional"),
		Variadic: boolOf(o, "isVariadic"), Command: boolOf(o, "isCommand"),
	}
	a.Templates = templatesOf(o.vals["template"])
	for _, g := range generatorsOf(o) {
		for _, t := range templatesOf(g.vals["template"]) {
			if !slices.Contains(a.Templates, t) {
				a.Templates = append(a.Templates, t)
			}
		}
	}
	for _, x := range arrayOf(o, "suggestions") {
		switch s := x.(type) {
		case string:
			a.Suggestions = append(a.Suggestions, Suggestion{Name: Names{s}})
		case *object:
			if names := namesOf(s); len(names) > 0 {
				a.Suggestions = append(a.Suggestions, Suggestion{Name: names, Description: stringOf(s, "description"), Hidden: boolOf(s, "hidden")})
			}
		}
	}
	return a
}

// generatorsOf lists the generators of an arg that are objects; their code is not kept, only their template.
func generatorsOf(o *object) []*object {
	var out []*object
	switch g := o.vals["generators"].(type) {
	case *object:
		out = append(out, g)
	case []any:
		for _, x := range g {
			if go1, ok := x.(*object); ok {
				out = append(out, go1)
			}
		}
	}
	return out
}

// templatesOf reads a template: one name or an array.
func templatesOf(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func namesOf(o *object) Names {
	switch n := o.vals["name"].(type) {
	case string:
		if n != "" {
			return Names{n}
		}
	case []any:
		var out Names
		for _, x := range n {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func stringOf(o *object, k string) string {
	s, _ := o.vals[k].(string)
	return s
}

func boolOf(o *object, k string) bool {
	b, _ := o.vals[k].(bool)
	return b
}

// deprecatedOf is whether a spec is deprecated: the flag is true, or a message.
func deprecatedOf(o *object) bool {
	switch d := o.vals["deprecated"].(type) {
	case bool:
		return d
	case string:
		return d != ""
	case *object:
		return true
	}
	return false
}

func arrayOf(o *object, k string) []any {
	a, _ := o.vals[k].([]any)
	return a
}

// encode writes a spec as compact JSON, without escaping the characters HTML cares about.
func encode(s *Spec) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
