package scm

import (
	"slices"
	"testing"
)

// TestHistory runs the cases of VS Code's "History Navigator 2" suite.
//
// Ported from VS Code: src/vs/base/test/common/history.test.ts (History Navigator 2: constructor, constructor - capacity limit,
// constructor - duplicate values, navigation, add, add - existing value, replaceLast, replaceLast - existing value). Skipped:
// "initial history is not empty" (the constructor throws there; here an empty list gets one empty message) and "prepend"
// (the history is not loaded from storage).
func TestHistory(t *testing.T) {
	type step struct {
		do   func(h *History) string // returns what the step reports
		want string
	}
	cur := func(h *History) string { return h.Current() }
	tests := []struct {
		name     string
		capacity int
		values   []string
		steps    []step
		atEnd    bool
		values_  []string
	}{
		{name: "constructor", capacity: 10, values: []string{"1", "2", "3", "4"},
			steps: []step{{cur, "4"}}, atEnd: true, values_: []string{"1", "2", "3", "4"}},
		{name: "capacity limit", capacity: 3, values: []string{"1", "2", "3", "4"},
			steps: []step{{cur, "4"}, {func(h *History) string { return boolText(h.Has("1")) }, "false"}}, atEnd: true,
			values_: []string{"2", "3", "4"}},
		{name: "duplicate values", capacity: 10, values: []string{"1", "2", "3", "4", "3", "2", "1"},
			steps: []step{{cur, "1"}}, atEnd: true, values_: []string{"4", "3", "2", "1"}},
		{name: "empty list is one empty message", capacity: 10, values: nil,
			steps: []step{{cur, ""}}, atEnd: true, values_: []string{""}},
		{name: "navigation", capacity: 10, values: []string{"1", "2", "3", "4"}, steps: []step{
			{cur, "4"}, {func(h *History) string { return h.Next() }, "4"},
			{func(h *History) string { return h.Previous() }, "3"}, {func(h *History) string { return h.Previous() }, "2"},
			{func(h *History) string { return h.Previous() }, "1"}, {func(h *History) string { return h.Previous() }, "1"},
			{cur, "1"}, {func(h *History) string { return h.Next() }, "2"}, {func(h *History) string { return h.ResetCursor() }, "4"},
		}, atEnd: true, values_: []string{"1", "2", "3", "4"}},
		{name: "add", capacity: 10, values: []string{"1", "2", "3", "4"}, steps: []step{
			{func(h *History) string { h.Add("5"); return h.Current() }, "5"},
		}, atEnd: true, values_: []string{"1", "2", "3", "4", "5"}},
		{name: "add existing value", capacity: 10, values: []string{"1", "2", "3", "4"}, steps: []step{
			{func(h *History) string { h.Add("2"); return h.Current() }, "2"},
			{func(h *History) string { return h.Previous() }, "4"}, {func(h *History) string { return h.Previous() }, "3"},
			{func(h *History) string { return h.Previous() }, "1"},
		}, atEnd: false, values_: []string{"1", "3", "4", "2"}},
		{name: "replaceLast", capacity: 10, values: []string{"1", "2", "3", "4"}, steps: []step{
			{func(h *History) string { h.ReplaceLast("5"); return h.Current() }, "5"},
			{func(h *History) string { return boolText(h.Has("4")) }, "false"},
			{func(h *History) string { return h.Previous() }, "3"}, {func(h *History) string { return h.Previous() }, "2"},
			{func(h *History) string { return h.Previous() }, "1"},
		}, atEnd: false, values_: []string{"1", "2", "3", "5"}},
		{name: "replaceLast existing value", capacity: 10, values: []string{"1", "2", "3", "4"}, steps: []step{
			{func(h *History) string { h.ReplaceLast("2"); return h.Current() }, "2"},
			{func(h *History) string { return boolText(h.Has("4")) }, "false"},
			{func(h *History) string { return h.Previous() }, "3"}, {func(h *History) string { return h.Previous() }, "1"},
		}, atEnd: false, values_: []string{"1", "3", "2"}},
		{name: "replaceLast with the same value", capacity: 10, values: []string{"a", "b"}, steps: []step{
			{func(h *History) string { return h.ReplaceLast("b") }, "b"},
		}, atEnd: true, values_: []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHistory(tt.capacity, tt.values...)
			for i, s := range tt.steps {
				if got := s.do(h); got != s.want {
					t.Errorf("step %d = %q, want %q", i, got, s.want)
				}
			}
			if got := h.IsAtEnd(); got != tt.atEnd {
				t.Errorf("IsAtEnd = %v, want %v", got, tt.atEnd)
			}
			if got := h.Values(); !slices.Equal(got, tt.values_) {
				t.Errorf("values = %q, want %q", got, tt.values_)
			}
		})
	}
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestInputHistory walks through what the box does with the messages of a repository: typing is not remembered until the user
// goes back, a message the provider sets is, Up and Down move through them, and the text being written is kept.
func TestInputHistory(t *testing.T) {
	type op struct {
		name string
		do   func(in *Input)
		want string
	}
	typed := func(v string) func(*Input) { return func(in *Input) { in.SetTransientValue(v) } }
	provider := func(v string) func(*Input) { return func(in *Input) { in.SetValue(v) } }
	prev := func(in *Input) { in.ShowPreviousHistoryValue() }
	next := func(in *Input) { in.ShowNextHistoryValue() }
	tests := []struct {
		name string
		ops  []op
		hist []string
	}{
		{name: "nothing before", ops: []op{{"Up in an empty history", prev, ""}, {"Down at the end", next, ""}}, hist: []string{""}},
		{name: "a commit and back", ops: []op{
			{"type", typed("Fix the cache"), "Fix the cache"},
			{"the provider commits and empties the box", provider(""), ""},
			{"Up brings the message back", prev, "Fix the cache"},
			{"Down goes to the empty box", next, ""},
		}, hist: []string{"Fix the cache", ""}},
		{name: "the text being written is kept", ops: []op{
			{"type one", typed("one"), "one"}, {"commit one", provider(""), ""},
			{"type two", typed("two"), "two"}, {"commit two", provider(""), ""},
			{"type a new text", typed("draft"), "draft"},
			{"Up", prev, "two"}, {"Up", prev, "one"}, {"Up at the first", prev, "one"},
			{"Down", next, "two"}, {"Down brings the draft back", next, "draft"}, {"Down at the end", next, "draft"},
		}, hist: []string{"one", "two", "draft"}},
		{name: "an edited old message becomes the last one", ops: []op{
			{"type", typed("one"), "one"}, {"commit", provider(""), ""},
			{"Up", prev, "one"},
			{"edit the old message", typed("one!"), "one!"},
			{"Down keeps the edit as the last message and stays on it", next, "one!"},
			{"Up", prev, "one"},
		}, hist: []string{"one", "one!"}},
		{name: "the same message twice is one", ops: []op{
			{"type", typed("same"), "same"}, {"commit", provider(""), ""},
			{"type it again", typed("same"), "same"}, {"commit", provider(""), ""},
		}, hist: []string{"same", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(DefaultConfig())
			s := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/r", Owner: "e"})
			in := s.Input()
			for _, o := range tt.ops {
				o.do(in)
				if got := in.Value(); got != o.want {
					t.Fatalf("after %s the text is %q, want %q", o.name, got, o.want)
				}
			}
			if got := in.History(); !slices.Equal(got, tt.hist) {
				t.Errorf("history = %q, want %q", got, tt.hist)
			}
		})
	}
}

func TestInputHistoryReasonAndListeners(t *testing.T) {
	m := NewModel(DefaultConfig())
	s := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/r", Owner: "e"})
	in := s.Input()
	var seen []string
	stop := in.OnChange(func(v string) { seen = append(seen, v) })
	defer stop()
	in.SetValue("one")
	in.SetValue("")
	in.ShowPreviousHistoryValue()
	if r := in.ChangeReason(); r != ChangedByHistoryPrevious {
		t.Errorf("reason after Up = %v", r)
	}
	in.ShowNextHistoryValue()
	if r := in.ChangeReason(); r != ChangedByHistoryNext {
		t.Errorf("reason after Down = %v", r)
	}
	in.SetTransientValue("x")
	if r := in.ChangeReason(); r != ChangedByValue {
		t.Errorf("reason after typing = %v", r)
	}
	if want := []string{"one", "", "one", "", "x"}; !slices.Equal(seen, want) {
		t.Errorf("OnChange saw %q, want %q", seen, want)
	}
}

// TestInputHistoryOutlivesTheSource: a repository that is closed and found again has its messages and its unsent text back;
// a source control with no folder keeps them with itself only.
func TestInputHistoryOutlivesTheSource(t *testing.T) {
	m := NewModel(DefaultConfig())
	spec := SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/r", Owner: "e"}
	s := m.Add(spec)
	s.Input().SetValue("sent")
	s.Input().SetValue("")
	s.Input().SetTransientValue("unsent")
	m.Remove(s)
	again := m.Add(spec)
	if got := again.Input().Value(); got != "unsent" {
		t.Errorf("text after reopening = %q, want unsent", got)
	}
	if got, want := again.Input().History(), []string{"sent", "unsent"}; !slices.Equal(got, want) {
		t.Errorf("history after reopening = %q, want %q", got, want)
	}
	bare := m.Add(SourceSpec{ProviderID: "x", Label: "X", Owner: "e"})
	bare.Input().SetValue("a")
	m.Remove(bare)
	if got := m.Add(SourceSpec{ProviderID: "x", Label: "X", Owner: "e"}).Input().Value(); got != "" {
		t.Errorf("a source control with no folder got the text %q back", got)
	}
}
