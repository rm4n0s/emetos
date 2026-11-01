package typescript

import (
	"reflect"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The settings of the TypeScript and JavaScript packs (SPEC §10.34 "As built (step 24b.5)"): the names and meanings of VS Code's
// `typescript.*` and `javascript.*` settings that typescript-language-server reads, written for Emetos. The settings that decide what
// Emetos runs (typescript.languageServer.path, typescript.nodePath, typescript.tsdk, typescript.npm) are read from the user's settings
// only. Every setting that differs from its default is sent to the server at its start, so a change of any restarts it (SPEC §10.34 "As
// built (step 24b.1)"). VS Code's newer `js/ts.*` names are not registered.

// The categories of the settings editor.
const (
	categoryTypeScript = "TypeScript"
	categoryJavaScript = "JavaScript"
)

// languages are the two families of settings and the category each has in the settings editor.
var languages = []struct{ prefix, category string }{{"typescript", categoryTypeScript}, {"javascript", categoryJavaScript}}

// target says how a setting reaches the server.
type target int

const (
	// toPreferences: a member of initializationOptions.preferences, which tsserver takes for every file (the value of typescript.* wins
	// when it differs from the default, else javascript.*'s).
	toPreferences target = iota
	// toInlayHints: a member of the inlayHints object of the section of the language (workspace/didChangeConfiguration), which the
	// server adds to the preferences of the files of that language.
	toInlayHints
	// toFormat: a member of the format object of the section of the language.
	toFormat
	// toSection: the setting's own name below the section of the language (referencesCodeLens.enabled).
	toSection
	// toCompletions: a member of the completions object of the section of the language.
	toCompletions
)

// option is a setting both languages have (or TypeScript only) and what it becomes for the server.
type option struct {
	// name is the setting without the prefix: preferences.quoteStyle.
	name string
	typ  string
	def  any
	enum []any
	desc string
	// tsOnly says that VS Code has the setting for TypeScript only.
	tsOnly bool
	to     target
	// key is the name the server knows it by (quotePreference); the setting's name below the section for toSection.
	key string
	// invert sends the opposite of a boolean (VS Code asks whether to suppress a hint, tsserver whether to include it).
	invert bool
}

// strs makes the values of an enum.
func strs(values ...string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// list makes the default of an array setting.
func list() []any { return []any{} }

// boolOption makes an option of type boolean.
func boolOption(name string, def bool, to target, key, desc string) option {
	return option{name: name, typ: "boolean", def: def, to: to, key: key, desc: desc}
}

// formatOption makes a setting of format.*.
func formatOption(name string, def bool, desc string) option {
	return boolOption("format."+name, def, toFormat, name, desc)
}

// options are the settings that are sent to the server.
func options() []option {
	return []option{
		{name: "preferences.quoteStyle", typ: "string", def: "auto", enum: strs("auto", "single", "double"), to: toPreferences, key: "quotePreference",
			desc: "The quotes imports and quick fixes write strings with. auto follows the quotes the file uses."},
		{name: "preferences.importModuleSpecifier", typ: "string", def: "shortest", enum: strs("shortest", "relative", "non-relative", "project-relative"),
			to: toPreferences, key: "importModuleSpecifierPreference", desc: "How the path of an automatic import is written: the shortest, relative to the file, from the baseUrl or paths of tsconfig.json, or from the project."},
		{name: "preferences.importModuleSpecifierEnding", typ: "string", def: "auto", enum: strs("auto", "minimal", "index", "js"),
			to: toPreferences, key: "importModuleSpecifierEnding", desc: "The ending of the path of an automatic import: auto, without index and extension (minimal), with index (index), or with the .js extension (js)."},
		{name: "preferences.jsxAttributeCompletionStyle", typ: "string", def: "auto", enum: strs("auto", "braces", "none"),
			to: toPreferences, key: "jsxAttributeCompletionStyle", desc: "What completing a JSX attribute adds after its name: braces, nothing, or by the type of the attribute (auto)."},
		{name: "preferences.autoImportFileExcludePatterns", typ: "array", def: list(), to: toPreferences, key: "autoImportFileExcludePatterns",
			desc: "Glob patterns of the files that automatic imports are never taken from (a package that has too many entries)."},
		{name: "preferences.autoImportSpecifierExcludeRegexes", typ: "array", def: list(), to: toPreferences, key: "autoImportSpecifierExcludeRegexes",
			desc: "Regular expressions of the module paths that automatic imports never write."},
		boolOption("preferences.useAliasesForRenames", true, toPreferences, "providePrefixAndSuffixTextForRename",
			"Rename a shorthand property or an import with an alias, instead of renaming the other places that use the name."),
		{name: "preferences.preferTypeOnlyAutoImports", typ: "boolean", def: false, tsOnly: true, to: toPreferences, key: "preferTypeOnlyAutoImports",
			desc: "Automatic imports of types are written as import type."},
		{name: "preferences.includePackageJsonAutoImports", typ: "string", def: "auto", enum: strs("auto", "on", "off"), tsOnly: true, to: toPreferences,
			key: "includePackageJsonAutoImports", desc: "Offer automatic imports from the packages that package.json lists: on, off, or only in a project that has many (auto)."},

		boolOption("suggest.autoImports", true, toPreferences, "includeCompletionsForModuleExports", "Offer completions that add an import for a symbol of another module."),
		boolOption("suggest.includeAutomaticOptionalChainCompletions", true, toPreferences, "includeAutomaticOptionalChainCompletions",
			"Offer completions of a value that may be undefined, and change . to ?. when one is chosen."),
		boolOption("suggest.includeCompletionsForImportStatements", true, toPreferences, "includeCompletionsForImportStatements",
			"Offer completions for the names in an import statement before the module is written."),
		boolOption("suggest.classMemberSnippets.enabled", true, toPreferences, "includeCompletionsWithClassMemberSnippets",
			"Offer snippets that complete a method or a property of a class from the one it implements."),
		{name: "suggest.objectLiteralMethodSnippets.enabled", typ: "boolean", def: true, tsOnly: true, to: toPreferences, key: "includeCompletionsWithObjectLiteralMethodSnippets",
			desc: "Offer snippets that complete a method of an object literal from its type."},
		boolOption("suggest.completeFunctionCalls", false, toCompletions, "completeFunctionCalls",
			"Complete a function with its parameters and parentheses."),

		{name: "inlayHints.parameterNames.enabled", typ: "string", def: "none", enum: strs("none", "literals", "all"), to: toInlayHints, key: "includeInlayParameterNameHints",
			desc: "Show the names of the parameters in a call: none, only before literals, or before every argument."},
		{name: "inlayHints.parameterNames.suppressWhenArgumentMatchesName", typ: "boolean", def: true, to: toInlayHints, key: "includeInlayParameterNameHintsWhenArgumentMatchesName", invert: true,
			desc: "Leave out the name of a parameter when the argument is a variable with the same name."},
		boolOption("inlayHints.parameterTypes.enabled", false, toInlayHints, "includeInlayFunctionParameterTypeHints", "Show the types of the parameters of a function that has none written."),
		boolOption("inlayHints.variableTypes.enabled", false, toInlayHints, "includeInlayVariableTypeHints", "Show the type after a variable that has none written."),
		{name: "inlayHints.variableTypes.suppressWhenTypeMatchesName", typ: "boolean", def: true, to: toInlayHints, key: "includeInlayVariableTypeHintsWhenTypeMatchesName", invert: true,
			desc: "Leave out the type of a variable when it is the name of the variable."},
		boolOption("inlayHints.propertyDeclarationTypes.enabled", false, toInlayHints, "includeInlayPropertyDeclarationTypeHints", "Show the type after a property of a class that has none written."),
		boolOption("inlayHints.functionLikeReturnTypes.enabled", false, toInlayHints, "includeInlayFunctionLikeReturnTypeHints", "Show the return type of a function that has none written."),
		{name: "inlayHints.enumMemberValues.enabled", typ: "boolean", def: false, tsOnly: true, to: toInlayHints, key: "includeInlayEnumMemberValueHints",
			desc: "Show the value of the members of an enum."},

		boolOption("referencesCodeLens.enabled", false, toSection, "referencesCodeLens.enabled", "Show the number of references above the declarations."),
		boolOption("referencesCodeLens.showOnAllFunctions", false, toSection, "referencesCodeLens.showOnAllFunctions", "Show the number of references above every function, besides the classes and interfaces."),
		{name: "implementationsCodeLens.enabled", typ: "boolean", def: false, to: toSection, key: "implementationsCodeLens.enabled",
			desc: "Show the number of implementations above interfaces and abstract classes."},

		formatOption("insertSpaceAfterCommaDelimiter", true, "A space after a comma."),
		formatOption("insertSpaceAfterConstructor", false, "A space after the constructor keyword."),
		formatOption("insertSpaceAfterSemicolonInForStatements", true, "A space after a semicolon in a for statement."),
		formatOption("insertSpaceBeforeAndAfterBinaryOperators", true, "A space before and after a binary operator."),
		formatOption("insertSpaceAfterKeywordsInControlFlowStatements", true, "A space after the keywords of a control flow statement (if, for, while)."),
		formatOption("insertSpaceAfterFunctionKeywordForAnonymousFunctions", true, "A space after the function keyword of an anonymous function."),
		formatOption("insertSpaceBeforeFunctionParenthesis", false, "A space before the parentheses of a function's parameters."),
		formatOption("insertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis", false, "A space after an opening and before a closing parenthesis, when it holds something."),
		formatOption("insertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets", false, "A space after an opening and before a closing bracket, when it holds something."),
		formatOption("insertSpaceAfterOpeningAndBeforeClosingNonemptyBraces", true, "A space after an opening and before a closing brace, when it holds something."),
		formatOption("insertSpaceAfterOpeningAndBeforeClosingEmptyBraces", true, "A space between the braces when they hold nothing."),
		formatOption("insertSpaceAfterOpeningAndBeforeClosingTemplateStringBraces", false, "A space after an opening and before a closing brace of a template string."),
		formatOption("insertSpaceAfterOpeningAndBeforeClosingJsxExpressionBraces", false, "A space after an opening and before a closing brace of a JSX expression."),
		formatOption("placeOpenBraceOnNewLineForFunctions", false, "The opening brace of a function goes on a new line."),
		formatOption("placeOpenBraceOnNewLineForControlBlocks", false, "The opening brace of a control block goes on a new line."),
		{name: "format.semicolons", typ: "string", def: "ignore", enum: strs("ignore", "insert", "remove"), to: toFormat, key: "semicolons",
			desc: "What formatting does with the semicolons at the ends of statements: nothing, add the missing ones, or remove the needless ones."},
		formatOption("indentSwitchCase", true, "Indent the case clauses of a switch."),
		{name: "format.insertSpaceAfterTypeAssertion", typ: "boolean", def: false, tsOnly: true, to: toFormat, key: "insertSpaceAfterTypeAssertion",
			desc: "A space after a type assertion (<T>x)."},
	}
}

// The settings that are not options: the program, node, the TypeScript and the server's resources.
const (
	// NodePathSetting is the node program that runs typescript-language-server, when it is not the first on PATH.
	NodePathSetting = "typescript.nodePath"
	// TsdkSetting is the lib folder of the TypeScript the server uses when the user has not chosen the folder's own.
	TsdkSetting = "typescript.tsdk"
	// PromptWorkspaceVersionSetting says whether the folder's own TypeScript is offered once (VS Code's name).
	PromptWorkspaceVersionSetting = "typescript.enablePromptUseWorkspaceTsdk"
	// NpmPathSetting is the npm program tsserver fetches the types of libraries with.
	NpmPathSetting = "typescript.npm"
	// LocaleSetting is the language of tsserver's messages.
	LocaleSetting = "typescript.locale"
	// DisableTypeAcquisitionSetting stops tsserver from fetching the @types packages of the imports it cannot resolve.
	DisableTypeAcquisitionSetting = "typescript.disableAutomaticTypeAcquisition"
	// MaxMemorySetting is the memory tsserver may use, in megabytes.
	MaxMemorySetting = "typescript.tsserver.maxTsServerMemory"
	// TraceSetting traces the traffic with the server (the host's).
	TraceSetting = "typescript.trace.server"
)

// settingSpecs are the settings the manifest declares besides the path setting of the server (langserver.Config.SettingSpecs).
func settingSpecs() []extension.SettingSpec {
	specs := []extension.SettingSpec{
		{
			Key: NodePathSetting, Type: "string", Default: "", Category: categoryTypeScript, Scope: "machine",
			Description: "The node program that runs typescript-language-server. When empty, the first node on PATH runs it. Only the user's settings are read.",
		},
		{
			Key: TsdkSetting, Type: "string", Default: "", Category: categoryTypeScript, Scope: "machine",
			Description: "The lib folder of the TypeScript the language server uses (the one that has tsserver.js, such as /usr/lib/node_modules/typescript/lib), " +
				"unless you chose the folder's own with TypeScript: Select TypeScript Version. When empty, the TypeScript installed beside the language server. " +
				"Only the user's settings are read.",
		},
		{
			Key: PromptWorkspaceVersionSetting, Type: "boolean", Default: true, Category: categoryTypeScript,
			Description: "Offer, once in each tab, the TypeScript in the node_modules of the folder. Emetos never runs it unless you accept: it is code that came with the folder.",
		},
		{
			Key: NpmPathSetting, Type: "string", Default: "", Category: categoryTypeScript, Scope: "machine",
			Description: "The npm program tsserver fetches the type definitions of libraries with. When empty, the first npm on PATH. Only the user's settings are read.",
		},
		{
			Key: LocaleSetting, Type: "string", Default: "auto", Category: categoryTypeScript,
			Enum:        strs("auto", "de", "es", "en", "fr", "it", "ja", "ko", "ru", "zh-CN", "zh-TW"),
			Description: "The language of the messages of TypeScript, such as the text of diagnostics. auto leaves it to the server (English).",
		},
		{
			Key: DisableTypeAcquisitionSetting, Type: "boolean", Default: false, Category: categoryTypeScript,
			Description: "Stop tsserver from fetching the @types packages of the libraries a JavaScript project imports (it runs npm and uses the network).",
		},
		{
			Key: MaxMemorySetting, Type: "number", Default: float64(3072), Category: categoryTypeScript,
			Description: "The memory tsserver may use, in megabytes, before it stops.",
		},
	}
	for _, o := range options() {
		for _, l := range languages {
			if o.tsOnly && l.prefix != "typescript" {
				continue
			}
			specs = append(specs, extension.SettingSpec{
				Key: l.prefix + "." + o.name, Type: o.typ, Default: o.def, Enum: o.enum, Category: l.category,
				Description: o.desc + " (" + l.category + " files)",
			})
		}
	}
	return specs
}

// serverSettings are what the server starts with, built from the settings.
type serverSettings struct {
	// preferences are the members of initializationOptions.preferences, and initialization the other members of initializationOptions.
	preferences, initialization map[string]any
	// sections are the settings of workspace/didChangeConfiguration: {"typescript": {...}, "javascript": {...}}.
	sections map[string]any
}

// readSettings reads every setting the server is given, so that a change of any of them restarts it, and keeps what differs from its
// default.
func readSettings(s *langserver.Settings) serverSettings {
	out := serverSettings{preferences: map[string]any{}, initialization: map[string]any{}, sections: map[string]any{}}
	for _, l := range languages {
		out.sections[l.prefix] = map[string]any{}
	}
	for _, o := range options() {
		for _, l := range languages {
			if o.tsOnly && l.prefix != "typescript" {
				continue
			}
			v, ok := s.Value(l.prefix + "." + o.name)
			if !ok || reflect.DeepEqual(normalize(v), normalize(o.def)) {
				continue
			}
			if b, isBool := v.(bool); isBool && o.invert {
				v = !b
			}
			section := out.sections[l.prefix].(map[string]any)
			switch o.to {
			case toPreferences:
				if _, taken := out.preferences[o.key]; !taken {
					out.preferences[o.key] = v
				}
			case toInlayHints:
				langserver.SetNested(section, []string{"inlayHints", o.key}, v)
			case toFormat:
				langserver.SetNested(section, []string{"format", o.key}, v)
			case toCompletions:
				langserver.SetNested(section, []string{"completions", o.key}, v)
			case toSection:
				langserver.SetNested(section, strings.Split(o.key, "."), v)
			}
		}
	}
	if v := s.String(LocaleSetting); v != "" && v != "auto" {
		out.initialization["locale"] = v
	}
	if s.Bool(DisableTypeAcquisitionSetting) {
		out.initialization["disableAutomaticTypingAcquisition"] = true
	}
	if mb := s.Float(MaxMemorySetting); mb > 0 {
		out.initialization["maxTsServerMemory"] = mb
	}
	if npm := s.String(NpmPathSetting); npm != "" {
		out.initialization["npmLocation"] = npm
	}
	for _, l := range languages {
		if len(out.sections[l.prefix].(map[string]any)) == 0 {
			delete(out.sections, l.prefix)
		}
	}
	return out
}

// normalize makes a value comparable with another read from JSON: a nil array and an empty one are the same default.
func normalize(v any) any {
	if list, ok := v.([]any); ok && len(list) == 0 {
		return []any{}
	}
	return v
}
