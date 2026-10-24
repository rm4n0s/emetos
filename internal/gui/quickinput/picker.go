package quickinput

import (
	"slices"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
)

// Ported from VS Code: src/vs/platform/quickinput/browser/quickInput.ts (QuickPick) and
// src/vs/platform/quickinput/browser/quickInputList.ts (filter, setElements, focus)

// Picker is the state of one quick pick: the text in its input, the items, which of them the
// filter lets through, and the active item. The filter is VS Code's: with MatchOnLabel (and
// MatchOnDescription, MatchOnDetail) an item shows when the filter matches the text, matches are
// highlighted and, with SortByLabel, the items with a match on the label come first in the order of
// fuzzy.CompareAnything. A provider that narrows the items itself switches the three off.
//
// A Picker is used on the UI thread only.
type Picker struct {
	// MatchOnLabel, MatchOnDescription, MatchOnDetail and SortByLabel are the switches of the
	// filter; NewPicker turns on the first and the last. Change them before the next SetValue or
	// SetItems.
	MatchOnLabel, MatchOnDescription, MatchOnDetail, SortByLabel bool
	// HideInput hides the input of the picker, which keeps the keyboard: the switcher that Ctrl+Tab opens.
	HideInput bool
	// KeepScrollPosition keeps the first row in view when the items change instead of showing the
	// top of the list.
	KeepScrollPosition bool

	placeholder string
	value       string
	// prefixLen is how much of the value the filter leaves out (the prefix of the provider).
	prefixLen int
	items     []Item
	rows      []Row
	version   int
	active    int
	valueSel  [2]int
	hasSel    bool

	activation ItemActivation

	changed     []*func()
	valueChange []*func(string)
	activeChang []*func(Row, bool)
}

// ItemActivation says which item is active once the items of a picker are set (VS Code's ItemActivation).
// It applies to the first items that give a row and then goes back to ItemFirst: typing a filter later
// activates the first row as always.
type ItemActivation int

// The ways the first active item is chosen.
const (
	// ItemFirst activates the first item that can be chosen.
	ItemFirst ItemActivation = iota
	// ItemSecond activates the second item, or the first when there is only one: the editor that was used
	// before the active one.
	ItemSecond
	// ItemLast activates the last item.
	ItemLast
	// ItemNone activates no item.
	ItemNone
)

// SetItemActivation says which row becomes active when the items are set next.
func (p *Picker) SetItemActivation(a ItemActivation) { p.activation = a }

// NewPicker makes an empty picker that filters by label and sorts by label.
func NewPicker() *Picker {
	return &Picker{MatchOnLabel: true, SortByLabel: true, active: -1}
}

// Placeholder is the hint shown in the input while it is empty.
func (p *Picker) Placeholder() string { return p.placeholder }

// SetPlaceholder sets the hint of the input.
func (p *Picker) SetPlaceholder(s string) {
	if p.placeholder != s {
		p.placeholder = s
		p.fireChanged()
	}
}

// Value is the text of the input, with the prefix of the provider.
func (p *Picker) Value() string { return p.value }

// Filter is the text the items are filtered by: the value without the prefix of the provider and
// without white space around it.
func (p *Picker) Filter() string { return strings.TrimSpace(p.RawFilter()) }

// RawFilter is the value without the prefix of the provider.
func (p *Picker) RawFilter() string {
	if p.prefixLen > len(p.value) {
		return ""
	}
	return p.value[p.prefixLen:]
}

// SetPrefixLen sets how many bytes of the value the filter leaves out.
func (p *Picker) SetPrefixLen(n int) {
	p.prefixLen = max(n, 0)
	p.applyFilter()
}

// SetValue changes the text of the input, filters the items and tells the listeners of the value.
func (p *Picker) SetValue(v string) {
	if p.value == v {
		return
	}
	p.value = v
	p.applyFilter()
	for _, f := range slices.Clone(p.valueChange) {
		(*f)(v)
	}
}

// ValueSelection is the part of the value the input should select when it is shown, as byte
// offsets; ok is false when nobody asked for one. Reading it does not clear it.
func (p *Picker) ValueSelection() (start, end int, ok bool) {
	return p.valueSel[0], p.valueSel[1], p.hasSel
}

// SetValueSelection asks for a part of the value to be selected.
func (p *Picker) SetValueSelection(start, end int) {
	p.valueSel, p.hasSel = [2]int{start, end}, true
	p.fireChanged()
}

// ClearValueSelection forgets the selection asked for, once the input has taken it.
func (p *Picker) ClearValueSelection() { p.hasSel = false }

// Items are the items set, whatever the filter says.
func (p *Picker) Items() []Item { return p.items }

// SetItems replaces the items. The filter is applied and the first item that can be chosen
// becomes active, as the list does after VS Code sets the items; with KeepScrollPosition the
// active item stays the same when it is still there.
func (p *Picker) SetItems(items []Item) {
	var keep *Item
	if p.KeepScrollPosition {
		if r, ok := p.Active(); ok {
			it := r.Item
			keep = &it
		}
	}
	p.items = items
	p.applyFilter()
	p.activate()
	if keep != nil {
		for i, r := range p.rows {
			if r.Item.ID == keep.ID && r.Item.Label == keep.Label && r.Item.ID != "" {
				p.setActive(i)
				break
			}
		}
	}
}

// Version counts how many times the rows were made again, so that a list can start at the top when
// they are new.
func (p *Picker) Version() int { return p.version }

// Rows are the items the filter lets through, in the order they are shown.
func (p *Picker) Rows() []Row { return p.rows }

// ActiveIndex is the position in Rows of the active row, or -1 when there is none.
func (p *Picker) ActiveIndex() int { return p.active }

// Active is the active row (VS Code's active item: the one the arrow keys moved to).
func (p *Picker) Active() (Row, bool) {
	if p.active < 0 || p.active >= len(p.rows) {
		return Row{}, false
	}
	return p.rows[p.active], true
}

// SetActive makes the row at an index active. An index out of range, or of a separator, does
// nothing.
func (p *Picker) SetActive(i int) {
	if i < 0 || i >= len(p.rows) || p.rows[i].Item.Separator {
		return
	}
	p.setActive(i)
}

// Move moves the active row by delta rows, wrapping round at the ends. Moving down from the
// last row goes to the first and moving up from the first goes to the last, as the list of the
// quick pick does.
func (p *Picker) Move(delta int) {
	n := len(p.rows)
	if n == 0 || delta == 0 {
		return
	}
	cur := p.active
	if cur < 0 {
		if delta > 0 {
			cur = -1
		} else {
			cur = n
		}
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	for left := abs(delta); left > 0; left-- {
		for tries := 0; tries < n; tries++ {
			cur = ((cur+step)%n + n) % n
			if !p.rows[cur].Item.Separator {
				break
			}
		}
	}
	p.setActive(cur)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Page moves the active row by a page of rows without wrapping: down goes to the last row at
// most and up to the first. rows is the number of rows in view.
func (p *Picker) Page(direction, rows int) {
	n := len(p.rows)
	if n == 0 || direction == 0 {
		return
	}
	p.setActive(min(max(max(p.active, 0)+direction*max(rows-1, 1), 0), n-1))
}

// First makes the first row that can be chosen active.
func (p *Picker) First() { p.setActive(p.firstSelectable()) }

// Last makes the last row that can be chosen active.
func (p *Picker) Last() {
	for i := len(p.rows) - 1; i >= 0; i-- {
		if !p.rows[i].Item.Separator {
			p.setActive(i)
			return
		}
	}
}

func (p *Picker) firstSelectable() int {
	for i, r := range p.rows {
		if !r.Item.Separator {
			return i
		}
	}
	return -1
}

func (p *Picker) setActive(i int) {
	if p.active == i {
		return
	}
	p.active = i
	p.fireActive()
	p.fireChanged()
}

// OnChange calls f whenever what the picker shows changes: the rows, the active row, the
// placeholder, the value selection.
func (p *Picker) OnChange(f func()) (remove func()) {
	return addListener(&p.changed, &f)
}

// OnDidChangeValue calls f with the value when SetValue changes it (VS Code's onDidChangeValue).
func (p *Picker) OnDidChangeValue(f func(string)) (remove func()) {
	return addListener(&p.valueChange, &f)
}

// OnDidChangeActive calls f when the active row changes (VS Code's onDidChangeActive); ok is
// false when no row is active. The theme preview uses it.
func (p *Picker) OnDidChangeActive(f func(r Row, ok bool)) (remove func()) {
	return addListener(&p.activeChang, &f)
}

func addListener[T any](list *[]*T, f *T) func() {
	*list = append(*list, f)
	return func() {
		if i := slices.Index(*list, f); i >= 0 {
			*list = slices.Delete(*list, i, i+1)
		}
	}
}

func (p *Picker) fireChanged() {
	for _, f := range slices.Clone(p.changed) {
		(*f)()
	}
}

func (p *Picker) fireActive() {
	r, ok := p.Active()
	for _, f := range slices.Clone(p.activeChang) {
		(*f)(r, ok)
	}
}

// applyFilter makes the rows from the items and the filter, and activates the first row.
func (p *Picker) applyFilter() {
	prevActive := p.active
	p.version++
	query := p.Filter()
	if query == "" || (!p.MatchOnLabel && !p.MatchOnDescription && !p.MatchOnDetail) {
		p.rows = p.rowsUnfiltered()
	} else {
		p.rows = p.rowsFiltered(query)
	}
	p.active = p.firstSelectable()
	if p.active != prevActive {
		p.fireActive()
	}
	p.fireChanged()
}

// rowsUnfiltered shows every item. A separator is drawn as the label of the row after it, so it has
// no row of its own.
func (p *Picker) rowsUnfiltered() []Row {
	rows := make([]Row, 0, len(p.items))
	for i, it := range p.items {
		if it.Separator {
			continue
		}
		r := Row{Item: it, Index: i, Label: it.Highlights.Label, Description: it.Highlights.Description, Detail: it.Highlights.Detail}
		if i > 0 && p.items[i-1].Separator {
			r.Separator = p.items[i-1].Label
		}
		r.Border = r.Separator != "" && len(rows) > 0
		rows = append(rows, r)
	}
	return rows
}

// rowsFiltered keeps the items that match the query and, with SortByLabel, puts them in order.
func (p *Picker) rowsFiltered(query string) []Row {
	var rows []Row
	var pending string
	var has bool
	for i, it := range p.items {
		if it.Separator {
			pending, has = it.Label, true
			continue
		}
		var lh, dh, th []fuzzy.Match
		var ok bool
		if p.MatchOnLabel {
			lh, ok = fuzzy.MatchesFuzzy(query, it.Label, false)
		}
		if p.MatchOnDescription && it.Description != "" {
			if m, found := fuzzy.MatchesFuzzy(query, it.Description, false); found {
				dh = m
			}
		}
		if p.MatchOnDetail && it.Detail != "" {
			if m, found := fuzzy.MatchesFuzzy(query, it.Detail, false); found {
				th = m
			}
		}
		show := ok || len(dh) > 0 || len(th) > 0 || it.AlwaysShow
		if !show {
			continue
		}
		r := Row{Item: it, Index: i, Label: lh, Description: dh, Detail: th}
		if has && !p.SortByLabel {
			r.Separator = pending
		}
		has = false
		rows = append(rows, r)
	}
	if p.SortByLabel {
		lookFor := strings.ToLower(query)
		slices.SortStableFunc(rows, func(a, b Row) int {
			switch {
			case len(a.Label) > 0 && len(b.Label) == 0:
				return -1
			case len(a.Label) == 0 && len(b.Label) > 0:
				return 1
			case len(a.Label) == 0:
				return 0
			}
			return fuzzy.CompareAnything(a.Item.Label, b.Item.Label, lookFor)
		})
	}
	for i := range rows {
		rows[i].Border = rows[i].Separator != "" && i > 0
	}
	return rows
}

// activate gives the first rows that a SetItems makes the active row that SetItemActivation asked for.
func (p *Picker) activate() {
	if p.activation == ItemFirst || len(p.rows) == 0 {
		return
	}
	a := p.activation
	p.activation = ItemFirst
	switch a {
	case ItemSecond:
		second, seen := -1, false
		for i, r := range p.rows {
			if r.Item.Separator {
				continue
			}
			if seen {
				second = i
				break
			}
			seen = true
		}
		if second >= 0 {
			p.setActive(second)
		}
	case ItemLast:
		p.Last()
	case ItemNone:
		p.setActive(-1)
	case ItemFirst:
	}
}
