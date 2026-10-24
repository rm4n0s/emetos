package quickinput

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
)

func labelsOf(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Item.Label
	}
	return out
}

func items(labels ...string) []Item {
	out := make([]Item, len(labels))
	for i, l := range labels {
		out[i] = Item{Label: l}
	}
	return out
}

func TestPickerFilter(t *testing.T) {
	sep := func(label string) Item { return Item{Separator: true, Label: label} }
	tests := []struct {
		name  string
		setup func(p *Picker)
		items []Item
		value string
		want  []string
		// highlights are the label matches of the first row
		highlights []fuzzy.Match
	}{
		{name: "no value shows every item in order", items: items("b", "a", "c"), want: []string{"b", "a", "c"}},
		{name: "filter keeps the matches", items: items("Insert Snippet", "Go to Line", "Snippet Mode"), value: "snip", want: []string{"Snippet Mode", "Insert Snippet"},
			highlights: []fuzzy.Match{{Start: 0, End: 4}}},
		{name: "sorted by label: prefix first, then shorter", items: items("xx line", "line two", "line"), value: "line", want: []string{"line", "line two", "xx line"}},
		{name: "no match shows nothing", items: items("a", "b"), value: "zzz", want: nil},
		{name: "always show survives the filter", items: []Item{{Label: "a"}, {Label: "keep", AlwaysShow: true}}, value: "zzz", want: []string{"keep"}},
		{name: "matching the description", setup: func(p *Picker) { p.MatchOnDescription = true },
			items: []Item{{Label: "one", Description: "alpha"}, {Label: "two", Description: "beta"}}, value: "bet", want: []string{"two"}},
		{name: "matching the detail", setup: func(p *Picker) { p.MatchOnDetail = true },
			items: []Item{{Label: "one", Detail: "alpha"}, {Label: "two", Detail: "beta"}}, value: "alp", want: []string{"one"}},
		{name: "description is not looked at by default", items: []Item{{Label: "one", Description: "alpha"}}, value: "alp", want: nil},
		{name: "separators are dropped while sorting", items: []Item{sep("A"), {Label: "bx"}, sep("B"), {Label: "ax"}}, value: "x", want: []string{"ax", "bx"}},
		{name: "separators stay when the list is not sorted", setup: func(p *Picker) { p.SortByLabel = false },
			items: []Item{sep("A"), {Label: "bx"}, sep("B"), {Label: "ax"}}, value: "x", want: []string{"bx", "ax"}},
		{name: "filter ignores white space around it", items: items("alpha"), value: "  alp  ", want: []string{"alpha"}},
		{name: "switches off: provider filters", setup: func(p *Picker) { p.MatchOnLabel, p.SortByLabel = false, false },
			items: items("one", "two"), value: "zzz", want: []string{"one", "two"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPicker()
			if tc.setup != nil {
				tc.setup(p)
			}
			p.SetItems(tc.items)
			p.SetValue(tc.value)
			got := labelsOf(p.Rows())
			if !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Fatalf("rows %v, want %v", got, tc.want)
			}
			if tc.highlights != nil && !reflect.DeepEqual(p.Rows()[0].Label, tc.highlights) {
				t.Errorf("highlights %v, want %v", p.Rows()[0].Label, tc.highlights)
			}
		})
	}
}

func TestPickerSeparatorsAndBorders(t *testing.T) {
	p := NewPicker()
	p.SetItems([]Item{
		{Separator: true, Label: "recently used"}, {Label: "a"}, {Label: "b"},
		{Separator: true, Label: "other commands"}, {Label: "c"}, {Label: "d"},
	})
	rows := p.Rows()
	if got := labelsOf(rows); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("rows %v", got)
	}
	// a separator is drawn on the row after it, with a line above it unless it is the first row
	wantSep := []string{"recently used", "", "other commands", ""}
	wantBorder := []bool{false, false, true, false}
	for i, r := range rows {
		if r.Separator != wantSep[i] || r.Border != wantBorder[i] {
			t.Errorf("row %d: separator %q border %v", i, r.Separator, r.Border)
		}
	}
	if rows[2].Index != 4 {
		t.Errorf("index of c = %d, want 4 (the position among the items)", rows[2].Index)
	}
}

func TestPickerActiveItem(t *testing.T) {
	tests := []struct {
		name string
		do   func(p *Picker)
		want int
	}{
		{"starts on the first", func(p *Picker) {}, 0},
		{"down", func(p *Picker) { p.Move(1) }, 1},
		{"down wraps to the first", func(p *Picker) { p.Move(4) }, 0},
		{"up wraps to the last", func(p *Picker) { p.Move(-1) }, 3},
		{"up from the middle", func(p *Picker) { p.Move(2); p.Move(-1) }, 1},
		{"page down is rows less one", func(p *Picker) { p.Page(1, 3) }, 2},
		{"page down stops at the last", func(p *Picker) { p.Page(1, 30) }, 3},
		{"page up stops at the first", func(p *Picker) { p.Move(1); p.Page(-1, 30) }, 0},
		{"last", func(p *Picker) { p.Last() }, 3},
		{"first", func(p *Picker) { p.Last(); p.First() }, 0},
		{"set", func(p *Picker) { p.SetActive(2) }, 2},
		{"set out of range does nothing", func(p *Picker) { p.SetActive(9) }, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPicker()
			p.SetItems(items("a", "b", "c", "d"))
			tc.do(p)
			if got := p.ActiveIndex(); got != tc.want {
				t.Errorf("active %d, want %d", got, tc.want)
			}
		})
	}
	t.Run("an empty list has no active row", func(t *testing.T) {
		p := NewPicker()
		p.Move(1)
		p.Page(1, 5)
		if _, ok := p.Active(); ok || p.ActiveIndex() != -1 {
			t.Errorf("active %d", p.ActiveIndex())
		}
	})
	t.Run("filtering activates the first row again", func(t *testing.T) {
		p := NewPicker()
		p.SetItems(items("alpha", "beta", "gamma"))
		p.Move(2)
		p.SetValue("a")
		if r, _ := p.Active(); r.Item.Label != p.Rows()[0].Item.Label || p.ActiveIndex() != 0 {
			t.Errorf("active %d", p.ActiveIndex())
		}
	})
	t.Run("events", func(t *testing.T) {
		p := NewPicker()
		var actives []string
		var values []string
		p.OnDidChangeActive(func(r Row, ok bool) {
			if ok {
				actives = append(actives, r.Item.Label)
			} else {
				actives = append(actives, "-")
			}
		})
		p.OnDidChangeValue(func(v string) { values = append(values, v) })
		p.SetItems(items("a", "b"))
		p.Move(1)
		p.SetValue("b")
		p.SetValue("b") // no change
		// the filter moves the active row from the second to the first, which is the item b again
		if !reflect.DeepEqual(actives, []string{"a", "b", "b"}) {
			t.Errorf("active events %v", actives)
		}
		if !reflect.DeepEqual(values, []string{"b"}) {
			t.Errorf("value events %v", values)
		}
	})
}

func TestPickerKeepScrollPositionKeepsTheActiveItem(t *testing.T) {
	p := NewPicker()
	p.MatchOnLabel, p.SortByLabel = false, false
	p.SetItems([]Item{{ID: "a", Label: "a"}, {ID: "b", Label: "b"}, {ID: "c", Label: "c"}})
	p.Move(2)
	p.KeepScrollPosition = true
	p.SetItems([]Item{{ID: "b", Label: "b"}, {ID: "c", Label: "c"}})
	if r, _ := p.Active(); r.Item.ID != "c" {
		t.Errorf("active %q, want c", r.Item.ID)
	}
	p.KeepScrollPosition = false
	p.SetItems([]Item{{ID: "b", Label: "b"}, {ID: "c", Label: "c"}})
	if r, _ := p.Active(); r.Item.ID != "b" {
		t.Errorf("active %q, want b", r.Item.ID)
	}
}

func TestPickerPrefixIsLeftOutOfTheFilter(t *testing.T) {
	p := NewPicker()
	p.SetPrefixLen(1)
	p.SetItems(items("line", "other"))
	p.SetValue(">line")
	if p.Filter() != "line" || p.RawFilter() != "line" {
		t.Errorf("filter %q", p.Filter())
	}
	if got := labelsOf(p.Rows()); !reflect.DeepEqual(got, []string{"line"}) {
		t.Errorf("rows %v", got)
	}
	p.SetValue(">")
	if p.Filter() != "" || len(p.Rows()) != 2 {
		t.Errorf("filter %q rows %d", p.Filter(), len(p.Rows()))
	}
}

func TestRowHeight(t *testing.T) {
	if (Row{Item: Item{Label: "a"}}).Height() != 22 || (Row{Item: Item{Label: "a", Detail: "d"}}).Height() != 44 {
		t.Errorf("heights")
	}
}
