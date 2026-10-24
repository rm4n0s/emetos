package workbench

import (
	"math"
	"reflect"
	"testing"
)

// ids lists the ids of the items of a side in the order the bar shows them.
func ids(b *StatusBar, a StatusAlignment) []string {
	var out []string
	for _, it := range b.Items(a) {
		out = append(out, it.ID)
	}
	return out
}

func TestStatusBarOrder(t *testing.T) {
	type add struct {
		id       string
		side     StatusAlignment
		priority float64
	}
	tests := []struct {
		name        string
		adds        []add
		left, right []string
	}{
		{"the higher priority is further left, on both sides",
			[]add{{"a", StatusLeft, 1}, {"b", StatusLeft, 50}, {"c", StatusRight, 100.1}, {"d", StatusRight, 100.5}},
			[]string{"b", "a"}, []string{"d", "c"}},
		{"equal priorities keep the order they were added in",
			[]add{{"a", StatusLeft, 0}, {"b", StatusLeft, 0}, {"c", StatusLeft, 0}},
			[]string{"a", "b", "c"}, nil},
		{"negative infinity is last, as the bell and the status message are",
			[]add{{"bell", StatusRight, math.Inf(-1)}, {"x", StatusRight, 100}, {"msg", StatusLeft, math.Inf(-1)}, {"problems", StatusLeft, 50}},
			[]string{"problems", "msg"}, []string{"x", "bell"}},
		{"fractions of the editor's items",
			[]add{{"lang", StatusRight, 100.1}, {"sel", StatusRight, 100.5}, {"eol", StatusRight, 100.2}, {"enc", StatusRight, 100.3}, {"ind", StatusRight, 100.4}},
			nil, []string{"sel", "ind", "enc", "eol", "lang"}},
		{"the sides do not mix",
			[]add{{"l", StatusLeft, 1}, {"r", StatusRight, 1000}},
			[]string{"l"}, []string{"r"}},
		{"no items", nil, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewStatusBar()
			for _, a := range tt.adds {
				b.Add(a.id, a.side, a.priority, StatusEntry{Text: a.id})
			}
			if got := ids(b, StatusLeft); !reflect.DeepEqual(got, tt.left) {
				t.Errorf("left = %v, want %v", got, tt.left)
			}
			if got := ids(b, StatusRight); !reflect.DeepEqual(got, tt.right) {
				t.Errorf("right = %v, want %v", got, tt.right)
			}
		})
	}
}

func TestStatusBarUpdateAndDispose(t *testing.T) {
	b := NewStatusBar()
	var changes int
	remove := b.OnChange(func() { changes++ })
	a := b.Add("x", StatusLeft, 0, StatusEntry{Text: "one", Args: map[string]any{"k": 1.0}})
	if changes != 1 {
		t.Fatalf("add told %d times", changes)
	}
	steps := []struct {
		name  string
		do    func()
		told  int
		check func(StatusItem) bool
	}{
		{"the same entry tells nobody", func() { a.Update(StatusEntry{Text: "one", Args: map[string]any{"k": 1.0}}) }, 0, func(i StatusItem) bool { return i.Entry.Text == "one" }},
		{"a new text", func() { a.Update(StatusEntry{Text: "two"}) }, 1, func(i StatusItem) bool { return i.Entry.Text == "two" }},
		{"other arguments", func() { a.Update(StatusEntry{Text: "two", Args: 3.0}) }, 1, func(i StatusItem) bool { return i.Entry.Args == 3.0 }},
		{"hide", func() { a.SetVisible(false) }, 1, func(i StatusItem) bool { return !i.Visible }},
		{"hide again", func() { a.SetVisible(false) }, 0, func(i StatusItem) bool { return !i.Visible }},
		{"show", func() { a.SetVisible(true) }, 1, func(i StatusItem) bool { return i.Visible }},
	}
	for _, st := range steps {
		before := changes
		st.do()
		if changes-before != st.told {
			t.Errorf("%s: told %d times, want %d", st.name, changes-before, st.told)
		}
		it, ok := b.Get("x")
		if !ok || !st.check(it) {
			t.Errorf("%s: item %+v", st.name, it)
		}
	}
	// a hidden item is not listed
	a.SetVisible(false)
	if got := ids(b, StatusLeft); len(got) != 0 {
		t.Errorf("a hidden item is listed: %v", got)
	}
	a.SetVisible(true)
	before := changes
	a.Dispose()
	a.Dispose()
	if changes-before != 1 {
		t.Errorf("dispose told %d times", changes-before)
	}
	if _, ok := b.Get("x"); ok {
		t.Error("the item is still there")
	}
	a.Update(StatusEntry{Text: "ghost"}) // does nothing
	a.SetVisible(true)
	if _, ok := b.Get("x"); ok || changes-before != 1 {
		t.Error("a disposed item came back")
	}
	remove()
	b.Add("y", StatusLeft, 0, StatusEntry{})
	if changes-before != 1 {
		t.Error("a listener that was removed is told")
	}
}

func TestParseStatusText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []StatusTextRun
	}{
		{"plain", "Ln 1, Col 2", []StatusTextRun{{Text: "Ln 1, Col 2"}}},
		{"empty", "", nil},
		{"an icon", "$(bell)", []StatusTextRun{{Icon: "bell"}}},
		{"the problems", "$(error) 0 $(warning) 3", []StatusTextRun{{Icon: "error"}, {Text: " 0 "}, {Icon: "warning"}, {Text: " 3"}}},
		{"spinning", "$(sync~spin) Syncing", []StatusTextRun{{Icon: "sync", Spin: true}, {Text: " Syncing"}}},
		{"another modifier is not spin", "$(sync~other)", []StatusTextRun{{Icon: "sync"}}},
		{"an escaped icon is text", `\$(bell) is $(bell)`, []StatusTextRun{{Text: "$(bell) is "}, {Icon: "bell"}}},
		{"two icons in a row", "$(a)$(b-c)", []StatusTextRun{{Icon: "a"}, {Icon: "b-c"}}},
		{"not an icon", "$(not an icon) $( ) $x", []StatusTextRun{{Text: "$(not an icon) $( ) $x"}}},
		{"text around", "x $(git-branch) main", []StatusTextRun{{Text: "x "}, {Icon: "git-branch"}, {Text: " main"}}},
		{"a backslash that escapes nothing stays", `a\b`, []StatusTextRun{{Text: `a\b`}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseStatusText(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseStatusText(%q) = %+v, want %+v", tt.text, got, tt.want)
			}
		})
	}
}
