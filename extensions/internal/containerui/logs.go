package containerui

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// The logs panel (SPEC §10.20 "Container actions"): one panel view with a tab for each container whose logs are followed, and for
// each command that was run in a container. A tab follows the logs (stdout in the colors of the text, stderr in the color of
// errors), has a filter, can be paused and cleared, and keeps at most maxLogLines lines. A command's tab shows its output and its
// exit status.

// maxLogLines is the most lines a tab keeps; the oldest go first.
const maxLogLines = 10000

// initialTail is how many lines of a container's log are read when its tab is opened (the rest is behind, in the engine).
const initialTail = 1000

// logLine is a line of output.
type logLine struct {
	stderr bool
	text   string
}

// session kinds
const (
	sessionLogs = iota
	sessionExec
)

// logSession is a tab of the panel. The fields before mu belong to the UI thread; mu guards the lines that the goroutine of the
// stream has not handed over yet.
type logSession struct {
	id        string
	kind      int
	container string
	title     string
	cancel    context.CancelFunc
	// status is the line under the tab strip, ended says the stream is over.
	status string
	ended  bool
	paused bool
	// lines are the kept lines, shown the indexes of the lines that pass the filter.
	lines  []logLine
	model  *ui.ListViewModel
	ctl    *ui.ListController
	filter *ui.InputBoxController

	mu      sync.Mutex
	pending []logLine
	posted  bool
	// held counts the lines that came while the tab was paused.
	held int
}

// logsPanel is the panel view.
type logsPanel struct {
	e        *ext
	handle   extension.ViewHandle
	sessions []*logSession
	active   *logSession
	seq      int
}

func newLogsPanel(e *ext) *logsPanel { return &logsPanel{e: e} }

// provide gives the panel view its content.
func (p *logsPanel) provide() error {
	h, err := p.e.api.Views().Provide(p.e.cfg.LogsViewID(), extension.ViewContent{Build: p.build})
	if err != nil {
		return err
	}
	p.handle = h
	context.AfterFunc(p.e.ctx, p.stopAll)
	return nil
}

// stopAll ends every stream and lets go of the filter fields.
func (p *logsPanel) stopAll() {
	for _, s := range p.sessions {
		if s.cancel != nil {
			s.cancel()
		}
	}
}

// newSession adds a tab.
func (p *logsPanel) newSession(kind int, id, container, title string) *logSession {
	s := &logSession{id: id, kind: kind, container: container, title: title, ctl: ui.NewListController()}
	s.model = ui.NewListViewModel(ui.ListViewModelOpts{RowHeight: foundation.Some(18.0)})
	s.filter = ui.NewInputBoxController("")
	p.sessions = append(p.sessions, s)
	p.activate(s)
	return s
}

// activate makes a tab the one that shows.
func (p *logsPanel) activate(s *logSession) {
	p.active = s
	p.e.setKey(p.e.cfg.keyLogs(), s != nil)
	p.e.setKey(p.e.cfg.keyLogsPaused(), s != nil && s.paused)
	p.handle.Refresh()
}

// follow opens the tab of a container and follows its logs. A tab that is open is shown (and follows again if it had ended).
func (p *logsPanel) follow(c containerapi.Container) {
	client, ok := p.e.ready()
	if !ok {
		return
	}
	id := "logs:" + c.ID
	for _, s := range p.sessions {
		if s.id == id {
			p.activate(s)
			if s.ended {
				p.start(client, s, 0)
			}
			p.handle.Reveal(false)
			return
		}
	}
	s := p.newSession(sessionLogs, id, c.ID, c.Name())
	p.start(client, s, initialTail)
	p.handle.Reveal(false)
}

// start follows the logs on a goroutine.
func (p *logsPanel) start(client *containerapi.Client, s *logSession, tail int) {
	ctx, cancel := context.WithCancel(p.e.ctx)
	s.cancel, s.ended, s.status = cancel, false, "Following"
	go func() {
		err := client.Logs(ctx, s.container, containerapi.LogsOptions{Follow: true, Tail: tail}, func(l containerapi.LogLine) error {
			p.add(s, logLine{stderr: l.Stream == containerapi.StreamStderr, text: l.Text})
			return nil
		})
		stopped := ctx.Err() != nil
		cancel()
		p.e.api.Post(func() {
			if s.cancel == nil { // the tab was closed
				return
			}
			s.ended = true
			switch {
			case err != nil:
				s.status = "Cannot follow the logs: " + reasonOf(err)
			case stopped:
				s.status = "Stopped"
			default:
				s.status = "The container stopped. Start it and use View Logs to follow again."
			}
			p.drain(s)
			if p.active == s {
				p.handle.Refresh()
			}
		})
	}()
}

// runExec runs a command in a container, without a terminal, and shows its output in a new tab.
func (p *logsPanel) runExec(c containerapi.Container, command string) {
	client, ok := p.e.ready()
	if !ok {
		return
	}
	p.seq++
	s := p.newSession(sessionExec, "exec:"+strconv.Itoa(p.seq), c.ID, c.Name()+": "+command)
	ctx, cancel := context.WithCancel(p.e.ctx)
	s.cancel, s.status = cancel, "Running"
	p.add(s, logLine{text: "$ " + command})
	p.handle.Reveal(false)
	go func() {
		res, err := client.Exec(ctx, c.ID, containerapi.ExecOptions{Cmd: []string{"sh", "-c", command}})
		cancel()
		p.e.api.Post(func() {
			if s.cancel == nil {
				return
			}
			s.ended = true
			for _, l := range splitLines(res.Stdout) {
				p.add(s, logLine{text: l})
			}
			for _, l := range splitLines(res.Stderr) {
				p.add(s, logLine{stderr: true, text: l})
			}
			switch {
			case err != nil:
				s.status = "Cannot run the command: " + reasonOf(err)
			case res.Running:
				s.status = "The command was still running when Emetos stopped waiting for it."
			default:
				s.status = "Exited with status " + strconv.Itoa(res.ExitCode)
				if res.Truncated {
					s.status += " (the output was cut)"
				}
			}
			p.drain(s)
			if p.active == s {
				p.handle.Refresh()
			}
		})
	}()
}

// add hands a line to the tab, from any goroutine; the UI thread takes the lines that came in a burst together.
func (p *logsPanel) add(s *logSession, l logLine) {
	s.mu.Lock()
	s.pending = append(s.pending, l)
	if len(s.pending) > 2*maxLogLines {
		s.pending = append([]logLine(nil), s.pending[len(s.pending)-maxLogLines:]...)
	}
	post := !s.posted
	s.posted = true
	s.mu.Unlock()
	if post {
		p.e.api.Post(func() {
			s.mu.Lock()
			s.posted = false
			s.mu.Unlock()
			p.drain(s)
			if p.active == s {
				p.handle.Refresh()
			}
		})
	}
}

// drain moves the lines that came into the tab, unless it is paused (then they wait, and are counted).
func (p *logsPanel) drain(s *logSession) {
	s.mu.Lock()
	if s.paused {
		s.held = len(s.pending)
		s.mu.Unlock()
		return
	}
	got := s.pending
	s.pending = nil
	s.held = 0
	s.mu.Unlock()
	if len(got) == 0 {
		return
	}
	atEnd := s.atEnd()
	s.lines = append(s.lines, got...)
	if len(s.lines) > maxLogLines {
		s.lines = append([]logLine(nil), s.lines[len(s.lines)-maxLogLines:]...)
	}
	s.refilter()
	if atEnd {
		s.ctl.ScrollTo(s.model.ContentHeight())
	}
}

// atEnd tells whether the tab is scrolled to its last line (also before it was shown).
func (s *logSession) atEnd() bool {
	h := s.ctl.ViewportHeight()
	if h == 0 {
		return true
	}
	return s.ctl.ScrollOffset()+h >= s.model.ContentHeight()-2
}

// refilter makes the rows from the lines that pass the filter.
func (s *logSession) refilter() {
	f := strings.ToLower(strings.TrimSpace(s.filter.Value()))
	items := make([]ui.ListViewItem, 0, len(s.lines))
	for i, l := range s.lines {
		if f != "" && !strings.Contains(strings.ToLower(l.text), f) {
			continue
		}
		items = append(items, ui.ListViewItem{Key: "l" + strconv.Itoa(i), Data: i})
	}
	_ = s.model.SetItems(items) // the keys are made unique above
}

// pause holds the lines that come, resume shows them.
func (p *logsPanel) togglePause() {
	s := p.active
	if s == nil {
		return
	}
	s.mu.Lock()
	s.paused = !s.paused
	s.mu.Unlock()
	if !s.paused {
		p.drain(s)
	}
	p.e.setKey(p.e.cfg.keyLogsPaused(), s.paused)
	p.handle.Refresh()
}

// clear empties the tab.
func (p *logsPanel) clear() {
	s := p.active
	if s == nil {
		return
	}
	s.mu.Lock()
	s.pending, s.held = nil, 0
	s.mu.Unlock()
	s.lines = nil
	s.refilter()
	p.handle.Refresh()
}

// closeActive closes the tab that shows and ends its stream.
func (p *logsPanel) closeActive() {
	s := p.active
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.cancel = nil
	s.filter.Dispose()
	for i, o := range p.sessions {
		if o == s {
			p.sessions = append(p.sessions[:i], p.sessions[i+1:]...)
			break
		}
	}
	var next *logSession
	if n := len(p.sessions); n > 0 {
		next = p.sessions[n-1]
	}
	p.activate(next)
	if next == nil {
		p.handle.Hide()
	}
}

// containersChanged follows the events of the engine: the tab of a container that started again follows its logs again.
func (p *logsPanel) containersChanged(evs []containerapi.Event) {
	if p.e.client == nil {
		return
	}
	for _, ev := range evs {
		if ev.Type != "container" || ev.Action != "start" {
			continue
		}
		for _, s := range p.sessions {
			if s.kind == sessionLogs && s.ended && strings.HasPrefix(s.container, ev.ID) {
				p.start(p.e.client, s, 0)
				if p.active == s {
					p.handle.Refresh()
				}
			}
		}
	}
}

// ---- the widget ----

// build makes the panel: the tabs, the filter and the lines.
func (p *logsPanel) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	s := p.active
	if s == nil {
		return widgets.NewCenter(widgets.NewText("Use View Logs on a container, or Run Command in one, to see its output here.",
			widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.DescriptionForeground)}))
	}
	tabs := make([]widgets.Widget, len(p.sessions))
	for i, o := range p.sessions {
		o := o
		tabs[i] = ui.NewButton(o.title, ui.ButtonOpts{Small: true, Secondary: o != s, OnPressed: func() { p.activate(o) }})
	}
	status := s.status
	s.mu.Lock()
	held := s.held
	paused := s.paused
	s.mu.Unlock()
	switch {
	case paused && held > 0:
		status = "Paused, " + plural(held, "new line") + " waiting. " + status
	case paused:
		status = "Paused. " + status
	}
	mono := ui.TextStyle(bc, 12, colors.Foreground).CopyWith(painting.TextStyleOpts{FontFamily: pageui.MonoFamily(bc)})
	list := ui.NewList(s.model, ui.ListOpts{Controller: s.ctl, Render: func(bc widgets.BuildContext, row ui.ListViewRow, st ui.RowState) widgets.Widget {
		i, _ := row.Data.(int)
		if i < 0 || i >= len(s.lines) {
			return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{})
		}
		l := s.lines[i]
		style := mono
		if l.stderr {
			style = mono.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.EditorErrorForeground)})
		}
		return widgets.NewText(l.text, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowClip})
	}})
	head := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(8, 4, 8, 4), widgets.NewColumn([]widgets.Widget{
		widgets.NewWrap(tabs, widgets.WrapOpts{Spacing: 4, RunSpacing: 4}),
		pageui.Gap(4),
		widgets.NewRow([]widgets.Widget{
			widgets.NewSizedBox(ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: s.filter, Placeholder: "Filter", OnChanged: func(string) {
				s.refilter()
				p.handle.Refresh()
			}}), widgets.SizedBoxOpts{Width: foundation.Some(220.0)}),
			pageui.HGap(10),
			widgets.NewExpanded(widgets.NewText(status, widgets.TextOpts{
				Style: ui.TextStyle(bc, 12, colors.DescriptionForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
			})),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	return widgets.NewColumn([]widgets.Widget{head, widgets.NewExpanded(list)}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}
