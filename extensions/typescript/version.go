package typescript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The TypeScript the language server uses (SPEC §10.34 "As built (step 24b.5)"). typescript-language-server brings none of its own: it
// runs the tsserver.js of a TypeScript package, and left to itself it takes the one in the node_modules of the folder before any other.
// That is a program the folder brought, so Emetos always names the one to use (initializationOptions.tsserver.path) and takes the folder's
// own only when the user chose it (decision 981; the rule of decision 961 for a folder's Python environment).

// The sources of a Version.
const (
	// SourceGlobal is the TypeScript installed beside the language server (the one `npm install -g typescript` puts there).
	SourceGlobal = "global"
	// SourceTsdk is the folder typescript.tsdk names.
	SourceTsdk = "typescript.tsdk"
	// SourceWorkspace is the node_modules/typescript of the folder (or of a folder above it).
	SourceWorkspace = "workspace"
)

// The choices kept in the state under StateKey.
const (
	// ChoiceGlobal uses the TypeScript beside the server (or typescript.tsdk); ChoiceWorkspace the folder's own.
	ChoiceGlobal    = "global"
	ChoiceWorkspace = "workspace"
)

// Version is a TypeScript package that has a tsserver.js.
type Version struct {
	// Source is where it was found: SourceGlobal, SourceTsdk or SourceWorkspace.
	Source string
	// Lib is the folder that holds tsserver.js (.../node_modules/typescript/lib) and Number the version its package.json tells
	// ("5.9.3"; empty when it cannot be read).
	Lib, Number string
}

// Server is the path of the tsserver.js of the version.
func (v Version) Server() string { return filepath.Join(v.Lib, "tsserver.js") }

// Label is how the status item and the picker write the version: "TypeScript 5.9.3", with "(workspace)" for the folder's own.
func (v Version) Label() string {
	label := "TypeScript"
	if v.Number != "" {
		label += " " + v.Number
	}
	if v.Source == SourceWorkspace {
		label += " (workspace)"
	}
	return label
}

// packageVersion reads the version of the package that lib belongs to.
func packageVersion(lib string) string {
	b, err := os.ReadFile(filepath.Join(filepath.Dir(lib), "package.json"))
	if err != nil {
		return ""
	}
	var p struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &p) != nil || p.Name != "typescript" {
		return ""
	}
	return p.Version
}

// hasServer tells whether the lib folder has a tsserver.js (TypeScript 7 is the native compiler and has none).
func hasServer(lib string) bool {
	info, err := os.Stat(filepath.Join(lib, "tsserver.js"))
	return err == nil && info.Mode().IsRegular()
}

// versionAt makes the Version of a lib folder, false when it has no tsserver.js.
func versionAt(source, lib string) (Version, bool) {
	if !hasServer(lib) {
		return Version{}, false
	}
	return Version{Source: source, Lib: lib, Number: packageVersion(lib)}, true
}

// WorkspaceVersion is the TypeScript of the node_modules of the folder root or of the nearest folder above it that has one, climbing as
// ResolveRoot does: not past the top of a repository (the folder with .git), the home folder or the root of the file system, and twelve
// folders at most. It reads files and runs nothing.
func WorkspaceVersion(root, home string) (Version, bool) {
	if root == "" {
		return Version{}, false
	}
	dir := filepath.Clean(root)
	home = filepath.Clean(home)
	for range 13 {
		if dir == home {
			return Version{}, false
		}
		if v, ok := versionAt(SourceWorkspace, filepath.Join(dir, "node_modules", "typescript", "lib")); ok {
			return v, true
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return Version{}, false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Version{}, false
		}
		dir = parent
	}
	return Version{}, false
}

// BesideServer is the TypeScript node would find from the language server's own file: the node_modules/typescript of the folders above the
// real file of the server (the link in a bin folder is followed), as require.resolve('typescript') looks for it. It reads files and runs
// nothing.
func BesideServer(server string) (Version, bool) {
	real, err := filepath.EvalSymlinks(server)
	if err != nil {
		return Version{}, false
	}
	dir := filepath.Dir(real)
	for {
		if filepath.Base(dir) != "node_modules" {
			if v, ok := versionAt(SourceGlobal, filepath.Join(dir, "node_modules", "typescript", "lib")); ok {
				return v, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Version{}, false
		}
		dir = parent
	}
}

// TsdkVersion resolves typescript.tsdk, which names the lib folder, the package folder or the tsserver.js itself. It fails with
// TypeScriptTsdkInvalid when the value is none of them.
func TsdkVersion(env langserver.Env, value string) (Version, error) {
	path := env.Expand(value)
	if !filepath.IsAbs(path) && env.Folder != "" {
		path = filepath.Join(env.Folder, path)
	}
	candidates := []string{path, filepath.Join(path, "lib")}
	if filepath.Base(path) == "tsserver.js" {
		candidates = []string{filepath.Dir(path)}
	}
	for _, lib := range candidates {
		if v, ok := versionAt(SourceTsdk, lib); ok {
			return v, nil
		}
	}
	return Version{}, errors.New("TypeScriptTsdkInvalid", TsdkSetting+" is set to "+value+", which is not the lib folder of a TypeScript with a tsserver.js", "setting", TsdkSetting, "value", value, "path", path)
}

// Plan is the TypeScript decision of one start of the server.
type Plan struct {
	// Use is the version the server is told to use.
	Use Version
	// Global is the version of the user's own installation (typescript.tsdk, else beside the server) when there is one, and Workspace
	// the folder's own when it has one.
	Global, Workspace *Version
}

// Decide chooses the TypeScript for a start. chosen is what the user chose for the folder (ChoiceGlobal, ChoiceWorkspace or ""), tsdk
// the value of typescript.tsdk and server the path of the language server's file. The folder's own TypeScript is used only when chosen
// says so (and it is there); otherwise the global one, which fails with TypeScriptNotFound when there is none, and the message says that
// the folder has its own, if it does. It fails with TypeScriptTsdkInvalid for a typescript.tsdk that is wrong.
func Decide(env langserver.Env, server, root, tsdk, chosen string) (Plan, error) {
	var p Plan
	if tsdk != "" {
		v, err := TsdkVersion(env, tsdk)
		if err != nil {
			return Plan{}, err
		}
		p.Global = &v
	} else if v, ok := BesideServer(server); ok {
		p.Global = &v
	}
	if v, ok := WorkspaceVersion(root, env.Home); ok {
		p.Workspace = &v
	}
	switch {
	case chosen == ChoiceWorkspace && p.Workspace != nil:
		p.Use = *p.Workspace
	case p.Global != nil:
		p.Use = *p.Global
	case p.Workspace != nil:
		return Plan{}, errors.New("TypeScriptNotFound", "no TypeScript was found beside the language server, and typescript.tsdk is not set. This folder has its own TypeScript, "+p.Workspace.Number+
			", which Emetos runs only if you choose it with TypeScript: Select TypeScript Version",
			"server", server, "workspace", p.Workspace.Lib)
	default:
		return Plan{}, errors.New("TypeScriptNotFound", "no TypeScript was found beside the language server, and typescript.tsdk is not set. Install it with npm install -g typescript@6", "server", server)
	}
	return p, nil
}

// short writes a path for a picker: relative to the folder when it is inside, ~ for the home folder.
func short(env langserver.Env, path string) string {
	for _, base := range []struct{ dir, mark string }{{env.Folder, ""}, {env.Home, "~"}} {
		if base.dir == "" {
			continue
		}
		rel, err := filepath.Rel(base.dir, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if base.mark == "" {
				return rel
			}
			return filepath.Join(base.mark, rel)
		}
	}
	return path
}
