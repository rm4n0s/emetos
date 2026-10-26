package golang

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// go vet and the lint tool on save (SPEC §10.17 "As built (step 14.3)"): after a Go file is saved the package is vetted and linted,
// debounced, and the findings go into the Problems panel under an owner of their own.

// Ported from VS Code Go: extension/src/goVet.ts, extension/src/goLint.ts and extension/src/util.ts (handleDiagnosticErrors: the
// findings of a run replace those of the same scope).

// The ids of the commands and the owners of the markers.
const (
	// VetPackageCommand and VetWorkspaceCommand run go vet, LintPackageCommand and LintWorkspaceCommand the lint tool.
	VetPackageCommand    = ID + ".vetPackage"
	VetWorkspaceCommand  = ID + ".vetWorkspace"
	LintPackageCommand   = ID + ".lintPackage"
	LintWorkspaceCommand = ID + ".lintWorkspace"
	// VetMarkerOwner is the owner of the markers of go vet and LintMarkerOwner that of the lint tool (whose name is the source).
	VetMarkerOwner  = "go vet"
	LintMarkerOwner = "go lint"
)

// The bounds of the runs.
const (
	// saveDebounce is how long after a save the run starts; a second save of the same package within it restarts the wait.
	saveDebounce = 400 * time.Millisecond
	// vetTimeout and lintTimeout end a run that does not finish.
	vetTimeout  = 5 * time.Minute
	lintTimeout = 10 * time.Minute
)

// The scopes of a run.
const (
	scopeFile      = "file"
	scopePackage   = "package"
	scopeWorkspace = "workspace"
)

// diagRequest is a run of go vet or of the lint tool.
type diagRequest struct {
	// lint says that the run is of the lint tool, else go vet.
	lint bool
	// scope is file, package or workspace; dir is the folder the program runs in and file the path of the file (scope file).
	scope, dir, file string
	// manual says that the user asked for it: a result is told, and a failure is shown.
	manual bool
}

func (r diagRequest) key() string {
	kind := "vet"
	if r.lint {
		kind = "lint"
	}
	return kind + "|" + r.scope + "|" + r.dir + "|" + r.file
}

// diagRun is a run that goes on.
type diagRun struct{ cancel context.CancelFunc }

// markerSet remembers the files an owner has markers for, so that a run can take away those it no longer finds: uris maps the URI
// of a file to the path of its folder.
type markerSet struct {
	owner string
	uris  map[string]string
}

func newMarkerSet(owner string) *markerSet {
	return &markerSet{owner: owner, uris: map[string]string{}}
}

// lintProgram is the name of the program of a lint tool.
func lintProgram(tool string) string {
	if tool == "golangci-lint-v2" {
		return "golangci-lint"
	}
	return tool
}

// lintArgs are the arguments of the lint tool for a target: the flags the tool needs to print places as text, the flags of
// go.lintFlags, then the target.
func lintArgs(tool string, flags []string, target string) []string {
	var args []string
	switch tool {
	case "golangci-lint":
		args = []string{"run", "--print-issued-lines=false", "--color=never"}
	case "golangci-lint-v2":
		args = []string{"run", "--output.text.print-issued-lines=false", "--color=never"}
	case "revive":
		args = []string{"-formatter", "unix"}
	}
	return append(append(args, flags...), target)
}

// stringList reads a setting that is a list of strings.
func (g *golang) stringList(key string) []string {
	v, err := extension.SettingValue[[]string](g.api.Settings(), key)
	if err != nil {
		g.api.Log().Warn("the setting is not a list of strings", "key", key, "err", err)
		return nil
	}
	return v
}

// stringSetting reads a setting that is a string, with a default for a value that is not one.
func (g *golang) stringSetting(key, def string) string {
	v, err := extension.SettingValue[string](g.api.Settings(), key)
	if err != nil || strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

// documentSaved is the handler of the event "a document was written to disk".
func (g *golang) documentSaved(ev extension.Event) {
	var s extension.DocumentSaved
	if ev.Decode(&s) != nil {
		return
	}
	path, ok := filePath(s.URI)
	root := g.api.Scope().Folder
	if !ok || !strings.HasSuffix(path, ".go") || root == "" || !within(root, path) {
		return
	}
	if vet := g.stringSetting(VetOnSaveSetting, scopePackage); vet == scopePackage || vet == scopeWorkspace {
		g.schedule(g.saveRequest(false, vet, path, root))
	}
	if lint := g.stringSetting(LintOnSaveSetting, scopePackage); lint == scopeFile || lint == scopePackage || lint == scopeWorkspace {
		g.schedule(g.saveRequest(true, lint, path, root))
	}
}

// saveRequest is the run for the saved file in the scope.
func (g *golang) saveRequest(lint bool, scope, path, root string) diagRequest {
	r := diagRequest{lint: lint, scope: scope, dir: filepath.Dir(path)}
	switch scope {
	case scopeWorkspace:
		r.dir = root
	case scopeFile:
		r.file = path
	}
	return r
}

// schedule starts the run after the debounce; a request for the same thing that is waiting is moved back.
func (g *golang) schedule(r diagRequest) {
	key := r.key()
	if t := g.timers[key]; t != nil {
		t.Reset(g.debounce)
		return
	}
	g.timers[key] = time.AfterFunc(g.debounce, func() {
		g.api.Post(func() {
			delete(g.timers, key)
			if g.ctx.Err() == nil {
				g.startDiagnostics(r)
			}
		})
	})
}

// startDiagnostics runs go vet or the lint tool for the request; an older run for the same thing is cancelled.
func (g *golang) startDiagnostics(r diagRequest) {
	name, owner := "go vet", VetMarkerOwner
	if r.lint {
		name, owner = g.lintTool(), LintMarkerOwner
	}
	program, args, err := g.diagCommand(r)
	if err != nil {
		g.diagFailed(r, name, nil, err)
		return
	}
	key := r.key()
	if old := g.diagRuns[key]; old != nil {
		old.cancel()
	}
	timeout := vetTimeout
	if r.lint {
		timeout = lintTimeout
	}
	ctx, cancel := context.WithTimeout(g.ctx, timeout)
	run := &diagRun{cancel: cancel}
	g.diagRuns[key] = run
	spec := cmdSpec{Program: program, Args: args, Dir: r.dir, Env: g.programEnv()}
	g.closers.Add(1)
	go func() {
		defer g.closers.Done()
		defer cancel()
		var lines []string
		runErr := runProgram(ctx, spec, func(l string) { lines = append(lines, l) })
		cancelled := ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded)
		g.api.Post(func() {
			if g.diagRuns[key] != run {
				return // a newer run took its place
			}
			delete(g.diagRuns, key)
			if cancelled {
				return
			}
			g.finishDiagnostics(r, name, owner, lines, runErr)
		})
	}()
}

// lintTool is go.lintTool.
func (g *golang) lintTool() string { return g.stringSetting(LintToolSetting, defaultLintTool) }

// diagCommand is the program and arguments of a request. It fails with ProgramNotFound when the program is missing.
func (g *golang) diagCommand(r diagRequest) (program string, args []string, err error) {
	target := "."
	switch r.scope {
	case scopeWorkspace:
		target = "./..."
	case scopeFile:
		target = filepath.Base(r.file)
	}
	if !r.lint {
		program, err = g.goProgram()
		return program, append(append([]string{"vet"}, g.stringList(VetFlagsSetting)...), target), err
	}
	tool := g.lintTool()
	name := g.alternateTool(tool)
	if name == "" {
		name = g.alternateTool(lintProgram(tool))
	}
	if name == "" {
		name = lintProgram(tool)
	}
	program, err = lookProgram(name, g.api.Scope().Folder)
	return program, lintArgs(tool, g.stringList(LintFlagsSetting), target), err
}

// finishDiagnostics reads the output of a run, on the UI thread, and replaces the markers of its scope.
func (g *golang) finishDiagnostics(r diagRequest, name, owner string, lines []string, runErr error) {
	root := g.api.Scope().Folder
	var found []finding
	for _, f := range parseFindings(lines, r.dir, r.lint, true) {
		if within(root, f.Path) && strings.HasSuffix(f.Path, ".go") {
			found = append(found, f)
		}
	}
	if len(found) == 0 && runErr != nil {
		// the tool printed no place and did not end well: it did not run (a package that does not load, a missing go.mod); the
		// markers of the last run stay
		g.diagFailed(r, name, lines, runErr)
		return
	}
	set := g.vetSet
	if r.lint {
		set = g.lintSet
	}
	g.publish(set, r, found, name)
	if r.manual && len(found) == 0 {
		g.api.Notifications().Info(name + ": no problems found in " + g.scopeLabel(r) + ".")
	}
}

// scopeLabel names what the run covered.
func (g *golang) scopeLabel(r diagRequest) string {
	switch r.scope {
	case scopeWorkspace:
		return "the workspace"
	case scopeFile:
		return filepath.Base(r.file)
	}
	return "the package " + g.runLabel(r.dir)
}

// diagFailed tells that the tool did not run or failed without naming a place: in the Output channel "Go", and for a run the user
// asked for in a notification. A lint tool that is not installed is told once, with a button for the tools the extension installs.
func (g *golang) diagFailed(r diagRequest, name string, output []string, err error) {
	g.api.Log().Warn("the tool failed", "tool", name, "err", err)
	if appErr, ok := errors.FromError(err); ok && appErr.HasRoute("ProgramNotFound") && !r.lint {
		if !g.missing["go"] || r.manual {
			g.missing["go"] = true
			g.api.Notifications().Error("The go command was not found. Install Go, or set go.alternateTools in the settings.")
		}
		return
	}
	if appErr, ok := errors.FromError(err); ok && appErr.HasRoute("ProgramNotFound") && r.lint {
		tool := g.lintTool()
		if tl, known := toolByName(tool); known {
			g.promptMissingTool(tl)
		} else if !g.missing[tool] {
			g.missing[tool] = true
			g.api.Notifications().Warn(tool + " was not found. Install it, or choose another tool with go.lintTool.")
		}
		return
	}
	if ch := g.channel(); ch != nil {
		ch.AppendLine("$ " + name + ": " + failureText(err, nil))
		for _, l := range output {
			if strings.TrimSpace(l) != "" {
				ch.AppendLine(l)
			}
		}
	}
	if r.manual {
		g.api.Notifications().Error(name + " failed: " + failureText(err, output))
	}
}

// publish replaces the markers of the owner in the scope of the run with the findings: the files of the scope that the run no
// longer finds lose theirs.
func (g *golang) publish(set *markerSet, r diagRequest, found []finding, source string) {
	byURI := map[string][]extension.Marker{}
	dirs := map[string]string{}
	for _, f := range found {
		uri := fileURI(f.Path)
		byURI[uri] = append(byURI[uri], markerOf(f, source))
		dirs[uri] = filepath.Dir(f.Path)
	}
	inScope := func(uri, dir string) bool {
		switch r.scope {
		case scopeWorkspace:
			return true
		case scopeFile:
			return uri == fileURI(r.file)
		}
		return dir == r.dir
	}
	for uri, dir := range set.uris {
		if _, still := byURI[uri]; !still && inScope(uri, dir) {
			if err := g.api.Problems().Set(set.owner, uri, nil); err != nil {
				g.api.Log().Warn("markers cannot be cleared", "owner", set.owner, "uri", uri, "err", err)
			}
			delete(set.uris, uri)
		}
	}
	uris := make([]string, 0, len(byURI))
	for uri := range byURI {
		uris = append(uris, uri)
	}
	slices.Sort(uris)
	for _, uri := range uris {
		if err := g.api.Problems().Set(set.owner, uri, byURI[uri]); err != nil {
			g.api.Log().Warn("markers cannot be set", "owner", set.owner, "uri", uri, "err", err)
			continue
		}
		set.uris[uri] = dirs[uri]
	}
}

// ---- the commands ----

// diagCommandHandler is the handler of the commands "Go: Vet Current Package", "Go: Vet Workspace", "Go: Lint Current Package" and
// "Go: Lint Workspace".
func (g *golang) diagCommandHandler(lint bool, scope string) extension.CommandHandler {
	return func(context.Context, any) error {
		r := diagRequest{lint: lint, scope: scope, manual: true}
		switch scope {
		case scopeWorkspace:
			if r.dir = g.api.Scope().Folder; r.dir == "" {
				g.api.Notifications().Info("This command needs a folder: open one first.")
				return nil
			}
		default:
			dir, ok := g.runDir()
			if !ok {
				g.api.Notifications().Info("This command needs a Go file or a folder: open one first.")
				return nil
			}
			r.dir = dir
		}
		g.startDiagnostics(r)
		return nil
	}
}
