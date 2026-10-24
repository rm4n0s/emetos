package quickinput

import (
	"reflect"
	"testing"
)

func newHelpController(t *testing.T) (*Controller, *Registry) {
	t.Helper()
	reg := NewRegistry()
	keys := map[string]string{"workbench.action.showCommands": "Ctrl+Shift+P", "workbench.action.gotoLine": "Ctrl+G"}
	reg.MustRegister(
		NewHelpProvider(reg, func(id string) string { return keys[id] }),
		&ListProvider{ProviderInfo: ProviderInfo{Prefix: ">", Help: []HelpEntry{{Description: "Show and Run Commands", Command: "workbench.action.showCommands"}}}},
		&ListProvider{ProviderInfo: ProviderInfo{Prefix: "view ", Help: []HelpEntry{{Description: "Open View", Command: "workbench.action.openView"}}}},
		NewGoToLineProvider(":", nil),
		NewGoToLineProvider("::", nil),
	)
	return NewController(ControllerOptions{Registry: reg}), reg
}

func TestHelpListsTheProviders(t *testing.T) {
	c, _ := newHelpController(t)
	c.Show("?", ShowOptions{})
	rows := c.Picker().Rows()
	var got [][3]string
	for _, r := range rows {
		got = append(got, [3]string{r.Item.Label, r.Item.Description, r.Item.Keybinding})
	}
	// ordered by prefix; the help itself is not listed
	want := [][3]string{
		{":", "Go to Line/Column", "Ctrl+G"},
		{"::", "Go to Offset", ""},
		{">", "Show and Run Commands", "Ctrl+Shift+P"},
		{"view ", "Open View", ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows %v\nwant %v", got, want)
	}
	if c.Picker().Placeholder() != "Type '?' to get help on the actions you can take from here." {
		t.Errorf("placeholder %q", c.Picker().Placeholder())
	}
}

func TestHelpFiltersAndChoosingOpensTheMode(t *testing.T) {
	t.Run("choosing a row opens its mode keeping the value", func(t *testing.T) {
		c, _ := newHelpController(t)
		c.Show("?", ShowOptions{})
		c.Picker().Move(2) // ">"
		c.Accept(KeyMods{})
		if !c.Visible() || c.Picker().Value() != ">" || c.Provider().Info().Prefix != ">" {
			t.Fatalf("visible %v value %q", c.Visible(), c.Picker().Value())
		}
		// with the caret at the end, no selection: the value is a prefix to type after
		if start, end, ok := c.Picker().ValueSelection(); !ok || start != 1 || end != 1 {
			t.Errorf("selection %d-%d %v", start, end, ok)
		}
	})
	t.Run("the view mode keeps its space", func(t *testing.T) {
		c, _ := newHelpController(t)
		c.Show("?", ShowOptions{})
		c.Picker().Last()
		c.Accept(KeyMods{})
		if c.Picker().Value() != "view " {
			t.Errorf("value %q", c.Picker().Value())
		}
	})
	t.Run("typing the prefix of a mode opens it", func(t *testing.T) {
		c, _ := newHelpController(t)
		c.Show("?", ShowOptions{})
		c.SetValue("?view ")
		if c.Picker().Value() != "view " || c.Provider().Info().Prefix != "view " {
			t.Errorf("value %q provider %v", c.Picker().Value(), c.Provider())
		}
	})
	t.Run("other text filters the list", func(t *testing.T) {
		c, _ := newHelpController(t)
		c.Show("?", ShowOptions{})
		c.SetValue("?x")
		if c.Provider().Info().Prefix != "?" {
			t.Fatalf("provider %q", c.Provider().Info().Prefix)
		}
		// the filter looks at the labels, which are the prefixes: none has an x
		if len(c.Picker().Rows()) != 0 {
			t.Errorf("rows %v", labelsOf(c.Picker().Rows()))
		}
		c.SetValue("?vi")
		if got := labelsOf(c.Picker().Rows()); !reflect.DeepEqual(got, []string{"view "}) {
			t.Errorf("rows %v", got)
		}
	})
}
