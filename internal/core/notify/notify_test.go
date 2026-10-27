package notify

import (
	"bytes"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
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

func TestDoNotDisturbMakesNotificationsSilent(t *testing.T) {
	cases := []struct {
		name       string
		dnd        bool
		n          Notification
		wantSilent bool
	}{
		{"off, info", false, Notification{Severity: Info, Message: "m"}, false},
		{"on, info", true, Notification{Severity: Info, Message: "m"}, true},
		{"on, warning", true, Notification{Severity: Warning, Message: "m"}, true},
		{"on, error", true, Notification{Severity: Error, Message: "m"}, false},
		{"off, asked silent", false, Notification{Severity: Error, Message: "m", Silent: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			s, _ := newTestService(&buf)
			s.SetDoNotDisturb(tc.dnd)
			id := s.Notify(tc.n)
			item, ok := s.Get(id)
			if !ok || item.Silent != tc.wantSilent {
				t.Errorf("silent = %v, want %v", item.Silent, tc.wantSilent)
			}
		})
	}
}

func TestDoNotDisturbEvents(t *testing.T) {
	var buf bytes.Buffer
	s, _ := newTestService(&buf)
	var events []Event
	s.OnChange(func(e Event) { events = append(events, e) })
	s.SetDoNotDisturb(false) // no change, no event
	s.SetDoNotDisturb(true)
	s.SetDoNotDisturb(true)
	s.SetDoNotDisturb(false)
	if len(events) != 2 || events[0].Kind != DoNotDisturbChanged || !events[0].DoNotDisturb || events[1].DoNotDisturb {
		t.Errorf("events: %+v", events)
	}
	if s.DoNotDisturb() {
		t.Error("do not disturb is off")
	}
}

func TestDuplicatesReplaceEachOther(t *testing.T) {
	cases := []struct {
		name  string
		a, b  Notification
		items int
	}{
		{"same", Notification{Message: "m"}, Notification{Message: "m"}, 1},
		{"other message", Notification{Message: "m"}, Notification{Message: "n"}, 2},
		{"other severity", Notification{Message: "m"}, Notification{Message: "m", Severity: Error}, 2},
		{"other source", Notification{Message: "m", Source: "a"}, Notification{Message: "m", Source: "b"}, 2},
		{"same buttons", Notification{Message: "m", Actions: []Action{{Label: "Yes"}}}, Notification{Message: "m", Actions: []Action{{Label: "Yes"}}}, 1},
		{"other buttons", Notification{Message: "m", Actions: []Action{{Label: "Yes"}}}, Notification{Message: "m", Actions: []Action{{Label: "No"}}}, 2},
		{"fewer buttons", Notification{Message: "m", Actions: []Action{{Label: "Yes"}}}, Notification{Message: "m"}, 2},
		{"with progress", Notification{Message: "m", Progress: &Progress{Infinite: true}}, Notification{Message: "m", Progress: &Progress{Infinite: true}}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			s, _ := newTestService(&buf)
			var events []Event
			first := s.Notify(tc.a)
			s.OnChange(func(e Event) { events = append(events, e) })
			second := s.Notify(tc.b)
			if got := len(s.Items()); got != tc.items {
				t.Fatalf("%d items, want %d", got, tc.items)
			}
			if tc.items == 1 {
				if _, ok := s.Get(first); ok {
					t.Error("the old one is still there")
				}
				if len(events) != 2 || events[0].Kind != Dismissed || events[0].Item.ID != first || events[1].Kind != Added || events[1].Item.ID != second {
					t.Errorf("events: %+v", events)
				}
			}
		})
	}
}

func TestStickyAndExpansion(t *testing.T) {
	yes := []Action{{Label: "Yes"}}
	cases := []struct {
		name       string
		n          Notification
		expanded   bool
		canCollaps bool
		sticky     bool
	}{
		{"plain", Notification{Message: "m"}, false, true, false},
		{"explicitly sticky", Notification{Message: "m", Sticky: true}, false, true, true},
		{"info with a button", Notification{Message: "m", Actions: yes}, true, false, false},
		{"error with a button", Notification{Message: "m", Severity: Error, Actions: yes}, true, false, true},
		{"error without one", Notification{Message: "m", Severity: Error}, false, true, false},
		{"running progress", Notification{Message: "m", Progress: &Progress{Infinite: true}}, false, true, true},
		{"progress with a total", Notification{Message: "m", Progress: &Progress{Total: 10}}, false, true, true},
		{"progress done", Notification{Message: "m", Progress: &Progress{Total: 10, Done: true}}, false, true, false},
		{"progress with nothing", Notification{Message: "m", Progress: &Progress{}}, false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			s, _ := newTestService(&buf)
			item, _ := s.Get(s.Notify(tc.n))
			if item.Expanded != tc.expanded || item.CanCollapse() != tc.canCollaps || item.IsSticky() != tc.sticky {
				t.Errorf("expanded %v, can collapse %v, sticky %v; want %v %v %v", item.Expanded, item.CanCollapse(), item.IsSticky(), tc.expanded, tc.canCollaps, tc.sticky)
			}
		})
	}

	var buf bytes.Buffer
	s, _ := newTestService(&buf)
	plain, withButton := s.Info("", "plain"), s.Info("", "button", yes...)
	var events []Event
	s.OnChange(func(e Event) { events = append(events, e) })
	if !s.SetExpanded(plain, true) || s.SetExpanded(plain, true) {
		t.Error("expanding changes the item once")
	}
	if item, _ := s.Get(plain); !item.Expanded || !item.IsSticky() {
		t.Error("an expanded item is sticky")
	}
	if !s.SetExpanded(plain, false) {
		t.Error("collapsing changes it back")
	}
	if s.SetExpanded(withButton, false) {
		t.Error("an item with a button cannot be collapsed")
	}
	if s.SetExpanded(99, true) {
		t.Error("no such item")
	}
	if len(events) != 2 || events[0].Kind != Updated || events[0].Item.ID != plain {
		t.Errorf("events: %+v", events)
	}
}

func TestUpdateProgress(t *testing.T) {
	var buf bytes.Buffer
	s, _ := newTestService(&buf)
	id := s.Notify(Notification{Message: "m", Progress: &Progress{Total: 100}})
	var events []Event
	s.OnChange(func(e Event) { events = append(events, e) })
	steps := []struct {
		to     Progress
		active bool
		events int
	}{
		{Progress{Total: 100, Worked: 30}, true, 1},
		{Progress{Total: 100, Worked: 30}, true, 0}, // the same: no event
		{Progress{Infinite: true}, true, 1},
		{Progress{Done: true}, false, 1},
	}
	for i, st := range steps {
		before := len(events)
		if !s.UpdateProgress(id, st.to) {
			t.Fatalf("step %d: the item is gone", i)
		}
		item, _ := s.Get(id)
		if *item.Progress != st.to || item.HasActiveProgress() != st.active || len(events)-before != st.events {
			t.Errorf("step %d: progress %+v active %v events %d", i, *item.Progress, item.HasActiveProgress(), len(events)-before)
		}
	}
	if s.UpdateProgress(99, Progress{}) {
		t.Error("no such item")
	}
	// the item the caller kept is not the one of the service
	own := &Progress{Total: 5}
	id = s.Notify(Notification{Message: "n", Progress: own})
	own.Worked = 4
	if item, _ := s.Get(id); item.Progress.Worked != 0 {
		t.Error("the service shares the progress of the caller")
	}
}

func TestClearAll(t *testing.T) {
	var buf bytes.Buffer
	s, _ := newTestService(&buf)
	a, b, c := s.Info("", "a"), s.Info("", "b"), s.Info("", "c")
	var dismissed []uint64
	s.OnChange(func(e Event) {
		if e.Kind == Dismissed {
			dismissed = append(dismissed, e.Item.ID)
		}
	})
	if n := s.ClearAll(); n != 3 || len(s.Items()) != 0 {
		t.Errorf("cleared %d, %d left", n, len(s.Items()))
	}
	if want := []uint64{c, b, a}; !slices.Equal(dismissed, want) {
		t.Errorf("dismissed %v, want %v (newest first)", dismissed, want)
	}
	if n := s.ClearAll(); n != 0 {
		t.Errorf("cleared %d of nothing", n)
	}
}

func TestActionsAreKept(t *testing.T) {
	var buf bytes.Buffer
	s, _ := newTestService(&buf)
	ran := 0
	id := s.Notify(Notification{Message: "m", Actions: []Action{{Label: "Go", Run: func() { ran++ }, KeepOpen: true}}})
	item, _ := s.Get(id)
	item.Actions[0].Run()
	if ran != 1 || !item.Actions[0].KeepOpen {
		t.Errorf("the action is not as given: ran %d", ran)
	}
}

func TestRunActionRunsTheButtonThenItsCommandAndDismissesUnlessItKeepsTheItemOpen(t *testing.T) {
	var buf bytes.Buffer
	s, _ := newTestService(&buf)
	var ran []string
	run := func(command string, args any) error {
		ran = append(ran, command)
		if command == "bad" {
			return errors.New("CommandBad", "it cannot")
		}
		return nil
	}
	cases := []struct {
		name      string
		action    Action
		index     int
		ran       []string
		wantOpen  bool
		wantFound bool
		wantError string
	}{
		{"run and command close the item", Action{Label: "Go", Command: "do", Run: func() { ran = append(ran, "run") }}, 0, []string{"run", "do"}, false, true, ""},
		{"keep open", Action{Label: "Go", KeepOpen: true, Run: func() { ran = append(ran, "run") }}, 0, []string{"run"}, true, true, ""},
		{"a command that fails is told", Action{Label: "Go", Command: "bad"}, 0, []string{"bad"}, false, true, "Cannot run bad: it cannot"},
		{"a button that is not there", Action{Label: "Go", Run: func() { ran = append(ran, "run") }}, 3, nil, true, false, ""},
		{"a negative place", Action{Label: "Go"}, -1, nil, true, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran = nil
			id := s.Warn("Git", tc.name, tc.action)
			if got := s.RunAction(id, tc.index, run); got != tc.wantFound {
				t.Errorf("RunAction = %v, want %v", got, tc.wantFound)
			}
			if !slices.Equal(ran, tc.ran) {
				t.Errorf("ran %v, want %v", ran, tc.ran)
			}
			if _, open := s.Get(id); open != tc.wantOpen {
				t.Errorf("open = %v, want %v", open, tc.wantOpen)
			}
			if tc.wantError != "" {
				last := s.Items()[len(s.Items())-1]
				if last.Severity != Error || last.Message != tc.wantError {
					t.Errorf("last notification = %+v", last)
				}
			}
			s.ClearAll()
		})
	}
	if s.RunAction(99, 0, run) {
		t.Error("RunAction found an item that does not exist")
	}
}
