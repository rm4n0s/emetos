package golang

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The status bar item (SPEC §10.17 "As built (step 14.1)"): the Go version of the workspace and the state of gopls, with a menu.

// statusPriority puts the item on the right side of the bar, just after the language mode.
const statusPriority = 100.05

// programSettings is the text of the settings that decide which programs run and with what environment: when it changes the
// version is asked for again.
func (g *golang) programSettings() string {
	var sb strings.Builder
	for _, key := range []string{AlternateToolsSetting, ToolsEnvVarsSetting} {
		v, _ := g.api.Settings().Get(key)
		sb.WriteString(toJSON(v))
		sb.WriteByte(0)
	}
	return sb.String()
}

// settingsChanged asks for the version again when a setting that decides the go program or its environment changed.
func (g *golang) settingsChanged() {
	if key := g.programSettings(); key != g.settingsKey {
		g.settingsKey = key
		g.askVersion()
	}
}

// askVersion runs `go env GOVERSION` off the UI thread and shows the answer. The answer of an older question is dropped. It runs
// with GOTOOLCHAIN=local: a question asked in the background must never download a toolchain.
func (g *golang) askVersion() {
	g.versionRun++
	run := g.versionRun
	program, err := g.goProgram()
	if err != nil {
		g.version, g.goError = "", "The go command was not found. Install Go, or set go.alternateTools."
		g.updateStatus()
		return
	}
	spec := cmdSpec{Program: program, Args: []string{"env", "GOVERSION"}, Dir: g.api.Scope().Folder, Env: g.programEnv("GOTOOLCHAIN=local")}
	g.closers.Add(1)
	go func() {
		defer g.closers.Done()
		ctx, cancel := context.WithTimeout(g.ctx, versionTimeout)
		defer cancel()
		var out []string
		err := runProgram(ctx, spec, func(l string) { out = append(out, l) })
		g.api.Post(func() {
			if run != g.versionRun {
				return
			}
			switch {
			case err != nil:
				g.version, g.goError = "", "go env GOVERSION failed: "+failureText(err, out)
			case len(out) == 0 || strings.TrimSpace(out[len(out)-1]) == "":
				g.version, g.goError = "", "go env GOVERSION printed nothing."
			default:
				g.version, g.goError = strings.TrimSpace(out[len(out)-1]), ""
			}
			g.updateStatus()
		})
	}()
}

// toJSON is the JSON of a value, for comparing settings.
func toJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

// failureText says why a program failed: what it wrote last, else the error.
func failureText(err error, out []string) string {
	for i := len(out) - 1; i >= 0; i-- {
		if strings.TrimSpace(out[i]) != "" {
			return strings.TrimSpace(out[i])
		}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.Error()
	}
	return err.Error()
}

// versionLabel is "Go 1.26.1" for "go1.26.1", and "Go" while the version is not known.
func versionLabel(version string) string {
	v := strings.TrimPrefix(version, "go")
	if v == "" {
		return "Go"
	}
	return "Go " + v
}

// serverChanged follows the state of gopls.
func (g *golang) serverChanged(st extension.ServerStatus) {
	g.server = st
	if tl, ok := toolByName(gopls); ok && st.State == extension.ServerMissing {
		g.promptMissingTool(tl)
	}
	g.updateStatus()
}

// statusItem is what the bar shows now.
func (g *golang) statusItem() extension.StatusItem {
	label := versionLabel(g.version)
	icon, suffix, tip := "", "", label
	switch {
	case g.goError != "":
		icon, tip = "$(warning) ", g.goError
	case g.server.State == extension.ServerStarting:
		icon, tip = "$(sync~spin) ", tip+"\ngopls is starting"
	case g.server.State == extension.ServerRunning && g.server.Progress != "":
		icon, suffix, tip = "$(sync~spin) ", ": "+g.server.Progress, tip+"\ngopls: "+g.server.Progress
	case g.server.State == extension.ServerRunning:
		icon, tip = "$(check) ", tip+"\ngopls is running"
	case g.server.State == extension.ServerFailed:
		icon, suffix, tip = "$(error) ", ": gopls stopped", tip+"\ngopls crashed too often and is not started again. Restart it from the menu."
	case g.server.State == extension.ServerMissing:
		icon, suffix, tip = "$(warning) ", ": gopls not found", tip+"\ngopls was not found. Install it from the menu, or set go.goplsPath."
	default:
		tip += "\ngopls has not started"
	}
	return extension.StatusItem{
		ID: StatusID, Name: "Go", Text: icon + label + suffix, Tooltip: tip + "\nClick for the menu.",
		Alignment: extension.StatusRight, Priority: statusPriority, OnClick: g.showMenu,
	}
}

// updateStatus shows the item for a Go file and hides it otherwise; the bar is told only when what it shows changes.
func (g *golang) updateStatus() {
	if g.status == nil {
		return
	}
	item := g.statusItem()
	if key := item.Text + "\x00" + item.Tooltip; key != g.statusKey {
		g.statusKey = key
		g.status.Update(item)
	}
	if show := g.activeGo && g.server.State != extension.ServerStopped; show != g.visible {
		g.visible = show
		g.status.SetVisible(show)
	}
}

// The entries of the menu of the status item, in order.
const (
	menuRestart = iota
	menuOutput
	menuTools
	menuEnvironment
	menuSettings
)

// showMenu opens the menu of the status item.
func (g *golang) showMenu() {
	err := g.api.QuickPick().Show(extension.QuickPickOptions{
		Placeholder: versionLabel(g.version) + " - gopls",
		Items: []extension.QuickPickItem{
			menuRestart:     {Label: "$(debug-restart) Restart Language Server", Description: "gopls"},
			menuOutput:      {Label: "$(output) Show gopls Output"},
			menuTools:       {Label: "$(tools) Install/Update Tools", Description: "gopls, dlv, staticcheck"},
			menuEnvironment: {Label: "$(info) Show Go Environment", Description: "go env"},
			menuSettings:    {Label: "$(settings-gear) Open Settings", Description: GoplsSetting, Command: "workbench.action.openSettings", Args: GoplsSetting},
		},
	}, g.menuChosen)
	if err != nil {
		g.api.Log().Warn("the menu of the Go status item cannot be shown", "err", err)
	}
}

// menuChosen runs the entry of the menu that was chosen.
func (g *golang) menuChosen(index int) {
	var err error
	switch index {
	case menuRestart:
		err = g.api.LanguageServers().Restart(ServerID)
	case menuOutput:
		err = g.api.LanguageServers().ShowOutput(ServerID, false)
	case menuTools:
		g.startInstall(nil)
	case menuEnvironment:
		g.showEnvironment()
	}
	if err != nil {
		g.api.Log().Warn("an entry of the menu of the Go status item failed", "index", index, "err", err)
	}
}
