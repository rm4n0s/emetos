package agents

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// What tells the user about the sessions that are not in front of them (SPEC §10.16 "As built (step 13.3)"): a status bar item
// with the sessions that work or wait for an answer, a notification when a turn ends or a permission is asked in a tab that is
// not on screen, and the Output channel "Agents" with the log of the adapter and the standard error of the programs.

const (
	// StatusID is the id of the status bar item.
	StatusID = "agents.status"
	// OutputChannel is the name of the Output channel.
	OutputChannel = "Agents"
)

// shortID is the start of a session ID, as the lines of the Output channel name a session.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// logf writes a line to the Output channel "Agents". It may be called from any goroutine.
func (a *agents) logf(format string, args ...any) {
	if a.out != nil {
		a.out.AppendLine(fmt.Sprintf(format, args...))
	}
}

// logger is the logger of the adapter and of the tabs: the log of the extension, and every line also in the Output channel.
func (a *agents) logger() *slog.Logger {
	if a.out == nil {
		return a.api.Log()
	}
	return slog.New(&channelHandler{inner: a.api.Log().Handler(), out: a.out})
}

// channelHandler is a slog.Handler that writes the lines it handles to the Output channel and to the handler of the
// extension's log.
type channelHandler struct {
	inner slog.Handler
	out   extension.OutputChannel
	attrs []slog.Attr
	group string
}

// Enabled implements slog.Handler: the channel takes every level.
func (h *channelHandler) Enabled(context.Context, slog.Level) bool { return true }

// Handle implements slog.Handler.
func (h *channelHandler) Handle(ctx context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(strings.ToLower(r.Level.String()))
	sb.WriteString(": ")
	sb.WriteString(r.Message)
	write := func(key string, v slog.Value) {
		sb.WriteString(" " + key + "=" + strconv.Quote(v.Resolve().String()))
	}
	for _, a := range h.attrs {
		write(a.Key, a.Value) // the keys carry the groups they were added in
	}
	r.Attrs(func(a slog.Attr) bool { write(h.group+a.Key, a.Value); return true })
	h.out.AppendLine(sb.String())
	if h.inner.Enabled(ctx, r.Level) {
		return h.inner.Handle(ctx, r)
	}
	return nil
}

// WithAttrs implements slog.Handler.
func (h *channelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.inner = h.inner.WithAttrs(attrs)
	c.attrs = append([]slog.Attr(nil), h.attrs...)
	for _, a := range attrs {
		c.attrs = append(c.attrs, slog.Attr{Key: h.group + a.Key, Value: a.Value})
	}
	return &c
}

// WithGroup implements slog.Handler.
func (h *channelHandler) WithGroup(name string) slog.Handler {
	c := *h
	c.inner = h.inner.WithGroup(name)
	c.group = h.group + name + "."
	return &c
}

// ---- the status bar item ----

// waiting tells whether the session asks the user for something.
func (t *sessionTab) waiting() bool { return len(t.perms) > 0 }

// working tells whether the session runs a turn that does not wait for the user.
func (t *sessionTab) working() bool { return t.busy && !t.waiting() }

// refreshStatus shows the sessions that work or wait in the status bar item, which is hidden when there are none.
func (a *agents) refreshStatus() {
	if a.status == nil {
		return
	}
	var working, waiting int
	var lines []string
	for _, t := range a.tabs {
		switch {
		case t.waiting():
			waiting++
			lines = append(lines, t.label()+": waiting for you")
		case t.working():
			working++
			lines = append(lines, t.label()+": working")
		}
	}
	if working+waiting == 0 {
		if a.statusKey != "" {
			a.statusKey = ""
			a.status.SetVisible(false)
		}
		return
	}
	var parts []string
	if waiting > 0 {
		parts = append(parts, strconv.Itoa(waiting)+" waiting")
	}
	if working > 0 {
		parts = append(parts, strconv.Itoa(working)+" working")
	}
	icon := "$(hubot)"
	if waiting > 0 {
		icon = "$(bell)"
	}
	item := extension.StatusItem{
		ID: StatusID, Name: "Agents", Text: icon + " " + strings.Join(parts, ", "),
		Tooltip: strings.Join(lines, "\n") + "\nClick to go to the session.", Alignment: extension.StatusLeft, Priority: 40,
		OnClick: a.showAttention,
	}
	// the tabs ask for a new build on every frame of a turn: the bar is told only when what it shows changes
	if key := item.Text + "\x00" + item.Tooltip; key != a.statusKey {
		a.statusKey = key
		a.status.Update(item)
		a.status.SetVisible(true)
	}
}

// showAttention goes to the session that waits for the user, else to the first that works.
func (a *agents) showAttention() {
	for _, pick := range []func(*sessionTab) bool{(*sessionTab).waiting, (*sessionTab).working} {
		for _, t := range a.tabs {
			if pick(t) {
				t.tab.Reveal(true)
				return
			}
		}
	}
}

// label is the name of the session as the list shows it.
func (t *sessionTab) label() string {
	if e := t.ext.entry(t.id); e != nil {
		return e.label()
	}
	return untitled
}

// ---- notifications ----

// notifyWaiting tells the user that a request waits for them, when the tab is not on screen.
func (t *sessionTab) notifyWaiting(p *permission) {
	if t.visible || t.replay {
		return
	}
	t.ext.api.Notifications().Warn(t.label()+": the agent is waiting for your permission to use "+p.req.ToolName+".", t.showAction())
}

// notifyDone tells the user that a turn ended, when the tab is not on screen.
func (t *sessionTab) notifyDone(failed bool) {
	if t.visible || t.replay {
		return
	}
	if failed {
		t.ext.api.Notifications().Warn(t.label()+": the agent stopped with an error.", t.showAction())
		return
	}
	t.ext.api.Notifications().Info(t.label()+": the agent has finished.", t.showAction())
}

// showAction is the button "Show" of a notification: it makes the tab the one in front.
func (t *sessionTab) showAction() extension.NotificationAction {
	return extension.NotificationAction{Label: "Show", Run: func() {
		if !t.closed {
			t.tab.Reveal(true)
		}
	}}
}
