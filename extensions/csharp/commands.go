package csharp

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The commands of the C# pack (SPEC §10.34 "C#"). "C#: Restart Language Server" is the command every pack with a server has
// (extension.RestartServerCommand, registered by the host), so the pack adds only the solution.

// SelectSolutionCommand lets the user choose the solution or project csharp-ls loads ("C#: Select Solution").
const SelectSolutionCommand = "csharp.selectSolution"

// commandSpecs are the commands the manifest declares.
func commandSpecs() []extension.CommandSpec {
	return []extension.CommandSpec{
		{ID: SelectSolutionCommand, Title: "Select Solution", Category: category, Description: "Chooses the solution or project csharp-ls loads for this folder."},
	}
}

// handlers are the commands of the pack and what runs them.
func (c *csharp) handlers() map[string]extension.CommandHandler {
	return map[string]extension.CommandHandler{
		SelectSolutionCommand: func(_ context.Context, args any) error {
			if file, ok := args.(string); ok && file != "" {
				c.chooseFromCommand(file)
				return nil
			}
			c.selectSolution()
			return nil
		},
	}
}

// benign tells whether an error only says that the host has no such UI or the extension ended.
func benign(err error) bool {
	appErr, ok := errors.FromError(err)
	return ok && (strings.HasSuffix(appErr.Tag, "Unavailable") || appErr.Tag == "ExtensionDeactivated")
}

// describeErr is the text of an error for the user.
func describeErr(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}

// hostOnly says, to a client of a hosted workspace, that the solution is the host's choice (SPEC §10.18); it reports whether the call is
// the host's.
func (c *csharp) hostOnly() bool {
	if c.api.Origins().Current() != extension.HostOrigin {
		c.api.Notifications().Info("The C# solution is a choice of the host and cannot be changed from here.")
		return false
	}
	return true
}

// choose makes the file the solution or project of this folder, keeps the choice in the state, and restarts the language server, which then
// loads it. An empty file forgets the choice, and the rules of resolveTarget decide again. It fails with CSharpSolutionNotLoadable when the file is
// no solution or project, and with the error of State().Set. UI thread.
func (c *csharp) choose(file string) error {
	if file == "" {
		if err := c.api.State().Set(StateKey, nil); err != nil {
			return err
		}
	} else {
		info, err := os.Stat(file)
		switch {
		case err != nil:
			return errors.NewErr("CSharpSolutionNotLoadable", err).SetMetadata("path", file)
		case info.IsDir() || !isLoadable(file):
			return errors.New("CSharpSolutionNotLoadable", file+" is not a .sln, .slnx or .csproj file", "path", file)
		}
		if err := c.api.State().Set(StateKey, file); err != nil {
			return err
		}
	}
	c.offered = true // the user has just decided; no question about it after the restart
	if err := c.api.LanguageServers().Restart(ServerID); err != nil && !benign(err) {
		c.api.Log().Warn("csharp-ls cannot be restarted after another solution was chosen", "err", err)
	}
	return nil
}

// chooseFromCommand is the command with a file as its argument: the choice without the picker.
func (c *csharp) chooseFromCommand(file string) {
	if !c.hostOnly() {
		return
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(c.api.Scope().Folder, file)
	}
	if err := c.choose(filepath.Clean(file)); err != nil {
		c.api.Notifications().Error("The solution cannot be chosen: " + describeErr(err))
	}
}

// candidate is a row of the picker: a solution or project of the folder.
type candidate struct {
	file string
}

// candidates lists the solutions and projects the user can choose: those of the nearest folder above that holds one, and those at or below
// the folder, solutions before projects. It reads the disk, so it runs off the UI thread.
func candidates(env langserver.Env) []candidate {
	seen := map[string]bool{}
	var solutions, projects []string
	add := func(files []string) {
		for _, f := range files {
			if seen[f] {
				continue
			}
			seen[f] = true
			if isSolution(f) {
				solutions = append(solutions, f)
			} else {
				projects = append(projects, f)
			}
		}
	}
	if env.Folder == "" {
		return nil
	}
	if root := langserver.ResolveRoot(env.Folder, solutionMarkers, env.Home); langserver.HasMarker(root, solutionMarkers) {
		add(filesIn(root, solutionMarkers))
	}
	add(below(env.Folder, solutionMarkers))
	if root := langserver.ResolveRoot(env.Folder, projectMarkers, env.Home); langserver.HasMarker(root, projectMarkers) {
		add(filesIn(root, projectMarkers))
	}
	add(below(env.Folder, projectMarkers))
	sort.SliceStable(solutions, func(i, j int) bool { return depth(solutions[i]) < depth(solutions[j]) })
	sort.SliceStable(projects, func(i, j int) bool { return depth(projects[i]) < depth(projects[j]) })
	out := make([]candidate, 0, len(solutions)+len(projects))
	for _, f := range append(solutions, projects...) {
		out = append(out, candidate{file: f})
	}
	return out
}

// depth is how many folders a path has; the nearer to the root, the earlier in the list.
func depth(p string) int { return strings.Count(p, string(filepath.Separator)) }

// selectSolution is "C#: Select Solution": the solutions and projects are found off the UI thread, then listed in a picker. A client of a
// hosted workspace is told the solution is the host's choice.
func (c *csharp) selectSolution() {
	if !c.hostOnly() {
		return
	}
	folder := c.api.Scope().Folder
	if folder == "" {
		c.api.Notifications().Info("Select Solution needs a folder: open one first.")
		return
	}
	env := langserver.DefaultEnv(folder, nil)
	c.work.Add(1)
	go func() {
		defer c.work.Done()
		found := candidates(env)
		c.api.Post(func() { c.showPicker(folder, found) })
	}()
}

// showPicker lists the candidates, the one in use marked, and the way to forget the choice.
func (c *csharp) showPicker(folder string, found []candidate) {
	c.mu.Lock()
	inUse := c.current.File
	c.mu.Unlock()
	type row struct {
		item extension.QuickPickItem
		file string
		run  func()
	}
	var rows []row
	if c.chosen() != "" {
		rows = append(rows, row{item: extension.QuickPickItem{Label: "$(discard) Use the automatic choice", Description: "forget the choice for this folder"}, run: func() {
			if err := c.choose(""); err != nil {
				c.api.Notifications().Error("The choice cannot be forgotten: " + describeErr(err))
			}
		}})
	}
	for _, f := range found {
		icon := "$(project)"
		if isSolution(f.file) {
			icon = "$(root-folder)"
		}
		if f.file == inUse {
			icon = "$(check)"
		}
		rows = append(rows, row{item: extension.QuickPickItem{Label: icon + " " + filepath.Base(f.file), Description: relative(folder, filepath.Dir(f.file))}, file: f.file})
	}
	if len(rows) == 0 {
		c.api.Notifications().Info("No solution (.sln, .slnx) or project (.csproj) was found in this folder or below it.")
		return
	}
	placeholder := "Select the solution or project csharp-ls loads"
	items := make([]extension.QuickPickItem, len(rows))
	for i, r := range rows {
		items[i] = r.item
	}
	err := c.api.QuickPick().Show(extension.QuickPickOptions{Title: "Select Solution", Placeholder: placeholder, Items: items, MatchOnDescription: true}, func(n int) {
		if n < 0 || n >= len(rows) {
			return
		}
		switch r := rows[n]; {
		case r.file != "":
			if err := c.choose(r.file); err != nil {
				c.api.Notifications().Error("The solution cannot be chosen: " + describeErr(err))
			}
		case r.run != nil:
			r.run()
		}
	})
	if err != nil && !benign(err) {
		c.api.Log().Warn("the list of solutions cannot be shown", "err", err)
	}
}
