package cpp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// What the status item says beyond the state of the process (SPEC §10.34 "C and C++"): clangd's file status, the state of the file in
// front ("idle", "parsing includes", "running action textDocument/hover"), and whether the project has a compilation database.

// fileStatusMethod is the notification clangd sends for each file because the client asked for it (clangdFileStatus in the
// initialization options): {uri, state}.
const fileStatusMethod = "textDocument/clangd.fileStatus"

// idleState is the state of a file clangd has nothing to do for.
const idleState = "idle"

// fileStatus follows the notification.
func (c *cpp) fileStatus(params json.RawMessage) {
	var p struct {
		URI   string `json:"uri"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.URI == "" {
		c.api.Log().Warn("the status of a file of clangd cannot be read", "err", err)
		return
	}
	c.states[p.URI] = p.State
	c.refreshStatus()
}

// serverChanged follows the state of the process: the states of the files belong to the process that told them.
func (c *cpp) serverChanged(st extension.ServerStatus) {
	c.running = st.State == extension.ServerRunning
	if !c.running {
		clear(c.states)
	}
	c.refreshStatus()
}

// refreshStatus draws the status item again: the file status of the file in front, and the warning for a project without a
// compilation database.
func (c *cpp) refreshStatus() {
	if c.ctl == nil {
		return
	}
	label, detail, health := "clangd", "", langserver.Health{}
	if c.running {
		var state string
		if doc, ok := c.api.Documents().Active(); ok && isCpp(doc.LanguageID) {
			state = c.states[doc.URI]
			if state != "" {
				detail = filepath.Base(doc.URI) + ": " + state
			}
		}
		busy := state != "" && state != idleState
		health.Busy = busy
		db := c.database()
		switch {
		case busy:
			label = "clangd: " + state
		case db.missing:
			label = "clangd (no compile_commands.json)"
		}
		if db.missing {
			health.Level, health.Message = langserver.HealthWarning, noDatabaseMessage
		} else if db.found != "" {
			detail = joinLines(detail, "Compilation database: "+db.found)
		}
	}
	c.ctl.SetHealth(health)
	c.ctl.SetDetail(detail)
	c.ctl.SetLabel(label)
}

// joinLines joins the lines that are not empty.
func joinLines(lines ...string) string {
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// noDatabaseMessage is what the tooltip says when clangd has nothing that tells it how the files are compiled.
const noDatabaseMessage = "No compile_commands.json, compile_flags.txt or .clangd with CompileFlags was found, so clangd guesses the flags of each file. " +
	"Create .clangd from the menu, or have the build write compile_commands.json (CMake: -DCMAKE_EXPORT_COMPILE_COMMANDS=ON)."

// databaseState is what the project has to tell clangd how its files are compiled.
type databaseState struct {
	// found is the file that does ("" when none does) and missing says that none does and the user gave no fallback flags either.
	found   string
	missing bool
}

// root is the folder clangd works on: the folder of the tab, or the nearest folder above it with a marker of a project.
func (c *cpp) root() string {
	folder := c.api.Scope().Folder
	home, _ := os.UserHomeDir()
	return langserver.ResolveRoot(folder, rootMarkers, home)
}

// database looks for what tells clangd how the files are compiled, where clangd looks for it (the folder of the project, and its
// build folder), in the folder clangd.arguments name, and in .clangd; a tab with no folder has nothing to say.
func (c *cpp) database() databaseState {
	if c.api.Scope().Folder == "" {
		return databaseState{}
	}
	args, _ := extension.SettingValue[[]string](c.api.Settings(), ArgumentsSetting)
	flags, _ := extension.SettingValue[[]string](c.api.Settings(), FallbackFlagsSetting)
	if found := findDatabase(c.root(), args); found != "" {
		return databaseState{found: found}
	}
	return databaseState{missing: len(flags) == 0}
}

// findDatabase is the file in the project that tells clangd how the files are compiled: compile_commands.json in the folder named by
// --compile-commands-dir, in the root or in its build folder, a compile_flags.txt, or a .clangd with CompileFlags; "" for none.
func findDatabase(root string, args []string) string {
	var candidates []string
	if dir := databaseDir(args); dir != "" {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		candidates = append(candidates, filepath.Join(dir, "compile_commands.json"))
	}
	candidates = append(candidates,
		filepath.Join(root, "compile_commands.json"),
		filepath.Join(root, "build", "compile_commands.json"),
		filepath.Join(root, "compile_flags.txt"))
	for _, p := range candidates {
		if fileExists(p) {
			return p
		}
	}
	if p := filepath.Join(root, ".clangd"); hasCompileFlags(p) {
		return p
	}
	return ""
}

// hasCompileFlags tells whether the .clangd file has a CompileFlags section (a line that starts with it, not a comment).
func hasCompileFlags(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "CompileFlags:") {
			return true
		}
	}
	return false
}

// projectConfigStub is the .clangd the command creates: a CompileFlags section with the warnings on and the usual settings as
// comments to take out.
const projectConfigStub = `# Configuration of clangd for this project (https://clangd.llvm.org/config).
# Emetos created it because the project has no compile_commands.json or compile_flags.txt.
CompileFlags:
  Add: [-Wall]
  # Flags for every file, such as the standard and the include folders:
  # Add: [-std=c++20, -I include, -DNAME=value]
  # The folder of the compile_commands.json your build writes:
  # CompilationDatabase: build
`

// projectConfig is Open project configuration file: .clangd in the root of the project opens, or the user is offered to create it
// with a CompileFlags stub.
func (c *cpp) projectConfig() {
	const what = "Open project configuration file"
	if c.api.Scope().Folder == "" {
		c.api.Notifications().Info(what + " needs a folder: open one first.")
		return
	}
	path := filepath.Join(c.root(), ".clangd")
	if fileExists(path) {
		c.openFile(path)
		return
	}
	items := []extension.QuickPickItem{
		{Label: "$(new-file) Create .clangd", Description: "with a CompileFlags section to fill in", Detail: path},
		{Label: "$(close) Cancel"},
	}
	err := c.api.QuickPick().Show(extension.QuickPickOptions{Title: what, Placeholder: "There is no .clangd in this project", Items: items}, func(i int) {
		if i != 0 {
			return
		}
		if err := createProjectConfig(path); err != nil {
			c.api.Log().Warn("the .clangd cannot be created", "path", path, "err", err)
			c.api.Notifications().Error(what + ": " + describe(err))
			return
		}
		c.openFile(path)
	})
	if err != nil {
		c.api.Log().Warn("the offer to create .clangd cannot be shown", "err", err)
		c.api.Notifications().Error(what + ": " + describe(err))
	}
}

// createProjectConfig writes the stub to a new file; a file that is there already is not touched.
func createProjectConfig(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return errors.NewErr("ClangdConfigCreateFailed", err).SetMetadata("path", path)
	}
	_, err = f.WriteString(projectConfigStub)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return errors.NewErr("ClangdConfigCreateFailed", err).SetMetadata("path", path)
	}
	return nil
}

// openFile opens the file in the editor.
func (c *cpp) openFile(path string) {
	if err := c.api.Documents().Show("file://"+path, 1, 1, false); err != nil {
		c.api.Log().Warn("a file cannot be shown", "path", path, "err", err)
		c.api.Notifications().Error("Cannot open " + filepath.Base(path) + ": " + describe(err))
	}
}
