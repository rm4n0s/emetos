package emmet

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// Ported from Emmet: src/stylesheet/snippets.ts

// cssSnippetKind tells what a snippet of a stylesheet is.
type cssSnippetKind int

const (
	// snippetRaw is a plain piece of text, like "@media ..." or a block of rules.
	snippetRaw cssSnippetKind = iota + 1
	// snippetProperty is a CSS property with the values it may have.
	snippetProperty
)

// keywordMap holds the keywords of a property by name, in the order a JavaScript object has: keys
// that are array indexes (whole numbers like 100) first in numeric order, then the others in the
// order they were added. The order matters for the fuzzy match, which prefers the last of equal scores.
type keywordMap struct {
	m     map[string]*CSSToken
	order []string
}

func newKeywordMap() *keywordMap { return &keywordMap{m: map[string]*CSSToken{}} }

func (k *keywordMap) set(name string, tok *CSSToken) {
	if _, ok := k.m[name]; !ok {
		k.order = append(k.order, name)
	}
	k.m[name] = tok
}

// keys gives the names as Object.keys does.
func (k *keywordMap) keys() []string {
	var index, other []string
	for _, name := range k.order {
		if isArrayIndex(name) {
			index = append(index, name)
		} else {
			other = append(other, name)
		}
	}
	sort.Slice(index, func(i, j int) bool {
		a, _ := strconv.ParseUint(index[i], 10, 64)
		b, _ := strconv.ParseUint(index[j], 10, 64)
		return a < b
	})
	return append(index, other...)
}

// isArrayIndex tells if a JavaScript object orders the key as a number: a whole number with no
// leading zero, below 2^32 - 1.
func isArrayIndex(s string) bool {
	if s == "" || len(s) > 10 || (len(s) > 1 && s[0] == '0') {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return err == nil && n < 1<<32-1
}

// cssSnippet is a snippet of a stylesheet: a raw one or a property.
type cssSnippet struct {
	kind cssSnippetKind
	key  string
	// value is the text of a raw snippet.
	value string

	// property is the name of the property of a property snippet.
	property string
	// values are the values it may have (the choices are separated by `|`).
	values [][]*CSSValue
	// keywords are the keywords of the values.
	keywords *keywordMap
	// dependencies are the more specific properties (background-position for background).
	dependencies []*cssSnippet
}

var cssPropertyRe = regexp.MustCompile(`^([a-z-]+)(?:\s*:\s*([^\n\r;]+?);*)?$`)

// createSnippet makes the structure of a snippet. A snippet is raw text, or a CSS property with
// values, separated by `|`: the values are parsed as an abbreviation.
func createSnippet(key, value string) (*cssSnippet, error) {
	m := cssPropertyRe.FindStringSubmatch(value)
	if m == nil {
		return &cssSnippet{kind: snippetRaw, key: key, value: value}, nil
	}
	sn := &cssSnippet{kind: snippetProperty, key: key, property: m[1], keywords: newKeywordMap()}
	if m[2] != "" {
		for _, part := range strings.Split(m[2], "|") {
			parsed, err := parseSnippetValue(part)
			if err != nil {
				return nil, err
			}
			sn.values = append(sn.values, parsed)
		}
	}
	for _, item := range sn.values {
		for _, v := range item {
			collectKeywords(v, sn.keywords)
		}
	}
	return sn, nil
}

// nest puts the more specific properties into the shorthand ones: background-position-x into
// background-position into background. It gives the snippets sorted by key.
func nest(snippets []*cssSnippet) []*cssSnippet {
	snippets = slices.Clone(snippets)
	slices.SortStableFunc(snippets, func(a, b *cssSnippet) int { return strings.Compare(a.key, b.key) })
	var stack []*cssSnippet

	// For the sorted properties, a graph where each shorthand has its specific ones:
	// background -> background-position -> background-position-x
	for _, cur := range snippets {
		if cur.kind != snippetProperty {
			continue
		}
		// Does the property belong to one in the stack? The list is sorted, so the items are
		// aligned from the shorthands to the specific ones
		for len(stack) > 0 {
			prev := stack[len(stack)-1]
			if strings.HasPrefix(cur.property, prev.property) && len(cur.property) > len(prev.property) && cur.property[len(prev.property)] == '-' {
				prev.dependencies = append(prev.dependencies, cur)
				stack = append(stack, cur)
				break
			}
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			stack = append(stack, cur)
		}
	}
	return snippets
}

// parseSnippetValue reads the values of a property snippet.
func parseSnippetValue(value string) ([]*CSSValue, error) {
	props, err := ParseCSS(strings.TrimSpace(value), true)
	if err != nil {
		return nil, err
	}
	if len(props) == 0 {
		return nil, errors.New("CSSSnippetValueEmpty", "the value of a property snippet must have a value", "value", value)
	}
	return props[0].Value, nil
}

func collectKeywords(cssVal *CSSValue, dest *keywordMap) {
	for _, v := range cssVal.Value {
		switch v.Type {
		case CSSLiteral:
			dest.set(v.Value, v)
		case CSSFunction:
			dest.set(v.Name, v)
		case CSSField:
			// A literal made of the field
			if name := strings.TrimSpace(v.Name); name != "" {
				dest.set(name, cssLiteralToken(name))
			}
		}
	}
}
