package quickinput

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// editorsModel stands for the editor groups: the editors of each group in tab order and in the order they
// were used, which entries turns into what the provider lists.
type editorsModel struct {
	groups []int
	// tabs and mru are the names of the editors of each group.
	tabs, mru map[int][]string
	active    int
	dirty     map[string]bool
	preview   map[string]bool
	opened    []string
	// keep makes Close refuse.
	keep bool
}

func newEditorsModel() *editorsModel {
	return &editorsModel{tabs: map[int][]string{}, mru: map[int][]string{}, dirty: map[string]bool{}, preview: map[string]bool{}}
}

// group adds a group with editors; the first of mru is the active one.
func (m *editorsModel) group(id int, tabs []string, mru ...string) *editorsModel {
	m.groups = append(m.groups, id)
	m.tabs[id] = tabs
	if len(mru) == 0 {
		mru = tabs
	}
	m.mru[id] = mru
	if m.active == 0 {
		m.active = id
	}
	return m
}

func (m *editorsModel) entry(group int, name string) EditorEntry {
	return EditorEntry{
		ID: fmt.Sprintf("%d:%s", group, name), GroupID: group, Label: name, Description: "src", Path: "/w/src/" + name,
		Dirty: m.dirty[name], Preview: m.preview[name],
		Open: func() { m.opened = append(m.opened, fmt.Sprintf("%d:%s", group, name)) },
		Close: func() bool {
			if m.keep {
				return false
			}
			m.tabs[group] = removeName(m.tabs[group], name)
			m.mru[group] = removeName(m.mru[group], name)
			return true
		},
	}
}

func removeName(list []string, name string) []string {
	var out []string
	for _, n := range list {
		if n != name {
			out = append(out, n)
		}
	}
	return out
}

func (m *editorsModel) options() EditorsOptions {
	return EditorsOptions{
		Entries: func(mode EditorsMode) []EditorEntry {
			var out []EditorEntry
			switch mode {
			case EditorsByAppearance:
				for _, g := range m.groups {
					for _, n := range m.tabs[g] {
						out = append(out, m.entry(g, n))
					}
				}
			case EditorsByRecentlyUsed:
				// the active group first, then the others
				order := []int{m.active}
				for _, g := range m.groups {
					if g != m.active {
						order = append(order, g)
					}
				}
				for _, g := range order {
					for _, n := range m.mru[g] {
						out = append(out, m.entry(g, n))
					}
				}
			case EditorsInActiveGroup:
				for _, n := range m.mru[m.active] {
					out = append(out, m.entry(m.active, n))
				}
			}
			return out
		},
		Groups:     func() []int { return m.groups },
		GroupLabel: func(id int) string { return fmt.Sprintf("Group %d", id) },
	}
}

// editorsRig is the quick input with the three editor modes registered.
func editorsRig(t *testing.T, m *editorsModel, colors kit.Colors) *rig {
	t.Helper()
	r := newRig(t, ui.Size{Width: 800, Height: 500}, colors)
	r.registry.MustRegister(
		NewEditorsProvider(EditorsByAppearance, m.options()),
		NewEditorsProvider(EditorsByRecentlyUsed, m.options()),
		NewEditorsProvider(EditorsInActiveGroup, m.options()),
	)
	return r
}

// withGroups lists the labels of the rows with the heading of their group before each first row of a group.
func (r *rig) withGroups() string {
	var parts []string
	for _, row := range r.ctl.Picker().Rows() {
		if row.Separator != "" {
			parts = append(parts, "["+row.Separator+"]")
		}
		parts = append(parts, row.Item.Label)
	}
	return strings.Join(parts, " ")
}

// active is the label of the active row.
func (r *rig) active() string {
	row, ok := r.ctl.Picker().Active()
	if !ok {
		return ""
	}
	return row.Item.Label
}

var ctrlTab = &QuickNavigate{Chords: []Modifiers{{Ctrl: true}}}

func TestEditorsModesListAndGroup(t *testing.T) {
	m := newEditorsModel().
		group(1, []string{"a.go", "b.go", "c.go"}, "b.go", "c.go", "a.go").
		group(2, []string{"d.go", "e.go"}, "e.go", "d.go")
	for _, tc := range []struct {
		name, value, want string
	}{
		{"by appearance", "edt ", "[Group 1] a.go b.go c.go [Group 2] d.go e.go"},
		{"by recently used", "edt mru ", "[Group 1] b.go c.go a.go [Group 2] e.go d.go"},
		{"the active group by recently used", "edt active ", "[Group 1] b.go c.go a.go"},
		{"a filter keeps the groups and scores the editors", "edt .go", "[Group 1] a.go b.go c.go [Group 2] d.go e.go"},
		{"a filter drops what does not match", "edt d", "[Group 2] d.go"},
		{"no match", "edt zzz", "No matching editors"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := editorsRig(t, m, kit.DarkModern())
			r.show(tc.value, ShowOptions{})
			if got := r.withGroups(); got != tc.want {
				t.Errorf("rows\n got %s\nwant %s", got, tc.want)
			}
			if r.scopeKey("inEditorsPicker") != true {
				t.Errorf("inEditorsPicker = %v", r.scopeKey("inEditorsPicker"))
			}
		})
	}
	t.Run("one group has no heading", func(t *testing.T) {
		one := newEditorsModel().group(1, []string{"a.go", "b.go"})
		r := editorsRig(t, one, kit.DarkModern())
		r.show("edt ", ShowOptions{})
		if got := r.withGroups(); got != "a.go b.go" {
			t.Errorf("rows %s", got)
		}
	})
}

func TestEditorsRowsShowTheStateOfTheEditor(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go", "b.go", "c.go"})
	m.dirty["b.go"] = true
	m.preview["c.go"] = true
	r := editorsRig(t, m, kit.DarkModern())
	r.show("edt ", ShowOptions{})
	rows := r.ctl.Picker().Rows()
	if rows[0].Item.Description != "src" || rows[0].Item.Italic || rows[0].Item.Buttons[0].AlwaysVisible {
		t.Errorf("a.go: %+v", rows[0].Item)
	}
	if b := rows[1].Item.Buttons[0]; !b.AlwaysVisible || b.Icon == b.HoverIcon || b.HoverIcon == 0 {
		t.Errorf("a dirty editor has a dot that turns into a cross: %+v", b)
	}
	if !rows[2].Item.Italic {
		t.Errorf("a preview editor is in italics")
	}
	// the dot of a dirty editor is on screen without the pointer over it
	if got := r.ts.Count(gluttertest.ByType[*kit.IconButton]()); got != 2 {
		t.Errorf("%d buttons on screen, want the dot of b.go and the cross of the active row", got)
	}
}

func TestEditorsFilterHighlightsAndSorts(t *testing.T) {
	m := newEditorsModel().group(1, []string{"xmain.go", "main.go", "domain.go"})
	r := editorsRig(t, m, kit.DarkModern())
	r.show("edt main", ShowOptions{})
	if got := r.labels(); !reflect.DeepEqual(got, []string{"main.go", "xmain.go", "domain.go"}) {
		t.Errorf("rows %v", got)
	}
	if len(r.ctl.Picker().Rows()[0].Label) == 0 {
		t.Errorf("no highlight on the label")
	}
}

func TestEditorsChoosingOpensTheEditor(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go", "b.go"}).group(2, []string{"c.go"})
	r := editorsRig(t, m, kit.DarkModern())
	r.show("edt ", ShowOptions{})
	r.press(services.LogicalKeyboardKeyArrowDown)
	r.press(services.LogicalKeyboardKeyArrowDown)
	r.press(services.LogicalKeyboardKeyEnter)
	if r.ctl.Visible() || !reflect.DeepEqual(m.opened, []string{"2:c.go"}) {
		t.Errorf("visible %v opened %v", r.ctl.Visible(), m.opened)
	}
}

func TestEditorsCloseButtonRemovesTheItemAndKeepsThePickerOpen(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go", "b.go", "c.go"})
	r := editorsRig(t, m, kit.DarkModern())
	r.show("edt ", ShowOptions{})
	r.ctl.Trigger(r.ctl.Picker().Rows()[1], 0, KeyMods{})
	r.settle()
	if !r.ctl.Visible() || !reflect.DeepEqual(r.labels(), []string{"a.go", "c.go"}) {
		t.Errorf("visible %v rows %v", r.ctl.Visible(), r.labels())
	}
	if !reflect.DeepEqual(m.tabs[1], []string{"a.go", "c.go"}) {
		t.Errorf("tabs %v", m.tabs[1])
	}
	// an editor that stays (the user cancelled the question about its changes) stays in the list
	m.keep = true
	r.ctl.Trigger(r.ctl.Picker().Rows()[0], 0, KeyMods{})
	if !reflect.DeepEqual(r.labels(), []string{"a.go", "c.go"}) {
		t.Errorf("rows %v", r.labels())
	}
}

func TestEditorsCloseButtonByClick(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go", "b.go"})
	r := editorsRig(t, m, kit.DarkModern())
	r.show("edt ", ShowOptions{})
	r.ts.Tap(gluttertest.ByType[*kit.IconButton]())
	r.settle()
	if !r.ctl.Visible() || !reflect.DeepEqual(r.labels(), []string{"b.go"}) {
		t.Errorf("visible %v rows %v", r.ctl.Visible(), r.labels())
	}
}

func TestSwitcherStartsAtTheSecondEditor(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go", "b.go", "c.go"}, "c.go", "a.go", "b.go")
	r := editorsRig(t, m, kit.DarkModern())
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	r.show("edt active ", ShowOptions{QuickNavigate: ctrlTab})
	if got := r.labels(); !reflect.DeepEqual(got, []string{"c.go", "a.go", "b.go"}) {
		t.Fatalf("rows %v", got)
	}
	if r.active() != "a.go" {
		t.Errorf("active %s, want the editor used before the active one", r.active())
	}
	if !r.ctl.Picker().HideInput {
		t.Errorf("the input is not hidden")
	}
	// the input still has the focus, so the keys come to the picker
	if r.primary().DebugLabel() != "InputBox" {
		t.Errorf("focus %v", r.primary().DebugLabel())
	}
}

func TestSwitcherWithOneEditorActivatesIt(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go"})
	r := editorsRig(t, m, kit.DarkModern())
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	r.show("edt active ", ShowOptions{QuickNavigate: ctrlTab})
	if r.active() != "a.go" {
		t.Errorf("active %q", r.active())
	}
}

func TestSwitcherFromTheLeastRecentlyUsedEditor(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go", "b.go", "c.go"}, "c.go", "a.go", "b.go")
	r := editorsRig(t, m, kit.DarkModern())
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyShift)
	r.show("edt active ", ShowOptions{QuickNavigate: &QuickNavigate{Chords: []Modifiers{{Ctrl: true, Shift: true}}}, Activation: ItemLast})
	if r.active() != "b.go" {
		t.Errorf("active %s, want the editor used longest ago", r.active())
	}
}

func TestSwitcherMovesWithTheNavigateCommands(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go", "b.go", "c.go", "d.go"})
	r := editorsRig(t, m, kit.DarkModern())
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	r.show("edt active ", ShowOptions{QuickNavigate: ctrlTab})
	steps := []struct {
		next bool
		want string
	}{{true, "c.go"}, {true, "d.go"}, {true, "a.go"}, {false, "d.go"}, {false, "c.go"}}
	for i, s := range steps {
		r.ctl.Navigate(s.next, ctrlTab)
		r.settle()
		if r.active() != s.want {
			t.Errorf("step %d: active %s, want %s", i, r.active(), s.want)
		}
	}
	if !r.ctl.QuickNavigating() {
		t.Errorf("the switcher stopped waiting for the release")
	}
}

func TestSwitcherPicksWhenControlIsReleased(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go", "b.go", "c.go"}, "c.go", "a.go", "b.go")
	r := editorsRig(t, m, kit.DarkModern())
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	r.show("edt active ", ShowOptions{QuickNavigate: ctrlTab})
	r.ctl.Navigate(true, ctrlTab) // Ctrl+Tab again
	r.settle()
	if !r.ctl.Visible() || len(m.opened) != 0 {
		t.Fatalf("visible %v opened %v before the release", r.ctl.Visible(), m.opened)
	}
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	r.settle()
	if r.ctl.Visible() || !reflect.DeepEqual(m.opened, []string{"1:b.go"}) {
		t.Errorf("visible %v opened %v", r.ctl.Visible(), m.opened)
	}
	if r.primary() != r.editor {
		t.Errorf("the focus did not go back to the editor: %v", r.primary())
	}
}

func TestSwitcherReleaseOfShiftWaitsForControl(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go", "b.go", "c.go"})
	r := editorsRig(t, m, kit.DarkModern())
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyShift)
	qn := &QuickNavigate{Chords: []Modifiers{{Ctrl: true, Shift: true}}}
	r.show("edt active ", ShowOptions{QuickNavigate: qn, Activation: ItemLast})
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyShift)
	r.settle()
	if !r.ctl.Visible() || len(m.opened) != 0 {
		t.Fatalf("Shift let go while Ctrl is held: visible %v opened %v", r.ctl.Visible(), m.opened)
	}
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	r.settle()
	if r.ctl.Visible() || !reflect.DeepEqual(m.opened, []string{"1:c.go"}) {
		t.Errorf("visible %v opened %v", r.ctl.Visible(), m.opened)
	}
}

func TestSwitcherPicksOnlyOnce(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go", "b.go"})
	r := editorsRig(t, m, kit.DarkModern())
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	r.show("edt active ", ShowOptions{QuickNavigate: ctrlTab})
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	r.settle()
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	r.settle()
	if !reflect.DeepEqual(m.opened, []string{"1:b.go"}) {
		t.Errorf("opened %v", m.opened)
	}
}

func TestSwitcherWithTheReleaseMissed(t *testing.T) {
	setup := func(t *testing.T) (*rig, *editorsModel) {
		m := newEditorsModel().group(1, []string{"a.go", "b.go", "c.go"})
		r := editorsRig(t, m, kit.DarkModern())
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
		r.show("edt active ", ShowOptions{QuickNavigate: ctrlTab})
		return r, m
	}
	t.Run("a key pressed while Ctrl is not held stops the wait, and Enter picks", func(t *testing.T) {
		m := newEditorsModel().group(1, []string{"a.go", "b.go", "c.go"})
		r := editorsRig(t, m, kit.DarkModern())
		// Ctrl was let go before the switcher showed (the release went to another window): no key holds it
		r.show("edt active ", ShowOptions{QuickNavigate: ctrlTab})
		if !r.ctl.QuickNavigating() {
			t.Fatalf("the switcher does not wait")
		}
		r.press(services.LogicalKeyboardKeyArrowDown)
		if r.ctl.QuickNavigating() {
			t.Errorf("the switcher still waits for a release that was missed")
		}
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
		r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
		r.settle()
		if !r.ctl.Visible() || len(m.opened) != 0 {
			t.Fatalf("a later Ctrl picked: visible %v opened %v", r.ctl.Visible(), m.opened)
		}
		r.press(services.LogicalKeyboardKeyEnter)
		if r.ctl.Visible() || !reflect.DeepEqual(m.opened, []string{"1:c.go"}) {
			t.Errorf("visible %v opened %v", r.ctl.Visible(), m.opened)
		}
	})
	t.Run("the window losing the keyboard closes the switcher", func(t *testing.T) {
		r, m := setup(t)
		r.ts.SetViewFocus(ui.ImplicitViewID, false)
		r.settle()
		if r.ctl.Visible() || r.ctl.QuickNavigating() {
			t.Errorf("visible %v waiting %v", r.ctl.Visible(), r.ctl.QuickNavigating())
		}
		if len(m.opened) != 0 {
			t.Errorf("opened %v", m.opened)
		}
	})
}

func TestSwitcherEscapeClosesAndClickPicks(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go", "b.go", "c.go"})
	t.Run("escape", func(t *testing.T) {
		r := editorsRig(t, m, kit.DarkModern())
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
		r.show("edt active ", ShowOptions{QuickNavigate: ctrlTab})
		r.press(services.LogicalKeyboardKeyEscape)
		if r.ctl.Visible() || r.ctl.QuickNavigating() {
			t.Errorf("visible %v waiting %v", r.ctl.Visible(), r.ctl.QuickNavigating())
		}
		// the release afterwards picks nothing
		r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
		r.settle()
		if len(m.opened) != 0 {
			t.Errorf("opened %v", m.opened)
		}
	})
	t.Run("a click picks the row it is on", func(t *testing.T) {
		m.opened = nil
		r := editorsRig(t, m, kit.DarkModern())
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
		r.show("edt active ", ShowOptions{QuickNavigate: ctrlTab})
		r.ts.Tap(gluttertest.ByKey(RowKey(2)))
		r.settle()
		if r.ctl.Visible() || !reflect.DeepEqual(m.opened, []string{"1:c.go"}) {
			t.Errorf("visible %v opened %v", r.ctl.Visible(), m.opened)
		}
	})
}

func TestSwitcherIgnoresTheModifiersOfItsOwnKeys(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go", "b.go"})
	r := editorsRig(t, m, kit.DarkModern())
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	r.show("edt active ", ShowOptions{QuickNavigate: ctrlTab})
	// Enter with Ctrl held (the key that opened the switcher) is a plain pick, not "open to the side"
	r.press(services.LogicalKeyboardKeyEnter)
	if got := m.opened; !reflect.DeepEqual(got, []string{"1:b.go"}) {
		t.Errorf("opened %v", got)
	}
}

func TestEditorsAreInTheHelp(t *testing.T) {
	m := newEditorsModel().group(1, []string{"a.go"})
	r := editorsRig(t, m, kit.DarkModern())
	r.show("?", ShowOptions{})
	got := map[string]string{}
	for _, row := range r.ctl.Picker().Rows() {
		if strings.HasPrefix(row.Item.Label, "edt") {
			got[row.Item.Label] = row.Item.Description
		}
	}
	want := map[string]string{
		"edt ":        "Show All Opened Editors By Appearance",
		"edt mru ":    "Show All Opened Editors By Most Recently Used",
		"edt active ": "Show Editors in Active Group by Most Recently Used",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("help lines %v", got)
	}
}
