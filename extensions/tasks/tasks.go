// Package tasks is the extension that runs commands in the folder of the workspace from a view of the panel (SPEC §10.15
// "As built (step 11.5)"). The commands are a setting (go test ./... by default). A run streams its output into the view and
// into an Output channel, puts the lines that name a place (file:line:col: message) into the Problems panel, and ends with the
// scope: the process and everything it started are stopped when the task is stopped, the extension is turned off or the
// window tab closes.
package tasks

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The ids of the extension.
const (
	// ID is the id of the extension.
	ID = "emetos.tasks"
	// ViewID is the tab of the panel.
	ViewID = "emetos.tasks.panel"
	// RunCommand, StopCommand and ClearCommand are its commands.
	RunCommand   = "emetos.tasks.run"
	StopCommand  = "emetos.tasks.stop"
	ClearCommand = "emetos.tasks.clear"
	// CommandsSetting lists the command lines the view offers.
	CommandsSetting = "emetos.tasks.commands"
	// RunningKey is the context key that is true while a task runs.
	RunningKey = "emetos.tasks.running"
	// ChannelName is the Output channel of the extension, and ProblemsOwner the owner of its markers.
	ChannelName   = "Tasks"
	ProblemsOwner = "task"
)

// New is the Factory of the extension.
func New() extension.Extension { return &tasks{} }

// Manifest implements extension.Extension. The tab is declared, so it is in the panel before the extension runs; showing it
// or running one of its commands activates it.
func (*tasks) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Tasks", Version: "0.1.0",
		Description: "Runs commands in the folder, shows their output and puts their errors in the Problems panel.",
		Contributes: extension.Contributes{
			Commands: []extension.CommandSpec{
				{ID: RunCommand, Title: "Run Task", Category: "Tasks", Description: "Runs the first command of emetos.tasks.commands, or the command or the number it is given."},
				{ID: StopCommand, Title: "Stop Task", Category: "Tasks", When: RunningKey},
				{ID: ClearCommand, Title: "Clear Output", Category: "Tasks"},
			},
			PanelViews: []extension.PanelViewSpec{{ID: ViewID, Title: "Tasks", Icon: "tasklist", Order: 5}},
			ViewActions: []extension.ViewActionSpec{
				{View: ViewID, Command: RunCommand, Icon: "play", Tooltip: "Run Task", When: "!" + RunningKey, Order: 1},
				{View: ViewID, Command: StopCommand, Icon: "debug-stop", Tooltip: "Stop Task", When: RunningKey, Order: 1},
				{View: ViewID, Command: ClearCommand, Icon: "clear-all", Tooltip: "Clear Output", Order: 2},
			},
		},
	}
}

// maxLines is how many lines of output the view keeps, and savedLines how many it keeps for the next session.
const (
	maxLines   = 5000
	savedLines = 200
)

// line is a line of output, with the place it names when it is a problem.
type line struct {
	text string
	// problem is where the line points to, with the absolute path; nil when it points nowhere.
	problem *Problem
}

// tasks is the extension of one scope. Everything but the reading of the output is used on the UI thread.
type tasks struct {
	api    extension.API
	ctx    context.Context
	folder string

	channel extension.OutputChannel
	handle  extension.ViewHandle
	status  extension.StatusHandle

	lines []line
	// base numbers the first line, so that the key of a line stays the same when the first lines are dropped.
	base    int
	model   *ui.ListViewModel
	ctl     *ui.ListController
	last    string // the command that ran last
	result  string // how it ended
	seconds string

	// problems are the markers of the run by file, and count how many there are.
	problems map[string][]extension.Marker
	count    int

	// outMu guards the lines the reader goroutine has read and the flush that waits for the UI thread.
	outMu   sync.Mutex
	pending []string
	flushed bool

	cancel  context.CancelFunc
	proc    *process
	started time.Time
	// stopping says that the user asked the task to end.
	stopping bool
}

// Activate implements extension.Extension.
func (t *tasks) Activate(ctx context.Context, api extension.API) error {
	t.api, t.ctx, t.folder = api, ctx, api.Scope().Folder
	t.problems = map[string][]extension.Marker{}
	t.model = ui.NewListViewModel()
	t.ctl = ui.NewListController()
	if err := api.Settings().Register(extension.SettingSpec{
		Key: CommandsSetting, Type: "array", Default: []string{"go test ./..."},
		Description: "The command lines the Tasks view offers. Each runs in the folder of the workspace, in a shell.",
	}); err != nil {
		return err
	}
	var err error
	if t.channel, err = api.Output().Channel(ChannelName); err != nil {
		return err
	}
	if t.status, err = api.StatusBar().Add(extension.StatusItem{
		ID: "task", Name: "Tasks", Text: "$(tasklist)", Tooltip: "Run a task", Alignment: extension.StatusLeft, Priority: -20,
		Command: RunCommand,
	}); err != nil {
		return err
	}
	if t.handle, err = api.Views().Provide(ViewID, extension.ViewContent{Build: t.build, SaveState: t.saveState, RestoreState: t.restoreState}); err != nil {
		return err
	}
	api.Settings().OnChange(func() { t.handle.Refresh() })
	for id, h := range map[string]extension.CommandHandler{
		RunCommand:   func(_ context.Context, args any) error { return t.runCommand(args) },
		StopCommand:  func(context.Context, any) error { t.stop(); return nil },
		ClearCommand: func(context.Context, any) error { t.clear(); return nil },
	} {
		if err := api.Commands().Handle(id, h); err != nil {
			return err
		}
	}
	return nil
}

// commands reads the setting.
func (t *tasks) commands() []string {
	list, err := extension.SettingValue[[]string](t.api.Settings(), CommandsSetting)
	if err != nil {
		t.api.Log().Warn("the setting of the tasks cannot be read", "err", err)
		return nil
	}
	var out []string
	for _, c := range list {
		if strings.TrimSpace(c) != "" {
			out = append(out, c)
		}
	}
	return out
}

// runCommand is the command Run Task: with no argument the first command of the setting runs, a string runs that command
// line and a number the command at that place of the list. The error is the error of the command.
func (t *tasks) runCommand(args any) error {
	list := t.commands()
	var cmdline string
	switch a := args.(type) {
	case string:
		cmdline = a
	case float64:
		i := int(a)
		if i < 0 || i >= len(list) {
			t.api.Notifications().Warn("There is no task number " + strconv.Itoa(i) + " in emetos.tasks.commands.")
			return nil
		}
		cmdline = list[i]
	default:
		if len(list) == 0 {
			t.api.Notifications().Warn("emetos.tasks.commands has no command to run.")
			return nil
		}
		cmdline = list[0]
	}
	t.run(cmdline)
	return nil
}

// run starts a command line, after the one that runs now, if any, was stopped by the user: a second task is refused.
func (t *tasks) run(cmdline string) {
	if t.cancel != nil {
		t.api.Notifications().Warn("A task is running already. Stop it first.")
		return
	}
	if t.folder == "" {
		t.api.Notifications().Warn("Open a folder to run tasks.")
		return
	}
	t.clearProblems()
	t.clear()
	t.last, t.result, t.stopping = cmdline, "running", false
	t.started = time.Now()
	t.channel.AppendLine("> " + cmdline)
	runCtx, cancel := context.WithCancel(t.ctx)
	t.cancel = cancel
	p, err := start(runCtx, t.folder, cmdline, t.read, func(exit int, err error) {
		t.api.Post(func() { t.finished(exit, err) })
	})
	if err != nil {
		t.cancel = nil
		cancel()
		t.result = "could not start: " + err.Error()
		t.channel.AppendLine(t.result)
		t.api.Notifications().Error("Cannot run " + cmdline + ": " + err.Error())
		t.state()
		return
	}
	t.proc = p
	_ = t.api.Context().Set(RunningKey, true)
	t.channel.Show(true)
	t.handle.Reveal(false)
	t.state()
}

// read is called by the reader goroutine for each line of output. The lines wait for the UI thread, which takes all that
// came while it was busy, once.
func (t *tasks) read(text string) {
	t.outMu.Lock()
	t.pending = append(t.pending, text)
	post := !t.flushed
	t.flushed = true
	t.outMu.Unlock()
	if post {
		t.api.Post(t.flush)
	}
}

// flush takes the lines that were read: they go to the view, to the Output channel and, when they name a place, to the
// Problems panel.
func (t *tasks) flush() {
	t.outMu.Lock()
	texts := t.pending
	t.pending, t.flushed = nil, false
	t.outMu.Unlock()
	touched := map[string]bool{}
	for _, text := range texts {
		t.channel.AppendLine(text)
		l := line{text: strings.ReplaceAll(text, "\t", "    ")}
		if p, ok := parseProblem(text); ok {
			if full, inside := resolve(t.folder, p.Path); inside && isFile(full) {
				p.Path = full
				l.problem = &p
				t.addMarker(full, p)
				touched[full] = true
			}
		}
		t.lines = append(t.lines, l)
	}
	if over := len(t.lines) - maxLines; over > 0 {
		t.lines = t.lines[over:]
		t.base += over
	}
	for full := range touched {
		if err := t.api.Problems().Set(ProblemsOwner, "file://"+filepath.ToSlash(full), t.problems[full]); err != nil {
			t.api.Log().Warn("markers cannot be set", "file", full, "err", err)
		}
	}
	t.state()
}

// addMarker keeps a marker of the run.
func (t *tasks) addMarker(full string, p Problem) {
	sev := extension.SeverityError
	if isWarning(p.Message) {
		sev = extension.SeverityWarning
	}
	t.problems[full] = append(t.problems[full], extension.Marker{
		Severity: sev, Message: p.Message, Source: "Tasks", StartLine: p.Line, StartColumn: p.Column,
	})
	t.count++
}

// finished is called, on the UI thread, when the process ended and its output was read.
func (t *tasks) finished(exit int, err error) {
	t.flush() // the last lines
	if t.cancel != nil {
		t.cancel()
	}
	t.cancel, t.proc = nil, nil
	t.api.Context().Reset(RunningKey)
	t.seconds = elapsedText(time.Since(t.started))
	switch {
	case err == nil:
		t.result = "done"
	case t.stopping || t.ctx.Err() != nil:
		t.result = "stopped"
	default:
		t.result = "exit " + strconv.Itoa(exit)
	}
	t.channel.AppendLine("> " + t.result + " (" + t.seconds + ")")
	t.state()
}

// elapsedText is how long a task took, as the view says it: under a second is "under 1s", so that a fast task reads the same
// every time; a longer one is rounded to a tenth of a second.
func elapsedText(d time.Duration) string {
	if d < time.Second {
		return "under 1s"
	}
	return d.Round(100 * time.Millisecond).String()
}

// stop ends the task that runs, if any.
func (t *tasks) stop() {
	if t.cancel == nil {
		return
	}
	t.stopping = true
	t.cancel()
	t.result = "stopping"
	t.state()
}

// clear empties the output of the view; the problems of the run stay until the next run.
func (t *tasks) clear() {
	t.lines, t.base = nil, 0
	t.outMu.Lock()
	t.pending = nil
	t.outMu.Unlock()
	t.channel.Clear()
	t.state()
}

// clearProblems takes the markers of the earlier run away.
func (t *tasks) clearProblems() {
	t.api.Problems().Clear(ProblemsOwner)
	t.problems = map[string][]extension.Marker{}
	t.count = 0
}

// syncModel gives the list of the view the lines, and follows the end of the output while a task runs.
func (t *tasks) syncModel() {
	items := make([]ui.ListViewItem, len(t.lines))
	for i, l := range t.lines {
		items[i] = ui.ListViewItem{Key: strconv.Itoa(t.base + i), Data: l, Label: l.text}
	}
	if err := t.model.SetItems(items); err != nil {
		t.api.Log().Warn("the output cannot be listed", "err", err)
		return
	}
	if t.cancel != nil && len(items) > 0 {
		t.ctl.Reveal(items[len(items)-1].Key)
	}
}

// state tells the badge, the status bar item and the view what changed.
func (t *tasks) state() {
	t.syncModel()
	badge, tip := t.count, strconv.Itoa(t.count)+" problems from the last run"
	if t.count == 1 {
		tip = "1 problem from the last run"
	}
	t.handle.SetBadge(badge, tip)
	text, tooltip := "$(tasklist)", "Run a task"
	switch {
	case t.cancel != nil:
		text, tooltip = "$(sync) "+t.last, "Running "+t.last
	case t.result == "done":
		text, tooltip = "$(check) "+t.last, t.last+": done, "+t.seconds
	case t.result != "":
		text, tooltip = "$(error) "+t.last, t.last+": "+t.result
	}
	t.status.Update(extension.StatusItem{ID: "task", Name: "Tasks", Text: text, Tooltip: tooltip, Alignment: extension.StatusLeft, Priority: -20, Command: RunCommand})
	t.handle.Refresh()
}

// savedState is what the view keeps across a restart: the command that ran last, how it ended and the end of its output.
type savedState struct {
	Command string   `json:"command,omitempty"`
	Result  string   `json:"result,omitempty"`
	Lines   []string `json:"lines,omitempty"`
}

func (t *tasks) saveState() (any, error) {
	s := savedState{Command: t.last, Result: t.result}
	if t.cancel != nil {
		s.Result = "stopped" // the process does not survive the session
	}
	from := max(len(t.lines)-savedLines, 0)
	for _, l := range t.lines[from:] {
		s.Lines = append(s.Lines, l.text)
	}
	return s, nil
}

func (t *tasks) restoreState(raw json.RawMessage) error {
	var s savedState
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	if t.cancel != nil || len(t.lines) > 0 {
		return nil // a task of this session is more recent than the state
	}
	t.last, t.result = s.Command, s.Result
	for _, text := range s.Lines {
		t.lines = append(t.lines, line{text: text})
	}
	t.syncModel()
	return nil
}

// build makes the widget of the view: the commands as buttons over the output.
func (t *tasks) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	small := func(s string, color gui.Color) widgets.Widget {
		return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 12, color), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	if t.folder == "" {
		return widgets.NewPadding(painting.NewEdgeInsetsAll(16), small("Open a folder to run tasks.", colors.DescriptionForeground))
	}
	var buttons []widgets.Widget
	running := t.cancel != nil
	for _, c := range t.commands() {
		cmdline := c
		buttons = append(buttons, ui.NewButton(shorten(c), ui.ButtonOpts{
			Small: true, Secondary: cmdline != t.last, Disabled: running, Tooltip: c,
			OnPressed: func() { t.run(cmdline) },
		}))
	}
	status := ""
	switch {
	case running:
		status = "Running..."
	case t.result != "":
		status = t.result
		if t.seconds != "" {
			status += ", " + t.seconds
		}
	}
	header := widgets.NewPadding(painting.NewEdgeInsetsSymmetric(6, 10), widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(widgets.NewWrap(buttons, widgets.WrapOpts{Spacing: 6, RunSpacing: 4})),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
		small(status, colors.DescriptionForeground),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))

	var body widgets.Widget
	if len(t.lines) == 0 {
		body = widgets.NewPadding(painting.NewEdgeInsetsSymmetric(6, 10), small("Run a task to see its output here.", colors.DescriptionForeground))
	} else {
		body = ui.NewList(t.model, ui.ListOpts{Controller: t.ctl, Render: t.renderLine, OnSelect: t.onSelect})
	}
	return widgets.NewColumn([]widgets.Widget{header, widgets.NewExpanded(body)}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// maxLabel is the longest label of a button of a command; a longer command line is cut, and its tooltip has all of it.
const maxLabel = 36

// shorten is the label of the button of a command line: its first line, cut with an ellipsis when it is long.
func shorten(cmdline string) string {
	first, _, _ := strings.Cut(cmdline, "\n")
	if r := []rune(first); len(r) > maxLabel {
		return string(r[:maxLabel-1]) + "…"
	}
	return first
}

// renderLine builds a row of the output: a line that names a place is shown as a problem.
func (t *tasks) renderLine(bc widgets.BuildContext, row ui.ListViewRow, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	l, _ := row.Data.(line)
	color := st.Foreground
	switch {
	case l.problem != nil && isWarning(l.problem.Message):
		color = colors.ProblemsWarningIconForeground
	case l.problem != nil:
		color = colors.ProblemsErrorIconForeground
	}
	return widgets.NewText(l.text, widgets.TextOpts{Style: ui.TextStyle(bc, 12, color), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// onSelect opens the place a line of output names.
func (t *tasks) onSelect(row ui.ListViewRow) {
	l, _ := row.Data.(line)
	if l.problem == nil {
		return
	}
	uri := "file://" + filepath.ToSlash(l.problem.Path)
	if err := t.api.Documents().Show(uri, l.problem.Line, l.problem.Column, true); err != nil {
		t.api.Notifications().Error("Cannot open " + l.problem.Path + ": " + err.Error())
	}
}
