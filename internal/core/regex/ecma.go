package regex

import (
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/rm4n0s/errors"
)

// compileECMAScript compiles a JavaScript pattern. regexp2 reads it with its ECMAScript option;
// what differs is translated first: named groups are numbered with the others as JavaScript does
// (regexp2 numbers them after), and [^] and [] are written in a way regexp2 reads.
func compileECMAScript(pattern string, opts Options) (*Regexp, error) {
	translated, groups, names, err := translateECMAScript(pattern, opts.Unicode)
	if err != nil {
		return nil, err
	}
	var ro regexp2.RegexOptions = regexp2.ECMAScript
	if opts.IgnoreCase {
		ro |= regexp2.IgnoreCase
	}
	if opts.Multiline {
		ro |= regexp2.Multiline
	}
	if opts.DotAll {
		ro |= regexp2.Singleline
	}
	if opts.Unicode {
		ro |= regexp2.Unicode
	}
	re, err := regexp2.Compile(translated, ro)
	if err != nil {
		return nil, errors.NewErr("RegexCompileFailed", err).SetMetadata("pattern", pattern, "translated", clip(translated))
	}
	if opts.Timeout > 0 {
		re.MatchTimeout = opts.Timeout
	}
	return &Regexp{re: re, source: pattern, translated: translated, groups: groups, names: names}, nil
}

// translateECMAScript rewrites the parts of a JavaScript pattern that regexp2 reads differently.
func translateECMAScript(pattern string, unicode bool) (translated string, groups int, names map[string]int, err error) {
	src := []rune(pattern)
	names = map[string]int{}
	// First pass: the group numbers, so that \k<name> can come before the group.
	inClass := false
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '(':
			if inClass {
				continue
			}
			if i+1 < len(src) && src[i+1] == '?' {
				if i+2 < len(src) && src[i+2] == '<' && i+3 < len(src) && src[i+3] != '=' && src[i+3] != '!' {
					j := i + 3
					for j < len(src) && src[j] != '>' {
						j++
					}
					groups++
					names[string(src[i+3:min(j, len(src))])] = groups
				}
				continue
			}
			groups++
		}
	}
	var sb strings.Builder
	inClass = false
	for i := 0; i < len(src); i++ {
		r := src[i]
		switch {
		case r == '\\' && i+1 < len(src) && unicode && (src[i+1] == 'p' || src[i+1] == 'P') && i+2 < len(src) && src[i+2] == '{':
			text, next, perr := unicodeProperty(src, i, inClass)
			if perr != nil {
				return "", 0, nil, perr
			}
			sb.WriteString(text)
			i = next
		case r == '\\' && i+1 < len(src):
			if src[i+1] == 'k' && i+2 < len(src) && src[i+2] == '<' && len(names) > 0 {
				j := i + 3
				for j < len(src) && src[j] != '>' {
					j++
				}
				if n, ok := names[string(src[i+3:min(j, len(src))])]; ok {
					sb.WriteString(`\` + strconv.Itoa(n))
					i = j
					continue
				}
			}
			sb.WriteRune(r)
			sb.WriteRune(src[i+1])
			i++
		case inClass:
			if r == ']' {
				inClass = false
			}
			sb.WriteRune(r)
		case r == '[':
			switch {
			case strings.HasPrefix(string(src[i:]), "[^]"):
				sb.WriteString(anyChar)
				i += 2
			case strings.HasPrefix(string(src[i:]), "[]"):
				sb.WriteString("(?!)")
				i++
			default:
				inClass = true
				sb.WriteRune(r)
			}
		case r == '(' && i+3 < len(src) && src[i+1] == '?' && src[i+2] == '<' && src[i+3] != '=' && src[i+3] != '!':
			j := i + 3
			for j < len(src) && src[j] != '>' {
				j++
			}
			if j >= len(src) {
				return "", 0, nil, errors.New("RegexInvalidGroupName", "the name of a group is not closed", "pattern", pattern)
			}
			sb.WriteRune('(')
			i = j
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String(), groups, names, nil
}

// unicodeProperty translates the \p{..} or \P{..} that starts at src[at] when the u flag is on.
// The name may be a general category, a script, or a POSIX-like name, and may be written
// Script=Greek or General_Category=Letter. It returns the pattern and the index of the closing
// brace.
func unicodeProperty(src []rune, at int, inClass bool) (string, int, error) {
	end := at + 3
	for end < len(src) && src[end] != '}' {
		end++
	}
	if end >= len(src) {
		return "", 0, errors.New("RegexInvalidEscape", "\\p{ is not closed", "pattern", string(src))
	}
	name := string(src[at+3 : end])
	if _, value, ok := strings.Cut(name, "="); ok {
		name = value
	}
	set, ok := propertySet(name)
	if !ok {
		return "", 0, errors.New("RegexUnknownProperty", "this property name is not known", "name", name, "pattern", string(src))
	}
	if src[at+1] == 'P' {
		set = set.negated()
	}
	if !inClass {
		return set.expr(), end, nil
	}
	// Inside a class only a set that is a list of escapes can be written.
	switch {
	case set.simple() && !set.neg:
		return joinItems(set.items), end, nil
	case set.simple() && len(set.items) == 1 && set.items[0].inv != "":
		return set.items[0].inv, end, nil
	}
	return "", 0, errors.New("RegexUnsupportedFeature", "this property cannot be used inside a class", "name", name, "pattern", string(src))
}
