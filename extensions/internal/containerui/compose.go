package containerui

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// Compose (SPEC §10.20 "Compose"): Up, Up (select services), Down, Down (select services), Restart and Logs on a Compose file, and
// Start, Stop, Restart, Down and Logs on a Compose group of the Containers section. The CLI is used, run in the folder of the
// workspace, and its output is streamed into a tab of the panel.
//
// Ported from vscode-containers: extensions/vscode-containers/src/commands/compose/compose.ts, getComposeSubsetList.ts and
// src/commands/containers/composeGroup.ts.

// The labels Compose puts on the containers of a project.
const (
	labelComposeWorkingDir = "com.docker.compose.project.working_dir"
	labelComposeFiles      = "com.docker.compose.project.config_files"
	labelComposeEnvFile    = "com.docker.compose.project.environment_file"
)

// composeAction is what a Compose command does.
type composeAction int

// The Compose commands on a file.
const (
	composeUp composeAction = iota
	composeUpSubset
	composeDown
	composeDownSubset
	composeRestart
	composeLogs
)

// resolveCompose finds how Compose is run: `docker compose` (the plugin, asked with `compose version`), else the stand-alone
// docker-compose. It runs a process, so it belongs on a goroutine. It fails with ContainerUIComposeNotFound.
func (e *ext) resolveCompose(ctx context.Context) ([]string, error) {
	cli, cliErr := e.findCLI()
	if cliErr == nil {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		code, err := runProcess(ctx, cliJob{argv: []string{cli, "compose", "version"}, env: e.cliEnviron()}, func(bool, string) {})
		if err == nil && code == 0 {
			return []string{cli, "compose"}, nil
		}
	}
	if fb, err := e.cfg.lookPath(e.cfg.composeFallbackName()); err == nil {
		return []string{fb}, nil
	}
	return nil, errors.New("ContainerUIComposeNotFound", e.cfg.Engine+" Compose was not found: install the compose plugin of "+e.cfg.cliName()+" (or "+e.cfg.composeFallbackName()+") and put it on PATH",
		"cli", e.cfg.cliName(), "fallback", e.cfg.composeFallbackName())
}

// withCompose calls fn, on the UI thread, with the program of Compose (["docker", "compose"] or ["docker-compose"]); when there is
// none the user is told. It is found once; a failure is not remembered.
func (e *ext) withCompose(fn func(base []string)) {
	if e.composeBase != nil {
		fn(slices.Clone(e.composeBase))
		return
	}
	go func() {
		base, err := e.resolveCompose(e.ctx)
		e.api.Post(func() {
			if err != nil {
				e.api.Notifications().Error("Cannot run Compose: " + reasonOf(err))
				return
			}
			e.composeBase = base
			fn(slices.Clone(base))
		})
	}()
}

// composeJob is a run of Compose; the tab writes the program as docker or docker-compose, not with its path.
func (e *ext) composeJob(base []string, t composeTarget, dir string, verb ...string) cliJob {
	argv := composeArgv(base, t, verb...)
	name := e.cfg.cliName()
	if len(base) == 1 {
		name = e.cfg.composeFallbackName()
	}
	return cliJob{argv: argv, dir: dir, env: e.cliEnviron(), name: name}
}

// composeOnFiles is a Compose command run on a Compose file (the Explorer's menu, the editor's menu, the palette).
func (e *ext) composeOnFiles(args any, act composeAction) {
	e.pickFiles(args, "Compose file", isComposeFile, "dockercompose", func(paths []string) { e.composeFiles(paths, act) })
}

// composeFiles does the action on the files.
func (e *ext) composeFiles(paths []string, act composeAction) {
	folder, ok := e.needFolder("use Compose")
	if !ok {
		return
	}
	switch act {
	case composeDown, composeDownSubset, composeRestart:
		if _, ready := e.ready(); !ready {
			return
		}
	}
	targets := make([]composeTarget, len(paths))
	dirs := make([]string, len(paths))
	for i, p := range paths {
		targets[i] = composeTarget{files: []string{relOrAbs(folder, p)}}
		dirs[i] = folder
		if rel := relOrAbs(folder, p); filepath.IsAbs(rel) {
			dirs[i] = filepath.Dir(p) // a file outside the workspace runs where it is
		}
	}
	title := composeTitle(folder, paths)
	detached := e.readBoolSetting(e.cfg.settingComposeDetached(), true)
	build := e.readBoolSetting(e.cfg.settingComposeBuild(), true)
	switch act {
	case composeUp:
		e.withCompose(func(base []string) {
			var jobs []cliJob
			for i := range paths {
				jobs = append(jobs, e.composeJob(base, targets[i], dirs[i], upVerb(detached, build, nil)...))
			}
			e.logs.runCLI("Compose Up: "+title, jobs, e.composeDone("Compose Up", title))
		})
	case composeLogs:
		e.withCompose(func(base []string) {
			for i := range paths {
				e.logs.runCLI("Compose Logs: "+composeTitle(folder, paths[i:i+1]), []cliJob{e.composeJob(base, targets[i], dirs[i], logsVerb()...)}, nil)
			}
		})
	case composeDown:
		e.askComposeDown(paths, title, func() {
			e.withCompose(func(base []string) {
				var jobs []cliJob
				for i := range paths {
					jobs = append(jobs, e.composeJob(base, targets[i], dirs[i], downVerb(nil)...))
				}
				e.logs.runCLI("Compose Down: "+title, jobs, e.composeDone("Compose Down", title))
			})
		})
	case composeRestart:
		e.askComposeDown(paths, title, func() {
			e.withCompose(func(base []string) {
				var jobs []cliJob
				for i := range paths {
					jobs = append(jobs, e.composeJob(base, targets[i], dirs[i], downVerb(nil)...))
				}
				for i := range paths {
					jobs = append(jobs, e.composeJob(base, targets[i], dirs[i], upVerb(detached, build, nil)...))
				}
				e.logs.runCLI("Compose Restart: "+title, jobs, e.composeDone("Compose Restart", title))
			})
		})
	case composeUpSubset, composeDownSubset:
		if len(paths) > 1 {
			e.api.Notifications().Info("Select Services works on one Compose file; the first one, " + relOrAbs(folder, paths[0]) + ", is used.")
		}
		e.composeSubset(paths[0], targets[0], dirs[0], act == composeUpSubset, detached, build)
	}
}

// composeTitle names the files a command works on.
func composeTitle(folder string, paths []string) string {
	if len(paths) == 1 {
		return relOrAbs(folder, paths[0])
	}
	return plural(len(paths), "file")
}

// composeDone is the function that tells how a run of Compose ended.
func (e *ext) composeDone(what, title string) func(cliResult) {
	return func(res cliResult) {
		switch {
		case res.canceled:
		case res.err != nil:
			e.api.Notifications().Error(what + " could not run: " + reasonOf(res.err))
		case !res.ok:
			e.api.Notifications().Error(what + " of " + title + " failed. The output is in the " + e.cfg.Name + " Logs tab.")
		}
	}
}

// askComposeDown asks before a Compose project is taken down: what the command does and which containers of the project the engine
// knows (those whose Compose file is one of the files). run is called when the question is answered with Down.
func (e *ext) askComposeDown(paths []string, title string, run func()) {
	var names []string
	for _, c := range sortedContainers(e.containers) {
		files := c.Labels[labelComposeFiles]
		for _, p := range paths {
			if files != "" && slices.Contains(strings.Split(files, ","), p) {
				names = append(names, c.Name())
				break
			}
		}
	}
	lines := []string{"It stops and removes the containers and the networks of the project. Volumes are kept."}
	if len(names) > 0 {
		lines = append([]string{plural(len(names), "container") + " of it: " + listNames(names)}, lines...)
	}
	e.ask(confirmation{section: resContainers, title: "Take down the Compose project of " + title + "?", lines: lines,
		choices: []choice{{label: "Down", run: run}}})
}

// listNames writes names in a line, with "and N more" after the fifth.
func listNames(names []string) string {
	if len(names) <= 5 {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:5], ", ") + " and " + plural(len(names)-5, "more")
}

// composeNames asks Compose for the services or the profiles of the files, one on a line. It runs a process.
func (e *ext) composeNames(ctx context.Context, base []string, t composeTarget, dir, kind string) ([]string, error) {
	job := e.composeJob(base, t, dir, configVerb(kind)...)
	var names []string
	var errText []string
	code, err := runProcess(ctx, job, func(stderr bool, line string) {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case stderr:
			errText = append(errText, line)
		default:
			names = append(names, line)
		}
	})
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, errors.New("ContainerUIComposeConfigFailed", "Compose could not read the file: "+strings.Join(errText, " "), "kind", kind, "status", code)
	}
	return names, nil
}

// composeSubset lets the user choose services (or profiles) of a Compose file and brings them up or takes them down.
func (e *ext) composeSubset(path string, t composeTarget, dir string, up, detached, build bool) {
	title := relOrAbs(e.folder(), path)
	e.withCompose(func(base []string) {
		go func() {
			services, sErr := e.composeNames(e.ctx, base, t, dir, "services")
			profiles, _ := e.composeNames(e.ctx, base, t, dir, "profiles") // profiles are not known to every Compose: no profiles then
			e.api.Post(func() {
				if sErr != nil && len(profiles) == 0 {
					e.api.Notifications().Error("Cannot read the services of " + title + ": " + reasonOf(sErr))
					return
				}
				if len(services) == 0 && len(profiles) == 0 {
					e.api.Notifications().Info("No services were found in " + title + ".")
					return
				}
				finish := func(kind string, names []string) {
					tt := t
					var verb []string
					var chosen []string
					if kind == "profiles" {
						tt.profiles = names
					} else {
						chosen = names
					}
					if up {
						verb = upVerb(detached, build, chosen)
					} else {
						verb = downVerb(chosen)
					}
					run := func() {
						word := "Up"
						if !up {
							word = "Down"
						}
						e.logs.runCLI("Compose "+word+": "+title+" ("+strings.Join(names, ", ")+")", []cliJob{e.composeJob(base, tt, dir, verb...)},
							e.composeDone("Compose "+word, title))
					}
					if up {
						run()
						return
					}
					e.ask(confirmation{section: resContainers, title: "Take down " + strings.Join(names, ", ") + " of " + title + "?",
						lines:   []string{"It stops and removes the containers of " + kind + " you chose, and the networks nothing else uses."},
						choices: []choice{{label: "Down", run: run}}})
				}
				choose := func(kind string, all []string) {
					key := path + ":" + kind
					pre := e.subsets[key]
					if len(pre) == 0 {
						pre = all
					}
					e.pickSome("Choose the "+kind+" to bring "+map[bool]string{true: "up", false: "down"}[up], all, pre, func(chosen []string) {
						e.subsets[key] = chosen
						finish(kind, chosen)
					})
				}
				switch {
				case len(profiles) == 0:
					choose("services", services)
				case len(services) == 0:
					choose("profiles", profiles)
				default:
					items := []extension.QuickPickItem{{Label: "Services"}, {Label: "Profiles"}}
					err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Do you want to start/stop services or profiles?", Items: items}, func(i int) {
						switch i {
						case 0:
							choose("services", services)
						case 1:
							choose("profiles", profiles)
						}
					})
					if err != nil {
						e.api.Log().Warn("the quick pick cannot be shown", "err", err)
					}
				}
			})
		}()
	})
}

// ---- the Compose group ----

// composeGroupAction is what a command on a Compose group does.
type composeGroupAction int

// The commands on a Compose group.
const (
	groupLogs composeGroupAction = iota
	groupStart
	groupStop
	groupRestart
	groupDown
)

// verb is the Compose command of the action and what the user calls it.
func (a composeGroupAction) verb() (compose []string, title string) {
	switch a {
	case groupLogs:
		return logsVerb(), "Logs"
	case groupStart:
		return []string{"start"}, "Start"
	case groupStop:
		return []string{"stop"}, "Stop"
	case groupRestart:
		return []string{"restart"}, "Restart"
	}
	return downVerb(nil), "Down"
}

// composeProjects lists the Compose projects the containers belong to.
func (e *ext) composeProjects() []string {
	var out []string
	for _, c := range e.containers {
		if p := c.ComposeProject(); p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// projectOfArgs finds the Compose project a command on a group was run on: the group row, a container of a project, else the
// selection; "" when there is none.
func (e *ext) projectOfArgs(args any) string {
	for _, k := range e.views[secContainers].keysFor(args) {
		if name, ok := strings.CutPrefix(k, keyGroup); ok && isComposeGroup(name) {
			return name
		}
		if id, ok := strings.CutPrefix(k, keyContainer); ok {
			if c, ok := e.containerByID(id); ok && c.ComposeProject() != "" {
				return c.ComposeProject()
			}
		}
	}
	return ""
}

// composeGroupCommand is a command on a Compose group: the group the command was run on, else the one the user picks.
func (e *ext) composeGroupCommand(args any, act composeGroupAction) {
	if _, ok := e.ready(); !ok {
		return
	}
	if p := e.projectOfArgs(args); p != "" {
		e.composeGroup(p, act)
		return
	}
	projects := e.composeProjects()
	if len(projects) == 0 {
		e.api.Notifications().Info("No Compose projects are running.")
		return
	}
	items := make([]extension.QuickPickItem, len(projects))
	for i, p := range projects {
		items[i] = extension.QuickPickItem{Label: p}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a Compose project", Items: items}, func(i int) {
		if i >= 0 && i < len(projects) {
			e.composeGroup(projects[i], act)
		}
	})
	if err != nil {
		e.api.Log().Warn("the quick pick cannot be shown", "err", err)
	}
}

// groupTarget reads where a Compose project was started from the labels of its containers: the folder, the files, the name and the
// environment file. ok is false when no container tells.
func (e *ext) groupTarget(project string) (t composeTarget, dir string, cs []containerapi.Container, ok bool) {
	for _, c := range sortedContainers(e.containers) {
		if c.ComposeProject() != project {
			continue
		}
		cs = append(cs, c)
		if dir != "" {
			continue
		}
		wd, files := c.Labels[labelComposeWorkingDir], c.Labels[labelComposeFiles]
		if wd == "" || files == "" {
			continue
		}
		dir = wd
		for _, f := range strings.Split(files, ",") {
			if !filepath.IsAbs(f) {
				f = filepath.Base(f) // relative to the folder the project was started in: the files lie in it
			}
			t.files = append(t.files, f)
		}
		t.project = project
		t.envFile = c.Labels[labelComposeEnvFile]
	}
	return t, dir, cs, dir != ""
}

// composeGroup does an action on a Compose project.
func (e *ext) composeGroup(project string, act composeGroupAction) {
	t, dir, cs, ok := e.groupTarget(project)
	if !ok {
		e.api.Notifications().Error("Unable to determine the Compose project info of the group '" + project + "'.")
		return
	}
	verb, word := act.verb()
	run := func() {
		e.withCompose(func(base []string) {
			e.logs.runCLI("Compose "+word+": "+project, []cliJob{e.composeJob(base, t, dir, verb...)}, e.composeDone("Compose "+word, project))
		})
	}
	if act != groupDown {
		run()
		return
	}
	names := make([]string, len(cs))
	for i, c := range cs {
		names[i] = c.Name()
	}
	e.ask(confirmation{section: resContainers, title: "Take down the Compose project " + project + "?",
		lines: []string{plural(len(cs), "container") + ": " + listNames(names),
			"It stops and removes them and the networks of the project. Volumes are kept."},
		choices: []choice{{label: "Down " + plural(len(cs), "container"), run: run}}})
}
