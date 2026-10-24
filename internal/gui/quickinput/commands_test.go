package quickinput

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/fuzzy"
)

func TestCommandsHistory(t *testing.T) {
	h := NewCommandsHistory(3)
	for _, id := range []string{"a", "b", "c", "a", "d"} {
		h.Push(id)
	}
	// the oldest ("b") is gone once there are more than 3: a, c, d remain, the last used first
	if got := h.IDs(); !reflect.DeepEqual(got, []string{"d", "a", "c"}) {
		t.Errorf("history %v", got)
	}
	if h.Peek("b") != 0 || h.Peek("d") <= h.Peek("a") || h.Peek("a") <= h.Peek("c") {
		t.Errorf("counters a=%d b=%d c=%d d=%d", h.Peek("a"), h.Peek("b"), h.Peek("c"), h.Peek("d"))
	}
	h.Remove("a")
	if got := h.IDs(); !reflect.DeepEqual(got, []string{"d", "c"}) {
		t.Errorf("after remove %v", got)
	}
	h.SetLimit(1)
	if got := h.IDs(); !reflect.DeepEqual(got, []string{"d"}) {
		t.Errorf("after the limit went down %v", got)
	}
	h.SetLimit(0)
	h.Push("x")
	if len(h.IDs()) != 0 {
		t.Errorf("a history of 0 keeps %v", h.IDs())
	}
	h.SetLimit(5)
	h.Push("y")
	h.Clear()
	if len(h.IDs()) != 0 {
		t.Errorf("clear left %v", h.IDs())
	}
}

// paletteRig is a registry of a few commands for the tests of the palette.
type paletteRig struct {
	commands *command.Registry
	menus    *command.MenuRegistry
	keys     contextkey.MapContext
	history  *CommandsHistory
	ran      []string
	provider *ListProvider
}

func newPaletteRig(t *testing.T) *paletteRig {
	t.Helper()
	r := &paletteRig{commands: command.NewRegistry(), keys: contextkey.MapContext{}, history: NewCommandsHistory(50)}
	r.commands.MustRegister(
		command.Meta{ID: "view.toggleWordWrap", Category: "View", Title: "Toggle Word Wrap"},
		command.Meta{ID: "editor.action.commentLine", Title: "Toggle Line Comment", Precondition: contextkey.Not("editorReadonly")},
		command.Meta{ID: "workbench.action.gotoLine", Title: "Go to Line/Column..."},
		command.Meta{ID: "a.copy", Title: "Copy"},
		command.Meta{ID: "b.copy", Title: "Copy"},
		command.Meta{ID: "dev.reload", Category: "Developer", Title: "Reload Window"},
		command.Meta{ID: "cursorUp"},
		command.Meta{ID: "workbench.action.quickOpenView", Title: "Quick Open View"},
	)
	r.menus = command.NewMenuRegistry(r.commands)
	r.menus.MustAdd(command.MenuItem{Menu: command.CommandPalette, Command: "workbench.action.quickOpenView", When: contextkey.False()})
	r.provider = NewCommandsProvider(CommandsOptions{
		Commands: r.commands, Menus: r.menus, History: r.history,
		Context: func() contextkey.Context { return r.keys },
		Keybinding: func(id string, _ contextkey.Context) string {
			return map[string]string{"workbench.action.gotoLine": "Ctrl+G"}[id]
		},
		Run: func(id string) error { r.ran = append(r.ran, id); return nil },
	})
	return r
}

func (r *paletteRig) labels(filter string) []string {
	var out []string
	for _, it := range r.provider.Picks(filter) {
		if it.Separator {
			out = append(out, "--"+it.Label)
		} else {
			out = append(out, it.Label)
		}
	}
	return out
}

func TestCommandsProviderPicks(t *testing.T) {
	t.Run("lists the commands that have a title, by label", func(t *testing.T) {
		r := newPaletteRig(t)
		want := []string{"Copy", "Copy", "Go to Line/Column...", "Toggle Line Comment", "View: Toggle Word Wrap", "Developer: Reload Window"}
		// Developer commands go last; the command without a title and the one the palette hides are not there
		if got := r.labels(""); !reflect.DeepEqual(got, want) {
			t.Errorf("got %v\nwant %v", got, want)
		}
	})
	t.Run("a command whose precondition fails is left out", func(t *testing.T) {
		r := newPaletteRig(t)
		r.keys["editorReadonly"] = true
		for _, l := range r.labels("") {
			if l == "Toggle Line Comment" {
				t.Errorf("listed a command that is not enabled")
			}
		}
	})
	t.Run("filters by the words of the label and highlights them", func(t *testing.T) {
		r := newPaletteRig(t)
		items := r.provider.Picks("line")
		if len(items) != 2 || items[0].Label != "Go to Line/Column..." || items[1].Label != "Toggle Line Comment" {
			t.Fatalf("items %+v", items)
		}
		if got := items[0].Highlights.Label; !reflect.DeepEqual(got, []fuzzy.Match{{Start: 6, End: 10}}) {
			t.Errorf("highlights %v", got)
		}
		if got := r.labels("gtl"); !reflect.DeepEqual(got, []string{"Go to Line/Column..."}) {
			t.Errorf("the first letters of the words: %v", got)
		}
		if got := r.labels("wrap"); !reflect.DeepEqual(got, []string{"View: Toggle Word Wrap"}) {
			t.Errorf("the category is part of the label: %v", got)
		}
		if got := r.labels("zzz"); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
	t.Run("the id of a command finds it", func(t *testing.T) {
		r := newPaletteRig(t)
		if got := r.labels("workbench.action.gotoLine"); !reflect.DeepEqual(got, []string{"Go to Line/Column..."}) {
			t.Errorf("got %v", got)
		}
	})
	t.Run("commands with the same label show their ids", func(t *testing.T) {
		r := newPaletteRig(t)
		var descriptions []string
		for _, it := range r.provider.Picks("copy") {
			descriptions = append(descriptions, it.Description)
		}
		if !reflect.DeepEqual(descriptions, []string{"a.copy", "b.copy"}) {
			t.Errorf("descriptions %v", descriptions)
		}
		if got := r.provider.Picks("line")[0].Description; got != "" {
			t.Errorf("a label that is alone has a description: %q", got)
		}
	})
	t.Run("the key of a command is shown", func(t *testing.T) {
		r := newPaletteRig(t)
		if got := r.provider.Picks("line")[0].Keybinding; got != "Ctrl+G" {
			t.Errorf("key %q", got)
		}
	})
	t.Run("a command the available function refuses is left out", func(t *testing.T) {
		r := newPaletteRig(t)
		r.provider = NewCommandsProvider(CommandsOptions{
			Commands: r.commands, History: r.history,
			Available: func(id string, _ contextkey.Context) bool { return id != "editor.action.commentLine" },
			Run:       func(string) error { return nil },
		})
		for _, l := range r.labels("") {
			if l == "Toggle Line Comment" {
				t.Errorf("listed")
			}
		}
	})
}

func TestCommandsProviderRecentFirst(t *testing.T) {
	r := newPaletteRig(t)
	r.history.Push("workbench.action.gotoLine")
	r.history.Push("view.toggleWordWrap")
	got := r.labels("")
	want := []string{"--recently used", "View: Toggle Word Wrap", "Go to Line/Column...", "--other commands", "Copy", "Copy", "Toggle Line Comment", "Developer: Reload Window"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	// the recent ones have a button that takes them out of the list
	items := r.provider.Picks("")
	var recent, others int
	for _, it := range items {
		if it.Separator {
			continue
		}
		if len(it.Buttons) == 1 && it.Trigger != nil {
			recent++
		} else {
			others++
		}
	}
	if recent != 2 || others != 4 {
		t.Errorf("recent %d others %d", recent, others)
	}
	// the limit is read from the option each time
	r.provider = NewCommandsProvider(CommandsOptions{
		Commands: r.commands, Menus: r.menus, History: r.history, HistoryLimit: func() int { return 1 },
		Context: func() contextkey.Context { return r.keys }, Run: func(string) error { return nil },
	})
	if got := r.labels("")[0:2]; !reflect.DeepEqual(got, []string{"--recently used", "View: Toggle Word Wrap"}) {
		t.Errorf("with a limit of 1: %v", got)
	}
	if r.history.Peek("workbench.action.gotoLine") != 0 {
		t.Errorf("the older command is still in the history")
	}
}

func TestCommandsProviderRunsAndRemembers(t *testing.T) {
	r := newPaletteRig(t)
	reg := NewRegistry()
	reg.MustRegister(r.provider)
	var failed []string
	r.provider = NewCommandsProvider(CommandsOptions{
		Commands: r.commands, History: r.history, Context: func() contextkey.Context { return r.keys },
		Run: func(id string) error {
			r.ran = append(r.ran, id)
			if id == "a.copy" {
				return errTest
			}
			return nil
		},
		OnError: func(id string, err error) { failed = append(failed, id) },
	})
	reg = NewRegistry()
	reg.MustRegister(r.provider)
	c := NewController(ControllerOptions{Registry: reg})
	c.Show(">goto", ShowOptions{PreserveValue: true})
	c.SetValue(">line")
	c.Accept(KeyMods{})
	if c.Visible() || !reflect.DeepEqual(r.ran, []string{"workbench.action.gotoLine"}) {
		t.Fatalf("visible %v ran %v", c.Visible(), r.ran)
	}
	if r.history.Peek("workbench.action.gotoLine") == 0 {
		t.Errorf("the command is not in the history")
	}
	// a command that fails is reported
	c.Show(">copy", ShowOptions{PreserveValue: true})
	c.Accept(KeyMods{})
	if !reflect.DeepEqual(failed, []string{"a.copy"}) {
		t.Errorf("failed %v", failed)
	}
	// the recent one is first the next time, and its button takes it out
	c.Show(">", ShowOptions{})
	rows := c.Picker().Rows()
	if rows[0].Item.ID != "a.copy" || rows[0].Separator != "recently used" {
		t.Fatalf("first row %q %q", rows[0].Item.ID, rows[0].Separator)
	}
	c.Trigger(rows[0], 0, KeyMods{})
	if r.history.Peek("a.copy") != 0 {
		t.Errorf("the button did not remove the command from the history")
	}
	if c.Picker().Rows()[0].Item.ID == "a.copy" {
		t.Errorf("the row is still there")
	}
	// no command: the no results pick does nothing
	c.Show(">", ShowOptions{})
	c.SetValue(">zzzz")
	if got := labelsOf(c.Picker().Rows()); !reflect.DeepEqual(got, []string{"No matching commands"}) {
		t.Errorf("rows %v", got)
	}
}

type testError string

func (e testError) Error() string { return string(e) }

const errTest = testError("it failed")
