package csharp

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The solution or project csharp-ls loads (SPEC §10.34 "As built (step 24b.7)"). csharp-ls loads one solution (.sln or .slnx) given with
// --solution, or else the solution or the projects it finds in its working directory, so the pack decides which one and where the server
// works: the choice of "C#: Select Solution" kept for the folder, else csharp.solution, else the one the folder (or the nearest folder above
// it) holds.

// The files that name what is loaded.
const (
	// StateKey is the key of the state under which the file the user chose for the folder is kept (extension.State).
	StateKey = "csharp.solution"
	// SolutionSetting names the solution or project to load when the user chose none for the folder.
	SolutionSetting = "csharp.solution"
)

// Solution markers are the globs of the solution files, and projectMarker the glob of the project files.
var (
	solutionMarkers = []string{"*.sln", "*.slnx"}
	projectMarkers  = []string{"*.csproj"}
	allMarkers      = []string{"*.sln", "*.slnx", "*.csproj"}
)

// How a target was decided, for the Output channel and the tooltip.
const (
	sourceChosen  = "chosen with Select Solution"
	sourceSetting = SolutionSetting
	sourceFolder  = "the only one in the folder"
	sourceNone    = "none found"
)

// target is what the server is started on.
type target struct {
	// Root is the folder the server works in (its working directory and the root of initialize).
	Root string
	// File is the solution or project file, "" when the server is left to find the projects of Root itself.
	File string
	// Source says what decided: one of the source constants.
	Source string
	// Several are the files to choose from when the folder holds more than one solution and nothing decided; the server then picks by its
	// own rule (or loads the projects it finds), and the user is told once that "C#: Select Solution" chooses.
	Several []string
}

// isSolution tells whether the file is a solution (--solution takes these only; a project is loaded from its folder).
func isSolution(file string) bool {
	ext := strings.ToLower(filepath.Ext(file))
	return ext == ".sln" || ext == ".slnx"
}

// isLoadable tells whether the file is a solution or a project.
func isLoadable(file string) bool {
	return isSolution(file) || strings.EqualFold(filepath.Ext(file), ".csproj")
}

// filesIn lists the files of a folder that match the globs, sorted by name.
func filesIn(dir string, markers []string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		for _, m := range markers {
			if ok, _ := path.Match(m, e.Name()); ok {
				out = append(out, filepath.Join(dir, e.Name()))
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// below lists the files that match the globs in the folder and below it (FindProjects: three levels, folders of dependencies and build
// output left out), the folder's own first.
func below(folder string, markers []string) []string {
	var out []string
	for _, dir := range langserver.FindProjects(folder, markers, langserver.ProjectOptions{}) {
		out = append(out, filesIn(dir, markers)...)
	}
	return out
}

// pick makes the target of a solution or project file: a solution is loaded from its folder with --solution, a project from its folder.
func pick(file, source string) target {
	return target{Root: filepath.Dir(file), File: file, Source: source}
}

// resolveTarget decides what the server loads for the folder of the tab. chosen is the file kept in the state ("" for none). A
// csharp.solution that names no solution or project, or a file that is not there, fails with CSharpSolutionInvalid; a chosen file that is
// gone is forgotten (the folder changed since).
func resolveTarget(env langserver.Env, chosen string) (target, error) {
	if chosen != "" {
		if info, err := os.Stat(chosen); err == nil && !info.IsDir() && isLoadable(chosen) {
			return pick(chosen, sourceChosen), nil
		}
	}
	if set := env.Settings.String(SolutionSetting); set != "" {
		file := env.Abs(set)
		info, err := os.Stat(file)
		switch {
		case err != nil:
			return target{}, errors.New("CSharpSolutionInvalid", SolutionSetting+" is set to "+set+", which does not exist", "setting", SolutionSetting, "value", set, "path", file)
		case info.IsDir():
			return target{}, errors.New("CSharpSolutionInvalid", SolutionSetting+" is set to "+set+", which is a folder", "setting", SolutionSetting, "value", set, "path", file)
		case !isLoadable(file):
			return target{}, errors.New("CSharpSolutionInvalid", SolutionSetting+" is set to "+set+", which is not a .sln, .slnx or .csproj file", "setting", SolutionSetting, "value", set, "path", file)
		}
		return pick(file, sourceSetting), nil
	}
	return autoTarget(env), nil
}

// autoTarget finds the solution of the folder: the nearest folder at or above it that holds a solution (a solution lists the projects of
// a folder below it, so it wins over a project nearer to the file), else the nearest that holds a project, else the solutions and
// projects below the folder. One file decides; several solutions in one folder are left to the server and offered to the user.
func autoTarget(env langserver.Env) target {
	folder := env.Folder
	if folder == "" {
		return target{Source: sourceNone}
	}
	if root := langserver.ResolveRoot(folder, solutionMarkers, env.Home); langserver.HasMarker(root, solutionMarkers) {
		files := filesIn(root, solutionMarkers)
		if len(files) == 1 {
			return pick(files[0], sourceFolder)
		}
		return target{Root: root, Source: sourceNone, Several: files}
	}
	if root := langserver.ResolveRoot(folder, projectMarkers, env.Home); langserver.HasMarker(root, projectMarkers) {
		files := filesIn(root, projectMarkers)
		if len(files) == 1 {
			return pick(files[0], sourceFolder)
		}
		return target{Root: root, Source: sourceFolder} // the server loads the projects of the folder
	}
	for _, markers := range [][]string{solutionMarkers, projectMarkers} {
		files := below(folder, markers)
		switch len(files) {
		case 0:
		case 1:
			return pick(files[0], sourceFolder)
		default:
			return target{Root: folder, Source: sourceNone, Several: files}
		}
	}
	return target{Root: folder, Source: sourceNone}
}

// args are the arguments of csharp-ls for the target: --solution and the name of the file, for a solution (it is relative to the working
// directory, which is the folder of the file).
func (t target) args() []string {
	if t.File != "" && isSolution(t.File) {
		return []string{"--solution", filepath.Base(t.File)}
	}
	return nil
}

// describe is a line for the Output channel and the tooltip of the item: what is loaded and why.
func (t target) describe(folder string) string {
	switch {
	case t.File != "":
		return "Loads " + relative(folder, t.File) + " (" + t.Source + ")"
	case len(t.Several) > 0:
		return "The folder has several solutions; csharp-ls picks one of them. Select Solution chooses."
	case t.Root != "":
		return "Loads the projects it finds in " + relative(folder, t.Root)
	}
	return "No solution or project was found"
}

// relative writes a path relative to the folder when it is inside it.
func relative(folder, p string) string {
	if folder != "" {
		if rel, err := filepath.Rel(folder, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	return p
}
