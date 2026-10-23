package regex

import (
	"strings"
	"unicode"
)

// categoryNames maps the normalized names of Unicode general categories (long and short) to
// regexp2's short names.
var categoryNames = map[string]string{
	"l": "L", "letter": "L",
	"lu": "Lu", "uppercaseletter": "Lu",
	"ll": "Ll", "lowercaseletter": "Ll",
	"lt": "Lt", "titlecaseletter": "Lt",
	"lm": "Lm", "modifierletter": "Lm",
	"lo": "Lo", "otherletter": "Lo",
	"m": "M", "mark": "M", "combiningmark": "M",
	"mn": "Mn", "nonspacingmark": "Mn",
	"mc": "Mc", "spacingmark": "Mc",
	"me": "Me", "enclosingmark": "Me",
	"n": "N", "number": "N",
	"nd": "Nd", "decimalnumber": "Nd",
	"nl": "Nl", "letternumber": "Nl",
	"no": "No", "othernumber": "No",
	"p": "P", "punctuation": "P",
	"pc": "Pc", "connectorpunctuation": "Pc",
	"pd": "Pd", "dashpunctuation": "Pd",
	"ps": "Ps", "openpunctuation": "Ps",
	"pe": "Pe", "closepunctuation": "Pe",
	"pi": "Pi", "initialpunctuation": "Pi",
	"pf": "Pf", "finalpunctuation": "Pf",
	"po": "Po", "otherpunctuation": "Po",
	"s": "S", "symbol": "S",
	"sm": "Sm", "mathsymbol": "Sm",
	"sc": "Sc", "currencysymbol": "Sc",
	"sk": "Sk", "modifiersymbol": "Sk",
	"so": "So", "othersymbol": "So",
	"z": "Z", "separator": "Z",
	"zs": "Zs", "spaceseparator": "Zs",
	"zl": "Zl", "lineseparator": "Zl",
	"zp": "Zp", "paragraphseparator": "Zp",
	"c": "C", "other": "C",
	"cc": "Cc", "control": "Cc",
	"cf": "Cf", "format": "Cf",
	"cs": "Cs", "surrogate": "Cs",
	"co": "Co", "privateuse": "Co",
	"cn": "Cn", "unassigned": "Cn",
}

// scriptTables maps the normalized names of Unicode scripts to their tables.
var scriptTables = buildScriptTables()

// buildScriptTables normalizes the names of the scripts of the unicode package.
func buildScriptTables() map[string]*unicode.RangeTable {
	m := make(map[string]*unicode.RangeTable, len(unicode.Scripts))
	for name, t := range unicode.Scripts {
		m[normalizeName(name)] = t
	}
	return m
}

// normalizeName makes a property name comparable: Oniguruma ignores case, spaces, hyphens and
// underscores in them.
func normalizeName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		switch r {
		case ' ', '-', '_':
		default:
			sb.WriteRune(unicode.ToLower(r))
		}
	}
	return sb.String()
}

// category is the set of a general category in regexp2's syntax.
func category(short string) *charSet {
	s := &charSet{}
	if short == "LC" {
		for _, c := range []string{"Lu", "Ll", "Lt"} {
			s.addItem(`\p{`+c+`}`, `\P{`+c+`}`)
		}
		return s
	}
	s.addItem(`\p{`+short+`}`, `\P{`+short+`}`)
	return s
}

// posixSet is the set of a POSIX bracket name ([:alpha:]) or the property of the same name
// (\p{Alpha}), as Oniguruma has them for Unicode text. The second result is false for a name
// that is not one of them.
func posixSet(name string) (*charSet, bool) {
	s := &charSet{}
	switch name {
	case "alpha":
		s.addItem(`\p{L}`, `\P{L}`)
		s.addItem(`\p{Nl}`, `\P{Nl}`)
		s.addItem(`\p{M}`, `\P{M}`)
	case "alnum":
		s.addItem(`\p{L}`, `\P{L}`)
		s.addItem(`\p{Nl}`, `\P{Nl}`)
		s.addItem(`\p{M}`, `\P{M}`)
		s.addItem(`\p{Nd}`, `\P{Nd}`)
	case "blank":
		s.addItem(`\p{Zs}`, `\P{Zs}`)
		s.addItem(`\t`, "")
	case "cntrl":
		s.addItem(`\p{Cc}`, `\P{Cc}`)
	case "digit":
		s.addItem(`\p{Nd}`, `\P{Nd}`)
	case "graph":
		s.neg = true
		for _, e := range []string{`\s`, `\p{Cc}`, `\p{Cs}`, `\p{Cn}`} {
			s.addItem(e, "")
		}
	case "print":
		s.neg = true
		for _, e := range []string{`\p{Cc}`, `\p{Cs}`, `\p{Cn}`, `\p{Zl}`, `\p{Zp}`} {
			s.addItem(e, "")
		}
	case "lower":
		s.addItem(`\p{Ll}`, `\P{Ll}`)
	case "upper":
		s.addItem(`\p{Lu}`, `\P{Lu}`)
	case "punct":
		s.addItem(`\p{P}`, `\P{P}`)
		s.addItem(`\p{S}`, `\P{S}`)
	case "space":
		s.addItem(`\s`, `\S`)
	case "word":
		s.addItem(`\w`, `\W`)
	case "xdigit":
		s.addRange('0', '9')
		s.addRange('A', 'F')
		s.addRange('a', 'f')
	case "ascii":
		s.addRange(0, 0x7f)
	default:
		return nil, false
	}
	return s, true
}

// propertySet is the set of \p{name}: a POSIX name, a general category or a script. The second
// result is false for a name that is none of them.
func propertySet(name string) (*charSet, bool) {
	n := normalizeName(name)
	if s, ok := posixSet(n); ok {
		return s, true
	}
	switch n {
	case "alphabetic":
		return posixSet("alpha")
	case "uppercase":
		return posixSet("upper")
	case "lowercase":
		return posixSet("lower")
	case "whitespace":
		return posixSet("space")
	case "any":
		return &charSet{items: []setItem{{text: `\s\S`}}}, true
	case "lc", "casedletter":
		return category("LC"), true
	}
	if c, ok := categoryNames[n]; ok {
		return category(c), true
	}
	if t, ok := scriptTables[n]; ok {
		return setOfTable(t), true
	}
	return nil, false
}
