package settings

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Ported from VS Code: src/vs/workbench/services/preferences/common/preferencesModels.ts (DefaultSettings,
// DefaultSettingsEditorModel: the default settings document, groups with their settings, each with its
// description as comments and its default value)

// defaultSettingsWrap is the column the comments of the default settings document wrap at.
const defaultSettingsWrap = 80

// DefaultSettingsJSON is the text of the default settings document ("Open Default Settings (JSON)", SPEC §11.1):
// a JSONC object with every registered setting once, grouped by the groups of the settings editor in their order
// (the Commonly Used group, which only repeats settings, is left out), each preceded by its description as
// `//` comments wrapped at 80 columns, the values it allows, its range and its deprecation message, and followed by
// its default value. Lines are separated by "\n" and indented with a tab, as VS Code's.
func DefaultSettingsJSON(reg *Registry) string {
	groups := BuildCategories(DefaultTOC(), reg.All())
	var b strings.Builder
	b.WriteString("{\n")
	first := true
	type section struct {
		title string
		keys  []string
	}
	var sections []section
	var walk func(c *Category, path []string)
	walk = func(c *Category, path []string) {
		path = append(path[:len(path):len(path)], c.Label)
		if len(c.Keys) > 0 {
			sections = append(sections, section{title: strings.Join(path, " > "), keys: c.Keys})
		}
		for _, ch := range c.Children {
			walk(ch, path)
		}
	}
	for _, c := range groups {
		if c.ID == CommonlyUsedID {
			continue
		}
		walk(c, nil)
	}
	total := 0
	for _, sec := range sections {
		total += len(sec.keys)
	}
	written := 0
	for _, sec := range sections {
		if !first {
			b.WriteString("\n")
		}
		first = false
		b.WriteString("\t// " + sec.title + "\n")
		for _, key := range sec.keys {
			s, ok := reg.Lookup(key)
			if !ok {
				continue
			}
			written++
			b.WriteString("\n")
			writeSettingComments(&b, s)
			b.WriteString("\t" + compactJSON(s.Key) + ": " + indentedJSON(s.Default))
			if written < total {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// writeSettingComments writes the comment lines of a setting.
func writeSettingComments(b *strings.Builder, s Setting) {
	comment := func(text string) {
		for _, line := range strings.Split(text, "\n") {
			for _, w := range wrapWords(line, defaultSettingsWrap-len("\t// ")) {
				b.WriteString("\t// " + w + "\n")
			}
		}
	}
	comment(s.Description)
	if len(s.Enum) > 0 {
		var values []string
		for _, v := range s.Enum {
			values = append(values, compactJSON(v))
		}
		comment("Allowed values: " + strings.Join(values, ", "))
		for i, d := range s.EnumDescriptions {
			if d != "" && i < len(s.Enum) {
				comment("  " + compactJSON(s.Enum[i]) + ": " + d)
			}
		}
	}
	if s.Minimum != nil {
		comment("Minimum: " + strconv.FormatFloat(*s.Minimum, 'g', -1, 64))
	}
	if s.Maximum != nil {
		comment("Maximum: " + strconv.FormatFloat(*s.Maximum, 'g', -1, 64))
	}
	if s.Deprecated != "" {
		comment("Deprecated: " + s.Deprecated)
	}
}

// wrapWords breaks a line at spaces so that no line is longer than width unless a word is; the indentation
// of the line is kept on each piece. An empty line stays one empty piece.
func wrapWords(line string, width int) []string {
	indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	cur := indent
	for _, w := range words {
		switch {
		case strings.TrimSpace(cur) == "":
			cur += w
		case len(cur)+1+len(w) > width:
			out = append(out, cur)
			cur = indent + w
		default:
			cur += " " + w
		}
	}
	return append(out, cur)
}

// compactJSON is a value as one line of JSON; a value JSON cannot write is null.
func compactJSON(v any) string {
	data, err := marshalNoEscape(v, "", "")
	if err != nil {
		return "null"
	}
	return data
}

// indentedJSON is a value as JSON whose inner lines continue after one tab (the property sits at one tab).
func indentedJSON(v any) string {
	data, err := marshalNoEscape(v, "\t", "\t")
	if err != nil {
		return "null"
	}
	return data
}

func marshalNoEscape(v any, prefix, indent string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent(prefix, indent)
	}
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
