package containerui

import (
	"context"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// Open Shell, Run Interactive and Push (SPEC §10.20 "Container actions", "Image actions"). The first two need a terminal: they open one
// in Emetos's own Terminal (the extension emetos.terminal, M17d, through its requests of SPEC §10.25) and type the command line in
// it, as VS Code's Container Tools does in the integrated terminal. Push runs the CLI so that the user's credential helpers are used,
// and streams its output into a tab of the panel.
//
// Ported from vscode-containers: extensions/vscode-containers/src/commands/containers/attachShellContainer.ts, images/runImage.ts and
// images/pushImage/pushImage.ts.

// The requests of the Terminal extension (SPEC §10.25 "Events and requests"). The topics are written here, not imported, so that the
// extensions stay apart (the Relay extension does the same).
const (
	terminalCreate = "emetos.terminal.create"
	terminalRun    = "emetos.terminal.run"
)

// terminalTimeout is how long the Terminal gets to answer a request.
const terminalTimeout = 10 * time.Second

// inTerminal opens a terminal named name and types the command line in it. The terminal is shown and takes the keyboard (the user
// asked for a shell). When the Terminal extension is off or fails, the user is told and gets the command line to run themselves.
func (e *ext) inTerminal(name, line string) {
	fail := func(why string) {
		e.copyText(line)
		e.api.Notifications().Error("Cannot open a terminal: " + why + ". The command line was copied: " + line)
	}
	ctx, cancel := context.WithTimeout(e.ctx, terminalTimeout)
	err := e.api.Events().Request(ctx, terminalCreate, map[string]any{"name": name, "show": true}, func(ev extension.Event, err error) {
		cancel()
		if err != nil {
			fail(reasonOf(err))
			return
		}
		reply, err := extension.EventValue[struct {
			ID int `json:"id"`
		}](ev)
		if err != nil {
			fail(reasonOf(err))
			return
		}
		ctx2, cancel2 := context.WithTimeout(e.ctx, terminalTimeout)
		err = e.api.Events().Request(ctx2, terminalRun, map[string]any{"id": reply.ID, "text": line}, func(_ extension.Event, err error) {
			cancel2()
			if err != nil {
				fail(reasonOf(err))
			}
		})
		if err != nil {
			cancel2()
			fail(reasonOf(err))
		}
	})
	if err != nil {
		cancel()
		fail(reasonOf(err))
	}
}

// openShell is Open Shell: a terminal with `docker exec -it <id> bash` (sh when the container has no bash).
func (e *ext) openShell(c containerapi.Container) {
	client, ok := e.ready()
	if !ok {
		return
	}
	if _, err := e.findCLI(); err != nil {
		e.api.Notifications().Error("Cannot open a shell: " + e.cliMissingText())
		return
	}
	go func() {
		shell := "sh"
		ctx, cancel := context.WithTimeout(e.ctx, 15*time.Second)
		defer cancel()
		// bash is used when the container has it (Container Tools asks `sh -c "which bash"` the same way)
		if res, err := client.Exec(ctx, c.ID, containerapi.ExecOptions{Cmd: []string{"sh", "-c", "which bash"}}); err == nil && !res.Running && res.ExitCode == 0 {
			shell = "bash"
		}
		e.api.Post(func() {
			line := shellJoin(attachShellArgv(e.cfg.cliName(), containerapi.ShortID(c.ID), shell))
			e.inTerminal("Shell: "+c.Name(), line)
		})
	}()
}

// runImageInteractive is Run Interactive: a terminal with `docker run --rm -it -p ... <image>`.
func (e *ext) runImageInteractive(r imageRow) {
	client, ok := e.ready()
	if !ok {
		return
	}
	if _, err := e.findCLI(); err != nil {
		e.api.Notifications().Error("Cannot run the image: " + e.cliMissingText())
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(e.ctx, 15*time.Second)
		defer cancel()
		var exposed []string
		if info, err := client.InspectImage(ctx, r.image.ID); err == nil {
			exposed = info.ExposedPorts
		} else {
			e.api.Log().Warn("the ports of the image cannot be read", "image", r.target(), "err", err)
		}
		e.api.Post(func() {
			e.inTerminal("Run: "+r.target(), shellJoin(runInteractiveArgv(e.cfg.cliName(), exposed, r.target())))
		})
	}()
}

// pushImage is Push: the name to push is asked (a changed name tags the image first, as Container Tools' wizard does), then the CLI
// pushes it.
func (e *ext) pushImage(r imageRow) {
	if _, ok := e.ready(); !ok {
		return
	}
	cli, err := e.findCLI()
	if err != nil {
		e.api.Notifications().Error("Cannot push: " + e.cliMissingText())
		return
	}
	if r.ref == "" {
		e.api.Notifications().Info("This image has no name. Tag it first (Tag...), with the registry in it, such as registry.example.com/team/app:1.0.")
		return
	}
	showErr := e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt:   "Name to push " + r.ref + " as (with the registry, such as registry.example.com/team/app:1.0)",
		Value:    r.ref,
		Validate: checkImageRef,
	}, func(ref string, ok bool) {
		if !ok {
			return
		}
		ref = strings.TrimSpace(ref)
		start := func() {
			job := cliJob{argv: pushArgv(cli, ref), dir: e.folder(), env: e.cfg.environ(), name: e.cfg.cliName()}
			e.logs.runCLI("Push "+ref, []cliJob{job}, func(res cliResult) {
				switch {
				case res.canceled:
				case res.err != nil:
					e.api.Notifications().Error("Cannot push " + ref + ": " + reasonOf(res.err))
				case res.ok:
					e.api.Notifications().Info("Pushed " + ref + ".")
				default:
					e.api.Notifications().Error("The push of " + ref + " failed. The output is in the " + e.cfg.Name + " Logs tab.")
				}
			})
		}
		if ref == r.ref {
			start()
			return
		}
		e.async(func(ctx context.Context, c *containerapi.Client) []string {
			repo, tag := splitRef(ref)
			if err := c.TagImage(ctx, r.image.ID, repo, tag); err != nil {
				return []string{reasonOf(err)}
			}
			return nil
		}, func(failures []string) {
			if len(failures) > 0 {
				e.api.Notifications().Error("Cannot tag " + r.ref + " as " + ref + ": " + failures[0])
				return
			}
			start()
		})
	})
	if showErr != nil {
		e.api.Log().Warn("the input box cannot be shown", "err", showErr)
	}
}

// splitRef cuts a reference into the repository and the tag ("registry:5000/app:1.0" is "registry:5000/app" and "1.0"; no tag is
// latest).
func splitRef(ref string) (repo, tag string) {
	if at := strings.Index(ref, "@"); at >= 0 {
		return ref[:at], ""
	}
	i := strings.LastIndex(ref, ":")
	if i < 0 || strings.Contains(ref[i+1:], "/") {
		return ref, "latest"
	}
	return ref[:i], ref[i+1:]
}
