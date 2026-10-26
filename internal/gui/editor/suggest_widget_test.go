package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

const suggestWidgetText = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.P\n}\n"

// suggestWidgetItems are members of a package, with the kinds the icons are drawn for.
func suggestWidgetItems() []langfeatures.CompletionItem {
	return []langfeatures.CompletionItem{
		{Label: "Println", Kind: 3, Detail: "func(a ...any) (n int, err error)", LabelDetail: "(a ...any)", Documentation: &langfeatures.Markup{Markdown: true, Value: "Println formats using the default formats for its operands and writes to standard output.\n\nSpaces are always added between operands and a newline is appended.\n\n**Returns** the number of bytes written and any write error encountered."}},
		{Label: "Printf", Kind: 3, Detail: "func(format string, a ...any)", LabelDetail: "(format string, a ...any)"},
		{Label: "Print", Kind: 3, Detail: "func(a ...any)"},
		{Label: "PrintState", Kind: 8},
		{Label: "Pointer", Kind: 22},
		{Label: "Pi", Kind: 21, Detail: "untyped float"},
		{Label: "Pf", Kind: 5, Detail: "int"},
		{Label: "Pm", Kind: 2},
		{Label: "Pk", Kind: 14},
		{Label: "Pv", Kind: 6},
		{Label: "Ps", Kind: 15, Detail: "snippet"},
		{Label: "Pdeprecated", Kind: 3, Deprecated: true},
	}
}

// suggestWidgetRig is an editor with the widget open on "fmt.P", in a theme.
func suggestWidgetRig(t *testing.T, size ui.Size, colors kit.Colors, details bool) (*ViewModel, *gluttertest.Tester) {
	t.Helper()
	opts := goOptions()
	opts.CursorBlinking = "solid"
	opts.Minimap.Enabled = false
	opts.Suggest = SuggestOptions{
		QuickSuggestions: QuickSuggestions{Other: "on", Comments: "off", Strings: "off"}, QuickSuggestionsDelay: 10,
		SuggestOnTriggerCharacters: true, AcceptSuggestionOnEnter: "on", AcceptSuggestionOnCommitCharacter: true, SuggestSelection: "first",
		SnippetSuggestions: "inline", WordBasedSuggestions: "off", InsertMode: "insert", FilterGraceful: true,
		ShowIcons: true, ShowInlineDetails: true, ShowDeprecated: true, MatchOnWordStartOnly: true, SelectionMode: "always",
	}
	doc := newTestDoc(suggestWidgetText)
	doc.SetLanguageID("go")
	vm := NewViewModel(doc, opts, nil)
	reg := langfeatures.NewRegistry()
	reg.RegisterCompletion("fake", langfeatures.Selector{Languages: []string{"go"}}, &fakeCompletions{items: suggestWidgetItems()})
	shared := NewSuggestShared()
	shared.DetailsVisible = details
	vm.SetSuggest(&SuggestHost{Ctx: t.Context(), Features: reg, Shared: shared})
	t.Cleanup(vm.suggestCancel)
	ts := testutil.New(t, size)
	pumpEditorColors(t, ts, vm, EditorOpts{Autofocus: true}, colors)
	cursorAt(vm, 5, 6)
	if err := vm.RunCommand("editor.action.triggerSuggest", nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !vm.SuggestVisible() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		ts.FlushPosted()
	}
	if !vm.SuggestVisible() {
		t.Fatal("the widget does not show")
	}
	for vm.SuggestView().Items[vm.SuggestView().Focus].Label != "Println" {
		if err := vm.RunCommand("selectNextSuggestion", nil); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		ts.Pump(time.Millisecond)
	}
	return vm, ts
}

func TestGoldenSuggestWidget(t *testing.T) {
	for _, tc := range []struct {
		golden, theme string
		details       bool
	}{
		{"suggest_list_dark", "Dark Modern", false},
		{"suggest_list_light", "Light Modern", false},
		{"suggest_details_dark", "Dark Modern", true},
		{"suggest_details_light", "Light Modern", true},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			_, ts := suggestWidgetRig(t, ui.Size{Width: 900, Height: 360}, bundledColors(t, tc.theme), tc.details)
			noErrors(t, ts)
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}

func TestGoldenSuggestWidgetStates(t *testing.T) {
	for _, tc := range []struct {
		golden string
		state  SuggestState
	}{
		{"suggest_loading", SuggestLoading},
		{"suggest_empty", SuggestEmpty},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			vm, ts := suggestWidgetRig(t, ui.Size{Width: 640, Height: 240}, kit.DarkModern(), false)
			vm.suggestCancel()
			vm.sg.session = &suggestSession{state: tc.state, req: suggestRequest{lineCtx: vm.currentLineCtx()}, focus: -1}
			vm.suggestChanged()
			ts.Pump(time.Millisecond)
			noErrors(t, ts)
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}
