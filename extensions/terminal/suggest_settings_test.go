package terminal

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

func TestSuggestSettingsHaveVSCodesDefaults(t *testing.T) {
	m := New().Manifest()
	byKey := map[string]extension.SettingSpec{}
	for _, s := range m.Contributes.Settings {
		byKey[s.Key] = s
	}
	tests := []struct {
		key  string
		want any
	}{
		{settingSugEnabled, true},
		{settingSugTrigger, false},
		{settingSugRunEnter, "never"},
		{settingSugSelect, "partial"},
		{settingSugCdPath, "absolute"},
		{settingSugSpace, false},
		{settingSugUpArrow, true},
	}
	for _, tt := range tests {
		s, ok := byKey[tt.key]
		if !ok {
			t.Errorf("%s is not declared", tt.key)
			continue
		}
		if s.Default != tt.want {
			t.Errorf("%s defaults to %v, want %v", tt.key, s.Default, tt.want)
		}
	}
	// VS Code's default of quickSuggestions is all three off (terminalSuggestConfiguration.ts, 1.143)
	q, ok := byKey[settingSugQuick].Default.(map[string]any)
	if !ok || q["commands"] != "off" || q["arguments"] != "off" || q["unknown"] != "off" {
		t.Errorf("quickSuggestions defaults to %v", byKey[settingSugQuick].Default)
	}
	if p, ok := byKey[settingSugProvider].Default.(map[string]any); !ok || len(p) != 3 {
		t.Errorf("providers default to %v", byKey[settingSugProvider].Default)
	}
}

func TestReadConfigReadsTheSuggestSettings(t *testing.T) {
	tests := []struct {
		name string
		set  map[string]any
		want func(c config) bool
	}{
		{"the defaults", nil, func(c config) bool {
			return c.sugEnabled && c.sugQuick == quickSettings{} && !c.sugTriggerChars && c.sugRunOnEnter == "never" &&
				c.sugSelection == "partial" && c.sugCdPath == "absolute" && !c.sugTrailingSpace && c.sugUpHistory && len(c.sugProviders) == 3
		}},
		{"an object", map[string]any{settingSugQuick: map[string]any{"commands": "on", "unknown": "on"}}, func(c config) bool {
			return c.sugQuick == quickSettings{commands: true, unknown: true}
		}},
		{"a true is commands and arguments", map[string]any{settingSugQuick: true}, func(c config) bool {
			return c.sugQuick == quickSettings{commands: true, arguments: true}
		}},
		{"a false is none", map[string]any{settingSugQuick: false}, func(c config) bool { return c.sugQuick == quickSettings{} }},
		{"the other settings", map[string]any{
			settingSugEnabled: false, settingSugTrigger: true, settingSugRunEnter: "always", settingSugSelect: "never",
			settingSugCdPath: "off", settingSugSpace: true, settingSugUpArrow: false, settingSugProvider: map[string]any{providerHistory: false},
		}, func(c config) bool {
			return !c.sugEnabled && c.sugTriggerChars && c.sugRunOnEnter == "always" && c.sugSelection == "never" && c.sugCdPath == "off" &&
				c.sugTrailingSpace && !c.sugUpHistory && c.sugProviders[providerHistory] == false && len(c.sugProviders) == 1
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := fakeapi.New(t, New().Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "p", Folder: t.TempDir()})
			for k, v := range tt.set {
				api.SetSetting(k, v)
			}
			if c := readConfig(api.Settings()); !tt.want(c) {
				t.Errorf("config = %+v", c)
			}
		})
	}
}

func TestTheSuggestCommandsAndKeys(t *testing.T) {
	m := New().Manifest()
	declared := map[string]bool{}
	for _, c := range m.Contributes.Commands {
		declared[c.ID] = true
	}
	skip := newSkipSet(nil)
	for _, id := range []string{CmdTriggerSuggest, CmdHideSuggest, CmdSelectNextSuggest, CmdSelectPrevSuggest, CmdSelectNextPageSuggest,
		CmdSelectPrevPageSuggest, CmdAcceptSuggest, CmdAcceptSuggestEnter, CmdToggleSuggestDetails} {
		if !declared[id] {
			t.Errorf("%s is not a command of the manifest", id)
		}
		// the key of each goes to the terminal's own command, not to the shell
		if !skip.has(id) {
			t.Errorf("%s is not in the commands that skip the shell", id)
		}
	}
	keys := map[string][]string{}
	for _, kb := range m.Contributes.Keybindings {
		keys[kb.Command] = append(keys[kb.Command], kb.Key)
	}
	for id, key := range map[string]string{
		CmdTriggerSuggest: "ctrl+space", CmdHideSuggest: "escape", CmdSelectNextSuggest: "down", CmdSelectPrevSuggest: "up",
		CmdAcceptSuggest: "tab", CmdAcceptSuggestEnter: "enter",
	} {
		if !slices.Contains(keys[id], key) {
			t.Errorf("%s has the keys %v, want %s", id, keys[id], key)
		}
	}
	// the keys that act on the list need it to be visible, so that with no list the shell gets Tab, Enter and the arrows
	for _, kb := range m.Contributes.Keybindings {
		switch kb.Command {
		case CmdAcceptSuggest, CmdAcceptSuggestEnter, CmdSelectNextSuggest, CmdSelectPrevSuggest, CmdHideSuggest:
			if kb.When == "" || !strings.Contains(kb.When, keySuggestVisible) {
				t.Errorf("the key %s of %s has the condition %q", kb.Key, kb.Command, kb.When)
			}
		}
	}
	for _, key := range []string{keySuggestVisible, keySuggestEnabled} {
		if !slices.Contains(m.Contributes.ContextKeys, key) {
			t.Errorf("the context key %s is not declared", key)
		}
	}
}
