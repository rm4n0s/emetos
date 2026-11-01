package graph

import (
	"reflect"
	"strings"
	"testing"
)

// wantRow is the lanes and the kind that a row of a test is expected to have.
type wantRow struct {
	kind    Kind
	in, out []Node
}

// layoutCase is a test of the layout. The cases of vscode_cases_test.go are the ones of VS Code's scmHistory.test.ts.
type layoutCase struct {
	name    string
	items   []Item
	colors  map[string]string
	current *RefInfo
	remote  *RefInfo
	// changes adds the incoming and the outgoing changes with mergeBase.
	changes   bool
	mergeBase string
	rows      []wantRow
}

func (c layoutCase) options() Options {
	return Options{ColorMap: c.colors, Current: c.current, Remote: c.remote}
}

func describe(rows []Row) string {
	var sb strings.Builder
	for _, r := range rows {
		sb.WriteString(r.Item.ID)
		sb.WriteString(" in=")
		for _, n := range r.Input {
			sb.WriteString(n.ID + ":" + n.Color + " ")
		}
		sb.WriteString("out=")
		for _, n := range r.Output {
			sb.WriteString(n.ID + ":" + n.Color + " ")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func check(t *testing.T, got []Row, want []wantRow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d rows, want %d:\n%s", len(got), len(want), describe(got))
	}
	for i, w := range want {
		g := got[i]
		if g.Kind != w.kind {
			t.Errorf("row %d (%s): kind %v, want %v", i, g.Item.ID, g.Kind, w.kind)
		}
		if !nodesEqual(g.Input, w.in) {
			t.Errorf("row %d (%s): input %v, want %v", i, g.Item.ID, g.Input, w.in)
		}
		if !nodesEqual(g.Output, w.out) {
			t.Errorf("row %d (%s): output %v, want %v", i, g.Item.ID, g.Output, w.out)
		}
	}
}

func nodesEqual(a, b []Node) bool { return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b) }

// layoutAll lays the items out in one page and adds the incoming and outgoing changes when the case wants them.
func layoutAll(c layoutCase) []Row {
	rows, _ := Layout(c.items, State{}, c.options())
	if c.changes {
		rows = WithChanges(rows, ChangesOptions{Current: c.current, Remote: c.remote, Incoming: true, Outgoing: true, MergeBase: c.mergeBase})
	}
	return rows
}

func TestLayoutOfVSCodesCases(t *testing.T) {
	for _, c := range vscodeCases {
		t.Run(c.name, func(t *testing.T) { check(t, layoutAll(c), c.rows) })
	}
}

// TestPagesGiveTheRowsOfTheWholeList lays the cases out in pages of every size: the rows are those of one page, because no case has a
// second parent whose color a label decides on a later page.
func TestPagesGiveTheRowsOfTheWholeList(t *testing.T) {
	for _, c := range vscodeCases {
		for size := 1; size <= 3; size++ {
			if c.name == "graph with color map" && size == 1 {
				continue // the color of g is decided by a reference that is on the page after e: see TestSecondParentOnALaterPage
			}
			t.Run(c.name+"/"+string(rune('0'+size)), func(t *testing.T) {
				var rows []Row
				var state State
				for i := 0; i < len(c.items); i += size {
					page := c.items[i:min(i+size, len(c.items))]
					var part []Row
					part, state = Layout(page, state, c.options())
					rows = append(rows, part...)
				}
				if c.changes {
					rows = WithChanges(rows, ChangesOptions{Current: c.current, Remote: c.remote, Incoming: true, Outgoing: true, MergeBase: c.mergeBase})
				}
				check(t, rows, c.rows)
			})
		}
	}
}

// TestSecondParentOnALaterPage is the one difference between laying out a list in pages and at once: the lane of a second parent takes
// the color of the reference of that parent when the parent is on the same page, and a color of the palette when it is on a later page
// (the whole list is not known then).
func TestSecondParentOnALaterPage(t *testing.T) {
	items := []Item{{ID: "e", ParentIDs: []string{"f", "g"}}, {ID: "g", ParentIDs: []string{"h"}, Refs: []Ref{{ID: "origin/main"}}}}
	o := Options{ColorMap: map[string]string{"origin/main": BaseRefColor}}
	tests := []struct {
		name  string
		pages [][]Item
		want  string
	}{
		{"one page", [][]Item{items}, BaseRefColor},
		{"two pages", [][]Item{items[:1], items[1:]}, Palette[1]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rows []Row
			var state State
			for _, p := range tt.pages {
				var part []Row
				part, state = Layout(p, state, o)
				rows = append(rows, part...)
			}
			if got := rows[0].Output[1].Color; got != tt.want {
				t.Errorf("the lane of g has the color %s, want %s", got, tt.want)
			}
		})
	}
}
