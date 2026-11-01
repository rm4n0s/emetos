package rust

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The settings of rust-analyzer (SPEC §10.34 "Rust"): the settings of its VS Code extension that change what the server does, with its
// names, types, defaults, enums and descriptions. They are registered, and the ones that have another value than their default are sent
// to the server as nested options (cargo.features becomes {"cargo": {"features": ...}}), when it starts and whenever one changes (the
// server is restarted, SPEC §10.34 "As built (step 24b.1)").

// settingsCategory is the group of the settings editor, the title VS Code's extension gives its settings.
const settingsCategory = "Rust Analyzer"

// raSettings is the list written by scripts/update-rust-analyzer-settings.sh from the package.json of rust-analyzer's VS Code
// extension (MIT OR Apache-2.0, third_party/rust-analyzer).
//
//go:embed ra-settings.json
var raSettings []byte

// raSetting is one setting of the list.
type raSetting struct {
	Key              string   `json:"key"`
	Type             string   `json:"type"`
	AlsoTypes        []string `json:"alsoTypes"`
	Default          any      `json:"default"`
	Enum             []any    `json:"enum"`
	EnumDescriptions []string `json:"enumDescriptions"`
	Description      string   `json:"description"`
	Minimum          *float64 `json:"minimum"`
	Maximum          *float64 `json:"maximum"`
}

// loadSettings reads the embedded list; it fails only if the file is damaged (a test checks it).
func loadSettings() ([]raSetting, error) {
	var list []raSetting
	if err := json.Unmarshal(raSettings, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// settingSpecs are the settings the manifest declares: the path of the program is the finder's (langserver.Config.SettingSpecs) and
// the trace is the host's (Config.TraceKey).
func settingSpecs() []extension.SettingSpec {
	list, err := loadSettings()
	if err != nil {
		return nil
	}
	specs := make([]extension.SettingSpec, 0, len(list))
	for _, s := range list {
		specs = append(specs, extension.SettingSpec{
			Key: s.Key, Type: s.Type, AlsoTypes: s.AlsoTypes, Default: s.Default, Description: s.Description,
			Enum: s.Enum, EnumDescriptions: s.EnumDescriptions, Minimum: s.Minimum, Maximum: s.Maximum, Category: settingsCategory,
		})
	}
	return specs
}

// serverOptions are the options rust-analyzer is initialized with and answered when it asks for the section rust-analyzer: the
// settings that are not at their default, nested by the dots of their names. Every setting of the list is read, so that a change of
// any of them restarts the server (langserver.Settings remembers the keys read). A setting at its default is left out, so that the
// server's own defaults, which change with its version, are the ones that apply.
func serverOptions(s *langserver.Settings) map[string]any {
	opts := map[string]any{}
	list, err := loadSettings()
	if err != nil {
		return opts
	}
	for _, set := range list {
		v, ok := s.Value(set.Key)
		if !ok || langserver.Fingerprint(v, true) == langserver.Fingerprint(set.Default, true) {
			continue
		}
		setNested(opts, strings.Split(strings.TrimPrefix(set.Key, SettingPrefix), "."), v)
	}
	return opts
}

// setNested puts the value at the path of keys in the nested maps. A path that would replace a value, or go through one, is left out
// (a setting whose name is the start of another's cannot be both).
func setNested(m map[string]any, path []string, v any) {
	for _, key := range path[:len(path)-1] {
		next, ok := m[key]
		if !ok {
			child := map[string]any{}
			m[key] = child
			m = child
			continue
		}
		child, isMap := next.(map[string]any)
		if !isMap {
			return
		}
		m = child
	}
	last := path[len(path)-1]
	if _, taken := m[last]; taken {
		return
	}
	m[last] = v
}
