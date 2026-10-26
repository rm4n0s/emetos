package golang

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// The keys of the settings of the Go extension (SPEC §10.17). Their names, defaults and descriptions are those of the Go
// extension of VS Code (golang/vscode-go), where they exist there.
const (
	// GoplsSetting is the object of settings the language server reads (the section the server is asked for).
	GoplsSetting = "go.gopls"
	// GoplsPathSetting is the path of gopls when it is not in PATH; the host registers it (SPEC §9.12).
	GoplsPathSetting = "go.goplsPath"
	// AlternateToolsSetting maps a tool ("go", "dlv", "staticcheck") to another program or path.
	AlternateToolsSetting = "go.alternateTools"
	// ToolsManagementGoSetting is the go program that installs the tools; ToolsGopathSetting is the GOPATH they are installed in
	// (their GOBIN is its bin folder) and ToolsEnvVarsSetting the environment of the programs the extension runs.
	ToolsManagementGoSetting = "go.toolsManagement.go"
	ToolsGopathSetting       = "go.toolsGopath"
	ToolsEnvVarsSetting      = "go.toolsEnvVars"
)

// category is the group of the settings editor.
const category = "Go"

// goplsSettings is the settings of gopls as VS Code's Go extension lists them (extensions/golang/gopls-settings.json, written by
// scripts/update-assets.sh from its package.json): the name of each, its type, default, enum and description.
//
//go:embed gopls-settings.json
var goplsSettings []byte

// goplsProperty is one setting of gopls.
type goplsProperty struct {
	Type        any    `json:"type"`
	Default     any    `json:"default"`
	Enum        []any  `json:"enum"`
	Description string `json:"description"`
}

// goplsProperties reads the embedded list.
func goplsProperties() (map[string]goplsProperty, error) {
	var props map[string]goplsProperty
	if err := json.Unmarshal(goplsSettings, &props); err != nil {
		return nil, err
	}
	return props, nil
}

// goplsIntro opens the description of go.gopls.
const goplsIntro = "Configure the default Go language server ('gopls'). In most cases, configuring this section is unnecessary. " +
	"See [the documentation](https://github.com/golang/tools/blob/master/gopls/doc/settings.md) for all available settings.\n\n" +
	"Write the settings as gopls names them, for example `\"go.gopls\": {\"ui.semanticTokens\": true}`. The object is sent to the server " +
	"as it is, when it starts and whenever it changes."

// goplsDescription is the description of go.gopls: the introduction and one line for each setting of gopls with its type and
// default, as the Go extension of VS Code lists them.
func goplsDescription() string {
	props, err := goplsProperties()
	if err != nil {
		return goplsIntro
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	var sb strings.Builder
	sb.WriteString(goplsIntro)
	sb.WriteString("\n\nThe settings of gopls, with their defaults:\n")
	for _, name := range names {
		p := props[name]
		def, _ := json.Marshal(p.Default)
		fmt.Fprintf(&sb, "\n- `%s` (%s, default `%s`", name, typeName(p.Type), def)
		if len(p.Enum) > 0 {
			var values []string
			for _, v := range p.Enum {
				values = append(values, fmt.Sprintf("`%v`", v))
			}
			sb.WriteString("; one of " + strings.Join(values, ", "))
		}
		sb.WriteString("): " + firstParagraph(p.Description))
	}
	return sb.String()
}

// typeName is the JSON type of a property: a name, or the names a list gives joined with "|".
func typeName(t any) string {
	switch v := t.(type) {
	case string:
		return v
	case []any:
		var names []string
		for _, n := range v {
			names = append(names, fmt.Sprint(n))
		}
		return strings.Join(names, "|")
	}
	return "any"
}

// maxSummary is how much of the description of a setting of gopls the list keeps.
const maxSummary = 220

// firstParagraph is the first paragraph of a description on one line, cut at maxSummary characters.
func firstParagraph(text string) string {
	para, _, _ := strings.Cut(strings.TrimSpace(text), "\n\n")
	para = strings.Join(strings.Fields(para), " ")
	if r := []rune(para); len(r) > maxSummary {
		para = strings.TrimSpace(string(r[:maxSummary])) + "..."
	}
	return para
}

// settingSpecs are the settings the extension declares in its manifest.
func settingSpecs() []extension.SettingSpec {
	return []extension.SettingSpec{
		{Key: GoplsSetting, Type: "object", Default: map[string]any{}, Category: category, Description: goplsDescription()},
		{
			Key: AlternateToolsSetting, Type: "object", Default: map[string]any{}, Category: category,
			Description: "Alternate tools or alternate paths for the same tools used by the Go extension. Provide either an absolute path or " +
				"the name of the program in PATH. Useful when you want to use a wrapper script for the Go tools. The names the extension " +
				"reads are `go` (the go program), `dlv` and `staticcheck`; gopls has its own setting, `go.goplsPath`.",
		},
		{
			Key: ToolsManagementGoSetting, Type: "string", Default: "", Category: category,
			Description: "The path to the `go` program used to install the Go tools. If it's empty, the same `go` program chosen for the project will be used for tool installation.",
		},
		{
			Key: ToolsGopathSetting, Type: "string", Default: "", Category: category,
			Description: "Location to install the Go tools that the extension depends on if you don't want them in your GOPATH. The tools are installed in its `bin` folder; empty uses the GOPATH of the `go` program.",
		},
		{
			Key: ToolsEnvVarsSetting, Type: "object", Default: map[string]any{}, Category: category,
			Description: "Environment variables that will be passed to the programs the Go extension runs (`go env`, `go install`), such as `CGO_CFLAGS`. Format as string key:value pairs.",
		},
	}
}
