package typescript

import (
	"encoding/json"
	"testing"
)

// What the settings become for the server: the preferences it starts with, and the sections of each language it is told after it started.

func TestTheSettingsThatAreNotDefaultsGoToTheServer(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		// prefs, other and sections are the JSON of initializationOptions.preferences, of the rest of the options besides the tsserver path
		// and hostInfo, and of the settings sent with didChangeConfiguration
		prefs, other, sections string
	}{
		{"nothing is set: only what the pack always sends", nil, `null`, `{"maxTsServerMemory":3072}`, `null`},
		{
			"the format of each language", map[string]any{"typescript.format.semicolons": "remove", "javascript.format.semicolons": "insert", "typescript.format.insertSpaceAfterCommaDelimiter": false},
			`null`, `{"maxTsServerMemory":3072}`,
			`{"javascript":{"format":{"semicolons":"insert"}},"typescript":{"format":{"insertSpaceAfterCommaDelimiter":false,"semicolons":"remove"}}}`,
		},
		{
			"the inlay hints, and the two that are asked the other way round", map[string]any{
				"typescript.inlayHints.parameterNames.enabled": "all", "typescript.inlayHints.parameterNames.suppressWhenArgumentMatchesName": false,
				"typescript.inlayHints.variableTypes.enabled": true, "typescript.inlayHints.variableTypes.suppressWhenTypeMatchesName": false,
				"javascript.inlayHints.functionLikeReturnTypes.enabled": true, "typescript.inlayHints.enumMemberValues.enabled": true,
			},
			`null`, `{"maxTsServerMemory":3072}`,
			`{"javascript":{"inlayHints":{"includeInlayFunctionLikeReturnTypeHints":true}},"typescript":{"inlayHints":{"includeInlayEnumMemberValueHints":true,` +
				`"includeInlayParameterNameHints":"all","includeInlayParameterNameHintsWhenArgumentMatchesName":true,"includeInlayVariableTypeHints":true,` +
				`"includeInlayVariableTypeHintsWhenTypeMatchesName":true}}}`,
		},
		{
			"the preferences are the server's: TypeScript's value wins when it differs from the default, else JavaScript's", map[string]any{
				"typescript.preferences.quoteStyle": "single", "javascript.preferences.quoteStyle": "double",
				"javascript.preferences.importModuleSpecifier":         "relative",
				"typescript.preferences.importModuleSpecifierEnding":   "js",
				"typescript.preferences.autoImportFileExcludePatterns": []any{"**/dist/**"},
				"typescript.preferences.useAliasesForRenames":          false,
				"typescript.preferences.includePackageJsonAutoImports": "off",
				"javascript.suggest.autoImports":                       false,
			},
			`{"autoImportFileExcludePatterns":["**/dist/**"],"importModuleSpecifierEnding":"js","importModuleSpecifierPreference":"relative",` +
				`"includeCompletionsForModuleExports":false,"includePackageJsonAutoImports":"off","providePrefixAndSuffixTextForRename":false,"quotePreference":"single"}`,
			`{"maxTsServerMemory":3072}`, `null`,
		},
		{
			"the code lenses and the completion of calls", map[string]any{
				"typescript.referencesCodeLens.enabled": true, "typescript.referencesCodeLens.showOnAllFunctions": true,
				"javascript.implementationsCodeLens.enabled": true, "typescript.suggest.completeFunctionCalls": true,
			},
			`null`, `{"maxTsServerMemory":3072}`,
			`{"javascript":{"implementationsCodeLens":{"enabled":true}},"typescript":{"completions":{"completeFunctionCalls":true},"referencesCodeLens":{"enabled":true,"showOnAllFunctions":true}}}`,
		},
		{
			"the settings of the server itself", map[string]any{
				LocaleSetting: "de", DisableTypeAcquisitionSetting: true, MaxMemorySetting: float64(8192), NpmPathSetting: "/opt/npm/bin/npm",
			},
			`null`, `{"disableAutomaticTypingAcquisition":true,"locale":"de","maxTsServerMemory":8192,"npmLocation":"/opt/npm/bin/npm"}`, `null`,
		},
		{"locale auto is not sent", map[string]any{LocaleSetting: "auto"}, `null`, `{"maxTsServerMemory":3072}`, `null`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{globalTS: "6.0.3", settings: tt.settings})
			l, err := r.run()
			if err != nil {
				t.Fatal(err)
			}
			var init map[string]json.RawMessage
			if err := json.Unmarshal([]byte(launchOptions(t, l)), &init); err != nil {
				t.Fatal(err)
			}
			prefs := string(init["preferences"])
			if prefs == "" {
				prefs = "null"
			}
			if prefs != tt.prefs {
				t.Errorf("preferences = %s\nwant %s", prefs, tt.prefs)
			}
			if string(init["hostInfo"]) != `"emetos"` {
				t.Errorf("hostInfo = %s", init["hostInfo"])
			}
			delete(init, "preferences")
			delete(init, "hostInfo")
			delete(init, "tsserver")
			other, _ := json.Marshal(init)
			if string(other) != tt.other {
				t.Errorf("options = %s\nwant %s", other, tt.other)
			}
			if got := launchSettings(t, l); got != tt.sections {
				t.Errorf("settings = %s\nwant %s", got, tt.sections)
			}
		})
	}
}

func TestTheIndentationOfTheEditsIsTheEditorsOwn(t *testing.T) {
	// the fake API has no editor settings: the defaults of the editor are used
	r := newRig(t, rigOptions{globalTS: "6.0.3"})
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := l.Configuration("formattingOptions")
	if !ok || string(raw) != `{"insertSpaces":true,"tabSize":4}` {
		t.Errorf("formattingOptions = %s %v", raw, ok)
	}
	if _, ok := l.Configuration("typescript"); ok {
		t.Error("the section typescript is answered by the pack: it belongs to the settings")
	}
}

func TestEveryOptionHasAUniqueSettingAndATarget(t *testing.T) {
	seen := map[string]bool{}
	for _, o := range options() {
		if o.name == "" || o.typ == "" || o.desc == "" || o.key == "" {
			t.Errorf("option %+v is incomplete", o)
		}
		if seen[o.name] {
			t.Errorf("option %s twice", o.name)
		}
		seen[o.name] = true
		if o.invert && o.typ != "boolean" {
			t.Errorf("option %s is inverted and not a boolean", o.name)
		}
	}
}
