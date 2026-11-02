package debug_test

import (
	"fmt"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/debug"
)

func newRepl(collapse bool, maximum int) *debug.ReplModel {
	return debug.NewReplModel(func() debug.ReplConfig {
		return debug.ReplConfig{CollapseIdenticalLines: collapse, MaximumLines: maximum}
	})
}

func outputs(m *debug.ReplModel) []*debug.ReplOutput {
	var out []*debug.ReplOutput
	for _, e := range m.Elements() {
		o, ok := e.(*debug.ReplOutput)
		if !ok {
			panic(fmt.Sprintf("%T is not an output", e))
		}
		out = append(out, o)
	}
	return out
}

func texts(m *debug.ReplModel) []string {
	var out []string
	for _, e := range m.Elements() {
		out = append(out, e.Text(false))
	}
	return out
}

// Ported from VS Code: src/vs/workbench/contrib/debug/test/browser/repl.test.ts ('repl output', 'repl output count', 'repl identical
// line collapsing - ...'): the cases of those tests, and the same appended with collapsing turned off.
func TestReplAppend(t *testing.T) {
	type app struct {
		out string
		sev debug.ReplSeverity
	}
	err, warn, info := debug.ReplError, debug.ReplWarning, debug.ReplInfo
	cases := []struct {
		name     string
		collapse bool
		in       []app
		want     []string
		counts   []int
	}{
		{"lines join until one ends", true, []app{{"first line\n", err}, {"second line ", err}, {"third line ", err}, {"fourth line", err}},
			[]string{"first line\n", "second line third line fourth line"}, []int{1, 1}},
		{"another severity is another line", true, []app{{"second line ", err}, {"1", warn}}, []string{"second line ", "1"}, []int{1, 1}},
		{"incomplete lines join across events", true, []app{{"1\n", info}, {"2", info}, {"3\n4", info}, {"5\n", info}, {"6", info}},
			[]string{"1\n", "23\n", "45\n", "6"}, []int{1, 1, 1, 1}},
		{"identical lines are counted", true, []app{{"first line\n", info}, {"first line\n", info}, {"first line\n", info}, {"second line\n", info},
			{"second line\n", info}, {"third line\n", info}}, []string{"first line\nfirst line\nfirst line\n", "second line\nsecond line\n", "third line\n"}, []int{3, 2, 1}},
		{"characters of a line are not counted", true, []app{{"1", info}, {"1", info}, {"1", info}, {"\n", info}}, []string{"111\n"}, []int{1}},
		{"two characters", true, []app{{"5", info}, {"5", info}, {"\n", info}}, []string{"55\n"}, []int{1}},
		{"one event, several identical lines", true, []app{{"hello\nhello\n", info}}, []string{"hello\nhello\n"}, []int{2}},
		{"single and multi-line events mix", true, []app{{"test\n", info}, {"test\ntest\n", info}}, []string{"test\ntest\ntest\n"}, []int{3}},
		{"no collapsing", false, []app{{"a\n", info}, {"a\n", info}, {"b\nb\n", info}}, []string{"a\n", "a\n", "b\nb\n"}, []int{1, 1, 1}},
		{"no collapsing, text joins", false, []app{{"a", info}, {"b", info}}, []string{"ab"}, []int{1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRepl(tc.collapse, 1000)
			for _, a := range tc.in {
				m.Append(nil, debug.ReplData{Output: a.out, Severity: a.sev})
			}
			if got := texts(m); !slices.Equal(got, tc.want) {
				t.Errorf("texts = %q, want %q", got, tc.want)
			}
			var counts []int
			for _, o := range outputs(m) {
				counts = append(counts, o.Count())
			}
			if !slices.Equal(counts, tc.counts) {
				t.Errorf("counts = %v, want %v", counts, tc.counts)
			}
		})
	}
}

func TestReplSeverityAndSource(t *testing.T) {
	m := newRepl(true, 100)
	src := &debug.ReplSource{Source: debug.Source{Path: "/p/a.go", Name: "a.go"}, Line: 3, Column: 1}
	m.Append(nil, debug.ReplData{Output: "x\n", Severity: debug.ReplInfo, Source: src})
	// the same text from another place is another line
	m.Append(nil, debug.ReplData{Output: "x\n", Severity: debug.ReplInfo, Source: &debug.ReplSource{Source: debug.Source{Path: "/p/b.go", Name: "b.go"}, Line: 3, Column: 1}})
	m.Append(nil, debug.ReplData{Output: "x\n", Severity: debug.ReplInfo, Source: src})
	got := outputs(m)
	if len(got) != 3 {
		t.Fatalf("lines = %d, want 3", len(got))
	}
	if s := got[0].Text(true); s != "x\n a.go" {
		t.Errorf("text with source = %q", s)
	}
	for cat, want := range map[string]debug.ReplSeverity{"stderr": debug.ReplError, "console": debug.ReplWarning, "stdout": debug.ReplInfo, "": debug.ReplInfo, "important": debug.ReplInfo} {
		if got := debug.ReplSeverityOfCategory(cat); got != want {
			t.Errorf("severity of %q = %v, want %v", cat, got, want)
		}
	}
}

// Ported from VS Code: repl.test.ts ('repl groups').
func TestReplGroups(t *testing.T) {
	m := newRepl(true, 100)
	m.Append(nil, debug.ReplData{Output: "first global line", Severity: debug.ReplInfo})
	m.StartGroup(nil, "group_1", true, nil)
	m.Append(nil, debug.ReplData{Output: "first line in group", Severity: debug.ReplInfo})
	m.Append(nil, debug.ReplData{Output: "second line in group", Severity: debug.ReplInfo})
	els := m.Elements()
	if len(els) != 2 {
		t.Fatalf("elements = %d, want 2", len(els))
	}
	group, ok := els[1].(*debug.ReplGroup)
	if !ok || group.Name != "group_1" || !group.AutoExpand || group.Ended() {
		t.Fatalf("group = %+v", els[1])
	}
	m.StartGroup(nil, "group_2", false, nil)
	m.Append(nil, debug.ReplData{Output: "first line in subgroup", Severity: debug.ReplInfo})
	m.Append(nil, debug.ReplData{Output: "second line in subgroup", Severity: debug.ReplInfo})
	children := group.Children()
	if len(children) != 3 {
		t.Fatalf("children = %d, want 3", len(children))
	}
	sub, ok := children[2].(*debug.ReplGroup)
	if !ok || sub.Name != "group_2" || sub.AutoExpand || sub.Ended() || len(sub.Children()) != 2 {
		t.Fatalf("subgroup = %+v", children[2])
	}
	m.EndGroup()
	if !sub.Ended() || group.Ended() {
		t.Errorf("after the first end: subgroup ended %v, group ended %v", sub.Ended(), group.Ended())
	}
	m.Append(nil, debug.ReplData{Output: "third line in group", Severity: debug.ReplInfo})
	if n := len(group.Children()); n != 4 {
		t.Errorf("children after a line = %d, want 4", n)
	}
	m.EndGroup()
	if !group.Ended() {
		t.Error("the group did not end")
	}
	m.Append(nil, debug.ReplData{Output: "second global line", Severity: debug.ReplInfo})
	els = m.Elements()
	if len(els) != 3 || els[2].Text(false) != "second global line" {
		t.Errorf("elements = %v", texts(m))
	}
	// an end with no open group does nothing
	m.EndGroup()
	if len(m.Elements()) != 3 {
		t.Error("a stray end changed the console")
	}
}

func TestReplClear(t *testing.T) {
	m := newRepl(true, 100)
	var changes atomic.Int32
	stop := m.Subscribe(func() { changes.Add(1) })
	m.Append(nil, debug.ReplData{Output: "before\n", Severity: debug.ReplInfo})
	m.Append(nil, debug.ReplData{Output: "old\x1b[2Jnew\n", Severity: debug.ReplInfo})
	if got, want := texts(m), []string{"Console was cleared", "new\n"}; !slices.Equal(got, want) {
		t.Errorf("after the clear sequence: %q, want %q", got, want)
	}
	if outputs(m)[0].Severity != debug.ReplIgnore {
		t.Error("the note is not an ignored line")
	}
	m.Clear()
	if m.Len() != 0 {
		t.Errorf("Clear left %d elements", m.Len())
	}
	before := changes.Load()
	m.Clear() // nothing to clear: no change
	if changes.Load() != before {
		t.Error("clearing an empty console told the listeners")
	}
	stop()
	m.Append(nil, debug.ReplData{Output: "x", Severity: debug.ReplInfo})
	if changes.Load() != before {
		t.Error("a stopped listener was called")
	}
}

func TestReplMaximumLines(t *testing.T) {
	m := newRepl(false, 5)
	for i := range 12 {
		m.Append(nil, debug.ReplData{Output: fmt.Sprintf("line %d\n", i), Severity: debug.ReplInfo})
	}
	got := texts(m)
	if len(got) != 5 || got[0] != "line 7\n" || got[4] != "line 11\n" {
		t.Errorf("lines = %q", got)
	}
}

func TestReplVersionCountsChanges(t *testing.T) {
	m := newRepl(true, 100)
	v0 := m.Version()
	m.Append(nil, debug.ReplData{Output: "a\n", Severity: debug.ReplInfo})
	v1 := m.Version()
	m.Append(nil, debug.ReplData{Output: "a\n", Severity: debug.ReplInfo}) // only the count changes
	v2 := m.Version()
	if v0 >= v1 || v1 >= v2 {
		t.Errorf("versions = %d %d %d, want them to go up", v0, v1, v2)
	}
}

// Ported from VS Code: repl.test.ts ('repl filter').
func TestReplFilter(t *testing.T) {
	m := newRepl(false, 100)
	for _, l := range []string{"first line\n", "second line\n", "third line\n", "fourth line\n"} {
		m.Append(nil, debug.ReplData{Output: l, Severity: debug.ReplInfo})
	}
	all := []string{"first line\n", "second line\n", "third line\n", "fourth line\n"}
	cases := []struct {
		query string
		want  []string
	}{
		{"", all},
		{"first", []string{"first line\n"}},
		{"!first", []string{"second line\n", "third line\n", "fourth line\n"}},
		{"first, line", all},
		{"line, !second", []string{"first line\n", "third line\n", "fourth line\n"}},
		{"!second, line", []string{"first line\n", "third line\n", "fourth line\n"}},
		{"!line", nil},
		{"smth", nil},
		{"\\!first", nil},
		{"  fourth  ", []string{"fourth line\n"}},
		{",,", all},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			f := debug.NewReplFilter(tc.query)
			var got []string
			for _, e := range m.Elements() {
				if f.Visible(e, true) {
					got = append(got, e.Text(false))
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("shown = %q, want %q", got, tc.want)
			}
		})
	}
	// only outputs and groups are filtered: what the user typed shows whatever the filter says
	f := debug.NewReplFilter("nothing matches this")
	m2 := newRepl(true, 10)
	m2.Append(nil, debug.ReplData{Output: "out\n", Severity: debug.ReplInfo})
	m2.StartGroup(nil, "a group", true, nil)
	if f.Visible(m2.Elements()[0], true) || f.Visible(m2.Elements()[1], true) {
		t.Error("an output and a group are filtered")
	}
	if !f.Visible(&debug.ReplInput{}, true) {
		t.Error("an input is filtered")
	}
	// a line that no `!` term hides is shown when its group is
	g := debug.NewReplFilter("!zzz")
	if !g.Visible(m2.Elements()[0], true) || g.Visible(m2.Elements()[0], false) {
		t.Error("the visibility of the group is not passed on")
	}
}
