package emmet

import (
	"maps"
	"regexp"
	"slices"
)

// Ported from VS Code: extensions/emmet/src/util.ts (LANGUAGE_MODES, getMappingForIncludedLanguages,
// getEmmetMode, getSyntaxes) and extensions/emmet/src/abbreviationActions.ts (getSyntaxFromArgs)

// mappedModes are the languages that VS Code maps to a parent language to have Emmet in them
// (MAPPED_MODES): they are used unless emmet.includeLanguages says otherwise.
var mappedModes = map[string]string{"handlebars": "html", "php": "html"}

// isLanguageMode tells if Emmet works in the language by itself (the keys of LANGUAGE_MODES of the
// extension): the ones an entry of emmet.includeLanguages may name.
func isLanguageMode(language string) bool {
	switch language {
	case "html", "jade", "slim", "haml", "xml", "xsl", "css", "scss", "sass", "less", "stylus",
		"javascriptreact", "typescriptreact":
		return true
	}
	return false
}

// emmetSyntaxes are the syntaxes the extension gives (getSyntaxes): the markup ones, then the stylesheet ones.
var emmetSyntaxes = [][]string{
	{"html", "xml", "xsl", "jsx", "js", "pug", "slim", "haml"},
	{"css", "sass", "scss", "less", "sss", "stylus"},
}

// MappingForIncludedLanguages gives the languages that map to a language of Emmet: the built-in
// ones (handlebars and php to html) and those of emmet.includeLanguages, which win. An entry
// that names a language Emmet does not work in is left out.
func MappingForIncludedLanguages(includeLanguages map[string]string) map[string]string {
	merged := maps.Clone(mappedModes)
	maps.Copy(merged, includeLanguages)
	final := make(map[string]string, len(merged))
	for language, mode := range merged {
		if isLanguageMode(mode) {
			final[language] = mode
		}
	}
	return final
}

var reactModeRe = regexp.MustCompile(`\b(typescriptreact|javascriptreact|jsx-tags)\b`)

// EmmetMode gives the syntax of Emmet for the language of the editor: jsx for the React
// languages, pug for jade, sass for sass-indented. The mapped languages (see
// MappingForIncludedLanguages) are mapped first. It gives false for a language Emmet does not work
// in or that is excluded.
func EmmetMode(language string, mapped map[string]string, excluded []string) (string, bool) {
	if language == "" || slices.Contains(excluded, language) {
		return "", false
	}
	if language == "jsx-tags" {
		language = "javascriptreact"
	}
	if m, ok := mapped[language]; ok && m != "" {
		language = m
	}

	switch {
	case reactModeRe.MatchString(language): // tsx is jsx
		language = "jsx"
	case language == "sass-indented":
		language = "sass"
	case language == "jade" || language == "pug":
		language = "pug"
	}

	for _, group := range emmetSyntaxes {
		if slices.Contains(group, language) {
			return language, true
		}
	}
	return "", false
}

// SyntaxFor gives the syntax of Emmet to expand abbreviations in for the language of a document
// (VS Code's getSyntaxFromArgs): includeLanguages is the setting emmet.includeLanguages (see
// IncludeLanguagesFromSettings) and excluded the setting emmet.excludeLanguages (see
// ExcludedLanguagesFromSettings). It gives false when Emmet does not work in the language: it is
// excluded, or neither Emmet nor the mapping know it.
func SyntaxFor(languageID string, includeLanguages map[string]string, excluded []string) (string, bool) {
	if slices.Contains(excluded, languageID) {
		return "", false
	}
	mapped := MappingForIncludedLanguages(includeLanguages)
	language := languageID
	if m, ok := mapped[languageID]; ok {
		language = m
	}
	return EmmetMode(language, mapped, excluded)
}

// IncludeLanguagesFromSettings reads the setting emmet.includeLanguages: an object of strings. As in
// VS Code, an entry whose value is not a string is ignored, and so is a value that is not an object.
func IncludeLanguagesFromSettings(value any) map[string]string {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(obj))
	for k, v := range obj {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result
}

// ExcludedLanguagesFromSettings reads the setting emmet.excludeLanguages: a list of strings. Items that
// are not strings are ignored, and so is a value that is not a list.
func ExcludedLanguagesFromSettings(value any) []string {
	var result []string
	switch list := value.(type) {
	case []any:
		for _, v := range list {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
	case []string:
		result = slices.Clone(list)
	}
	return result
}
