package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/statefile"
	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// The shell integration of the terminals of a window tab (SPEC §10.25 "Shell integration"): the scripts that are injected when a
// shell starts, what is done with a command that ended (the history, the quick fixes), and the commands that use the commands
// the shell told about.

// fixSet are the quick fixes of the last command of a terminal: start is the marker of that command, which says which command
// they belong to.
type fixSet struct {
	start emulator.MarkerID
	list  []quickFix
}

// newNonce makes the secret of a terminal, that only the scripts of its shell know: they print it with the command line.
func newNonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "" // no secret: no command line is believed, and the lines are read from the screen
	}
	return hex.EncodeToString(b[:])
}

// cacheDir is where the scripts are written: the cache folder of Emetos ($XDG_CACHE_HOME/emetos, else ~/.cache/emetos).
func (t *terminals) cacheDir() string {
	base := t.env.get("XDG_CACHE_HOME")
	if !filepath.IsAbs(base) {
		if t.env.home == "" {
			return ""
		}
		base = filepath.Join(t.env.home, ".cache")
	}
	return filepath.Join(base, "emetos")
}

// shellIntegration decides how a shell starts: with the scripts injected (the profile with its arguments changed and the
// variables the scripts read), or as it is. The second result is the variables to add to the environment, the third the secret
// of the terminal and the last whether the scripts were injected. A shell that cannot be injected, or scripts that cannot be
// written, leave the terminal without shell integration; the reason is logged.
func (t *terminals) shellIntegration(prof profile) (profile, []string, string, bool) {
	if !t.cfg.shellInteg {
		return prof, nil, "", false
	}
	cache := t.cacheDir()
	if cache == "" {
		return prof, nil, "", false
	}
	root, err := writeScripts(cache)
	if err != nil {
		t.api.Log().Warn("the shell integration scripts cannot be written", "err", err)
		return prof, nil, "", false
	}
	tmp := os.TempDir()
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real
	}
	username := "unknown"
	if u, err := user.Current(); err == nil && u.Username != "" {
		username = u.Username
	}
	nonce := newNonce()
	inj, reason := shellIntegrationInjection(prof.path, prof.args, injectOpts{
		enabled: true, root: root, nonce: nonce, home: t.env.home, zdotdir: t.env.get("ZDOTDIR"), tmp: tmp, username: username, appName: "emetos",
	})
	if reason != injectNone {
		t.api.Log().Debug("shell integration is not injected", "shell", prof.path, "reason", reason)
		return prof, nil, "", false
	}
	if err := prepareZdotdir(inj, false); err != nil {
		t.api.Log().Warn("the shell integration of zsh cannot be prepared", "err", err)
		return prof, nil, "", false
	}
	prof.args = inj.args
	var env []string
	for k, v := range inj.env {
		env = append(env, k+"="+v)
	}
	return prof, env, nonce, true
}

// setEnv adds variables to an environment, replacing the ones that are there.
func setEnv(env []string, add []string) []string {
	for _, kv := range add {
		name, _, _ := strings.Cut(kv, "=")
		replaced := false
		for i, old := range env {
			if strings.HasPrefix(old, name+"=") {
				env[i], replaced = kv, true
				break
			}
		}
		if !replaced {
			env = append(env, kv)
		}
	}
	return env
}

// hookShell connects the shell integration of a terminal to the extension: a command that ended goes to the history and gets
// its quick fixes, a folder the shell reported goes to the history of folders.
func (t *terminals) hookShell(e *entry) {
	si := e.term.si
	shell := e.term.ShellName()
	si.SetHooks(func(v cmdView) {
		// the quick fixes read the output now, on the goroutine that feeds the emulator: a later chunk may have drawn the prompt
		var fixes []quickFix
		if v.hasExit && t.cfg.quickFix {
			fixes = quickFixesFor(builtinFixes(), v.text, v.exit, si.outputLines(v))
		}
		start := v.start
		go func() {
			if err := t.hist.AddCommand(v.text, shell); err != nil {
				t.api.Log().Debug("the command cannot be added to the history", "err", err)
			}
		}()
		t.api.Post(func() { t.commandFinished(e, start, fixes) })
	}, func(dir string) {
		go func() {
			if err := t.hist.AddDir(dir); err != nil {
				t.api.Log().Debug("the folder cannot be added to the history", "err", err)
			}
		}()
	})
}

// commandFinished takes the quick fixes of the command that ended, replacing the ones of the one before.
func (t *terminals) commandFinished(e *entry, start emulator.MarkerID, fixes []quickFix) {
	if _, _, _, ok := t.find(e); !ok {
		return
	}
	if len(fixes) == 0 {
		e.fixes = nil
	} else {
		e.fixes = &fixSet{start: start, list: fixes}
	}
	if e.pv.render != nil {
		e.pv.render.MarkNeedsPaint()
	}
}

// fixesOf is the quick fixes of the terminal of a pane.
func (t *terminals) fixesOf(pv *paneView) *fixSet {
	for _, e := range t.all() {
		if e.pv == pv {
			return e.fixes
		}
	}
	return nil
}

// runFix carries out a quick fix in the terminal of a pane.
func (t *terminals) runFix(pv *paneView, f quickFix) {
	switch f.kind {
	case fixCommand:
		pv.sendText(f.command, f.execute, false)
	case fixOpen:
		t.openLink(f.uri, false)
	case fixPort:
		port, err := strconv.Atoi(f.port)
		if err != nil {
			return
		}
		n := killPort("/proc", port, func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) })
		if n == 0 {
			t.api.Notifications().Warn("No process of yours holds port " + f.port + ".")
		}
		// the command that failed is typed again for the user to run
		pv.sendText(f.command, false, false)
	}
}

// showCommandMenu opens the menu of a decoration (or of the quick fixes) at a place of the window.
func (t *terminals) showCommandMenu(pv *paneView, items []ui.MenuItem, at gui.Offset) {
	if len(items) == 0 {
		return
	}
	t.menu.Show(at, items)
}

// showStickyMenu opens the menu of the sticky scroll overlay: it turns it off.
func (t *terminals) showStickyMenu(pv *paneView, at gui.Offset) {
	t.menu.Show(at, []ui.MenuItem{
		{ID: "toggleSticky", Label: "Toggle Sticky Scroll", Checked: t.cfg.stickyOn, OnRun: t.toggleStickyScroll},
	})
}

// toggleStickyScroll turns terminal.integrated.stickyScroll.enabled on or off.
func (t *terminals) toggleStickyScroll() {
	if err := t.api.Settings().Update(settingStickyEnabled, !t.cfg.stickyOn); err != nil {
		t.api.Log().Warn("the setting cannot be written", "err", err)
		t.api.Notifications().Error("The setting cannot be saved: " + err.Error())
	}
}

// ---- the commands ----

// lastCommand is the last command that ended in the terminal a command acts on.
func (t *terminals) lastCommand() (*entry, *cmdView) {
	e := t.target()
	if e == nil {
		return nil, nil
	}
	cmds := e.term.si.Commands()
	if len(cmds) == 0 {
		return e, nil
	}
	return e, &cmds[len(cmds)-1]
}

// lastText is the text Copy Last Command, Copy Last Command Output and Copy Last Command and Output copy: the command line,
// its output, or both with a line break between; "" when no command ended yet.
func (t *terminals) lastText(command, output bool) (*entry, string) {
	e, v := t.lastCommand()
	if v == nil {
		return e, ""
	}
	var parts []string
	if command {
		parts = append(parts, v.text)
	}
	if output {
		parts = append(parts, strings.TrimSuffix(e.term.si.output(*v), "\n"))
	}
	return e, strings.Join(parts, "\n")
}

// copyLast copies the last command, its output, or both.
func (t *terminals) copyLast(command, output bool) {
	if e, text := t.lastText(command, output); text != "" {
		e.pv.copyText(text)
	}
}

// showQuickFixes opens the menu of the quick fixes of the last command (Ctrl+.).
func (t *terminals) showQuickFixes() {
	e, v := t.lastCommand()
	if v == nil || e.fixes == nil || e.fixes.start != v.start || !t.cfg.quickFix {
		return
	}
	at := gui.Offset{Dx: padLeft, Dy: 0}
	if r := e.pv.render; r != nil && e.pv.shown != nil && e.pv.factory != nil {
		a, _ := e.pv.atlases()
		row := float64(v.startLine-e.pv.shown.Top) * a.m.h
		at = r.LocalToGlobal(gui.Offset{Dx: padLeft, Dy: min(max(row, 0), r.Size().Height-a.m.h) + a.m.h}, nil)
	}
	t.showCommandMenu(e.pv, e.pv.commandMenu(*v), at)
}

// runRecentCommand shows the commands of this session, of the earlier sessions and of the shell's history file, and runs the
// one that is picked in the terminal (Ctrl+Alt+R).
func (t *terminals) runRecentCommand() {
	e := t.activeOrCreate()
	if e == nil {
		return
	}
	t.handle.Reveal(false)
	shell := e.term.ShellName()
	type item struct {
		text string
		pick extension.QuickPickItem
	}
	var items []item
	seen := map[string]bool{}
	add := func(text, description string) {
		clean := strings.TrimSpace(text)
		if clean == "" || seen[clean] {
			return
		}
		seen[clean] = true
		items = append(items, item{text: clean, pick: extension.QuickPickItem{Label: formatPickLabel(clean), Description: description}})
	}
	// this session, newest first
	if cur := e.term.si.Current(); cur != nil && cur.executed != 0 && cur.text != "" {
		add(cur.text, "running")
	}
	cmds := e.term.si.Commands()
	for i := len(cmds) - 1; i >= 0; i-- {
		d := collapseTilde(cmds[i].cwd, t.env.home)
		if cmds[i].hasExit && cmds[i].exit != 0 {
			d += " exitCode: " + strconv.Itoa(cmds[i].exit)
		}
		add(cmds[i].text, strings.TrimSpace(d))
	}
	// earlier sessions
	old, _, err := t.hist.snapshot(shell)
	if err != nil {
		t.api.Log().Debug("the command history cannot be read", "err", err)
	}
	for _, c := range old {
		add(c, "previous session")
	}
	// the history file of the shell
	if sh := shellHistoryFile(shell, t.env.home, t.env.get("XDG_DATA_HOME")); sh != nil {
		start := max(len(sh.commands)-500, 0)
		for i := len(sh.commands) - 1; i >= start; i-- {
			add(sh.commands[i], sh.label)
		}
	}
	if len(items) == 0 {
		t.api.Notifications().Info("There are no commands to run yet.")
		return
	}
	picks := make([]extension.QuickPickItem, len(items))
	for i, it := range items {
		picks[i] = it.pick
	}
	err = t.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a command to run", Items: picks, MatchOnDescription: true}, func(i int) {
		if i < 0 || i >= len(items) {
			return
		}
		e.pv.scrollTo(0)
		e.pv.sendText(items[i].text, true, strings.Contains(items[i].text, "\n") && e.term.Emulator().Modes().BracketedPaste)
		t.focusEntry(e, false)
	})
	if err != nil {
		t.api.Log().Warn("the commands cannot be listed", "err", err)
	}
}

// goToRecentDirectory shows the folders of this session and of the earlier ones, and changes to the one that is picked with cd
// in the terminal (Ctrl+G).
func (t *terminals) goToRecentDirectory() {
	e := t.activeOrCreate()
	if e == nil {
		return
	}
	t.handle.Reveal(false)
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	session := e.term.si.Cwds()
	for i := len(session) - 1; i >= 0; i-- {
		add(session[i])
	}
	nSession := len(dirs)
	_, old, err := t.hist.snapshot("")
	if err != nil {
		t.api.Log().Debug("the folder history cannot be read", "err", err)
	}
	for _, d := range old {
		add(d)
	}
	if len(dirs) == 0 {
		t.api.Notifications().Info("There are no folders to go to yet.")
		return
	}
	picks := make([]extension.QuickPickItem, len(dirs))
	for i, d := range dirs {
		desc := "previous session"
		if i < nSession {
			desc = "current session"
		}
		picks[i] = extension.QuickPickItem{Label: collapseTilde(d, t.env.home), Description: desc}
	}
	err = t.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a directory to go to", Items: picks}, func(i int) {
		if i < 0 || i >= len(dirs) {
			return
		}
		e.pv.scrollTo(0)
		e.pv.sendText("cd "+preparePathForShell(dirs[i], e.term.Profile().path, e.term.Meta().title), true, false)
		t.focusEntry(e, false)
	})
	if err != nil {
		t.api.Log().Warn("the folders cannot be listed", "err", err)
	}
}

// clearPreviousSessionHistory forgets the command history of the earlier sessions.
func (t *terminals) clearPreviousSessionHistory() {
	if err := t.hist.Clear(); err != nil {
		t.api.Log().Warn("the command history cannot be cleared", "err", err)
		t.api.Notifications().Error("The command history cannot be cleared: " + err.Error())
	}
}

// formatPickLabel shortens a command line for a list: line breaks become the "return" symbol and runs of spaces a midline
// ellipsis (VS Code's formatLabel).
func formatPickLabel(label string) string {
	label = strings.NewReplacer("\r\n", "⏎", "\n", "⏎").Replace(label)
	var b strings.Builder
	spaces := 0
	flush := func() {
		if spaces >= 3 {
			b.WriteString("⋯")
		} else {
			b.WriteString(strings.Repeat(" ", spaces))
		}
		spaces = 0
	}
	for _, r := range label {
		if r == ' ' || r == '\t' {
			spaces++
			continue
		}
		flush()
		b.WriteRune(r)
	}
	flush()
	return b.String()
}

// collapseTilde writes a path under the home folder as ~/...
func collapseTilde(path, home string) string {
	if home != "" && (path == home || strings.HasPrefix(path, home+"/")) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// stateDir is the folder of the state files of Emetos: $XDG_STATE_HOME/emetos, else ~/.local/state/emetos, read from the
// environment of the terminals (the same as statefile.DirFromEnv, which reads the environment of the process).
func (t *terminals) stateDir() (string, error) {
	base := t.env.get("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		if t.env.home == "" {
			return statefile.DirFromEnv()
		}
		base = filepath.Join(t.env.home, ".local", "state")
	}
	return filepath.Join(filepath.Clean(base), "emetos"), nil
}
