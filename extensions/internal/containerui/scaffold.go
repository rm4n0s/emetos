package containerui

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerscaffold"
)

// Add Docker Files to Workspace (SPEC §10.20 "Scaffolding"): the command asks the platform, the file that starts the app, the port
// and whether to add a compose.yaml, makes the files with extensions/internal/containerscaffold and writes them to the folder of the
// workspace. A file that exists is never replaced without asking, and one with the same text is left as it is.
//
// Ported from vscode-containers: extensions/vscode-containers/src/scaffolding/scaffold.ts and wizard/ (the questions, their order and
// the question before a file is overwritten).

// scaffold is the command "Add Docker Files to Workspace...".
func (e *ext) scaffold() {
	folder, ok := e.needFolder("add " + e.cfg.Engine + " files")
	if !ok {
		return
	}
	platforms := containerscaffold.Platforms
	items := make([]extension.QuickPickItem, len(platforms))
	for i, p := range platforms {
		items[i] = extension.QuickPickItem{Label: string(p)}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select Application Platform", Items: items}, func(i int) {
		if i >= 0 && i < len(platforms) {
			e.scaffoldArtifact(folder, containerscaffold.Input{Platform: platforms[i], Folder: folder, FileName: e.cfg.scaffoldFileName()})
		}
	})
	if err != nil {
		e.api.Log().Warn("the quick pick cannot be shown", "err", err)
	}
}

// artifactCandidates lists the files of the workspace that can be the artifact of the platform, the likeliest first.
func (e *ext) artifactCandidates(p containerscaffold.Platform) ([]string, string) {
	list, err := e.api.Workspace().Files()
	if err != nil {
		e.api.Notifications().Warn("Cannot list the files of the workspace: " + reasonOf(err))
		return nil, ""
	}
	var out []string
	hidden := func(f string) bool {
		for _, part := range strings.Split(f, "/") {
			switch strings.ToLower(part) {
			case "node_modules", ".venv", "venv", "site-packages", "__pycache__":
				return true
			}
		}
		return false
	}
	for _, f := range list.Files {
		if hidden(f) {
			continue
		}
		base := path.Base(f)
		switch p.ArtifactKind() {
		case containerscaffold.PackageJSON:
			if base == "package.json" {
				out = append(out, f)
			}
		case containerscaffold.ProjectFile:
			if suffix := strings.ToLower(path.Ext(base)); suffix == ".csproj" || suffix == ".fsproj" {
				out = append(out, f)
			}
		case containerscaffold.PythonEntry:
			if strings.ToLower(path.Ext(base)) == ".py" {
				if p == containerscaffold.Django && (strings.EqualFold(base, "wsgi.py") || strings.EqualFold(base, "asgi.py")) {
					continue
				}
				out = append(out, f)
			}
		}
	}
	rank := func(f string) int {
		switch path.Base(f) {
		case "manage.py":
			return 0
		case "app.py":
			return 1
		}
		return 2
	}
	slices.SortStableFunc(out, func(a, b string) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return strings.Compare(a, b)
	})
	what := map[containerscaffold.Artifact]string{
		containerscaffold.PackageJSON: "package.json", containerscaffold.ProjectFile: "C# or F# project file",
		containerscaffold.PythonEntry: "Python file",
	}[p.ArtifactKind()]
	return out, what
}

// scaffoldArtifact asks for the file that starts the app, when the platform needs one.
func (e *ext) scaffoldArtifact(folder string, in containerscaffold.Input) {
	if in.Platform.ArtifactKind() == containerscaffold.NoArtifact {
		e.scaffoldPorts(folder, in)
		return
	}
	cands, what := e.artifactCandidates(in.Platform)
	python := in.Platform.IsPython()
	if len(cands) == 0 && !python {
		e.api.Notifications().Warn("No " + what + " was found in the workspace.")
		return
	}
	if len(cands) == 1 && !python {
		in.Artifact = cands[0]
		e.scaffoldPorts(folder, in)
		return
	}
	items := make([]extension.QuickPickItem, 0, len(cands)+1)
	for _, c := range cands {
		items = append(items, extension.QuickPickItem{Label: c})
	}
	if python {
		items = append(items, extension.QuickPickItem{Label: "Enter a Python module instead..."})
	}
	placeholder := "Choose a " + what
	if python {
		placeholder = "Choose the app's entry point (e.g. manage.py, app.py)"
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: placeholder, Items: items}, func(i int) {
		switch {
		case i < 0:
		case i < len(cands):
			in.Artifact = cands[i]
			e.scaffoldPorts(folder, in)
		default:
			e.askPythonModule(folder, in)
		}
	})
	if err != nil {
		e.api.Log().Warn("the quick pick cannot be shown", "err", err)
	}
}

// askPythonModule asks for a Python module (myapp.manage) instead of a file.
func (e *ext) askPythonModule(folder string, in containerscaffold.Input) {
	err := e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Enter a Python module name (e.g. myapp.manage)",
		Validate: func(v string) string {
			for _, part := range strings.Split(strings.TrimSpace(v), ".") {
				if part == "" || strings.Trim(part, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
					return "Enter a valid Python module name (e.g. myapp.manage)"
				}
			}
			return ""
		},
	}, func(v string, ok bool) {
		if ok {
			in.Module = strings.TrimSpace(v)
			e.scaffoldPorts(folder, in)
		}
	})
	if err != nil {
		e.api.Log().Warn("the input box cannot be shown", "err", err)
	}
}

// scaffoldPorts asks which ports the app listens on, when the platform listens on one.
func (e *ext) scaffoldPorts(folder string, in containerscaffold.Input) {
	if !in.Platform.AsksPorts() {
		e.scaffoldCompose(folder, in)
		return
	}
	def := make([]string, len(in.Platform.DefaultPorts()))
	for i, p := range in.Platform.DefaultPorts() {
		def[i] = strconv.Itoa(p)
	}
	err := e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "What port(s) does your app listen on? Enter a comma-separated list, or empty for no exposed port.",
		Value:  strings.Join(def, ", "),
		Validate: func(v string) string {
			if _, err := containerscaffold.ParsePorts(v); err != nil {
				return "Ports must be a comma-separated list of positive integers (1 to 65535), or empty for no exposed port."
			}
			return ""
		},
	}, func(v string, ok bool) {
		if !ok {
			return
		}
		ports, err := containerscaffold.ParsePorts(v)
		if err != nil {
			e.api.Notifications().Error(reasonOf(err))
			return
		}
		in.Ports = ports
		e.scaffoldCompose(folder, in)
	})
	if err != nil {
		e.api.Log().Warn("the input box cannot be shown", "err", err)
	}
}

// scaffoldCompose asks whether to add a compose.yaml, then makes and writes the files.
func (e *ext) scaffoldCompose(folder string, in containerscaffold.Input) {
	err := e.api.QuickPick().Show(extension.QuickPickOptions{
		Placeholder: "Include optional Docker Compose files?",
		Items:       []extension.QuickPickItem{{Label: "No"}, {Label: "Yes"}},
	}, func(i int) {
		if i < 0 {
			return
		}
		in.Compose = i == 1
		files, err := containerscaffold.Generate(in)
		if err != nil {
			e.api.Notifications().Error("Cannot make the files: " + reasonOf(err))
			return
		}
		e.writeScaffold(folder, files, false, nil)
	})
	if err != nil {
		e.api.Log().Warn("the quick pick cannot be shown", "err", err)
	}
}

// writeScaffold writes the files one after the other. A file that exists with other text is asked about (Overwrite, Overwrite All
// or Cancel, which stops; what was written stays); one with the text it would get, or a KeepExisting file, is left alone.
func (e *ext) writeScaffold(folder string, files []containerscaffold.File, overwriteAll bool, written []string) {
	if len(files) == 0 {
		e.scaffoldDone(folder, written)
		return
	}
	f, rest := files[0], files[1:]
	old, err := readUnder(folder, f.Path)
	exists := err == nil
	switch {
	case exists && (f.KeepExisting || string(old) == f.Content):
		e.writeScaffold(folder, rest, overwriteAll, written)
		return
	case exists && !overwriteAll:
		qerr := e.api.QuickPick().Show(extension.QuickPickOptions{
			Placeholder: "Do you want to overwrite '" + f.Path + "'?",
			Items:       []extension.QuickPickItem{{Label: "Overwrite"}, {Label: "Overwrite All"}, {Label: "Cancel"}},
		}, func(i int) {
			switch i {
			case 0, 1:
				e.putScaffold(folder, f, rest, i == 1, written)
			case 2, -1:
				e.scaffoldDone(folder, written)
			}
		})
		if qerr != nil {
			e.api.Log().Warn("the quick pick cannot be shown", "err", qerr)
		}
		return
	}
	e.putScaffold(folder, f, rest, overwriteAll, written)
}

// putScaffold writes one file and goes on with the rest.
func (e *ext) putScaffold(folder string, f containerscaffold.File, rest []containerscaffold.File, overwriteAll bool, written []string) {
	if err := writeUnder(folder, f.Path, []byte(f.Content)); err != nil {
		e.api.Notifications().Error("Cannot write " + f.Path + ": " + reasonOf(err))
		e.scaffoldDone(folder, written)
		return
	}
	e.writeScaffold(folder, rest, overwriteAll, append(written, f.Path))
}

// scaffoldDone tells what was written and opens the file that describes the image.
func (e *ext) scaffoldDone(folder string, written []string) {
	if len(written) == 0 {
		return
	}
	e.api.Notifications().Info("Added " + strings.Join(written, ", ") + ".")
	for _, w := range written {
		if isDockerfile(w) {
			if err := e.api.Documents().Show(fileURI(filepath.Join(folder, filepath.FromSlash(w))), 0, 0, false); err != nil {
				e.api.Log().Warn("the file cannot be shown", "path", w, "err", err)
			}
			break
		}
	}
}

// readUnder reads a file of the folder, never one outside it (a link that leads out is refused).
func readUnder(folder, rel string) ([]byte, error) {
	root, err := os.OpenRoot(folder)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(filepath.FromSlash(rel))
}

// writeUnder writes a file of the folder, making the folders it needs; nothing outside the folder is touched. It fails with
// ContainerUIScaffoldWriteFailed.
func writeUnder(folder, rel string, data []byte) error {
	root, err := os.OpenRoot(folder)
	if err != nil {
		return errors.NewErr("ContainerUIScaffoldWriteFailed", err).SetMetadata("path", rel)
	}
	defer func() { _ = root.Close() }()
	name := filepath.FromSlash(rel)
	if dir := filepath.Dir(name); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return errors.NewErr("ContainerUIScaffoldWriteFailed", err).SetMetadata("path", rel)
		}
	}
	if err := root.WriteFile(name, data, 0o644); err != nil {
		return errors.NewErr("ContainerUIScaffoldWriteFailed", err).SetMetadata("path", rel)
	}
	return nil
}
