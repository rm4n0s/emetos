package language

import (
	"encoding/json"
	"strings"

	"github.com/rm4n0s/errors"
)

// Ported from VS Code: reference/vscode/src/vs/workbench/services/language/common/languageService.ts
// and reference/vscode/src/vs/workbench/services/textMate/common/TMGrammars.ts (MIT)

// Manifest is what the registry reads of the package.json of a VS Code extension: the languages
// and the grammars it contributes.
type Manifest struct {
	// Name is the name of the extension.
	Name string
	// Languages are the contributes.languages that are valid.
	Languages []LanguageContribution
	// Grammars are the contributes.grammars that are valid.
	Grammars []GrammarContribution
	// ConfigurationDefaults are the settings that the extension gives to languages
	// (contributes.configurationDefaults, whose "[go]" blocks name languages): by language id.
	// The keys of the file that are not language blocks are not read.
	ConfigurationDefaults map[string]map[string]any
	// Problems are the contributions that were left out, and why.
	Problems []error
}

// LanguageContribution is an entry of contributes.languages.
type LanguageContribution struct {
	// ID is the language id, such as go.
	ID string
	// Aliases are the names of the language; the first is its display name.
	Aliases []string
	// Extensions are file name endings, such as .go.
	Extensions []string
	// Filenames are exact file names, such as Makefile.
	Filenames []string
	// FilenamePatterns are file name globs.
	FilenamePatterns []string
	// MIMETypes are the MIME types of the language.
	MIMETypes []string
	// FirstLine is a regular expression for the first line of a file of the language.
	FirstLine string
	// Configuration is the path of the language configuration file, relative to the extension.
	Configuration string
}

// GrammarContribution is an entry of contributes.grammars.
type GrammarContribution struct {
	// Language is the language the grammar tokenizes; empty for a grammar that other grammars
	// include.
	Language string
	// ScopeName is the scope of the grammar.
	ScopeName string
	// Path is the path of the grammar file, relative to the extension.
	Path string
	// EmbeddedLanguages maps scope names in the grammar to the ids of the languages in them.
	EmbeddedLanguages map[string]string
	// TokenTypes maps scope selectors to a token type: string, comment, regex or other.
	TokenTypes map[string]string
	// InjectTo lists the scopes of the grammars this one is injected into.
	InjectTo []string
}

// rawManifest is the part of package.json that is read.
type rawManifest struct {
	Name        string `json:"name"`
	Contributes struct {
		Languages []json.RawMessage `json:"languages"`
		Grammars  []json.RawMessage `json:"grammars"`
		// ConfigurationDefaults maps "[go]" or "[a][b]" to an object of settings.
		ConfigurationDefaults map[string]json.RawMessage `json:"configurationDefaults"`
	} `json:"contributes"`
}

// ParseManifest reads the languages and grammars of a package.json. A contribution that is not
// valid is left out and listed in Problems (the tags are LanguageContributionInvalid and
// GrammarContributionInvalid); a file that is not JSON has the error ManifestInvalid.
func ParseManifest(data []byte) (*Manifest, error) {
	var raw rawManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.NewErr("ManifestInvalid", err)
	}
	m := &Manifest{Name: raw.Name}
	for i, item := range raw.Contributes.Languages {
		c, err := parseLanguageContribution(item, "extension", raw.Name, "index", i)
		if err != nil {
			m.Problems = append(m.Problems, err)
			continue
		}
		m.Languages = append(m.Languages, c)
	}
	for i, item := range raw.Contributes.Grammars {
		c, err := parseGrammarContribution(item, "extension", raw.Name, "index", i)
		if err != nil {
			m.Problems = append(m.Problems, err)
			continue
		}
		m.Grammars = append(m.Grammars, c)
	}
	for key, value := range raw.Contributes.ConfigurationDefaults {
		ids, ok := languageBlock(key)
		if !ok {
			continue
		}
		var values map[string]any
		if err := json.Unmarshal(value, &values); err != nil {
			m.Problems = append(m.Problems, errors.NewErr("ConfigurationDefaultsInvalid", err).SetMetadata("extension", raw.Name, "key", key))
			continue
		}
		if m.ConfigurationDefaults == nil {
			m.ConfigurationDefaults = map[string]map[string]any{}
		}
		for _, id := range ids {
			if m.ConfigurationDefaults[id] == nil {
				m.ConfigurationDefaults[id] = map[string]any{}
			}
			for k, v := range values {
				m.ConfigurationDefaults[id][k] = v
			}
		}
	}
	return m, nil
}

// languageBlock reads a key such as "[go]" or "[json][jsonc]" into the language ids it names.
func languageBlock(key string) ([]string, bool) {
	var ids []string
	rest := strings.TrimSpace(key)
	for rest != "" {
		if rest[0] != '[' {
			return nil, false
		}
		end := strings.IndexByte(rest, ']')
		if end < 2 {
			return nil, false
		}
		ids = append(ids, rest[1:end])
		rest = rest[end+1:]
	}
	return ids, len(ids) > 0
}

// parseLanguageContribution reads and validates an entry of contributes.languages
// (isValidLanguageExtensionPoint).
func parseLanguageContribution(data json.RawMessage, at ...any) (LanguageContribution, error) {
	var raw struct {
		ID               string          `json:"id"`
		Aliases          json.RawMessage `json:"aliases"`
		Extensions       json.RawMessage `json:"extensions"`
		Filenames        json.RawMessage `json:"filenames"`
		FilenamePatterns json.RawMessage `json:"filenamePatterns"`
		MIMETypes        json.RawMessage `json:"mimetypes"`
		FirstLine        json.RawMessage `json:"firstLine"`
		Configuration    json.RawMessage `json:"configuration"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return LanguageContribution{}, errors.NewErr("LanguageContributionInvalid", err).SetMetadata(with(at, "reason", "not an object with a string id")...)
	}
	if strings.TrimSpace(raw.ID) == "" {
		return LanguageContribution{}, errors.New("LanguageContributionInvalid", "a language needs an id", with(at, "reason", "id")...)
	}
	c := LanguageContribution{ID: raw.ID}
	for field, dst := range map[string]*[]string{
		"aliases": &c.Aliases, "extensions": &c.Extensions, "filenames": &c.Filenames,
		"filenamePatterns": &c.FilenamePatterns, "mimetypes": &c.MIMETypes,
	} {
		src := map[string]json.RawMessage{"aliases": raw.Aliases, "extensions": raw.Extensions, "filenames": raw.Filenames,
			"filenamePatterns": raw.FilenamePatterns, "mimetypes": raw.MIMETypes}[field]
		if len(src) == 0 {
			continue
		}
		if err := json.Unmarshal(src, dst); err != nil {
			return LanguageContribution{}, errors.NewErr("LanguageContributionInvalid", err).SetMetadata(with(at, "id", raw.ID, "reason", field+" is not an array of strings")...)
		}
	}
	for field, dst := range map[string]*string{"firstLine": &c.FirstLine, "configuration": &c.Configuration} {
		src := map[string]json.RawMessage{"firstLine": raw.FirstLine, "configuration": raw.Configuration}[field]
		if len(src) == 0 {
			continue
		}
		if err := json.Unmarshal(src, dst); err != nil {
			return LanguageContribution{}, errors.NewErr("LanguageContributionInvalid", err).SetMetadata(with(at, "id", raw.ID, "reason", field+" is not a string")...)
		}
	}
	return c, nil
}

// parseGrammarContribution reads and validates an entry of contributes.grammars
// (validateGrammarExtensionPoint).
func parseGrammarContribution(data json.RawMessage, at ...any) (GrammarContribution, error) {
	var raw struct {
		Language          json.RawMessage `json:"language"`
		ScopeName         string          `json:"scopeName"`
		Path              string          `json:"path"`
		EmbeddedLanguages json.RawMessage `json:"embeddedLanguages"`
		TokenTypes        json.RawMessage `json:"tokenTypes"`
		InjectTo          json.RawMessage `json:"injectTo"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return GrammarContribution{}, errors.NewErr("GrammarContributionInvalid", err).SetMetadata(with(at, "reason", "not an object with a string scopeName and path")...)
	}
	if raw.ScopeName == "" || raw.Path == "" {
		return GrammarContribution{}, errors.New("GrammarContributionInvalid", "a grammar needs a scopeName and a path", with(at, "reason", "scopeName and path")...)
	}
	c := GrammarContribution{ScopeName: raw.ScopeName, Path: raw.Path}
	if len(raw.Language) > 0 && string(raw.Language) != "null" {
		if err := json.Unmarshal(raw.Language, &c.Language); err != nil {
			return GrammarContribution{}, errors.NewErr("GrammarContributionInvalid", err).SetMetadata(with(at, "scope", raw.ScopeName, "reason", "language is not a string")...)
		}
	}
	if len(raw.EmbeddedLanguages) > 0 {
		if err := json.Unmarshal(raw.EmbeddedLanguages, &c.EmbeddedLanguages); err != nil {
			return GrammarContribution{}, errors.NewErr("GrammarContributionInvalid", err).SetMetadata(with(at, "scope", raw.ScopeName, "reason", "embeddedLanguages is not an object of strings")...)
		}
	}
	if len(raw.TokenTypes) > 0 {
		if err := json.Unmarshal(raw.TokenTypes, &c.TokenTypes); err != nil {
			return GrammarContribution{}, errors.NewErr("GrammarContributionInvalid", err).SetMetadata(with(at, "scope", raw.ScopeName, "reason", "tokenTypes is not an object of strings")...)
		}
	}
	if len(raw.InjectTo) > 0 {
		if err := json.Unmarshal(raw.InjectTo, &c.InjectTo); err != nil {
			return GrammarContribution{}, errors.NewErr("GrammarContributionInvalid", err).SetMetadata(with(at, "scope", raw.ScopeName, "reason", "injectTo is not an array of strings")...)
		}
	}
	return c, nil
}

// with adds key/value pairs to the pairs that say where a contribution is.
func with(at []any, pairs ...any) []any {
	return append(append([]any(nil), at...), pairs...)
}
