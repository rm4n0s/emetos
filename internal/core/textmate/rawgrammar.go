package textmate

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/rm4n0s/errors"
)

// RawGrammar is a TextMate grammar as it is written in a file (SPEC §8.3). It is read-only once
// it is loaded: grammars are shared by every document that uses them.
type RawGrammar struct {
	// ScopeName is the scope of the grammar, such as source.go.
	ScopeName string
	// Name is the display name.
	Name string
	// FileTypes are the extensions the grammar is for.
	FileTypes []string
	// FirstLineMatch is a regular expression for the first line of the files of the grammar.
	FirstLineMatch string
	// InjectionSelector, for a grammar that is an injection, says where it is injected.
	InjectionSelector string
	// Patterns are the rules of the top level.
	Patterns []*RawRule
	// Repository holds the named rules that patterns include with #name.
	Repository map[string]*RawRule
	// Injections are the rules the grammar injects into the scopes that match their selectors,
	// in the order of the file.
	Injections []RawInjection
}

// RawInjection is a rule of the injections of a grammar and the selector for it.
type RawInjection struct {
	// Selector says where the rule applies, such as "L:source.js -comment".
	Selector string
	// Rule is the rule.
	Rule *RawRule
}

// RawRule is a rule of a grammar as it is written in a file.
type RawRule struct {
	// Include names another rule: $self, $base, #name, scope.name or scope.name#name.
	Include string
	// Name is the scope of what the rule matches.
	Name string
	// ContentName is the scope of what is between begin and end.
	ContentName string
	// Match is the regular expression of a rule that matches a single span.
	Match string
	// HasBegin tells whether the rule has a begin (even an empty one), which makes it a rule with
	// a beginning and an end.
	HasBegin bool
	// Begin starts a rule that spans text.
	Begin string
	// End finishes a rule that began with Begin; it may refer to the captures of Begin as \1.
	End string
	// While keeps a rule that began with Begin going for as long as it matches at the start of
	// each line.
	While string
	// Captures name the scopes of the groups of Match (or of Begin and End when the specific
	// ones are not there), by group number.
	Captures map[string]*RawRule
	// BeginCaptures are the captures of Begin.
	BeginCaptures map[string]*RawRule
	// EndCaptures are the captures of End.
	EndCaptures map[string]*RawRule
	// WhileCaptures are the captures of While.
	WhileCaptures map[string]*RawRule
	// Patterns are the rules that apply between Begin and End.
	Patterns []*RawRule
	// HasPatterns tells whether the file has patterns (an empty list as well).
	HasPatterns bool
	// Repository holds the rules the patterns of an include-only rule can include with #name.
	Repository map[string]*RawRule
	// ApplyEndPatternLast makes End the last pattern tried and not the first.
	ApplyEndPatternLast bool
}

// ParseGrammar reads a grammar from JSON. The error has the tag GrammarInvalid.
func ParseGrammar(data []byte) (*RawGrammar, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, errors.NewErr("GrammarInvalid", err).SetMetadata("reason", "not a JSON object")
	}
	g := &RawGrammar{}
	var err error
	for key, raw := range top {
		switch key {
		case "scopeName":
			g.ScopeName, err = parseString(key, raw)
		case "name":
			g.Name, err = parseString(key, raw)
		case "firstLineMatch":
			g.FirstLineMatch, err = parseString(key, raw)
		case "injectionSelector":
			g.InjectionSelector, err = parseString(key, raw)
		case "fileTypes":
			err = parseStrings(key, raw, &g.FileTypes)
		case "patterns":
			g.Patterns, err = parseRules(key, raw)
		case "repository":
			g.Repository, err = parseRuleMap(key, raw)
		case "injections":
			g.Injections, err = parseInjections(raw)
		}
		if err != nil {
			return nil, err
		}
	}
	if g.ScopeName == "" {
		return nil, errors.New("GrammarInvalid", "the grammar has no scopeName")
	}
	return g, nil
}

// parseString reads a JSON string. A null is the empty string.
func parseString(key string, raw json.RawMessage) (string, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", errors.NewErr("GrammarInvalid", err).SetMetadata("key", key, "reason", "not a string")
	}
	return s, nil
}

// parseStrings reads a JSON array of strings.
func parseStrings(key string, raw json.RawMessage, out *[]string) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.NewErr("GrammarInvalid", err).SetMetadata("key", key, "reason", "not an array of strings")
	}
	return nil
}

// parseRules reads a JSON array of rules.
func parseRules(key string, raw json.RawMessage) ([]*RawRule, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, errors.NewErr("GrammarInvalid", err).SetMetadata("key", key, "reason", "not an array")
	}
	rules := make([]*RawRule, 0, len(items))
	for _, item := range items {
		r, err := parseRule(key, item)
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// parseRuleMap reads a JSON object of rules.
func parseRuleMap(key string, raw json.RawMessage) (map[string]*RawRule, error) {
	var items map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, errors.NewErr("GrammarInvalid", err).SetMetadata("key", key, "reason", "not an object")
	}
	rules := make(map[string]*RawRule, len(items))
	for name, item := range items {
		r, err := parseRule(key+"."+name, item)
		if err != nil {
			return nil, err
		}
		rules[name] = r
	}
	return rules, nil
}

// parseInjections reads the injections object, keeping the order of its keys, which is the
// order the injections are tried in.
func parseInjections(raw json.RawMessage) ([]RawInjection, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("GrammarInvalid", "injections is not an object", "key", "injections")
	}
	var out []RawInjection
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, errors.NewErr("GrammarInvalid", err).SetMetadata("key", "injections")
		}
		selector, _ := t.(string)
		var item json.RawMessage
		if err := dec.Decode(&item); err != nil {
			return nil, errors.NewErr("GrammarInvalid", err).SetMetadata("key", "injections")
		}
		r, err := parseRule("injections."+selector, item)
		if err != nil {
			return nil, err
		}
		out = append(out, RawInjection{Selector: selector, Rule: r})
	}
	return out, nil
}

// parseRule reads a JSON object as a rule.
func parseRule(key string, raw json.RawMessage) (*RawRule, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, errors.NewErr("GrammarInvalid", err).SetMetadata("key", key, "reason", "a rule is not an object")
	}
	r := &RawRule{}
	var err error
	for k, v := range fields {
		path := key + "." + k
		switch k {
		case "include":
			r.Include, err = parseString(path, v)
		case "name":
			r.Name, err = parseString(path, v)
		case "contentName":
			r.ContentName, err = parseString(path, v)
		case "match":
			r.Match, err = parseString(path, v)
		case "begin":
			r.HasBegin = !bytes.Equal(bytes.TrimSpace(v), []byte("null"))
			r.Begin, err = parseString(path, v)
		case "end":
			r.End, err = parseString(path, v)
		case "while":
			r.While, err = parseString(path, v)
		case "captures":
			r.Captures, err = parseRuleMap(path, v)
		case "beginCaptures":
			r.BeginCaptures, err = parseRuleMap(path, v)
		case "endCaptures":
			r.EndCaptures, err = parseRuleMap(path, v)
		case "whileCaptures":
			r.WhileCaptures, err = parseRuleMap(path, v)
		case "patterns":
			r.HasPatterns = !bytes.Equal(bytes.TrimSpace(v), []byte("null"))
			r.Patterns, err = parseRules(path, v)
		case "repository":
			r.Repository, err = parseRuleMap(path, v)
		case "applyEndPatternLast":
			r.ApplyEndPatternLast, err = parseBool(path, v)
		}
		if err != nil {
			return nil, err
		}
	}
	return r, nil
}

// parseBool reads a JSON boolean; the numbers 1 and 0 are accepted as grammars written for
// JavaScript use them.
func parseBool(key string, raw json.RawMessage) (bool, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n != 0, nil
	}
	return false, errors.New("GrammarInvalid", "not a boolean", "key", key, "value", strconv.Quote(string(raw)))
}
