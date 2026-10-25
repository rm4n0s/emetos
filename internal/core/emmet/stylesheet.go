package emmet

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Ported from Emmet: src/stylesheet/index.ts and src/index.ts (the stylesheet part)

// gradientName is the abbreviation of a linear gradient.
const gradientName = "lg"

// ExpandStylesheet expands a stylesheet abbreviation and writes it in the syntax and the style of the configuration.
func ExpandStylesheet(abbr string, cfg Config) (string, error) {
	props, err := ParseStylesheet(abbr, cfg)
	if err != nil {
		return "", err
	}
	return StringifyStylesheet(props, cfg), nil
}

// ParseStylesheet reads a stylesheet abbreviation and does what makes it ready to be written: the
// properties are found with a fuzzy match, the keywords of their values and the numbers are resolved.
func ParseStylesheet(abbr string, cfg Config) ([]*CSSProperty, error) {
	snippets := convertSnippets(cfg.Snippets, &cfg)
	props, err := ParseCSS(abbr, isValueScope(&cfg))
	if err != nil {
		return nil, err
	}
	return resolveProperties(props, snippets, &cfg), nil
}

// ResolveStylesheet does the second part of ParseStylesheet for properties that are parsed already.
func ResolveStylesheet(props []*CSSProperty, cfg Config) []*CSSProperty {
	return resolveProperties(props, convertSnippets(cfg.Snippets, &cfg), &cfg)
}

func resolveProperties(props []*CSSProperty, snippets []*cssSnippet, cfg *Config) []*CSSProperty {
	filtered := snippetsForScope(snippets, cfg)
	result := make([]*CSSProperty, 0, len(props))
	for _, node := range props {
		if resolved := resolveNode(node, filtered, cfg); resolved != nil {
			result = append(result, resolved)
		}
	}
	return result
}

// convertSnippets makes the structure of the snippets of the configuration, sorted by name. A
// snippet that does not parse is left out and Config.Warn is told.
func convertSnippets(snippets map[string]string, cfg *Config) []*cssSnippet {
	result := make([]*cssSnippet, 0, len(snippets))
	for _, key := range slices.Sorted(maps.Keys(snippets)) {
		sn, err := createSnippet(key, snippets[key])
		if err != nil {
			cfg.warn("Unable to parse \""+snippets[key]+"\" snippet", err)
			continue
		}
		result = append(result, sn)
	}
	return nest(result)
}

// resolveNode finds the snippet of a property with a fuzzy match and resolves the keywords of its
// value. It gives nil for a property to leave out.
func resolveNode(node *CSSProperty, snippets []*cssSnippet, cfg *Config) *CSSProperty {
	if !resolveGradient(node, cfg) {
		score := cfg.Options.StylesheetFuzzySearchMinScore
		if isValueScope(cfg) {
			// The abbreviation is the value of the property of the context
			propName := cfg.Context.Name
			var snippet *cssSnippet
			for _, s := range snippets {
				if s.kind == snippetProperty && s.property == propName {
					snippet = s
					break
				}
			}
			resolveValueKeywords(node, cfg, snippet, score)
			node.snippet = snippet
		} else if node.Name != "" {
			snippet := findBestSnippet(node.Name, snippets, score)
			node.snippet = snippet

			if snippet != nil {
				var resolved *CSSProperty
				if snippet.kind == snippetProperty {
					resolved = resolveAsProperty(node, snippet, cfg)
				} else {
					resolved = resolveAsSnippet(node, snippet)
				}
				if resolved != nil {
					node = resolved
				} else if cfg.Options.StylesheetStrictMatch {
					return nil
				}
			}
		}
	}

	if node.Name != "" || cfg.Context != nil {
		// The numbers of the properties only
		resolveNumericValue(node, cfg)
	}
	return node
}

// resolveGradient resolves the shortcut of a CSS gradient, if the property has it.
func resolveGradient(node *CSSProperty, cfg *Config) bool {
	var gradientFn *CSSToken
	if len(node.Value) == 1 && len(node.Value[0].Value) == 1 {
		if v := node.Value[0].Value[0]; v.Type == CSSFunction && v.Name == gradientName {
			gradientFn = v
		}
	}

	if gradientFn == nil && node.Name != gradientName {
		return false
	}
	if gradientFn == nil {
		gradientFn = &CSSToken{Type: CSSFunction, Name: "linear-gradient", Arguments: []*CSSValue{cssValue(cssFieldToken(0, ""))}, noPos: true}
	} else {
		fn := *gradientFn
		fn.Name = "linear-gradient"
		gradientFn = &fn
	}
	if cfg.Context == nil {
		node.Name = "background-image"
	}
	node.Value = []*CSSValue{cssValue(gradientFn)}
	return true
}

// resolveAsProperty resolves a property that matched the snippet of a property. It gives nil if
// the snippet is not what the abbreviation means.
func resolveAsProperty(node *CSSProperty, snippet *cssSnippet, cfg *Config) *CSSProperty {
	abbr := node.Name

	// The part of the abbreviation that did not match. In `dib` the matched part is `d` and `ib`
	// is a value. If there is such a part, it has to match a value of the snippet. If a value is
	// given or the part is no keyword, the snippet that matched is not the right one
	if inlineValue := unmatchedPart(abbr, snippet.key); inlineValue != "" {
		if len(node.Value) > 0 {
			return nil
		}
		kw := resolveKeyword(inlineValue, cfg, snippet, 0)
		if kw == nil {
			return nil
		}
		node.Value = append(node.Value, cssValue(kw))
	}

	node.Name = snippet.property

	if len(node.Value) > 0 {
		// The keyword of the abbreviation is replaced with the keyword it matched
		resolveValueKeywords(node, cfg, snippet, 0)
	} else if len(snippet.values) > 0 {
		defaultValue := snippet.values[0]

		// Select the value that is inserted only if there is more than one choice (emmet issue 558)
		if len(snippet.values) == 1 || slices.ContainsFunc(defaultValue, cssHasField) {
			node.Value = defaultValue
		} else {
			node.Value = make([]*CSSValue, len(defaultValue))
			for i, n := range defaultValue {
				node.Value[i] = wrapWithField(n, cfg, &wrapState{index: 1})
			}
		}
	}
	return node
}

func resolveValueKeywords(node *CSSProperty, cfg *Config, snippet *cssSnippet, minScore float64) {
	for _, cssVal := range node.Value {
		value := make([]*CSSToken, 0, len(cssVal.Value))
		for _, tok := range cssVal.Value {
			switch tok.Type {
			case CSSLiteral:
				if kw := resolveKeyword(tok.Value, cfg, snippet, minScore); kw != nil {
					value = append(value, kw)
				} else {
					value = append(value, tok)
				}
			case CSSFunction:
				// The matching function call is found and the arguments are merged
				match := resolveKeyword(tok.Name, cfg, snippet, minScore)
				if match != nil && match.Type == CSSFunction {
					merged := *match
					merged.Arguments = slices.Clone(tok.Arguments)
					if len(match.Arguments) > len(tok.Arguments) {
						merged.Arguments = append(merged.Arguments, match.Arguments[len(tok.Arguments):]...)
					}
					value = append(value, &merged)
				} else {
					value = append(value, tok)
				}
			default:
				value = append(value, tok)
			}
		}
		cssVal.Value = value
	}
}

var cssFieldRe = regexp.MustCompile(`\$\{(\d+)(:[^}]+)?\}`)

// resolveAsSnippet resolves a property as a snippet: a plain piece of code. The placeholders of the
// snippet become fields, or take the values the user gave.
func resolveAsSnippet(node *CSSProperty, snippet *cssSnippet) *CSSProperty {
	offset := 0
	var inputValue *CSSValue
	if len(node.Value) > 0 {
		inputValue = node.Value[0]
	}
	var output []*CSSToken

	for _, m := range cssFieldRe.FindAllStringSubmatchIndex(snippet.value, -1) {
		if offset != m[0] {
			output = append(output, cssLiteralToken(snippet.value[offset:m[0]]))
		}
		offset = m[1]
		if inputValue != nil && len(inputValue.Value) > 0 {
			output = append(output, inputValue.Value[0])
			inputValue.Value = inputValue.Value[1:]
		} else {
			name := ""
			if m[4] >= 0 {
				name = snippet.value[m[4]+1 : m[5]]
			}
			index := parseCount(snippet.value[m[2]:m[3]])
			output = append(output, cssFieldToken(index, name))
		}
	}

	if tail := snippet.value[offset:]; tail != "" {
		output = append(output, cssLiteralToken(tail))
	}

	node.Name = ""
	node.Value = []*CSSValue{cssValue(output...)}
	return node
}

// findBestSnippet finds the snippet that is the best match of the abbreviation; nil if none is good enough.
func findBestSnippet(abbr string, items []*cssSnippet, minScore float64) *cssSnippet {
	i := findBestMatch(abbr, len(items), func(i int) string { return items[i].key }, minScore, true)
	if i < 0 {
		return nil
	}
	return items[i]
}

// findBestMatch gives the index of the best matching of n items (0 if none matches well enough):
// the item with a score of 1 at once, or the last item with the highest score if it is at
// least minScore.
func findBestMatch(abbr string, n int, part func(int) string, minScore float64, partialMatch bool) int {
	matched := -1
	maxScore := 0.0

	for i := 0; i < n; i++ {
		score := scoreMatch(abbr, part(i), partialMatch)
		if score == 1 {
			// A direct hit: no need to look further
			return i
		}
		if score != 0 && score >= maxScore {
			maxScore = score
			matched = i
		}
	}
	if maxScore >= minScore {
		return matched
	}
	return -1
}

// unmatchedPart gives the part of the abbreviation that was not found in str: for `poas` against
// `position` it is `as`, as the `a` is not found after the `o`.
func unmatchedPart(abbr, str string) string {
	lastPos := 0
	for i := 0; i < len(abbr); i++ {
		idx := strings.IndexByte(str[lastPos:], abbr[i])
		if idx < 0 {
			return abbr[i:]
		}
		lastPos += idx + 1
	}
	return ""
}

// resolveKeyword finds the keyword that a shorthand stands for, in the snippet, in the properties
// it contains, and in the global keywords. It gives nil if there is none.
func resolveKeyword(kw string, cfg *Config, snippet *cssSnippet, minScore float64) *CSSToken {
	if snippet != nil {
		if tok := bestKeyword(kw, snippet.keywords, minScore); tok != nil {
			return tok
		}
		for _, dep := range snippet.dependencies {
			if tok := bestKeyword(kw, dep.keywords, minScore); tok != nil {
				return tok
			}
		}
	}

	keywords := cfg.Options.StylesheetKeywords
	if i := findBestMatch(kw, len(keywords), func(i int) string { return keywords[i] }, minScore, false); i >= 0 {
		return cssLiteralToken(keywords[i])
	}
	return nil
}

func bestKeyword(kw string, keywords *keywordMap, minScore float64) *CSSToken {
	keys := keywords.keys()
	if i := findBestMatch(kw, len(keys), func(i int) string { return keys[i] }, minScore, false); i >= 0 {
		return keywords.m[keys[i]]
	}
	return nil
}

// resolveNumericValue gives the numbers of the property their units.
func resolveNumericValue(node *CSSProperty, cfg *Config) {
	aliases := cfg.Options.StylesheetUnitAliases
	unitless := cfg.Options.StylesheetUnitless

	for _, v := range node.Value {
		for _, t := range v.Value {
			if t.Type != CSSNumber {
				continue
			}
			switch {
			case t.Unit != "":
				if alias, ok := aliases[t.Unit]; ok && alias != "" {
					t.Unit = alias
				}
			case t.Number != 0 && !slices.Contains(unitless, node.Name):
				if strings.Contains(t.RawValue, ".") {
					t.Unit = cfg.Options.StylesheetFloatUnit
				} else {
					t.Unit = cfg.Options.StylesheetIntUnit
				}
			}
		}
	}
}

func cssValue(tokens ...*CSSToken) *CSSValue { return &CSSValue{Value: tokens} }

func cssLiteralToken(value string) *CSSToken {
	return &CSSToken{Type: CSSLiteral, Value: value, noPos: true}
}

func cssFieldToken(index int, name string) *CSSToken {
	return &CSSToken{Type: CSSField, Index: index, HasIndex: true, Name: name, noPos: true}
}

// cssHasField tells if the value has a field.
func cssHasField(v *CSSValue) bool {
	for _, t := range v.Value {
		if t.Type == CSSField || (t.Type == CSSFunction && slices.ContainsFunc(t.Arguments, cssHasField)) {
			return true
		}
	}
	return false
}

// wrapState numbers the fields that wrapWithField makes.
type wrapState struct{ index int }

// wrapWithField makes a field of every token of the value.
func wrapWithField(node *CSSValue, cfg *Config, st *wrapState) *CSSValue {
	var value []*CSSToken
	next := func(name string) *CSSToken {
		f := cssFieldToken(st.index, name)
		st.index++
		return f
	}
	for _, v := range node.Value {
		switch v.Type {
		case CSSColor:
			value = append(value, next(cssColor(v, cfg.Options.StylesheetShortHex)))
		case CSSLiteral:
			value = append(value, next(v.Value))
		case CSSNumber:
			value = append(value, next(jsNumber(v.Number)+v.Unit))
		case CSSString:
			q := `"`
			if v.Single {
				q = "'"
			}
			value = append(value, next(q+v.Value+q))
		case CSSFunction:
			value = append(value, next(v.Name), cssLiteralToken("("))
			for i, arg := range v.Arguments {
				value = append(value, wrapWithField(arg, cfg, st).Value...)
				if i != len(v.Arguments)-1 {
					value = append(value, cssLiteralToken(", "))
				}
			}
			value = append(value, cssLiteralToken(")"))
		default:
			value = append(value, v)
		}
	}
	return &CSSValue{Value: value}
}

// isValueScope tells if the abbreviation is expanded as the value of a property.
func isValueScope(cfg *Config) bool {
	if cfg.Context != nil {
		return cfg.Context.Name == ScopeValue || !strings.HasPrefix(cfg.Context.Name, "@@")
	}
	return false
}

// snippetsForScope gives the snippets that the scope of the context lets match.
func snippetsForScope(snippets []*cssSnippet, cfg *Config) []*cssSnippet {
	if cfg.Context != nil {
		var kind cssSnippetKind
		switch cfg.Context.Name {
		case ScopeSection:
			kind = snippetRaw
		case ScopeProperty:
			kind = snippetProperty
		default:
			return snippets
		}
		var out []*cssSnippet
		for _, s := range snippets {
			if s.kind == kind {
				out = append(out, s)
			}
		}
		return out
	}
	return snippets
}
