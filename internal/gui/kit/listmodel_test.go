package kit

import (
	"slices"
	"strings"
	"testing"
)

// listItems are five items of which the second is disabled.
func listItems() []ListItem {
	return []ListItem{{Label: "alpha"}, {Label: "beta", Disabled: true}, {Label: "gamma"}, {Label: "delta"}, {Label: "epsilon"}}
}

func TestListModelKeys(t *testing.T) {
	cases := []struct {
		name string
		ops  string // space separated: down up pgdn pgup home end filter:<text>
		want int    // the focused item, -1 for none
		top  int
	}{
		{"the first row has the focus", "", 0, 0},
		{"down skips a disabled row", "down", 2, 0},
		{"down scrolls the row into view", "down down", 3, 1},
		{"down to the end", "down down down", 4, 2},
		{"down stops at the end", "down down down down down", 4, 2},
		{"up stops at the start", "up", 0, 0},
		{"up after down skips the disabled row", "down up", 0, 0},
		{"end", "end", 4, 2},
		{"home after end", "end home", 0, 0},
		{"a page down moves by the rows in view less one", "pgdn", 2, 0},
		{"two pages down stop at the end", "pgdn pgdn", 4, 2},
		{"a page up", "end pgup", 2, 2},
		{"a filter ignores case", "filter:ALPHA", 0, 0},
		{"a filter focuses the first row that is not disabled", "filter:ta", 3, 0},
		{"a filter keeps what the label contains", "filter:ep", 4, 0},
		{"no match: nothing is focused", "filter:zz", -1, 0},
		{"keys with nothing shown do nothing", "filter:zz down end pgdn", -1, 0},
		{"up on the first enabled row stays", "filter:ta up", 3, 0},
		{"clearing the filter shows all", "filter:zz filter:", 0, 0},
		{"a filter resets the scroll", "end filter:a", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewListModel(listItems(), 3)
			for _, op := range strings.Fields(tc.ops) {
				switch {
				case op == "down":
					m.Move(1)
				case op == "up":
					m.Move(-1)
				case op == "pgdn":
					m.Page(1)
				case op == "pgup":
					m.Page(-1)
				case op == "home":
					m.First()
				case op == "end":
					m.Last()
				case strings.HasPrefix(op, "filter:"):
					m.SetFilter(strings.TrimPrefix(op, "filter:"))
				default:
					t.Fatalf("unknown op %q", op)
				}
			}
			if got := m.Focused(); got != tc.want {
				t.Errorf("focused %d, want %d", got, tc.want)
			}
			if m.Top() != tc.top {
				t.Errorf("top %d, want %d", m.Top(), tc.top)
			}
		})
	}
}

func TestListModelShownAndInView(t *testing.T) {
	m := NewListModel(listItems(), 3)
	if got := m.InView(); !slices.Equal(got, []int{0, 1, 2}) {
		t.Errorf("in view %v", got)
	}
	m.Scroll(10)
	if got := m.InView(); !slices.Equal(got, []int{2, 3, 4}) || m.Top() != 2 {
		t.Errorf("scrolled to the end: in view %v, top %d", got, m.Top())
	}
	if m.Focused() != 0 {
		t.Errorf("scrolling moved the focus to %d", m.Focused())
	}
	m.Scroll(-10)
	if m.Top() != 0 {
		t.Errorf("scrolled back: top %d", m.Top())
	}
	m.SetFilter("ta")
	if got := m.Shown(); !slices.Equal(got, []int{1, 3}) {
		t.Errorf("shown %v", got)
	}
	if m.Filter() != "ta" || m.FocusedRow() != 1 || len(m.Items()) != 5 {
		t.Errorf("filter %q, row %d, items %d", m.Filter(), m.FocusedRow(), len(m.Items()))
	}
}

func TestListModelFocusItem(t *testing.T) {
	cases := []struct {
		name string
		item int
		want int
		top  int
	}{
		{"a row below the view", 4, 4, 2},
		{"a disabled item is not focused", 1, 0, 0},
		{"an item that does not exist", 99, 0, 0},
		{"a negative item", -1, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewListModel(listItems(), 3)
			m.FocusItem(tc.item)
			if m.Focused() != tc.want || m.Top() != tc.top {
				t.Errorf("focused %d top %d, want %d and %d", m.Focused(), m.Top(), tc.want, tc.top)
			}
		})
	}
	// an item the filter hides cannot be focused
	m := NewListModel(listItems(), 3)
	m.SetFilter("eps")
	m.FocusItem(0)
	if m.Focused() != 4 {
		t.Errorf("focused %d", m.Focused())
	}
}

func TestListModelAllDisabled(t *testing.T) {
	m := NewListModel([]ListItem{{Label: "a", Disabled: true}, {Label: "b", Disabled: true}}, 0)
	m.Move(1)
	m.Last()
	m.Page(1)
	if m.Focused() != -1 || m.FocusedRow() != -1 {
		t.Errorf("focused %d", m.Focused())
	}
	if got := m.InView(); len(got) != 1 {
		t.Errorf("in view %v: a model of 0 rows shows 1 at least", got)
	}
}
