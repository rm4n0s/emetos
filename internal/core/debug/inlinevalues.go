package debug

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Inline values (step 25.5, `debug.inlineValues`): the values of the variables of the focused frame written after the lines of the
// editor that name them. This is the one-size-fits-all strategy of VS Code: the variables of the scopes of the frame are looked for
// among the words of the lines that come before the stopped line (and after the start of the scope), in the part of the file the editor
// shows. Emetos has no inline value providers, so this is the only strategy.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugEditorContribution.ts (createInlineValueDecorationsInsideRange,
// updateInlineValueDecorations, formatHoverContent)

// MaxInlineValues is the number of variables of a scope that are looked for (the global scope of JavaScript has 700 or more).
const MaxInlineValues = 100

// MaxInlineLength is the number of characters of the text after a line; a longer text is cut and ends with "...".
const MaxInlineLength = 150

// InlineVariable is a variable and its value.
type InlineVariable struct{ Name, Value string }

// InlineScope is the variables of a scope of the frame.
type InlineScope struct {
	// Variables are the children of the scope.
	Variables []InlineVariable
	// FirstLine is the line (counted from 1) the scope starts at, or 0 for a scope that has no range, which starts at the first line.
	FirstLine int
}

// InlineLine is a line and the values written after it, in the order the variables come in the line.
type InlineLine struct {
	// Line counts from 1.
	Line   int
	Values []InlineVariable
}

// InlineValues works out the values to write after the lines first to last (counted from 1) when the program stopped at the line
// stopped. The scopes are given from the narrowest to the widest, so that a name that is in two of them has the value of the nearest
// (a shadowed variable is not written twice). words gives the identifiers of a line (those that are not in a string or a comment);
// content gives the text of a line, to order the values as the names come in it.
func InlineValues(scopes []InlineScope, stopped, first, last int, words func(line int) []string, content func(line int) string) []InlineLine {
	type key struct {
		line int
		name string
	}
	seen := map[key]bool{}
	perLine := map[int][]InlineVariable{}
	cache := map[int]map[string]bool{}
	wordsOf := func(line int) map[string]bool {
		if set, ok := cache[line]; ok {
			return set
		}
		set := map[string]bool{}
		for _, w := range words(line) {
			set[w] = true
		}
		cache[line] = set
		return set
	}
	for _, sc := range scopes {
		from, to := max(first, 1), min(last, stopped)
		if sc.FirstLine > 0 {
			from = max(from, sc.FirstLine)
		}
		if from > to {
			continue
		}
		// the name and its value; a later variable of the same name replaces the value and keeps the place
		values := map[string]string{}
		var names []string
		for _, v := range sc.Variables {
			if _, ok := values[v.Name]; !ok {
				names = append(names, v.Name)
			}
			values[v.Name] = v.Value
			if len(names) >= MaxInlineValues {
				break
			}
		}
		for line := from; line <= to; line++ {
			set := wordsOf(line)
			for _, name := range names {
				if !set[name] || seen[key{line, name}] {
					continue
				}
				seen[key{line, name}] = true
				perLine[line] = append(perLine[line], InlineVariable{Name: name, Value: values[name]})
			}
		}
	}
	lines := make([]int, 0, len(perLine))
	for line := range perLine {
		lines = append(lines, line)
	}
	sort.Ints(lines)
	out := make([]InlineLine, 0, len(lines))
	for _, line := range lines {
		vars := perLine[line]
		text := content(line)
		sort.SliceStable(vars, func(i, j int) bool { return strings.Index(text, vars[i].Name) < strings.Index(text, vars[j].Name) })
		out = append(out, InlineLine{Line: line, Values: vars})
	}
	return out
}

// InlineText is the text written after a line for its values, `a = 1, b = 2`, cut to maxChars characters (with "..." after it) when it
// is longer; full is the text before it was cut. A new line or a tab in a value is a space.
func InlineText(values []InlineVariable, maxChars int) (shown, full string) {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = v.Name + " = " + v.Value
	}
	full = strings.Join(parts, ", ")
	shown = strings.NewReplacer("\r\n", " ", "\n", " ", "\t", " ").Replace(full)
	if maxChars > 0 && utf8.RuneCountInString(shown) > maxChars {
		shown = string([]rune(shown)[:maxChars]) + "..."
	}
	return shown, full
}

// InlineHover is the text of the hover of the values of a line: each `name = value` on a line of its own when there are several, a
// value that spans lines indented under its name. A text of one value is shown as it is.
func InlineHover(full string) string {
	if !strings.Contains(full, ",") || !strings.Contains(full, "=") {
		return full
	}
	// for each equals sign after the first, go back to the nearest comma
	var cuts []int
	equals, start := 0, 0
	for i := 0; i < len(full); i++ {
		if full[i] != '=' {
			continue
		}
		if equals == 0 {
			equals++
			continue
		}
		if c := strings.LastIndex(full[:i], ","); c != -1 && c >= start {
			cuts = append(cuts, c)
			start = c + 1
		}
		equals++
	}
	var pairs []string
	from := 0
	for _, c := range cuts {
		pairs = append(pairs, strings.TrimSpace(full[from:c]))
		from = c + 1
	}
	if from < len(full) {
		pairs = append(pairs, strings.TrimSpace(full[from:]))
	}
	for i, p := range pairs {
		eq := strings.Index(p, "=")
		if eq == -1 {
			continue
		}
		indent := strings.Repeat(" ", eq+2)
		lines := strings.Split(strings.ReplaceAll(p, "\r\n", "\n"), "\n")
		for j := 1; j < len(lines); j++ {
			lines[j] = indent + lines[j]
		}
		pairs[i] = strings.Join(lines, "\n")
	}
	return strings.Join(pairs, ",\n")
}
