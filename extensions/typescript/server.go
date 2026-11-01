package typescript

import (
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// typescript-language-server for the TypeScript and JavaScript packs (SPEC §10.34 "TypeScript and JavaScript"). The TypeScript pack owns
// the server; it serves the JavaScript pack's languages too (decision 980).

// The ids the pack uses.
const (
	// ID is the id of the extension, and ServerID the id of its language server in the manifest.
	ID       = "emetos.typescript"
	ServerID = "typescript"
	// PathSetting holds the path of the typescript-language-server program.
	PathSetting = "typescript.languageServer.path"
	// StateKey is the key of the state under which the TypeScript the user chose for the folder is kept (ChoiceGlobal or ChoiceWorkspace).
	StateKey = "typescript.version"
)

// serverLanguages are the languages the server serves: those of this pack and of the JavaScript pack.
var serverLanguages = []string{"typescript", "typescriptreact", "javascript", "javascriptreact"}

// isServed tells whether the language is one the server serves.
func isServed(languageID string) bool {
	for _, l := range serverLanguages {
		if l == languageID {
			return true
		}
	}
	return false
}

// rootMarkers are the files that mark a TypeScript or JavaScript project: the server is started in the nearest folder that holds one.
var rootMarkers = []string{"tsconfig.json", "jsconfig.json", "package.json"}

// serverConfig describes typescript-language-server for extensions/internal/langserver.
func (t *typescript) serverConfig() *langserver.Config {
	return &langserver.Config{
		ID: ServerID, Name: "TypeScript", Languages: serverLanguages, Category: "TypeScript",
		Purpose: "type checking, diagnostics, completion, hover, go to definition, rename, organize imports and code actions for TypeScript and JavaScript",
		Programs: []langserver.Program{{
			// the folder's node_modules/.bin is not searched: it holds programs the folder brought (decision 982); the path setting can name one
			Name: "typescript-language-server", PathSetting: PathSetting, Args: []string{"--stdio"},
			// typescript-language-server --version prints "6.0.2"; node 22.22.2 or newer is the server's own check
			Version: langserver.VersionCheck{Args: []string{"--version"}},
			Install: langserver.Install{
				Docs: "https://github.com/typescript-language-server/typescript-language-server#installing",
				Commands: []langserver.InstallCommand{
					{Family: langserver.FamilyAny, Command: "npm install -g typescript-language-server typescript@6"},
				},
			},
		}},
		RootMarkers:   rootMarkers,
		Customize:     t.customize,
		TraceKey:      TraceSetting,
		WatchedFiles:  []string{"**/tsconfig.json", "**/jsconfig.json", "**/package.json"},
		SettingsQuery: "typescript",
		Menu: []langserver.MenuEntry{
			{Label: "$(versions) Select TypeScript Version...", Description: "the TypeScript the server uses", Run: t.selectVersion},
			{Label: "$(go-to-file) Go to Project Configuration", Description: "the tsconfig.json or jsconfig.json of the file in front", Run: func() { t.goToProjectConfig() }},
		},
	}
}

// resolveNode finds the node program that runs the server: typescript.nodePath, else the first node on PATH. It fails with
// TypeScriptNodeInvalid for a setting that is wrong and TypeScriptNodeNotFound when there is none.
func resolveNode(env langserver.Env, setting string) (string, error) {
	if setting != "" {
		if why := langserver.CheckPath(env, setting); why != "" {
			return "", errors.New("TypeScriptNodeInvalid", NodePathSetting+" is set to "+setting+": "+why, "setting", NodePathSetting, "value", setting)
		}
		expanded := env.Expand(setting)
		if strings.Contains(expanded, "/") {
			if !filepath.IsAbs(expanded) {
				expanded = filepath.Join(env.Folder, expanded)
			}
			return expanded, nil
		}
		if path, ok := langserver.FindExecutable(expanded, filepath.SplitList(env.Path)); ok {
			return path, nil
		}
		return "", errors.New("TypeScriptNodeInvalid", NodePathSetting+" is set to "+setting+", which is not a program in PATH", "setting", NodePathSetting, "value", setting)
	}
	if path, ok := langserver.FindExecutable("node", filepath.SplitList(env.Path)); ok {
		return path, nil
	}
	return "", errors.New("TypeScriptNodeNotFound", "node was not found in PATH: typescript-language-server is a Node.js program. Install Node.js 22.22.2 or newer, or set "+NodePathSetting,
		"path", env.Path)
}

// customize is Config.Customize: it checks that node is there, names the TypeScript the server uses, and gives it the settings that are not
// at their default. It runs on the launcher's goroutine.
func (t *typescript) customize(l *langserver.Launch, env langserver.Env) error {
	t.forgetPlan()
	s := env.Settings
	nodeSetting := s.String(NodePathSetting)
	node, err := resolveNode(env, nodeSetting)
	if err != nil {
		return err
	}
	if nodeSetting != "" {
		// the script is run by the node the user named, not by the one its first line asks for
		real, err := filepath.EvalSymlinks(l.Found.Path)
		if err != nil {
			real = l.Found.Path
		}
		l.Command = node
		l.Args = append([]string{real}, l.Args...)
	}

	chosen, _ := extension.StateValue[string](t.api.State(), StateKey)
	plan, err := Decide(env, l.Found.Path, l.Root, s.String(TsdkSetting), chosen)
	if err != nil {
		return err
	}
	t.setPlan(plan)

	ss := readSettings(s)
	init := map[string]any{"hostInfo": "emetos", "tsserver": map[string]any{"path": plan.Use.Server()}}
	if len(ss.preferences) > 0 {
		init["preferences"] = ss.preferences
	}
	for k, v := range ss.initialization {
		init[k] = v
	}
	l.InitializationOptions = init
	if len(ss.sections) > 0 {
		l.Settings = ss.sections
	}
	l.Configuration = map[string]any{"formattingOptions": formattingOptions(s)}
	return nil
}

// formattingOptions are the tab size and the indentation the server's edits (organize imports, a code action) are written with: the
// editor's settings, which tsserver asks the client for (workspace/configuration, section formattingOptions).
func formattingOptions(s *langserver.Settings) map[string]any {
	size := s.Float("editor.tabSize")
	if size < 1 {
		size = 4
	}
	v, ok := s.Value("editor.insertSpaces")
	spaces, isBool := v.(bool)
	if !ok || !isBool {
		spaces = true
	}
	return map[string]any{"tabSize": size, "insertSpaces": spaces}
}
