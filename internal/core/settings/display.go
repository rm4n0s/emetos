package settings

import (
	"regexp"
	"strings"
)

// Ported from VS Code: src/vs/workbench/contrib/preferences/common/preferences.ts (wordifyKey, knownAcronyms,
// knownTermMappings) and src/vs/workbench/contrib/preferences/browser/settingsTreeModels.ts
// (settingKeyToDisplayFormat, trimCategoryForGroup)

var (
	wordDot         = regexp.MustCompile(`\.([a-z0-9])`)
	wordCamel       = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	wordCapitals    = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	wordFirst       = regexp.MustCompile(`^[a-z]`)
	wordAny         = regexp.MustCompile(`\b\w+\b`)
	knownAcronyms   = map[string]bool{"css": true, "html": true, "scss": true, "less": true, "json": true, "js": true, "ts": true, "ie": true, "id": true, "php": true, "scm": true}
	knownTermOrder  = []string{"power shell", "powershell", "javascript", "typescript", "github", "ocaml", "jet brains", "jetbrains", "re sharper", "resharper"}
	knownTerms      = map[string]string{"power shell": "PowerShell", "powershell": "PowerShell", "javascript": "JavaScript", "typescript": "TypeScript", "github": "GitHub", "ocaml": "OCaml", "jet brains": "JetBrains", "jetbrains": "JetBrains", "re sharper": "ReSharper", "resharper": "ReSharper"}
	knownTermRegexp = func() []*regexp.Regexp {
		out := make([]*regexp.Regexp, len(knownTermOrder))
		for i, k := range knownTermOrder {
			out[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(k) + `\b`)
		}
		return out
	}()
)

// WordifyKey writes a part of a setting key as words: dots become " › ", camel case is split, the first letter
// and known acronyms are capitalized ("editor.fontSize" gives "Editor › Font Size").
func WordifyKey(key string) string {
	key = wordDot.ReplaceAllStringFunc(key, func(m string) string { return " › " + strings.ToUpper(m[1:]) })
	key = wordCamel.ReplaceAllString(key, "${1} ${2}")
	key = wordCapitals.ReplaceAllString(key, "${1} ${2}")
	key = wordFirst.ReplaceAllStringFunc(key, strings.ToUpper)
	key = wordAny.ReplaceAllStringFunc(key, func(m string) string {
		if knownAcronyms[strings.ToLower(m)] {
			return strings.ToUpper(m)
		}
		return m
	})
	for i, re := range knownTermRegexp {
		key = re.ReplaceAllString(key, knownTerms[knownTermOrder[i]])
	}
	return key
}

// KeyToDisplayFormat is the category and the label a setting is shown with: the key up to the last dot, and what
// follows it, both written as words. groupID is the id of the group the setting is shown in ("editor/font"); the
// part of the category that the group already says is left out, so "editor.fontSize" in the Font group is just
// "Font Size", and "Editor: Font Size" anywhere else (groupID empty).
func KeyToDisplayFormat(key, groupID string) (category, label string) {
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		category, key = key[:i], key[i+1:]
	}
	groupID = strings.ReplaceAll(groupID, "/", ".")
	category = WordifyKey(trimCategoryForGroup(category, groupID))
	return category, WordifyKey(key)
}

// Title is "Category: Label", or the label alone while the category is empty.
func Title(category, label string) string {
	if category == "" {
		return label
	}
	return category + ": " + label
}

// trimCategoryForGroup removes the sections of the category that the id of the group already says.
func trimCategoryForGroup(category, groupID string) string {
	if groupID == "" {
		return category
	}
	doTrim := func(forward bool) (string, bool) {
		parts := strings.Split(groupID, ".")
		for i, part := range parts {
			// hyphens go, but only if that makes the part equal to the category
			if strings.EqualFold(strings.ReplaceAll(part, "-", ""), category) {
				parts[i] = strings.ReplaceAll(part, "-", "")
			}
		}
		for len(parts) > 0 {
			quoted := make([]string, len(parts))
			for i, p := range parts {
				quoted[i] = regexp.QuoteMeta(p)
			}
			re := regexp.MustCompile(`(?i)^` + strings.Join(quoted, `\.`) + `(\.|$)`)
			if re.MatchString(category) {
				return re.ReplaceAllString(category, ""), true
			}
			if forward {
				parts = parts[:len(parts)-1]
			} else {
				parts = parts[1:]
			}
		}
		return "", false
	}
	if t, ok := doTrim(true); ok {
		return t
	}
	if t, ok := doTrim(false); ok {
		return t
	}
	return category
}
