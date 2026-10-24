package workbench

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/notify"
)

func TestBellEntry(t *testing.T) {
	tests := []struct {
		name    string
		state   BellState
		text    string
		tooltip string
		command string
		aria    string
	}{
		{"nothing", BellState{}, "$(bell)", "No Notifications", "notifications.showList", "Notifications"},
		{"read ones", BellState{Total: 2}, "$(bell)", "No New Notifications", "notifications.showList", "Notifications"},
		{"one new", BellState{Total: 1, New: 1}, "$(bell-dot)", "1 New Notification", "notifications.showList", "Notifications"},
		{"three new", BellState{Total: 3, New: 3}, "$(bell-dot)", "3 New Notifications", "notifications.showList", "Notifications"},
		{"the center is open", BellState{Total: 3, CenterVisible: true}, "$(bell)", "Hide Notifications", "notifications.hideList", "Notifications"},
		{"a progress", BellState{Total: 1, InProgress: 1}, "$(bell-dot)", "No New Notifications (1 in progress)", "notifications.showList", "Notifications"},
		{"one new and two progresses", BellState{Total: 3, New: 1, InProgress: 2}, "$(bell-dot)", "1 New Notification (2 in progress)", "notifications.showList", "Notifications"},
		{"many new and a progress", BellState{Total: 5, New: 4, InProgress: 1}, "$(bell-dot)", "4 New Notifications (1 in progress)", "notifications.showList", "Notifications"},
		{"do not disturb", BellState{DoNotDisturb: true}, "$(bell-slash)", "Do Not Disturb Mode is Enabled", "notifications.showList", "Do Not Disturb"},
		{"do not disturb with news", BellState{Total: 1, New: 1, DoNotDisturb: true}, "$(bell-slash-dot)", "Do Not Disturb Mode is Enabled", "notifications.showList", "Do Not Disturb"},
		{"do not disturb with the center open", BellState{Total: 1, DoNotDisturb: true, CenterVisible: true}, "$(bell-slash)", "Do Not Disturb Mode is Enabled", "notifications.hideList", "Do Not Disturb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := tt.state.Entry()
			if e.Text != tt.text || e.Tooltip != tt.tooltip || e.Command != tt.command || e.AriaLabel != tt.aria || e.Name != "Notifications" {
				t.Errorf("entry %+v", e)
			}
		})
	}
}

func TestBellFollowsThePresenter(t *testing.T) {
	now := time.Unix(0, 0)
	svc := notify.NewService(slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now })
	p := notify.NewPresenter(svc)
	bar := NewStatusBar()
	bell := NewBell(bar, p, nil)
	entry := func() StatusEntry {
		it, ok := bar.Get("status.notifications")
		if !ok {
			t.Fatal("no bell in the bar")
		}
		return it.Entry
	}
	steps := []struct {
		name    string
		do      func()
		text    string
		tooltip string
	}{
		{"at first", func() {}, "$(bell)", "No Notifications"},
		{"a notification", func() { svc.Info("x", "one") }, "$(bell-dot)", "1 New Notification"},
		{"another", func() { svc.Warn("x", "two") }, "$(bell-dot)", "2 New Notifications"},
		{"the center opens: all are read", p.ShowCenter, "$(bell)", "Hide Notifications"},
		{"one that comes while the center is open is read", func() { svc.Error("x", "three") }, "$(bell)", "Hide Notifications"},
		{"the center closes", p.HideCenter, "$(bell)", "No New Notifications"},
		{"a new one", func() { svc.Info("x", "four") }, "$(bell-dot)", "1 New Notification"},
		{"it is closed", func() { svc.Dismiss(4) }, "$(bell)", "No New Notifications"},
		{"do not disturb", p.ToggleDoNotDisturb, "$(bell-slash)", "Do Not Disturb Mode is Enabled"},
		{"do not disturb off", p.ToggleDoNotDisturb, "$(bell)", "No New Notifications"},
		{"all cleared", p.ClearAll, "$(bell)", "No Notifications"},
	}
	for _, st := range steps {
		st.do()
		if e := entry(); e.Text != st.text || e.Tooltip != st.tooltip {
			t.Errorf("%s: %q, %q; want %q, %q", st.name, e.Text, e.Tooltip, st.text, st.tooltip)
		}
	}
	// a progress that runs shows the dot while nothing else is on screen
	id := svc.Notify(notify.Notification{Severity: notify.Info, Message: "work", Progress: &notify.Progress{Infinite: true}})
	p.HideToasts()
	p.ShowCenter()
	p.HideCenter()
	if e := entry(); e.Text != "$(bell-dot)" {
		t.Errorf("a running progress: %q %q", e.Text, e.Tooltip)
	}
	_ = id
	bell.Dispose()
	if _, ok := bar.Get("status.notifications"); ok {
		t.Error("the bell is still in the bar")
	}
	svc.Info("x", "after the bell")
}
