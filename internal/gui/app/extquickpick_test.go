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
				Chosen:      func(i int) { chosen = append(chosen, i) },
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
