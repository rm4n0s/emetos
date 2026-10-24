package workbench

import (
	"strconv"

	"github.com/rm4n0s/emetos/internal/core/notify"
)

// BellState is what the notifications bell of the status bar shows (SPEC §10.7).
type BellState struct {
	// Total is the number of notifications there are.
	Total int
	// New is the number of unread notifications: those that came while the notification center was
	// closed and that were not closed since.
	New int
	// InProgress is the number of notifications with a progress that runs, counted only while neither the
	// toasts nor the center show (so that the bell still tells that something runs).
	InProgress int
	// CenterVisible is whether the notification center is open.
	CenterVisible bool
	// DoNotDisturb is whether the do not disturb mode is on.
	DoNotDisturb bool
}

// Entry is the item of the bell: $(bell), or $(bell-dot) with something unread or running, and the
// bell-slash ones in do not disturb mode. A click opens the center, or closes it when it is open.
//
// Ported from VS Code: src/vs/workbench/browser/parts/notifications/notificationsStatus.ts
func (s BellState) Entry() StatusEntry {
	dot := s.InProgress > 0 || s.New > 0
	e := StatusEntry{Name: "Notifications", AriaLabel: "Notifications", Command: "notifications.showList", Tooltip: s.tooltip()}
	if s.CenterVisible {
		e.Command = "notifications.hideList"
	}
	switch {
	case s.DoNotDisturb && dot:
		e.Text = "$(bell-slash-dot)"
	case s.DoNotDisturb:
		e.Text = "$(bell-slash)"
	case dot:
		e.Text = "$(bell-dot)"
	default:
		e.Text = "$(bell)"
	}
	if s.DoNotDisturb {
		e.AriaLabel = "Do Not Disturb"
		e.Tooltip = "Do Not Disturb Mode is Enabled"
	}
	return e
}

func (s BellState) tooltip() string {
	switch {
	case s.CenterVisible:
		return "Hide Notifications"
	case s.Total == 0:
		return "No Notifications"
	}
	n := strconv.Itoa(s.New)
	switch {
	case s.InProgress == 0 && s.New == 0:
		return "No New Notifications"
	case s.InProgress == 0 && s.New == 1:
		return "1 New Notification"
	case s.InProgress == 0:
		return n + " New Notifications"
	case s.New == 0:
		return "No New Notifications (" + strconv.Itoa(s.InProgress) + " in progress)"
	case s.New == 1:
		return "1 New Notification (" + strconv.Itoa(s.InProgress) + " in progress)"
	}
	return n + " New Notifications (" + strconv.Itoa(s.InProgress) + " in progress)"
}

// Bell follows a notification presenter and keeps the item of the notifications bell up to date. It is used on
// the UI thread only.
type Bell struct {
	presenter *notify.Presenter
	accessor  *StatusAccessor
	unread    int
	center    bool
	stop      []func()
}

// BellPriority is the priority of the bell: the last item on the right side (VS Code uses negative
// infinity).
func BellPriority() float64 { return negInf }

// NewBell adds the bell to a status bar and makes it follow the presenter: a notification that is added while the
// center is closed is unread until the center opens, and one that is closed is no longer unread. post hands a
// function to the UI thread (the events of the service come from any goroutine); nil runs it at once.
func NewBell(bar *StatusBar, p *notify.Presenter, post func(func())) *Bell {
	if post == nil {
		post = func(f func()) { f() }
	}
	b := &Bell{presenter: p}
	b.accessor = bar.Add("status.notifications", StatusRight, BellPriority(), b.State().Entry())
	b.stop = append(b.stop,
		p.Service().OnChange(func(e notify.Event) { post(func() { b.HandleEvent(e) }) }),
		p.OnChange(b.Refresh))
	return b
}

// State is what the bell shows now.
func (b *Bell) State() BellState {
	svc := b.presenter.Service()
	items := svc.Items()
	st := BellState{Total: len(items), New: b.unread, CenterVisible: b.presenter.CenterVisible(), DoNotDisturb: svc.DoNotDisturb()}
	if !b.presenter.CenterVisible() && !b.presenter.ToastsVisible() {
		for _, it := range items {
			if it.HasActiveProgress() {
				st.InProgress++
			}
		}
	}
	return st
}

// HandleEvent counts an event of the service: an added notification is unread while the center is closed, and a
// removed one is read.
func (b *Bell) HandleEvent(e notify.Event) {
	if !b.presenter.CenterVisible() {
		switch e.Kind {
		case notify.Added:
			b.unread++
		case notify.Dismissed:
			if b.unread > 0 {
				b.unread--
			}
		case notify.Updated, notify.DoNotDisturbChanged:
		}
	}
	b.Refresh()
}

// Refresh updates the item: the center opening reads every notification.
func (b *Bell) Refresh() {
	if b.presenter.CenterVisible() != b.center {
		b.center = b.presenter.CenterVisible()
		b.unread = 0
	}
	b.accessor.Update(b.State().Entry())
}

// Dispose removes the bell from the bar.
func (b *Bell) Dispose() {
	for _, f := range b.stop {
		f()
	}
	b.accessor.Dispose()
}
