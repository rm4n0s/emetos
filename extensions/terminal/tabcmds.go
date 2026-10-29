package terminal

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The rest of the tabs list (SPEC §10.25 "The rest of the tabs list"): the icon and the color of a tab, joining and unsplitting
// terminals, killing them all, a new terminal in a folder, and the commands that put a terminal in the editor area.

// standardColors are the colors a tab can be given (VS Code's getStandardColors: the ansi colors that are not bright), with
// the names it shows.
var standardColors = []struct{ id, name string }{
	{"terminal.ansiBlack", "Black"}, {"terminal.ansiRed", "Red"}, {"terminal.ansiGreen", "Green"}, {"terminal.ansiYellow", "Yellow"},
	{"terminal.ansiBlue", "Blue"}, {"terminal.ansiMagenta", "Magenta"}, {"terminal.ansiCyan", "Cyan"}, {"terminal.ansiWhite", "White"},
}

// ansiIndex is the place of one of the sixteen terminal.ansi* colors among them; -1 for any other id.
func ansiIndex(id string) int {
	names := [16]string{"Black", "Red", "Green", "Yellow", "Blue", "Magenta", "Cyan", "White", "BrightBlack", "BrightRed", "BrightGreen",
		"BrightYellow", "BrightBlue", "BrightMagenta", "BrightCyan", "BrightWhite"}
	if n, ok := strings.CutPrefix(id, "terminal.ansi"); ok {
		return slices.Index(names[:], n)
	}
	return -1
}

// changeIcon is the command Change Icon...: a list of the Codicons, and the pick becomes the icon of the tab of the terminal.
func (t *terminals) changeIcon(e *entry) {
	if e == nil {
		return
	}
	names := ui.CodiconNames()
	items := make([]extension.QuickPickItem, len(names))
	for i, n := range names {
		items[i] = extension.QuickPickItem{Label: "$(" + n + ") " + n}
	}
	err := t.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select an icon for the terminal", Items: items}, func(i int) {
		if i >= 0 && i < len(names) && t.alive(e) {
			t.setIcon(e, names[i])
		}
	})
	if err != nil {
		t.api.Log().Warn("the icons cannot be listed", "err", err)
	}
}

// setIcon gives a terminal's tab an icon ("" for its profile's).
func (t *terminals) setIcon(e *entry, icon string) {
	e.term.SetIcon(icon)
	t.syncTab(e)
	t.handle.Refresh()
}

// changeColor is the command Change Color...: the colors of a tab in a quick pick, and "Default" to take the color away.
func (t *terminals) changeColor(e *entry) {
	if e == nil {
		return
	}
	items := []extension.QuickPickItem{{Label: "$(circle-slash) Default"}}
	for _, c := range standardColors {
		items = append(items, extension.QuickPickItem{Label: "$(circle-filled) " + c.name, Description: c.id})
	}
	err := t.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a color for the terminal", Items: items, MatchOnDescription: true}, func(i int) {
		if i < 0 || i >= len(items) || !t.alive(e) {
			return
		}
		color := ""
		if i > 0 {
			color = standardColors[i-1].id
		}
		t.setColor(e, color)
	})
	if err != nil {
		t.api.Log().Warn("the colors cannot be listed", "err", err)
	}
}

// setColor gives a terminal's tab the color of one of the ansi colors ("" for none).
func (t *terminals) setColor(e *entry, color string) {
	if color != "" && ansiIndex(color) < 0 {
		return
	}
	e.term.SetColor(color)
	t.syncTab(e)
	t.handle.Refresh()
}

// joinPick is the command Join Terminals...: the other terminals of the panel in a quick pick; the one picked, with the terminals
// beside it, joins the tab of the active terminal.
func (t *terminals) joinPick() {
	target := t.target()
	if target == nil || target.inEditor() {
		return
	}
	var others []*entry
	for _, e := range t.panelEntries() {
		if g, _, _, _ := t.find(e); g != nil && g != groupOf(t, target) {
			others = append(others, e)
		}
	}
	if len(others) == 0 {
		t.api.Notifications().Warn("Insufficient terminals for the join action")
		return
	}
	items := make([]extension.QuickPickItem, len(others))
	for i, e := range others {
		items[i] = extension.QuickPickItem{Label: "$(" + t.tabIconOf(e) + ") " + t.title(e)}
	}
	err := t.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a terminal to join", Items: items}, func(i int) {
		if i >= 0 && i < len(others) && t.alive(others[i]) && t.alive(target) {
			t.join(target, others[i])
		}
	})
	if err != nil {
		t.api.Log().Warn("the terminals cannot be listed", "err", err)
	}
}

// groupOf is the group a panel terminal is in.
func groupOf(t *terminals, e *entry) *group {
	g, _, _, _ := t.find(e)
	return g
}

// join puts the terminals of the tab of from beside the terminals of the tab of into (VS Code's joinInstances): into's tab holds
// them all, and from's tab is gone.
func (t *terminals) join(into, from *entry) {
	gi, gj := groupOf(t, into), groupOf(t, from)
	if gi == nil || gj == nil || gi == gj {
		return
	}
	for i, e := range gj.entries {
		size := 1000.0
		if i < len(gj.sizes) {
			size = gj.sizes[i]
		}
		gi.entries = append(gi.entries, e)
		gi.sizes = append(gi.sizes, size)
	}
	gi.active = slices.Index(gi.entries, from)
	t.dropGroup(gj)
	t.cur = slices.Index(t.groups, gi)
	t.syncKeys()
	t.handle.Refresh()
	t.focusEntry(from, false)
}

// dropGroup takes a group out of the list, keeping the same group selected.
func (t *terminals) dropGroup(g *group) {
	var keep *group
	if t.cur >= 0 && t.cur < len(t.groups) {
		keep = t.groups[t.cur]
	}
	if i := slices.Index(t.groups, g); i >= 0 {
		t.groups = slices.Delete(t.groups, i, i+1)
	}
	if i := slices.Index(t.groups, keep); i >= 0 {
		t.cur = i
	} else {
		t.cur = max(min(t.cur, len(t.groups)-1), 0)
	}
}

// unsplit gives a terminal of a split a tab of its own, after its group (VS Code's unsplitInstance).
func (t *terminals) unsplit(e *entry) {
	g, gi, ei, ok := t.find(e)
	if !ok || len(g.entries) < 2 {
		return
	}
	g.entries = slices.Delete(g.entries, ei, ei+1)
	if ei < len(g.sizes) {
		g.sizes = slices.Delete(g.sizes, ei, ei+1)
	}
	g.active = min(g.active, len(g.entries)-1)
	t.nextGr++
	ng := &group{id: t.nextGr, entries: []*entry{e}, sizes: []float64{1000}, split: ui.NewSplitViewController()}
	t.groups = slices.Insert(t.groups, gi+1, ng)
	t.cur = gi + 1
	t.syncKeys()
	t.handle.Refresh()
	t.focusEntry(e, false)
}

// killAll ends every terminal, in the panel and in editor tabs.
func (t *terminals) killAll() {
	for _, e := range slices.Clone(t.all()) {
		t.kill(e)
	}
}

// newWithCwd is the command Create New Terminal Starting in a Custom Working Directory. Its argument {cwd} is the folder; without
// it a box asks for one.
func (t *terminals) newWithCwd(args any) {
	if m, ok := args.(map[string]any); ok {
		if cwd, _ := m["cwd"].(string); cwd != "" {
			t.openIn(cwd)
			return
		}
	}
	start := t.folder
	if start == "" {
		start = t.env.home
	}
	err := t.api.InputBox().Show(extension.InputBoxOptions{Prompt: "Working directory of the new terminal", Value: start}, func(value string, ok bool) {
		if ok && strings.TrimSpace(value) != "" {
			t.openIn(strings.TrimSpace(value))
		}
	})
	if err != nil {
		t.api.Log().Warn("the box for the folder cannot be shown", "err", err)
	}
}

// openIn makes a terminal in a folder (~ is the home folder, a relative path is relative to the folder of the workspace); a
// folder that does not exist is said in a notification.
func (t *terminals) openIn(dir string) {
	dir = expandHome(dir, t.env.home)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(t.folder, dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.api.Notifications().Warn("The folder " + dir + " does not exist.")
		return
	}
	if _, err := t.open(openOpts{dir: dir}); err != nil {
		t.api.Log().Warn("the terminal cannot be started", "err", err)
	}
}

// createEditor is the command Create New Terminal in Editor Area; its argument may be a profile name or {profileName, cwd, name}.
func (t *terminals) createEditor(args any) {
	o := openOpts{location: locEditor}
	switch a := args.(type) {
	case string:
		o.profile = t.profileNamed(a)
	case map[string]any:
		if n, _ := a["profileName"].(string); n != "" {
			o.profile = t.profileNamed(n)
		}
		o.dir, _ = a["cwd"].(string)
		o.name, _ = a["name"].(string)
	}
	if _, err := t.open(o); err != nil {
		t.api.Log().Warn("the terminal cannot be started", "err", err)
	}
}

// onTarget is the handler of a command on the terminal it acts on.
func (t *terminals) onTarget(f func(e *entry)) extension.CommandHandler {
	return func(context.Context, any) error {
		if e := t.target(); e != nil {
			f(e)
		}
		return nil
	}
}

// toggleSizeToContent is the command Toggle Size to Content Width.
func (t *terminals) toggleSizeToContent(e *entry) {
	e.pv.toggleSizeToContent()
	t.focusEntry(e, false)
}
