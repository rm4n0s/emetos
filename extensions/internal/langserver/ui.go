package langserver

import (
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// The status bar item, its menu and the three answers to a missing server (SPEC §10.34, as §10.17 does for Go).

// statusItem is what the bar shows now.
func (c *Controller) statusItem() extension.StatusItem {
	// the program that runs is named in the texts about the process (Python's three servers share one item)
	name := c.program().Name
	icon, suffix := "", ""
	tip := []string{c.label}
	switch c.server.State {
	case extension.ServerStarting:
		icon = "$(sync~spin) "
		tip = append(tip, name+" is starting")
	case extension.ServerRunning:
		if c.server.Progress != "" {
			icon, suffix = "$(sync~spin) ", ": "+c.server.Progress
			tip = append(tip, name+": "+c.server.Progress)
		} else {
			switch {
			case c.health.Level == HealthError:
				icon = "$(error) "
				tip = append(tip, name+" is running, with errors")
			case c.health.Level == HealthWarning:
				icon = "$(warning) "
				tip = append(tip, name+" is running, with warnings")
			case c.health.Busy:
				icon = "$(sync~spin) "
				tip = append(tip, name+" is working")
			default:
				icon = "$(check) "
				tip = append(tip, name+" is running")
			}
			if c.health.Message != "" {
				tip = append(tip, c.health.Message)
			}
		}
	case extension.ServerFailed:
		icon, suffix = "$(error) ", ": "+name+" stopped"
		tip = append(tip, name+" crashed too often and is not started again. Restart it from the menu.")
	case extension.ServerMissing:
		icon = "$(warning) "
		kind, msg := c.problem()
		if kind == problemDisabled {
			icon = "$(circle-slash) "
		}
		switch kind {
		case problemNotFound:
			suffix = ": " + name + " not found"
		case problemPathInvalid:
			suffix = ": wrong path"
		case problemTooOld:
			suffix = ": " + name + " too old"
		case problemDisabled:
			suffix = ": disabled"
		default:
			suffix = ": " + name + " cannot start"
		}
		if msg != "" {
			tip = append(tip, msg)
		}
	default:
		tip = append(tip, name+" has not started")
	}
	if res := c.lastResult(); res.err == nil && res.found.Path != "" && c.server.State != extension.ServerMissing {
		line := res.found.Path
		if res.version != "" {
			line = name + " " + res.version + ": " + line
		}
		tip = append(tip, line)
	}
	if c.detail != "" {
		tip = append(tip, c.detail)
	}
	tip = append(tip, "Click for the menu.")
	return extension.StatusItem{
		ID: StatusID(c.cfg.ID), Name: c.cfg.Name, Text: icon + c.label + suffix, Tooltip: strings.Join(tip, "\n"),
		Alignment: extension.StatusRight, Priority: statusPriority, OnClick: c.showMenu,
	}
}

// updateStatus shows the item for a file of the server's languages and hides it otherwise; the bar is told only when what it shows
// changes.
func (c *Controller) updateStatus() {
	if c.status == nil {
		return
	}
	item := c.statusItem()
	if key := item.Text + "\x00" + item.Tooltip; key != c.key {
		c.key = key
		c.status.Update(item)
	}
	if show := c.activeIn && c.server.State != extension.ServerStopped; show != c.visible {
		c.visible = show
		c.status.SetVisible(show)
	}
}

// showMenu opens the menu of the status item: Restart, Show Output, Open Settings, the entries of the pack and, for a server that
// is missing, the ways to get it.
func (c *Controller) showMenu() {
	type entry struct {
		item extension.QuickPickItem
		run  func()
	}
	name := c.program().Name
	query := c.cfg.SettingsQuery
	if query == "" {
		query = c.cfg.Name
	}
	entries := []entry{
		{extension.QuickPickItem{Label: "$(debug-restart) Restart Language Server", Description: name}, c.restart},
		{extension.QuickPickItem{Label: "$(output) Show " + c.cfg.Name + " Output"}, c.showOutput},
		{extension.QuickPickItem{Label: "$(settings-gear) Open Settings", Description: query, Command: "workbench.action.openSettings", Args: query}, nil},
	}
	for _, m := range c.cfg.Menu {
		entries = append(entries, entry{extension.QuickPickItem{Label: m.Label, Description: m.Description}, m.Run})
	}
	if c.server.State == extension.ServerMissing {
		entries = append(entries, entry{extension.QuickPickItem{Label: "$(book) How to Install", Description: name}, c.howToInstall})
		if _, ok := c.program().Install.CommandFor(Family(c.osRelease())); ok {
			entries = append(entries, entry{extension.QuickPickItem{Label: "$(copy) Copy Install Command"}, c.copyInstallCommand})
		}
	}
	if c.program().PathSetting != "" {
		entries = append(entries, entry{extension.QuickPickItem{Label: "$(folder-opened) Set Path...", Description: c.program().PathSetting}, c.setPath})
	}
	items := make([]extension.QuickPickItem, len(entries))
	for i, e := range entries {
		items[i] = e.item
	}
	err := c.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: c.label + " - " + name, Items: items}, func(i int) {
		if i >= 0 && i < len(entries) && entries[i].run != nil {
			entries[i].run()
		}
	})
	if err != nil && !benign(err) {
		c.api.Log().Warn("the menu of a language server cannot be shown", "server", c.cfg.ID, "err", err)
	}
}

// copyInstallCommand puts the usual command that installs the server for this system on the clipboard. Emetos never runs it (D47).
func (c *Controller) copyInstallCommand() {
	cmd, ok := c.program().Install.CommandFor(Family(c.osRelease()))
	if !ok {
		return
	}
	if err := c.api.Clipboard().WriteText(cmd.Command); err != nil {
		if !benign(err) {
			c.api.Log().Warn("the install command cannot be copied", "server", c.cfg.ID, "err", err)
		}
		return
	}
	c.api.Notifications().Info("Copied: " + cmd.Command)
}

// installText is the text of the dialog How to Install.
func (c *Controller) installText() string {
	p := c.program()
	family := Family(c.osRelease())
	var sb strings.Builder
	if c.cfg.Purpose != "" {
		sb.WriteString(p.Name + " provides " + c.cfg.Purpose + ".\n\n")
	}
	sb.WriteString("Emetos does not install language servers. Install " + p.Name + " yourself and restart the server from the menu of the status bar item.\n")
	if cmd, ok := p.Install.CommandFor(family); ok {
		sb.WriteString("\nFor this system:\n    " + cmd.Command + "\n")
		var others []string
		for _, o := range p.Install.Commands {
			if o != cmd {
				others = append(others, "    "+installLabel(o.Family)+": "+o.Command)
			}
		}
		if len(others) > 0 {
			sb.WriteString("\nAlso:\n" + strings.Join(others, "\n") + "\n")
		}
	}
	if p.Install.Docs != "" {
		sb.WriteString("\nInstructions: " + p.Install.Docs + "\n")
	}
	if p.PathSetting != "" {
		sb.WriteString("\nIf " + p.Name + " is installed somewhere else, set " + p.PathSetting + " (Set Path... in the menu).")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// installLabel names a family in the dialog.
func installLabel(family string) string {
	switch family {
	case FamilyFedora:
		return "Fedora"
	case FamilyDebian:
		return "Debian and Ubuntu"
	case FamilyArch:
		return "Arch"
	case FamilySuse:
		return "openSUSE"
	}
	return "Any system"
}

// howToInstall shows how to get the server in a dialog, with a button that copies the command.
func (c *Controller) howToInstall() {
	text := c.installText()
	p := c.program()
	buttons := []extension.DialogButton{{Label: "Close", Cancel: true}}
	_, haveCommand := p.Install.CommandFor(Family(c.osRelease()))
	if haveCommand {
		buttons = []extension.DialogButton{{Label: "Copy Install Command"}, {Label: "Close", Cancel: true}}
	}
	err := c.api.Dialogs().Ask(extension.DialogSpec{Type: extension.DialogInfo, Message: "How to install " + p.Name, Detail: text, Buttons: buttons}, func(r extension.DialogResult) {
		if haveCommand && !r.Cancelled && r.Button == 0 {
			c.copyInstallCommand()
		}
	})
	switch {
	case err == nil:
	case benign(err):
		// a host with no dialogs (a window tab that is not on screen): the notification carries the text
		c.api.Notifications().Info(text)
	default:
		c.api.Log().Warn("the install instructions of a language server cannot be shown", "server", c.cfg.ID, "err", err)
	}
}

// setPath asks for the path of the program and writes it to the user's settings. The path is a setting of the host: a client of a
// hosted workspace cannot change it (SPEC §10.18).
func (c *Controller) setPath() {
	p := c.program()
	if p.PathSetting == "" {
		return
	}
	if c.api.Origins().Current() != extension.HostOrigin {
		c.api.Notifications().Info("The path of " + p.Name + " is a setting of the host and cannot be changed from here.")
		return
	}
	current, _ := extension.SettingValue[string](c.api.Settings(), p.PathSetting)
	env := c.env()
	err := c.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Path of the " + p.Name + " program", Value: current,
		Message:  "Saved as " + p.PathSetting + " in your settings. Leave it empty to look for " + p.Name + " in the usual places.",
		Validate: func(v string) string { return CheckPath(env, v) },
	}, func(value string, ok bool) {
		if !ok {
			return
		}
		if err := c.api.Settings().Update(p.PathSetting, strings.TrimSpace(value)); err != nil && !benign(err) {
			c.api.Log().Warn("the path of a language server cannot be saved", "server", c.cfg.ID, "setting", p.PathSetting, "err", err)
			c.api.Notifications().Error("The path cannot be saved: " + err.Error())
		}
	})
	if err != nil && !benign(err) {
		c.api.Log().Warn("the box for the path of a language server cannot be shown", "server", c.cfg.ID, "err", err)
	}
}
