package golang

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// "Go: Run" (SPEC §10.17 "As built (step 14.2)"): `go run .` in the folder of the package of the Go file in front, with its output
// streamed into the panel view "Go Run", a Stop button and the exit status.

// The ids of the command and of the panel view.
const (
	// RunCommand is the command that runs the package, RunViewID the panel view.
	RunCommand = ID + ".run"
	RunViewID  = "emetos.golang.run"
)

// The states of a run.
const (
	runRunning = iota
	runExited
	runFailed
	runStopped
	runNotStarted
)

// runJob is one run of `go run .`. The fields before mu belong to the UI thread; the output is filled by the goroutine of the run
// and guarded by mu.
type runJob struct {
	// dir is the folder it runs in and label how the panel names it (relative to the folder of the workspace).
	dir, label string
	state      int
	// status is the text of the end: "exit status 2", "stopped", why it did not start.
	status string
	cancel context.CancelFunc
	// stopping says that Stop was pressed: the end is a stop, not a failure.
	stopping bool

	mu          sync.Mutex
	output      []string
	flushPosted bool
}

// runPanel is the panel view "Go Run": it shows the newest run.
type runPanel struct {
	g      *golang
	handle extension.ViewHandle
	run    *runJob
	scroll ui.ScrollHandle
}

func newRunPanel(g *golang) *runPanel { return &runPanel{g: g} }

// addLine adds a line of output and asks the panel to build again (once for a burst of lines). It may be called from any goroutine.
func (p *runPanel) addLine(r *runJob, line string) {
	r.mu.Lock()
	r.output = append(r.output, line)
	if len(r.output) > maxOutputLines {
		r.output = append([]string(nil), r.output[len(r.output)-maxOutputLines:]...)
	}
	post := !r.flushPosted
	r.flushPosted = true
	r.mu.Unlock()
	if post {
		p.g.api.Post(func() {
			r.mu.Lock()
			r.flushPosted = false
			r.mu.Unlock()
			if p.run == r {
				p.scroll.ScrollTo(0, scrollEnd)
				p.handle.Refresh()
			}
		})
	}
}

// runDir is the folder `go run .` runs in: the folder of the Go file in front, else the folder of the workspace.
func (g *golang) runDir() (dir string, ok bool) {
	if path, isFile := filePath(g.editor.uri); isFile && strings.HasSuffix(path, ".go") {
		return filepath.Dir(path), true
	}
	if f := g.api.Scope().Folder; f != "" {
		return f, true
	}
	return "", false
}

// runCommand is the command "Go: Run".
func (g *golang) runCommand(context.Context, any) error {
	dir, ok := g.runDir()
	if !ok {
		g.api.Notifications().Info("Run needs a Go file or a folder: open one first.")
		return nil
	}
	g.startRun(dir)
	return nil
}

// startRun runs `go run .` in the folder and shows the panel. A run that is going on is stopped first.
func (g *golang) startRun(dir string) {
	p := g.runs
	program, err := g.goProgram()
	if err != nil {
		g.api.Log().Warn("the go program was not found", "err", err)
		g.api.Notifications().Error("The go command was not found. Install Go, or set go.alternateTools in the settings.")
		return
	}
	if old := p.run; old != nil && old.state == runRunning {
		old.stopping = true
		old.cancel()
	}
	r := &runJob{dir: dir, label: g.runLabel(dir)}
	ctx, cancel := context.WithCancel(g.ctx)
	r.cancel = cancel
	p.run = r
	p.addLine(r, "$ go run .")
	spec := cmdSpec{Program: program, Args: []string{"run", "."}, Dir: dir, Env: g.programEnv()}
	g.closers.Add(1)
	go func() {
		defer g.closers.Done()
		defer cancel()
		err := runProgram(ctx, spec, func(l string) { p.addLine(r, l) })
		g.api.Post(func() { p.finish(r, err) })
	}()
	p.handle.Refresh()
	p.handle.Reveal(true)
}

// runLabel names a folder for the panel: relative to the folder of the workspace, "." for the folder itself, the name of the folder
// when it is outside.
func (g *golang) runLabel(dir string) string {
	if root := g.api.Scope().Folder; root != "" {
		if rel, err := filepath.Rel(root, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(dir)
}

// finish is the end of a run, on the UI thread.
func (p *runPanel) finish(r *runJob, err error) {
	var exit *exec.ExitError
	switch {
	case err == nil:
		r.state, r.status = runExited, "exit status 0"
	case r.stopping || p.g.ctx.Err() != nil:
		r.state, r.status = runStopped, "stopped"
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		r.state, r.status = runFailed, "exit status "+strconv.Itoa(exit.ExitCode())
	case errors.As(err, &exit):
		r.state, r.status = runFailed, "killed ("+exit.String()+")"
	default:
		r.state, r.status = runNotStarted, describe(err)
	}
	if p.run == r {
		p.handle.Refresh()
	}
}

// ---- the widgets ----

// build is the panel view.
func (p *runPanel) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	plain := ui.TextStyle(bc, fontSize, colors.Foreground)
	r := p.run
	if r == nil {
		return widgets.NewPadding(painting.NewEdgeInsetsAll(12), widgets.NewColumn([]widgets.Widget{
			widgets.NewText("Go: Run runs go run . in the folder of the Go file in front and shows what it prints here.", widgets.TextOpts{Style: plain}),
			gap(8),
			widgets.NewAlign(ui.NewButton("Run", ui.ButtonOpts{OnPressed: func() { _ = p.g.runCommand(p.g.ctx, nil) }}), widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft}),
		}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	}
	where := r.label
	if where == "." {
		where = "the workspace folder"
	}
	icon, color, header := "sync", colors.DescriptionForeground, "Running go run . in "+where
	switch r.state {
	case runExited:
		icon, header = "check", "Finished: go run . in "+where+" ("+r.status+")"
	case runFailed:
		icon, color, header = "error", colors.ErrorForeground, "Failed: go run . in "+where+" ("+r.status+")"
	case runStopped:
		icon, header = "circle-slash", "Stopped: go run . in "+where
	case runNotStarted:
		icon, color, header = "error", colors.ErrorForeground, "Could not run go run . in "+where+": "+r.status
	}
	var action widgets.Widget
	if r.state == runRunning {
		action = ui.NewButton("Stop", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: func() { r.stopping = true; r.cancel() }})
	} else {
		action = ui.NewButton("Run Again", ui.ButtonOpts{Small: true, OnPressed: func() { p.g.startRun(r.dir) }})
	}
	r.mu.Lock()
	text := strings.Join(r.output, "\n")
	r.mu.Unlock()
	mono := ui.TextStyle(bc, monoSize, colors.Foreground).CopyWith(painting.TextStyleOpts{FontFamily: monoFamily(bc)})
	return widgets.NewColumn([]widgets.Widget{
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 8, 12, 4), widgets.NewRow([]widgets.Widget{
			ui.Icon(icon, 14, color), hgap(8),
			widgets.NewExpanded(widgets.NewText(header, widgets.TextOpts{Style: plain})), hgap(8), action,
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
		widgets.NewExpanded(ui.NewScrollView(widgets.NewPadding(painting.NewEdgeInsetsAll(12), widgets.NewText(text, widgets.TextOpts{Style: mono})), &p.scroll)),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}
