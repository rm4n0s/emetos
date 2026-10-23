package settings

import "github.com/rm4n0s/emetos/internal/core/fileio"

// usualWordSeparators is VS Code's USUAL_WORD_SEPARATORS.
//
// Ported from VS Code: src/vs/editor/common/core/wordHelper.ts
const usualWordSeparators = "`~!@#$%^&*()-=+[{]}\\|;:'\",.<>/?"

const (
	categoryEditor = "Text Editor"
	categoryFiles  = "Files"
)

// DefaultRegistry makes a registry with the settings Emetos has. Each step that reads a
// setting adds it here (or to a function it calls) with the values of VS Code.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	registerEditor(r)
	registerFiles(r)
	return r
}

// registerEditor registers the editor.* settings.
//
// Ported from VS Code: src/vs/editor/common/config/editorConfigurationSchema.ts and
// src/vs/editor/common/config/editorOptions.ts
func registerEditor(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "editor.tabSize", Types: []Type{Number}, Default: 4, Minimum: Float(1), Maximum: Float(100),
			Description: "The number of spaces a tab is equal to. This setting is overridden based on the file contents when `editor.detectIndentation` is on.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.insertSpaces", Types: []Type{Boolean}, Default: true,
			Description: "Insert spaces when pressing `Tab`. This setting is overridden based on the file contents when `editor.detectIndentation` is on.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.detectIndentation", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether `editor.tabSize` and `editor.insertSpaces` will be automatically detected when a file is opened based on the file contents.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.wordSeparators", Types: []Type{String}, Default: usualWordSeparators,
			Description: "Characters that will be used as word separators when doing word related navigations or operations.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
	)
}

// registerFiles registers the files.* settings.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/files.contribution.ts
func registerFiles(r *Registry) {
	var ids []any
	var labels []string
	for _, e := range fileio.Encodings() {
		ids = append(ids, e.ID)
		labels = append(labels, e.LabelLong)
	}
	r.MustRegister(
		Setting{
			Key: "files.encoding", Types: []Type{String}, Default: "utf8", Enum: ids, EnumDescriptions: labels,
			Description: "The default character set encoding to use when reading and writing files. This setting can also be configured per language.",
			Scope:       ScopeLanguageOverridable, Category: categoryFiles,
		},
		Setting{
			Key: "files.autoGuessEncoding", Types: []Type{Boolean}, Default: false,
			Description: "When enabled, the editor will attempt to guess the character set encoding when opening files. This setting can also be configured per language. Note, this setting is not respected by text search. Only `files.encoding` is respected.",
			Scope:       ScopeLanguageOverridable, Category: categoryFiles,
		},
		Setting{
			Key: "files.eol", Types: []Type{String}, Default: "auto", Enum: []any{"\n", "\r\n", "auto"},
			EnumDescriptions: []string{"LF", "CRLF", "Uses operating system specific end of line character."},
			Description:      "The default end of line character.",
			Scope:            ScopeLanguageOverridable, Category: categoryFiles,
		},
	)
}
