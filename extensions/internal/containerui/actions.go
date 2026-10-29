package containerui

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// stopTimeout is how long an engine waits for a container to stop before it kills it (Docker's own default).
const stopTimeout = 10 * time.Second

// notConnected says that an action needs an engine and there is none.
func (e *ext) notConnected() {
	e.api.Notifications().Warn(e.cfg.Engine + " is not running or cannot be reached. Use Retry in the Containers view.")
}

// ready returns the Client, or tells the user that there is none.
func (e *ext) ready() (*containerapi.Client, bool) {
	if e.client == nil {
		e.notConnected()
		return nil, false
	}
	return e.client, true
}

// async runs work on a goroutine with the Client and hands the lines that tell what failed (none for success) to done on the UI
// thread. The work ends when the extension does.
func (e *ext) async(work func(ctx context.Context, c *containerapi.Client) []string, done func(failures []string)) {
	c, ok := e.ready()
	if !ok {
		return
	}
	gen := e.gen
	go func() {
		failures := work(e.ctx, c)
		e.api.Post(func() {
			if gen != e.gen {
				return
			}
			if done != nil {
				done(failures)
			}
		})
	}()
}

// containerOp is an action on containers that needs no question.
type containerOp int

// The actions that run at once.
const (
	opStart containerOp = iota
	opStop
	opRestart
	opPause
	opUnpause
	opKill
)

// verb is how the user calls the action.
func (o containerOp) verb() string {
	return [...]string{"start", "stop", "restart", "pause", "unpause", "kill"}[o]
}

// call makes the engine do the action to one container.
func (o containerOp) call(ctx context.Context, c *containerapi.Client, id string) error {
	switch o {
	case opStart:
		return c.StartContainer(ctx, id)
	case opStop:
		return c.StopContainer(ctx, id, stopTimeout)
	case opRestart:
		return c.RestartContainer(ctx, id, stopTimeout)
	case opPause:
		return c.PauseContainer(ctx, id)
	case opUnpause:
		return c.UnpauseContainer(ctx, id)
	}
	return c.KillContainer(ctx, id, "")
}

// nameOf is how a container is called in a message: its name, else its short id.
func (e *ext) nameOf(id string) string {
	if c, ok := e.containerByID(id); ok {
		return c.Name()
	}
	return containerapi.ShortID(id)
}

// runContainerOp does the action to the containers, one after the other; what fails is told in one notification.
func (e *ext) runContainerOp(op containerOp, ids []string) {
	if len(ids) == 0 {
		return
	}
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = e.nameOf(id)
	}
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var failures []string
		for i, id := range ids {
			if err := op.call(ctx, c, id); err != nil {
				failures = append(failures, names[i]+": "+reasonOf(err))
			}
		}
		return failures
	}, func(failures []string) {
		if len(failures) > 0 {
			e.api.Notifications().Error("Cannot " + op.verb() + " " + strings.Join(failures, "; "))
		}
	})
}

// askRemoveContainers asks before containers are removed: how many, which, and how many of them run. A running container is
// removed only by the choice that says it is killed first (SPEC §10.20 "Safety").
func (e *ext) askRemoveContainers(ids []string) {
	var stopped, running []containerapi.Container
	for _, id := range ids {
		if c, ok := e.containerByID(id); ok {
			if c.Running() {
				running = append(running, c)
			} else {
				stopped = append(stopped, c)
			}
		}
	}
	all := append(append([]containerapi.Container(nil), stopped...), running...)
	if len(all) == 0 {
		return
	}
	lines := namesOf(all)
	if len(running) > 0 {
		lines = append(lines, plural(len(running), "container")+" running: "+strings.Join(shortList(containerNames(running), 3), ", "))
	}
	remove := func(cs []containerapi.Container, force bool) func() {
		return func() { e.removeContainers(idsOf(cs), force) }
	}
	var choices []choice
	switch {
	case len(running) == 0:
		choices = []choice{{label: "Remove " + plural(len(all), "container"), run: remove(all, false)}}
	default:
		choices = []choice{{label: "Force remove " + strconv.Itoa(len(all)) + " (kills " + strconv.Itoa(len(running)) + " running)", run: remove(all, true)}}
		if len(stopped) > 0 {
			choices = append(choices, choice{label: "Remove only the " + strconv.Itoa(len(stopped)) + " stopped", run: remove(stopped, false)})
		}
	}
	e.ask(confirmation{section: resContainers, title: "Remove " + plural(len(all), "container") + "?", lines: lines, choices: choices})
}

// containerNames lists the names of containers.
func containerNames(cs []containerapi.Container) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name()
	}
	return out
}

// namesOf lists the names of containers for a confirmation: the first few, then "and N more".
func namesOf(cs []containerapi.Container) []string {
	return []string{strings.Join(shortList(containerNames(cs), 8), ", ")}
}

// shortList cuts a list of names to n, with a last entry that says how many more there are.
func shortList(names []string, n int) []string {
	if len(names) <= n {
		return names
	}
	return append(append([]string(nil), names[:n]...), "and "+strconv.Itoa(len(names)-n)+" more")
}

// removeContainers removes containers, killing the running ones when force is set.
func (e *ext) removeContainers(ids []string, force bool) {
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = e.nameOf(id)
	}
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var failures []string
		for i, id := range ids {
			if err := c.RemoveContainer(ctx, id, containerapi.RemoveOptions{Force: force}); err != nil {
				failures = append(failures, names[i]+": "+reasonOf(err))
			}
		}
		return failures
	}, func(failures []string) {
		if len(failures) > 0 {
			e.api.Notifications().Error("Cannot remove " + strings.Join(failures, "; "))
			return
		}
		e.api.Notifications().Info("Removed " + plural(len(ids), "container") + ".")
	})
}

// copyIDs puts the ids on the clipboard, one per line.
func (e *ext) copyIDs(ids []string) { e.copyText(strings.Join(ids, "\n")) }

// copyText puts a text on the clipboard.
func (e *ext) copyText(text string) {
	if text == "" {
		return
	}
	if err := e.api.Clipboard().WriteText(text); err != nil {
		e.api.Notifications().Warn("Cannot copy to the clipboard: " + err.Error())
	}
}

// openInBrowser sends the browser of the desktop to a port the container publishes; with several ports it asks which.
func (e *ext) openInBrowser(c containerapi.Container) {
	type target struct {
		host string
		port uint16
	}
	var targets []target
	seen := map[uint16]bool{}
	for _, p := range c.Ports {
		if p.PublicPort == 0 || (p.Type != "" && p.Type != "tcp") || seen[p.PublicPort] {
			continue
		}
		seen[p.PublicPort] = true
		host := p.IP
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "localhost"
		}
		targets = append(targets, target{host, p.PublicPort})
	}
	open := func(t target) {
		uri := "http://" + t.host + ":" + strconv.Itoa(int(t.port))
		go func() {
			err := e.api.Workspace().OpenURI(e.ctx, uri)
			e.api.Post(func() {
				if err != nil {
					e.api.Notifications().Warn("Cannot open " + uri + ": " + reasonOf(err))
				}
			})
		}()
	}
	switch len(targets) {
	case 0:
		e.api.Notifications().Info(c.Name() + " publishes no port.")
	case 1:
		open(targets[0])
	default:
		items := make([]extension.QuickPickItem, len(targets))
		for i, t := range targets {
			items[i] = extension.QuickPickItem{Label: t.host + ":" + strconv.Itoa(int(t.port))}
		}
		err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Open which port of " + c.Name() + "?", Items: items}, func(i int) {
			if i >= 0 && i < len(targets) {
				open(targets[i])
			}
		})
		if err != nil {
			e.api.Log().Warn("the quick pick cannot be shown", "err", err)
		}
	}
}

// inspectContainer opens the JSON of a container in a read-only tab.
func (e *ext) inspectContainer(id string) {
	name := e.nameOf(id)
	e.inspectJSON(name+" (inspect)", func(ctx context.Context, c *containerapi.Client) (json.RawMessage, error) {
		info, err := c.InspectContainer(ctx, id)
		if err != nil {
			return nil, err
		}
		return info.Raw, nil
	})
}

// inspectJSON reads an inspect on a goroutine and shows it, indented, in a read-only tab.
func (e *ext) inspectJSON(title string, read func(ctx context.Context, c *containerapi.Client) (json.RawMessage, error)) {
	c, ok := e.ready()
	if !ok {
		return
	}
	go func() {
		raw, err := read(e.ctx, c)
		e.api.Post(func() {
			if err != nil {
				e.api.Notifications().Error("Cannot inspect: " + reasonOf(err))
				return
			}
			e.docs.open(title, indentJSON(raw), "json")
		})
	}()
}

// indentJSON writes JSON with a line for each member, in the order the engine wrote them; text that is not JSON is returned as it
// is.
func indentJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	out.WriteByte('\n')
	return out.String()
}

// runCommandIn asks for a command and runs it in a running container; its output goes to the panel.
func (e *ext) runCommandIn(id string) {
	c, ok := e.containerByID(id)
	if !ok {
		return
	}
	err := e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Command to run in " + c.Name() + " (it runs with sh -c, without a terminal)",
		Validate: func(v string) string {
			if strings.TrimSpace(v) == "" {
				return "Enter a command."
			}
			return ""
		},
	}, func(v string, ok bool) {
		if ok {
			e.logs.runExec(c, strings.TrimSpace(v))
		}
	})
	if err != nil {
		e.api.Log().Warn("the input box cannot be shown", "err", err)
	}
}

// viewLogs follows the logs of a container in the panel.
func (e *ext) viewLogs(id string) {
	if c, ok := e.containerByID(id); ok {
		e.logs.follow(c)
	}
}

// ask shows the question of a confirmation in the view of its section.
func (e *ext) ask(c confirmation) {
	e.confirm = &c
	if v := e.views[sectionOf(c.section)]; v != nil {
		v.handle.Reveal(false)
	}
	e.refreshViews()
}
