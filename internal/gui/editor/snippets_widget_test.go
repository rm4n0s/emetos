package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func snippetRig(t *testing.T, colors kit.Colors, text string) (*ViewModel, *gluttertest.Tester) {
	t.Helper()
	vm := NewViewModel(newTestDoc(text), goOptions(), nil)
	ts := testutil.New(t, ui.Size{Width: 480, Height: 160})
	pumpEditorColors(t, ts, vm, EditorOpts{Autofocus: true}, colors)
	ts.Pump(time.Millisecond)
	return vm, ts
}

func TestSnippetTabStopsAreDrawn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		colors kit.Colors
	}{{"snippet_tabstops_dark", bundledColors(t, "Dark Modern")}, {"snippet_tabstops_light", bundledColors(t, "Light Modern")}} {
		t.Run(tc.name, func(t *testing.T) {
			vm, ts := snippetRig(t, tc.colors, "func main() {\n\t\n}\n")
			cursorAt(vm, 1, 1)
			if err := vm.RunCommand("editor.action.insertSnippet", map[string]any{"snippet": "fmt.Println(${1:message}, ${2:other})\nreturn ${1:message}$0"}); err != nil {
				t.Fatal(err)
			}
			ts.Pump(time.Millisecond)
			ts.Pump(time.Millisecond)
			ts.MatchesGolden(tc.name)
		})
	}
}

func TestSnippetChoiceListIsShownBelowTheCaret(t *testing.T) {
	vm, ts := snippetRig(t, bundledColors(t, "Dark Modern"), "x := \n")
	cursorAt(vm, 0, 5)
	if err := vm.RunCommand("editor.action.insertSnippet", map[string]any{"snippet": "${1|alpha,beta,gamma|}$0"}); err != nil {
		t.Fatal(err)
	}
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByText("beta")) {
		t.Fatal("the options are not shown")
	}
	ts.MatchesGolden("snippet_choices")

	// Down and Enter choose the second option and give the focus back to the editor
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyArrowDown, gluttertest.KeyOpts{})
	ts.SendKeyUpEvent(services.LogicalKeyboardKeyArrowDown)
	ts.Pump(time.Millisecond)
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyEnter, gluttertest.KeyOpts{})
	ts.SendKeyUpEvent(services.LogicalKeyboardKeyEnter)
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if ts.Exists(gluttertest.ByText("beta")) {
		t.Error("the list stays open")
	}
	if got := text(vm); got != "x := beta\n" {
		t.Errorf("text %q", got)
	}
}
