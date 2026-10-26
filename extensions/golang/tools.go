package golang

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// "Go: Install/Update Tools" (SPEC §10.17 "As built (step 14.1)"): `go install <package>@<version>` for each tool, one after the
// other, in the panel view "Go Tools" with the output of the programs and a Cancel button, and the notification for a tool that
// is missing.

// gopls is the name of the language server among the tools.
const gopls = "gopls"

// tool is a program the Go extension uses and installs.
type tool struct {
	// Name is the name of the program, Package the package that `go install` builds it from and Version the version asked for.
	Name, Package, Version string
	// Purpose says what the extension uses it for, in the notification for a missing tool; Hint is added to it.
	Purpose, Hint string
}

// allTools are the tools of the extension, in the order they are installed (SPEC §10.17: gopls, dlv, staticcheck).
func allTools() []tool {
	return []tool{
		{Name: gopls, Package: "golang.org/x/tools/gopls", Version: "latest", Purpose: "provides the language features of Go: completion, hover, diagnostics, formatting, rename and more", Hint: "Set go.goplsPath if it is somewhere else."},
		{Name: "dlv", Package: "github.com/go-delve/delve/cmd/dlv", Version: "latest", Purpose: "debugs Go programs"},
		{Name: "staticcheck", Package: "honnef.co/go/tools/cmd/staticcheck", Version: "latest", Purpose: "checks Go code for mistakes"},
	}
}

// toolByName finds a tool.
func toolByName(name string) (tool, bool) {
	for _, t := range allTools() {
		if t.Name == name {
			return t, true
		}
	}
	return tool{}, false
}

// ---- the programs ----

// alternateTool is the program go.alternateTools names for the tool, or "".
func (g *golang) alternateTool(name string) string {
	m, err := extension.SettingValue[map[string]any](g.api.Settings(), AlternateToolsSetting)
	if err != nil {
		return ""
	}
	s, _ := m[name].(string)
	return strings.TrimSpace(s)
}

// goProgram finds the go program: go.alternateTools.go, else go in PATH.
func (g *golang) goProgram() (string, error) {
	name := g.alternateTool("go")
	if name == "" {
		name = "go"
	}
	return lookProgram(name, g.api.Scope().Folder)
}

// installProgram finds the go program that installs the tools: go.toolsManagement.go, else the go program of the project.
func (g *golang) installProgram() (string, error) {
	if name, _ := extension.SettingValue[string](g.api.Settings(), ToolsManagementGoSetting); strings.TrimSpace(name) != "" {
		return lookProgram(strings.TrimSpace(name), g.api.Scope().Folder)
	}
	return g.goProgram()
}

// programEnv is the environment of the programs: that of Emetos, then go.toolsEnvVars, then extra.
func (g *golang) programEnv(extra ...string) []string {
	var vars []string
	if m, err := extension.SettingValue[map[string]any](g.api.Settings(), ToolsEnvVarsSetting); err == nil {
		for k, v := range m {
			if s, ok := v.(string); ok && k != "" {
				vars = append(vars, k+"="+s)
			}
		}
	}
	return environment(append(vars, extra...)...)
}

// installEnv is the environment of `go install`: modules on, and the GOPATH and GOBIN of go.toolsGopath when it is set. The
// GOFLAGS of the user stay as they are.
func (g *golang) installEnv() []string {
	extra := []string{"GO111MODULE=on"}
	if gp, _ := extension.SettingValue[string](g.api.Settings(), ToolsGopathSetting); strings.TrimSpace(gp) != "" {
		gp = strings.TrimSpace(gp)
		extra = append(extra, "GOPATH="+gp, "GOBIN="+strings.TrimRight(gp, "/")+"/bin")
	}
	return g.programEnv(extra...)
}

// ---- the panel ----

// The states of a tool in a run.
const (
	toolPending = iota
	toolInstalling
	toolInstalled
	toolFailed
	toolSkipped
)

// maxOutputLines is how many lines of output the panel keeps; the oldest go first.
const maxOutputLines = 2000

// toolRow is a tool in a run.
type toolRow struct {
	tool  tool
	state int
}

// installRun is one run of `go install` over some tools. rows and the flags are used on the UI thread; the output is filled by
// the goroutine of the run and guarded by mu.
type installRun struct {
	rows    []toolRow
	running bool
	cancel  context.CancelFunc
	summary string

	mu          sync.Mutex
	output      []string
	flushPosted bool
}

// toolsPanel is the panel view "Go Tools".
type toolsPanel struct {
	g      *golang
	handle extension.ViewHandle
	run    *installRun
	scroll ui.ScrollHandle
}

func newToolsPanel(g *golang) *toolsPanel { return &toolsPanel{g: g} }

// addLine adds a line of output and asks the panel to build again (once for a burst of lines). It may be called from any
// goroutine.
func (t *toolsPanel) addLine(r *installRun, line string) {
	r.mu.Lock()
	r.output = append(r.output, line)
	if len(r.output) > maxOutputLines {
		r.output = append([]string(nil), r.output[len(r.output)-maxOutputLines:]...)
	}
	post := !r.flushPosted
	r.flushPosted = true
	r.mu.Unlock()
	if post {
		t.g.api.Post(func() {
			r.mu.Lock()
			r.flushPosted = false
			r.mu.Unlock()
			t.scroll.ScrollTo(0, scrollEnd)
			t.handle.Refresh()
		})
	}
}

// scrollEnd is a position past the end of any output: ScrollTo clamps it to the end.
const scrollEnd = 1e9

// installToolsCommand is the command "Go: Install/Update Tools". An argument {"tools": ["gopls"]} installs only those.
func (g *golang) installToolsCommand(_ context.Context, args any) error {
	var names []string
	if m, ok := args.(map[string]any); ok {
		if list, ok := m["tools"].([]any); ok {
			for _, v := range list {
				name, _ := v.(string)
				if _, known := toolByName(name); !known {
					return errors.New("ToolUnknown", "no tool has this name", "tool", fmt.Sprint(v))
				}
				names = append(names, name)
			}
		}
	}
	g.startInstall(names)
	return nil
}

// startInstall installs the tools with the names (all of them when there are none) and shows the panel. A run that is going on is
// only shown.
func (g *golang) startInstall(names []string) {
	t := g.tools
	if r := t.run; r != nil && r.running {
		t.handle.Reveal(true)
		return
	}
	program, err := g.installProgram()
	if err != nil {
		g.api.Log().Warn("the go program for the tools was not found", "err", err)
		g.api.Notifications().Error("The go command was not found. Install Go, or set go.toolsManagement.go or go.alternateTools in the settings.")
		return
	}
	r := &installRun{running: true}
	for _, tl := range allTools() {
		if len(names) == 0 || contains(names, tl.Name) {
			r.rows = append(r.rows, toolRow{tool: tl})
		}
	}
	ctx, cancel := context.WithCancel(g.ctx)
	r.cancel = cancel
	t.run = r
	env := g.installEnv()
	g.closers.Add(1)
	go func() {
		defer g.closers.Done()
		defer cancel()
		t.execute(ctx, r, program, env)
	}()
	t.handle.Refresh()
	t.handle.Reveal(true)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// execute runs `go install` for each tool of the run, from the goroutine of the run. The state of a tool is handed to the UI
// thread before and after its program runs.
func (t *toolsPanel) execute(ctx context.Context, r *installRun, program string, env []string) {
	g := t.g
	dir, err := os.MkdirTemp("", "emetos-go-tools-")
	if err != nil {
		t.addLine(r, "The folder to install in cannot be made: "+err.Error())
	} else {
		defer func() {
			if err := os.RemoveAll(dir); err != nil {
				g.api.Log().Warn("the folder of the install was not removed", "dir", dir, "err", err)
			}
		}()
	}
	set := func(i, state int) {
		g.api.Post(func() { r.rows[i].state = state; t.handle.Refresh() })
	}
	for i, row := range r.rows {
		switch {
		case ctx.Err() != nil:
			set(i, toolSkipped)
			continue
		case err != nil: // no folder to install in
			set(i, toolFailed)
			continue
		}
		target := row.tool.Package + "@" + row.tool.Version
		set(i, toolInstalling)
		t.addLine(r, "$ go install "+target)
		tctx, cancel := context.WithTimeout(ctx, installTimeout)
		runErr := runProgram(tctx, cmdSpec{Program: program, Args: []string{"install", target}, Dir: dir, Env: env}, func(l string) { t.addLine(r, l) })
		cancel()
		switch {
		case runErr == nil:
			t.addLine(r, row.tool.Name+" is installed.")
			set(i, toolInstalled)
		case ctx.Err() != nil:
			t.addLine(r, row.tool.Name+": cancelled.")
			set(i, toolSkipped)
		default:
			t.addLine(r, row.tool.Name+": "+failureText(runErr, nil))
			set(i, toolFailed)
		}
	}
	g.api.Post(func() { t.finish(r) })
}

// finish is the end of a run, on the UI thread: the summary, the notification, and gopls started again when it was missing and
// is installed now.
func (t *toolsPanel) finish(r *installRun) {
	g := t.g
	r.running = false
	var done, failed []string
	cancelled := false
	for _, row := range r.rows {
		switch row.state {
		case toolInstalled:
			done = append(done, row.tool.Name)
		case toolFailed:
			failed = append(failed, row.tool.Name)
		case toolSkipped:
			cancelled = true
		}
	}
	switch {
	case len(failed) > 0:
		r.summary = "Could not install: " + strings.Join(failed, ", ") + "."
		g.api.Notifications().Error(r.summary+" The output is in the Go Tools panel.", extension.NotificationAction{Label: "Show Output", Run: func() { t.handle.Reveal(true) }})
	case cancelled:
		r.summary = "Cancelled."
		g.api.Notifications().Info("Installing the Go tools was cancelled.")
	default:
		r.summary = "Installed: " + strings.Join(done, ", ") + ". They are in the bin folder of GOBIN, or of GOPATH when GOBIN is not set; it must be in PATH."
		g.api.Notifications().Info("Installed the Go tools: " + strings.Join(done, ", ") + ".")
	}
	t.handle.Refresh()
	if contains(done, gopls) && (g.server.State == extension.ServerMissing || g.server.State == extension.ServerFailed) {
		if err := g.api.LanguageServers().Restart(ServerID); err != nil {
			g.api.Log().Warn("gopls could not be started again", "err", err)
		}
	}
}

// promptMissingTool tells the user once that the tool is missing, with a button that installs it.
func (g *golang) promptMissingTool(tl tool) {
	if g.missing[tl.Name] {
		return
	}
	g.missing[tl.Name] = true
	msg := tl.Name + " was not found. It " + tl.Purpose + "."
	if tl.Hint != "" {
		msg += " " + tl.Hint
	}
	g.api.Notifications().Warn(msg, extension.NotificationAction{Label: "Install", Run: func() { g.startInstall([]string{tl.Name}) }})
}

// ---- the widgets ----

const (
	fontSize = 13.0
	monoSize = 12.0
)

func gap(h float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(h)})
}

func hgap(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

// monoFamily is the monospace font of the platform.
func monoFamily(bc widgets.BuildContext) string {
	family, _ := widgets.WidgetsBindingOf(bc).PlatformDispatcher().MonospaceFont()
	if family == "" {
		return "monospace"
	}
	return family
}

// stateIcon and stateText say how a tool is.
func stateIcon(state int) string {
	switch state {
	case toolInstalling:
		return "sync"
	case toolInstalled:
		return "check"
	case toolFailed:
		return "error"
	case toolSkipped:
		return "circle-slash"
	}
	return "circle-outline"
}

func stateText(state int) string {
	switch state {
	case toolInstalling:
		return "installing..."
	case toolInstalled:
		return "installed"
	case toolFailed:
		return "failed"
	case toolSkipped:
		return "not installed"
	}
	return "waiting"
}

// build is the panel view.
func (t *toolsPanel) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	dim := ui.TextStyle(bc, fontSize, colors.DescriptionForeground)
	plain := ui.TextStyle(bc, fontSize, colors.Foreground)
	r := t.run
	if r == nil {
		var rows []widgets.Widget
		for _, tl := range allTools() {
			rows = append(rows, widgets.NewText(tl.Name+"  "+tl.Package+"@"+tl.Version, widgets.TextOpts{Style: dim}), gap(2))
		}
		return widgets.NewPadding(painting.NewEdgeInsetsAll(12), widgets.NewColumn(append([]widgets.Widget{
			widgets.NewText("Go: Install/Update Tools runs go install for these tools, one after the other.", widgets.TextOpts{Style: plain}),
			gap(8),
		}, append(rows, gap(8), widgets.NewAlign(
			ui.NewButton("Install/Update Tools", ui.ButtonOpts{OnPressed: func() { t.g.startInstall(nil) }}),
			widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft}))...), widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	}
	header := "Installing the Go tools..."
	if !r.running {
		header = r.summary
	}
	var action widgets.Widget
	if r.running {
		action = ui.NewButton("Cancel", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: r.cancel})
	} else {
		action = ui.NewButton("Install/Update Tools", ui.ButtonOpts{Small: true, OnPressed: func() { t.g.startInstall(nil) }})
	}
	var list []widgets.Widget
	for _, row := range r.rows {
		color := colors.DescriptionForeground
		if row.state == toolFailed {
			color = colors.ErrorForeground
		}
		list = append(list, widgets.NewRow([]widgets.Widget{
			ui.Icon(stateIcon(row.state), 14, color), hgap(8),
			widgets.NewText(row.tool.Name, widgets.TextOpts{Style: plain}), hgap(8),
			widgets.NewExpanded(widgets.NewText(row.tool.Package+"@"+row.tool.Version, widgets.TextOpts{Style: dim})),
			widgets.NewText(stateText(row.state), widgets.TextOpts{Style: ui.TextStyle(bc, fontSize, color)}),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), gap(4))
	}
	r.mu.Lock()
	text := strings.Join(r.output, "\n")
	r.mu.Unlock()
	mono := ui.TextStyle(bc, monoSize, colors.Foreground).CopyWith(painting.TextStyleOpts{FontFamily: monoFamily(bc)})
	return widgets.NewColumn([]widgets.Widget{
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 8, 12, 4), widgets.NewRow([]widgets.Widget{
			widgets.NewExpanded(widgets.NewText(header, widgets.TextOpts{Style: plain})), hgap(8), action,
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 0, 12, 4), widgets.NewColumn(list, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})),
		widgets.NewExpanded(ui.NewScrollView(widgets.NewPadding(painting.NewEdgeInsetsAll(12), widgets.NewText(text, widgets.TextOpts{Style: mono})), &t.scroll)),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}
