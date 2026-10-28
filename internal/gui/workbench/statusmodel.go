package workbench

import (
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// StatusAlignment is the side of the status bar an item is on.
type StatusAlignment int

// The sides of the status bar.
const (
	// StatusLeft puts the item at the left end of the bar.
	StatusLeft StatusAlignment = iota
	// StatusRight puts the item at the right end of the bar.
	StatusRight
)

// StatusKind is the look of an item: the colors it has in place of the bar's.
type StatusKind int

// The kinds of item (VS Code's StatusbarEntryKind, with the remote kind and without the offline one).
const (
	// StatusStandard is an item in the colors of the bar.
	StatusStandard StatusKind = iota
	// StatusWarning is an item on statusBarItem.warningBackground.
	StatusWarning
	// StatusError is an item on statusBarItem.errorBackground.
	StatusError
	// StatusProminent is an item on statusBarItem.prominentBackground.
	StatusProminent
	// StatusRemote is an item on statusBarItem.remoteBackground, VS Code's remote indicator (step 17e.4: the Sharing item while the
	// workspace is hosted).
	StatusRemote
)

// StatusEntry is what an item of the status bar shows and does (VS Code's IStatusbarEntry).
type StatusEntry struct {
	// Name is the name of the item for the user (the list of items one can hide), such as "Editor Selection".
	Name string
	// Text is the text of the item. A codicon is written $(name), such as $(error); $(name~spin) is one that
	// spins (drawn still), and \$(name) is the text itself. An item with no text is not shown.
	Text string
	// AriaLabel is what a screen reader says; the text when empty.
	AriaLabel string
	// Tooltip is the text of the hover over the item. The key of the command follows it, between brackets.
	Tooltip string
	// Command is the id of the command a click runs, with Args as its argument; an item without one is not
	// clickable.
	Command string
	Args    any
	// Kind is the look of the item.
	Kind StatusKind
	// ShowProgress puts a loading icon before the text.
	ShowProgress bool
}

// equal tells whether two entries are the same.
func (e StatusEntry) equal(o StatusEntry) bool {
	return e.Name == o.Name && e.Text == o.Text && e.AriaLabel == o.AriaLabel && e.Tooltip == o.Tooltip &&
		e.Command == o.Command && e.Kind == o.Kind && e.ShowProgress == o.ShowProgress && reflect.DeepEqual(e.Args, o.Args)
}

// StatusItem is an item of the bar as the widget shows it.
type StatusItem struct {
	// ID identifies the item.
	ID        string
	Entry     StatusEntry
	Alignment StatusAlignment
	// Priority orders the items of a side: the higher the priority, the closer to the left end of the bar,
	// on both sides.
	Priority float64
	// Visible is false for an item that is hidden; the bar leaves it out.
	Visible bool
}

type statusEntry struct {
	StatusItem
	// seq is the order the items were added in, which orders those of equal priority.
	seq int
}

// StatusBar is the registry of the items of the status bar (SPEC §10.1): the pieces of the window and the
// steps to come add their items to it and update them, and the status bar widget shows them. It is safe for
// concurrent use; its listeners are called on the goroutine that changed it, with no lock held.
//
// Ported from VS Code: src/vs/workbench/browser/parts/statusbar/statusbarModel.ts (StatusbarViewModel)
// and statusbarPart.ts (addEntry)
type StatusBar struct {
	mu      sync.Mutex
	entries []*statusEntry
	seq     int

	listeners map[int]func()
	next      int
}

// NewStatusBar makes a status bar with no items.
func NewStatusBar() *StatusBar {
	return &StatusBar{listeners: map[int]func(){}}
}

// StatusAccessor changes or removes an item that was added to a StatusBar.
type StatusAccessor struct {
	bar   *StatusBar
	entry *statusEntry
}

// Add puts an item in the bar and returns the accessor to update or remove it. Items of the same side with the
// same priority stay in the order they were added in.
func (b *StatusBar) Add(id string, alignment StatusAlignment, priority float64, entry StatusEntry) *StatusAccessor {
	b.mu.Lock()
	b.seq++
	e := &statusEntry{StatusItem: StatusItem{ID: id, Entry: entry, Alignment: alignment, Priority: priority, Visible: true}, seq: b.seq}
	b.entries = append(b.entries, e)
	b.mu.Unlock()
	b.changed()
	return &StatusAccessor{bar: b, entry: e}
}

// Update changes what the item shows and does. Nothing is told when it is the same entry. It does nothing for an
// item that was disposed.
func (a *StatusAccessor) Update(entry StatusEntry) {
	a.bar.mu.Lock()
	if !a.bar.has(a.entry) || a.entry.Entry.equal(entry) {
		a.bar.mu.Unlock()
		return
	}
	a.entry.Entry = entry
	a.bar.mu.Unlock()
	a.bar.changed()
}

// SetVisible shows or hides the item without removing it.
func (a *StatusAccessor) SetVisible(visible bool) {
	a.bar.mu.Lock()
	if !a.bar.has(a.entry) || a.entry.Visible == visible {
		a.bar.mu.Unlock()
		return
	}
	a.entry.Visible = visible
	a.bar.mu.Unlock()
	a.bar.changed()
}

// Dispose removes the item from the bar. A second call does nothing.
func (a *StatusAccessor) Dispose() {
	a.bar.mu.Lock()
	for i, e := range a.bar.entries {
		if e == a.entry {
			a.bar.entries = append(a.bar.entries[:i], a.bar.entries[i+1:]...)
			a.bar.mu.Unlock()
			a.bar.changed()
			return
		}
	}
	a.bar.mu.Unlock()
}

// has tells whether an entry is still in the bar; b.mu is held.
func (b *StatusBar) has(e *statusEntry) bool {
	for _, o := range b.entries {
		if o == e {
			return true
		}
	}
	return false
}

// Items are the visible items of a side in the order the bar shows them, from the left: the higher
// priorities first, and the earlier added first among equal ones. On the right side the item with the lowest
// priority is the one at the right end, so the notifications bell (negative infinity) ends the bar.
func (b *StatusBar) Items(alignment StatusAlignment) []StatusItem {
	b.mu.Lock()
	var list []*statusEntry
	for _, e := range b.entries {
		if e.Alignment == alignment && e.Visible {
			list = append(list, e)
		}
	}
	b.mu.Unlock()
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Priority != list[j].Priority {
			return list[i].Priority > list[j].Priority
		}
		return list[i].seq < list[j].seq
	})
	out := make([]StatusItem, len(list))
	for i, e := range list {
		out[i] = e.StatusItem
	}
	return out
}

// Get finds an item by its id (the first, when several have it).
func (b *StatusBar) Get(id string) (StatusItem, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.entries {
		if e.ID == id {
			return e.StatusItem, true
		}
	}
	return StatusItem{}, false
}

// OnChange registers a function that is called whenever an item is added, changed or removed, and returns the
// function that removes it.
func (b *StatusBar) OnChange(fn func()) (remove func()) {
	b.mu.Lock()
	id := b.next
	b.next++
	b.listeners[id] = fn
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		delete(b.listeners, id)
		b.mu.Unlock()
	}
}

func (b *StatusBar) changed() {
	b.mu.Lock()
	ids := make([]int, 0, len(b.listeners))
	for id := range b.listeners {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	fns := make([]func(), 0, len(ids))
	for _, id := range ids {
		fns = append(fns, b.listeners[id])
	}
	b.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// StatusTextRun is a piece of the text of an item: a codicon or text.
type StatusTextRun struct {
	// Icon is the name of the codicon ("error"), or empty for a run of text.
	Icon string
	// Spin is true for $(name~spin).
	Spin bool
	// Text is the text of a run that is not an icon.
	Text string
}

// iconRe is the syntax of an icon in a text: $(name) or $(name~modifier), with a backslash before the dollar
// sign to write it as text.
var iconRe = regexp.MustCompile(`(\\)?\$\(([A-Za-z0-9\-]+(?:~[A-Za-z0-9\-]+)?)\)`)

// ParseStatusText splits the text of an item into text and icons. An escaped icon, \$(name), is text.
//
// Ported from VS Code: src/vs/base/browser/ui/iconLabel/iconLabels.ts (renderLabelWithIcons) and
// src/vs/base/common/iconLabels.ts
func ParseStatusText(text string) []StatusTextRun {
	var runs []StatusTextRun
	var pending strings.Builder
	flush := func() {
		if pending.Len() > 0 {
			runs = append(runs, StatusTextRun{Text: pending.String()})
			pending.Reset()
		}
	}
	at := 0
	for _, m := range iconRe.FindAllStringSubmatchIndex(text, -1) {
		pending.WriteString(text[at:m[0]])
		at = m[1]
		name := text[m[4]:m[5]]
		if m[2] >= 0 { // escaped: the icon is text
			pending.WriteString("$(" + name + ")")
			continue
		}
		flush()
		icon, modifier, _ := strings.Cut(name, "~")
		runs = append(runs, StatusTextRun{Icon: icon, Spin: modifier == "spin"})
	}
	pending.WriteString(text[at:])
	flush()
	return runs
}

// negInf is negative infinity, the priority that puts an item last.
var negInf = math.Inf(-1)
