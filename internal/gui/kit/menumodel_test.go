package kit

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

func TestMnemonicKey(t *testing.T) {
	cases := []struct {
		label string
		want  string
	}{
		{"&File", "f"},
		{"E&xit", "x"},
		{"Word &Wrap", "w"},
		{"&Файл", "ф"},
		{"&Éditer", "é"},
		{"&文件", "文"},
		{"文件(&F)", "f"},
		{"(&F)", "f"},
		{"Save &&All", ""},
		{"&&Save &All", "a"},
		{"Fish && &Chips", "c"},
		{"No mnemonic", ""},
		{"", ""},
		{"Trailing &", ""},
		{"A& B", ""},
		{"&", ""},
		{"&&", ""},
		{"&Two &Marks", "t"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			if got := MnemonicKey(tc.label); got != tc.want {
				t.Errorf("MnemonicKey(%q) = %q, want %q", tc.label, got, tc.want)
			}
		})
	}
}

func TestCleanMnemonic(t *testing.T) {
	cases := []struct{ label, want string }{
		{"&File", "File"},
		{"E&xit", "Exit"},
		{"文件(&F)", "文件"},
		{"  &Open  ", "Open"},
		{"Save &&All", "Save &&All"}, // the doubled ampersand stays: MenuLabelText unescapes it
		{"No marker", "No marker"},
		{"A &Menu &Two", "A Menu &Two"},
	}
	for _, tc := range cases {
		if got := CleanMnemonic(tc.label); got != tc.want {
			t.Errorf("CleanMnemonic(%q) = %q, want %q", tc.label, got, tc.want)
		}
	}
}

func TestMenuLabelText(t *testing.T) {
	cases := []struct{ label, want string }{
		{"&File", "File"},
		{"E&xit", "Exit"},
		{"文件(&F)", "文件"},
		{"Save &&All", "Save &All"},
		{"R&&D &Mode", "R&D Mode"},
		{" Padded ", "Padded"},
		{"&Файл", "Файл"},
	}
	for _, tc := range cases {
		if got := MenuLabelText(tc.label); got != tc.want {
			t.Errorf("MenuLabelText(%q) = %q, want %q", tc.label, got, tc.want)
		}
	}
}

func TestParseMenuLabel(t *testing.T) {
	cases := []struct {
		name      string
		label     string
		mnemonics bool
		want      MnemonicLabel
	}{
		{"a mnemonic at the start", "&File", true, MnemonicLabel{"", "F", "ile"}},
		{"in the middle", "E&xit", true, MnemonicLabel{"E", "x", "it"}},
		{"after other words", "Word &Wrap", true, MnemonicLabel{"Word ", "W", "rap"}},
		{"off", "&File", false, MnemonicLabel{Before: "File"}},
		{"Cyrillic", "&Файл", true, MnemonicLabel{"", "Ф", "айл"}},
		{"an accented letter", "&Éditer", true, MnemonicLabel{"", "É", "diter"}},
		{"Chinese with the letter in parentheses", "文件(&F)", true, MnemonicLabel{"文件(", "F", ")"}},
		{"Chinese with the mnemonics off", "文件(&F)", false, MnemonicLabel{Before: "文件"}},
		{"a literal ampersand", "Save &&All", true, MnemonicLabel{Before: "Save &All"}},
		{"a literal ampersand and a mnemonic", "Save &&All &Files", true, MnemonicLabel{"Save &All ", "F", "iles"}},
		{"a literal ampersand and a mnemonic, off", "Save &&All &Files", false, MnemonicLabel{Before: "Save &All Files"}},
		{"no mnemonic", "Plain", true, MnemonicLabel{Before: "Plain"}},
		{"white space around", "  &Open  ", true, MnemonicLabel{"", "O", "pen"}},
		{"an ampersand at the end", "Trailing &", true, MnemonicLabel{Before: "Trailing &"}},
		{"an ampersand before a space", "A& B", true, MnemonicLabel{Before: "A& B"}},
		{"the first of two marks", "&One &Two", true, MnemonicLabel{"", "O", "ne &Two"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseMenuLabel(tc.label, tc.mnemonics)
			if got != tc.want {
				t.Errorf("ParseMenuLabel(%q, %v) = %+v, want %+v", tc.label, tc.mnemonics, got, tc.want)
			}
		})
	}
}

func TestPrepareMenuItems(t *testing.T) {
	a, b, sep := MenuItem{ID: "a"}, MenuItem{ID: "b"}, SeparatorItem()
	ids := func(items []MenuItem) []string {
		out := []string{}
		for _, it := range items {
			if it.Separator {
				out = append(out, "-")
			} else {
				out = append(out, it.ID)
			}
		}
		return out
	}
	cases := []struct {
		name string
		in   []MenuItem
		want []string
	}{
		{"nothing", nil, []string{}},
		{"no separators", []MenuItem{a, b}, []string{"a", "b"}},
		{"a separator between", []MenuItem{a, sep, b}, []string{"a", "-", "b"}},
		{"leading", []MenuItem{sep, a}, []string{"a"}},
		{"trailing", []MenuItem{a, sep}, []string{"a"}},
		{"repeated", []MenuItem{a, sep, sep, sep, b}, []string{"a", "-", "b"}},
		{"only separators", []MenuItem{sep, sep, sep}, []string{}},
		{"all of it", []MenuItem{sep, sep, a, sep, sep, b, sep}, []string{"a", "-", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := append([]MenuItem(nil), tc.in...)
			if got := ids(PrepareMenuItems(tc.in)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
			if len(in) != len(tc.in) {
				t.Error("the list was changed")
			}
		})
	}
}

func TestMenuModelNavigation(t *testing.T) {
	items := []MenuItem{{ID: "a"}, SeparatorItem(), {ID: "b", Disabled: true}, {ID: "c"}, SubmenuItem("d", nil), {ID: "e", Disabled: true}}
	// focusable: 0, 3, 4 (a submenu is never disabled)
	cases := []struct {
		name string
		do   func(m *menuModel)
		want int
	}{
		{"Down from nothing", func(m *menuModel) { m.Move(1) }, 0},
		{"Up from nothing", func(m *menuModel) { m.Move(-1) }, 4},
		{"Down skips the separator and the disabled item", func(m *menuModel) { m.Move(1); m.Move(1) }, 3},
		{"Down to the submenu", func(m *menuModel) { m.Move(1); m.Move(1); m.Move(1) }, 4},
		{"Down wraps around the disabled last item", func(m *menuModel) { m.Move(1); m.Move(1); m.Move(1); m.Move(1) }, 0},
		{"Up wraps", func(m *menuModel) { m.Move(1); m.Move(-1) }, 4},
		{"First", func(m *menuModel) { m.Last(); m.First() }, 0},
		{"Last skips the disabled item", func(m *menuModel) { m.Last() }, 4},
		{"Focus a disabled item", func(m *menuModel) { m.Focus(2) }, -1},
		{"Focus a separator", func(m *menuModel) { m.Focus(1) }, -1},
		{"Focus an enabled item", func(m *menuModel) { m.Focus(3) }, 3},
		{"Focus out of range", func(m *menuModel) { m.Focus(9) }, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMenuModel(items)
			tc.do(m)
			if m.focused != tc.want {
				t.Errorf("focused %d, want %d", m.focused, tc.want)
			}
		})
	}
	t.Run("nothing can take the focus", func(t *testing.T) {
		m := newMenuModel([]MenuItem{{ID: "x", Disabled: true}, SeparatorItem()})
		if m.Move(1) || m.Move(-1) || m.First() || m.Last() || m.focused != -1 {
			t.Errorf("the focus moved to %d", m.focused)
		}
	})
	t.Run("an empty menu", func(t *testing.T) {
		m := newMenuModel(nil)
		if m.Move(1) || m.focused != -1 {
			t.Error("the focus moved")
		}
	})
	t.Run("one item that is focused already", func(t *testing.T) {
		m := newMenuModel([]MenuItem{{ID: "x"}})
		m.Move(1)
		if m.Move(1) || m.focused != 0 {
			t.Errorf("focused %d", m.focused)
		}
	})
}

func TestMenuModelMnemonic(t *testing.T) {
	m := newMenuModel([]MenuItem{
		{Label: "&Alpha"}, {Label: "&Apple"}, {Label: "&Beta"}, {Label: "&Avocado", Disabled: true}, SeparatorItem(), {Label: "C&herry"},
	})
	cases := []struct {
		key        string
		wantIndex  int
		wantUnique bool
	}{
		{"b", 2, true}, {"a", 0, false}, {"a", 1, false}, {"a", 0, false}, {"h", 5, true}, {"z", -1, false}, {"", -1, false},
	}
	for _, tc := range cases {
		i, unique := m.Mnemonic(tc.key)
		if i != tc.wantIndex || unique != tc.wantUnique {
			t.Errorf("Mnemonic(%q) = %d, %v; want %d, %v", tc.key, i, unique, tc.wantIndex, tc.wantUnique)
		}
	}
}

// The menu bar titles of the tests of VS Code's menubar.test.ts: the mnemonic is the letter in the
// label, wherever it is.
func TestMenuBarTitlesShowTheirMnemonic(t *testing.T) {
	cases := []struct {
		label, title, mnemonic string
	}{
		{"&File", "File", "F"},
		{"&Файл", "Файл", "Ф"},
		{"文件(&F)", "文件(F)", "F"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			ctrl := NewMenuBarController()
			ts := pumpSized(t, ui.Size{Width: 300, Height: 100}, NewMenuBar(
				[]MenuBarMenu{{Label: tc.label, Items: []MenuItem{{ID: "x", Label: "X"}}}},
				MenuBarOpts{AlwaysOnMnemonics: true, Controller: ctrl}))
			txt, ok := ts.Widget(gluttertest.ByText(tc.title)).(*widgets.Text)
			if !ok {
				t.Fatalf("no title %q", tc.title)
			}
			p := ParseMenuLabel(tc.label, true)
			if p.Mnemonic != tc.mnemonic {
				t.Errorf("mnemonic %q, want %q", p.Mnemonic, tc.mnemonic)
			}
			if txt.TextSpan() == nil {
				t.Error("the mnemonic is not set apart from the rest of the title")
			}
		})
	}
}
