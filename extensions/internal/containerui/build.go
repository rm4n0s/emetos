package containerui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/rm4n0s/emetos/extension"
)

// Build Image (SPEC §10.20 "Image actions"): the CLI's `build` with the folder of the Dockerfile as the context, the output streamed
// into a tab of the panel and the errors with file:line into Problems.
//
// Ported from vscode-containers: extensions/vscode-containers/src/commands/images/buildImage.ts (the choice of the Dockerfile, the
// context, the tag that is remembered for the file).

// buildProblemsOwner is the owner of the markers a build puts in Problems.
func (c Config) buildProblemsOwner() string { return c.Prefix + ".build" }

// buildImage is the command Build Image: on the Dockerfile the command was run on, in front, or chosen. The argument {"tag": "app:1"}
// gives the name of the image, so that nothing is asked (a keybinding or a script can build with it).
func (e *ext) buildImage(args any) {
	tag := ""
	if m, ok := args.(map[string]any); ok {
		tag, _ = m["tag"].(string)
		tag = strings.TrimSpace(tag)
	}
	e.pickFiles(args, "Dockerfile", isDockerfile, "dockerfile", func(paths []string) { e.buildFile(paths[0], tag) })
}

// buildFile asks for the tag of the image (unless it was given) and builds it.
func (e *ext) buildFile(dockerfile, given string) {
	folder, ok := e.needFolder("build an image")
	if !ok {
		return
	}
	cli, err := e.findCLI()
	if err != nil {
		e.api.Notifications().Error("Cannot build: " + e.cliMissingText())
		return
	}
	contextDir := filepath.Dir(dockerfile)
	if c := e.readStringSetting(e.cfg.settingBuildContextPath()); c != "" {
		contextDir = absPath(folder, c)
	}
	if given != "" {
		if why := checkImageRef(given); why != "" {
			e.api.Notifications().Error("Cannot build with the name \"" + given + "\": " + why)
			return
		}
		e.tags[dockerfile] = given
		e.startBuild(cli, folder, dockerfile, contextDir, given)
		return
	}
	suggested := e.tags[dockerfile]
	if suggested == "" {
		suggested = defaultTag(folder, dockerfile)
	}
	showErr := e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt:   "Name and tag of the image to build from " + relOrAbs(folder, dockerfile),
		Value:    suggested,
		Validate: checkImageRef,
	}, func(tag string, ok bool) {
		if !ok {
			return
		}
		tag = strings.TrimSpace(tag)
		e.tags[dockerfile] = tag
		e.startBuild(cli, folder, dockerfile, contextDir, tag)
	})
	if showErr != nil {
		e.api.Log().Warn("the input box cannot be shown", "err", showErr)
	}
}

// cliMissingText says that the program of the engine is not on PATH.
func (e *ext) cliMissingText() string {
	return "the program " + e.cfg.cliName() + " is not on PATH. Install " + e.cfg.Engine + " (or start Emetos from a shell that finds it)."
}

// startBuild runs the build in a tab of the panel and reads its output when it ends.
func (e *ext) startBuild(cli, folder, dockerfile, contextDir, tag string) {
	job := cliJob{
		argv: buildArgv(cli, relOrAbs(folder, dockerfile), tag, relOrAbs(folder, contextDir)),
		dir:  folder,
		env:  append(e.cfg.environ(), "BUILDKIT_PROGRESS=plain"),
		name: e.cfg.cliName(),
	}
	e.api.Problems().Clear(e.cfg.buildProblemsOwner())
	e.logs.runCLI("Build "+tag, []cliJob{job}, func(res cliResult) {
		e.buildDone(folder, dockerfile, contextDir, tag, res)
	})
}

// buildDone reads the problems in the output, publishes the result and tells the user.
func (e *ext) buildDone(folder, dockerfile, contextDir, tag string, res cliResult) {
	if res.canceled {
		return
	}
	problems := e.putBuildProblems(folder, dockerfile, contextDir, parseBuildOutput(res.tail))
	if err := e.api.Events().Publish(e.cfg.BuildFinishedTopic(), map[string]any{"image": tag, "ok": res.ok}); err != nil {
		e.api.Log().Warn("the build event cannot be published", "err", err)
	}
	switch {
	case res.ok:
		e.api.Notifications().Info("Built " + tag + ".")
	case res.err != nil:
		e.api.Notifications().Error("Cannot build " + tag + ": " + reasonOf(res.err))
	default:
		msg := "The build of " + tag + " failed (status " + strconv.Itoa(res.exit) + ")."
		if problems > 0 {
			msg += " " + plural(problems, "problem") + " in Problems."
		}
		e.api.Notifications().Error(msg + " The output is in the " + e.cfg.Name + " Logs tab.")
	}
}

// putBuildProblems sets the markers of the problems that were found, per file, and returns how many there were. A problem names the
// file as the build wrote it (relative to the context, or to the folder) or not at all: it is looked for next to the Dockerfile, in
// the context and in the folder, and the Dockerfile that was built is the fallback.
func (e *ext) putBuildProblems(folder, dockerfile, contextDir string, found []buildProblem) int {
	byURI := map[string][]extension.Marker{}
	seen := map[string]bool{}
	var order []string
	for _, p := range found {
		path := dockerfile
		if p.file != "" {
			path = resolveBuildFile(p.file, dockerfile, contextDir, folder)
		}
		key := path + ":" + strconv.Itoa(p.line) + ":" + p.message
		if seen[key] {
			continue
		}
		seen[key] = true
		sev := extension.SeverityError
		if p.warning {
			sev = extension.SeverityWarning
		}
		uri := fileURI(path)
		if _, ok := byURI[uri]; !ok {
			order = append(order, uri)
		}
		byURI[uri] = append(byURI[uri], extension.Marker{
			Severity: sev, Message: p.message, Source: e.cfg.cliName() + " build",
			StartLine: p.line, StartColumn: 1, EndLine: p.line, EndColumn: lineEndColumn(path, p.line),
		})
	}
	for _, uri := range order {
		if err := e.api.Problems().Set(e.cfg.buildProblemsOwner(), uri, byURI[uri]); err != nil {
			e.api.Log().Warn("problems cannot be set", "uri", uri, "err", err)
		}
	}
	return len(seen)
}

// resolveBuildFile finds the file a build message names: an absolute path as it is, else relative to the folder of the Dockerfile,
// the context and the workspace in turn, the first that exists; the Dockerfile when none does and the name is its own.
func resolveBuildFile(name, dockerfile, contextDir, folder string) string {
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	for _, base := range []string{filepath.Dir(dockerfile), contextDir, folder} {
		if base == "" {
			continue
		}
		if p := filepath.Join(base, name); fileExists(p) {
			return p
		}
	}
	return dockerfile
}

// fileExists tells whether a path is a file.
func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// lineEndColumn is the column (counted from 1, in UTF-16 units) just after the text of a line of a file, so that a marker covers
// the whole line; 2 when the file or the line cannot be read.
func lineEndColumn(path string, line int) int {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > extension.MaxReadFileSize {
		return 2
	}
	lines := strings.Split(string(data), "\n")
	if line < 1 || line > len(lines) {
		return 2
	}
	text := strings.TrimRight(lines[line-1], "\r")
	text = strings.TrimRight(text, " \t")
	if text == "" {
		return 2
	}
	return len(utf16.Encode([]rune(text))) + 1
}
