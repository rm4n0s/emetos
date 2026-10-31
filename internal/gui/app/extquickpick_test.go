package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// The list an extension shows with QuickPick().Show (step 14.1) is the quick input of the window.

func TestSplitLabelIcon(t *testing.T) {
	sync, _ := fonts.CodiconByName("sync")
	tests := []struct {
		name      string
		label     string
		want      string
		wantIcon  int
		wantKnown bool
	}{
		{"a codicon", "$(sync) Restart", "Restart", sync, true},
		{"a codicon that spins", "$(sync~spin) Restart", "Restart", sync, true},
		{"no icon", "Restart", "Restart", 0, false},
		{"an icon that does not exist", "$(no-such-icon) Restart", "$(no-such-icon) Restart", 0, false},
		{"an unfinished icon", "$(sync Restart", "$(sync Restart", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, icon := splitLabelIcon(tt.label)
			if got != tt.want || icon != tt.wantIcon || (icon != 0) != tt.wantKnown {
				t.Errorf("splitLabelIcon(%q) = %q, %#x; want %q, %#x", tt.label, got, icon, tt.want, tt.wantIcon)
			}
		})
	}
}

func TestAnExtensionListIsShownInTheQuickInput(t *testing.T) {
	tests := []struct {
		name string
		end  func(w *Workspace)
		want []int
	}{
		{"an item is chosen", func(w *Workspace) { w.QuickInput.Accept(quickinput.KeyMods{}) }, []int{0}},
		{"the list is closed", func(w *Workspace) { w.QuickInput.Hide() }, []int{-1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := settingsWindow(t, nil)
			t.Cleanup(w.release)
			var chosen []int
			var hidden int
			w.extensionQuickPick(exthost.QuickPickRequest{
				Placeholder: "Go 1.26 - gopls",
				Items:       []exthost.QuickPickEntry{{Label: "$(sync) Restart", Description: "gopls"}, {Label: "Output"}},
				Chosen:      func(i int, _ string) { chosen = append(chosen, i) },
				Hidden:      func() { hidden++; chosen = append(chosen, -1) },
			})
			if !w.QuickInput.Visible() {
				t.Fatal("the list is not shown")
			}
			items := w.QuickInput.Picker().Items()
			if len(items) != 2 || items[0].Label != "Restart" || items[0].Icon == 0 || items[0].Description != "gopls" || items[1].Label != "Output" {
				t.Fatalf("items = %+v", items)
			}
			if w.QuickInput.Picker().Placeholder() != "Go 1.26 - gopls" {
				t.Errorf("placeholder = %q", w.QuickInput.Picker().Placeholder())
			}
			tt.end(w)
			if len(chosen) != 1 || chosen[0] != tt.want[0] {
				t.Errorf("chosen = %v, want %v", chosen, tt.want)
			}
		})
	}
}

// TestAnExtensionListWithSeparatorsAndATitle: a separator is the heading of the items after it and is never chosen, an item that is always
// shown stays when the filter does not match it, the title is the picker's, and the choice carries the text typed in the filter.
func TestAnExtensionListWithSeparatorsAndATitle(t *testing.T) {
	w := settingsWindow(t, nil)
	t.Cleanup(w.release)
	var chosen []int
	var typed []string
	w.extensionQuickPick(exthost.QuickPickRequest{
		Title:       "Checkout",
		Placeholder: "Select a branch or tag to checkout",
		Items: []exthost.QuickPickEntry{
			{Label: "$(plus) Create new branch...", AlwaysShow: true},
			{Label: "branches", Separator: true},
			{Label: "main", Description: "abc1234"},
			{Label: "tags", Separator: true},
			{Label: "v1.0"},
		},
		Chosen: func(i int, v string) { chosen = append(chosen, i); typed = append(typed, v) },
		Hidden: func() {},
	})
	p := w.QuickInput.Picker()
	if p.Title() != "Checkout" {
		t.Errorf("title = %q", p.Title())
	}
	rows := p.Rows()
	if len(rows) != 3 || rows[1].Separator != "branches" || rows[2].Separator != "tags" || !rows[1].Border || rows[0].Separator != "" {
		t.Fatalf("rows = %+v", rows)
	}
	// a filter keeps the item that is always shown, after the matches
	w.QuickInput.SetValue("v1")
	rows = p.Rows()
	if len(rows) != 2 || rows[0].Item.Label != "v1.0" || rows[1].Item.Label != "Create new branch..." {
		t.Fatalf("filtered rows = %+v", rows)
	}
	w.QuickInput.Accept(quickinput.KeyMods{})
	if !slices.Equal(chosen, []int{4}) || !slices.Equal(typed, []string{"v1"}) {
		t.Errorf("chosen = %v, typed = %q", chosen, typed)
	}
}

func TestAnExtensionInputBoxIsShownInTheQuickInput(t *testing.T) {
	tests := []struct {
		name       string
		typed      string
		end        func(w *Workspace)
		wantAccept []string
		wantHidden int
		wantMsg    string
	}{
		{"a valid text is accepted", "json,xml", func(w *Workspace) { w.QuickInput.Accept(quickinput.KeyMods{}) }, []string{"json,xml"}, 0, ""},
		{"an invalid text keeps the box open", "a b", func(w *Workspace) { w.QuickInput.Accept(quickinput.KeyMods{}) }, nil, 0, "no spaces"},
		{"the box is closed", "json", func(w *Workspace) { w.QuickInput.Hide() }, nil, 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := settingsWindow(t, nil)
			t.Cleanup(w.release)
			var accepted []string
			var hidden int
			w.extensionInputBox(exthost.InputBoxRequest{
				Prompt: "Enter comma separated tag names", Value: "json",
				Validate: func(v string) string {
					if strings.Contains(v, " ") {
						return "no spaces"
					}
					return ""
				},
				Accepted: func(v string) { accepted = append(accepted, v) },
				Hidden:   func() { hidden++ },
			})
			if !w.QuickInput.Prompting() {
				t.Fatal("the input box is not shown")
			}
			if w.QuickInput.Picker().Placeholder() != "Enter comma separated tag names" {
				t.Errorf("placeholder = %q", w.QuickInput.Picker().Placeholder())
			}
			w.QuickInput.SetValue(tt.typed)
			tt.end(w)
			if !slices.Equal(accepted, tt.wantAccept) || hidden != tt.wantHidden {
				t.Errorf("accepted = %q, hidden = %d; want %q, %d", accepted, hidden, tt.wantAccept, tt.wantHidden)
			}
			if tt.wantMsg != "" && w.QuickInput.Picker().Message() != tt.wantMsg {
				t.Errorf("message = %q, want %q", w.QuickInput.Picker().Message(), tt.wantMsg)
			}
		})
	}
}

// TestAnExtensionQuestionThatIgnoresFocusOutStaysOpen: the Message, the focus rule of an input box and the focus rule of a list reach the
// quick input of the window.
func TestAnExtensionQuestionThatIgnoresFocusOutStaysOpen(t *testing.T) {
	w := settingsWindow(t, nil)
	t.Cleanup(w.release)
	w.extensionInputBox(exthost.InputBoxRequest{Prompt: "Password", Message: "Git: http://h", IgnoreFocusOut: true, Password: true, Accepted: func(string) {}, Hidden: func() {}})
	if p := w.QuickInput.Picker(); p.Info() != "Git: http://h" || !p.IgnoreFocusOut {
		t.Errorf("the input box: info %q, ignore focus out %v", p.Info(), p.IgnoreFocusOut)
	}
	w.extensionInputBox(exthost.InputBoxRequest{Prompt: "Name", Accepted: func(string) {}, Hidden: func() {}})
	if p := w.QuickInput.Picker(); p.Info() != "" || p.IgnoreFocusOut {
		t.Errorf("the next input box: info %q, ignore focus out %v", p.Info(), p.IgnoreFocusOut)
	}
	w.extensionQuickPick(exthost.QuickPickRequest{Placeholder: "Sure?", IgnoreFocusOut: true, Items: []exthost.QuickPickEntry{{Label: "Yes"}}, Chosen: func(int, string) {}, Hidden: func() {}})
	if !w.QuickInput.Picker().IgnoreFocusOut {
		t.Error("the list does not ignore the focus leaving it")
	}
}

// TestAnExtensionPasswordBoxHidesWhatIsTyped: the input box of an extension that asks for a password obscures its input.
func TestAnExtensionPasswordBoxHidesWhatIsTyped(t *testing.T) {
	for _, password := range []bool{true, false} {
		w := settingsWindow(t, nil)
		t.Cleanup(w.release)
		var accepted []string
		w.extensionInputBox(exthost.InputBoxRequest{Prompt: "Password", Password: password, Accepted: func(v string) { accepted = append(accepted, v) }, Hidden: func() {}})
		if got := w.QuickInput.Picker().Obscure; got != password {
			t.Errorf("Password %v: the input is obscured: %v", password, got)
		}
		w.QuickInput.SetValue("hunter2")
		w.QuickInput.Accept(quickinput.KeyMods{})
		if len(accepted) != 1 || accepted[0] != "hunter2" {
			t.Errorf("accepted = %q", accepted)
		}
	}
}
