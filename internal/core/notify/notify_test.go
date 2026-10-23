package notify

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func newTestService(buf *bytes.Buffer) (*Service, *time.Time) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	log := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return NewService(log, func() time.Time { return now }), &now
}

func TestNotifyKeepsAndLogs(t *testing.T) {
	var buf bytes.Buffer
	s, now := newTestService(&buf)

	a := s.Info("Git", "Fetched", Action{Label: "Open", Command: "git.open", Args: map[string]any{"x": 1.0}})
	*now = now.Add(time.Minute)
	b := s.Warn("", "Careful")
	c := s.Error("LSP", "Crashed")
	if a != 1 || b != 2 || c != 3 {
		t.Fatalf("ids %d %d %d", a, b, c)
	}
	items := s.Items()
	if len(items) != 3 || items[0].Message != "Fetched" || items[0].Severity != Info || items[0].Source != "Git" ||
		items[0].Actions[0].Command != "git.open" || items[1].Severity != Warning || items[2].Severity != Error {
		t.Fatalf("items: %+v", items)
	}
	if !items[0].Time.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) || !items[1].Time.Equal(items[0].Time.Add(time.Minute)) {
		t.Errorf("times: %v %v", items[0].Time, items[1].Time)
	}
	logged := buf.String()
	for _, want := range []string{"level=INFO msg=\"notification: Fetched\"", "source=Git", "level=WARN msg=\"notification: Careful\"", "level=ERROR msg=\"notification: Crashed\""} {
		if !strings.Contains(logged, want) {
			t.Errorf("log lacks %q:\n%s", want, logged)
		}
	}
}

func TestNotifyWithoutMessage(t *testing.T) {
	var buf bytes.Buffer
	s, _ := newTestService(&buf)
	if id := s.Notify(Notification{Severity: Error}); id != 0 || len(s.Items()) != 0 || buf.Len() != 0 {
		t.Errorf("a notification without a message is dropped: %d %v %q", id, s.Items(), buf.String())
	}
}

func TestDismissAndEvents(t *testing.T) {
	var buf bytes.Buffer
	s, _ := newTestService(&buf)
	var events []Event
	remove := s.OnChange(func(e Event) { events = append(events, e) })

	a := s.Info("", "one")
	b := s.Info("", "two")
	if !s.Dismiss(a) || s.Dismiss(a) || s.Dismiss(99) {
		t.Error("Dismiss reports whether the item was there")
	}
	if items := s.Items(); len(items) != 1 || items[0].ID != b {
		t.Errorf("items: %+v", items)
	}
	remove()
	s.Info("", "three")
	if len(events) != 3 || events[0].Kind != Added || events[2].Kind != Dismissed || events[2].Item.ID != a {
		t.Errorf("events: %+v", events)
	}
}

func TestItemsAreCopies(t *testing.T) {
	var buf bytes.Buffer
	s, _ := newTestService(&buf)
	actions := []Action{{Label: "A", Command: "a"}}
	s.Notify(Notification{Message: "m", Actions: actions})
	actions[0].Label = "changed"
	items := s.Items()
	items[0].Message = "changed"
	if got := s.Items()[0]; got.Message != "m" || got.Actions[0].Label != "A" {
		t.Errorf("the service must not share its data: %+v", got)
	}
}

func TestSeverityString(t *testing.T) {
	if Info.String() != "info" || Warning.String() != "warning" || Error.String() != "error" {
		t.Error("names")
	}
}

func TestNilArguments(t *testing.T) {
	s := NewService(nil, nil)
	if id := s.Info("", "x"); id != 1 || s.Items()[0].Time.IsZero() {
		t.Error("defaults for log and clock")
	}
}
