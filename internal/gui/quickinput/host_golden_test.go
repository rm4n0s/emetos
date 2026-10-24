package quickinput

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

var goldenThemes = []struct {
	name   string
	colors func() kit.Colors
}{{"dark", kit.DarkModern}, {"light", kit.LightModern}}

func TestGoldenQuickInput(t *testing.T) {
	cases := []struct {
		name string
		// prepare shows the quick input in the state of the golden.
		prepare func(r *rig)
	}{
		{"commands_line", func(r *rig) { r.show(">line", ShowOptions{PreserveValue: true}) }},
		{"commands_recent", func(r *rig) {
			r.history.Push("workbench.action.gotoLine")
			r.history.Push("editor.action.commentLine")
			r.show(">", ShowOptions{})
		}},
		{"commands_none", func(r *rig) { r.show(">zzzz", ShowOptions{PreserveValue: true}) }},
		{"goto_line", func(r *rig) { r.show(":2:3", ShowOptions{PreserveValue: true}) }},
		{"help", func(r *rig) { r.show("?", ShowOptions{}) }},
		{"views", func(r *rig) { r.show("view ", ShowOptions{}) }},
		{"pick_with_separators", func(r *rig) {
			r.ctl.Pick(PickOptions{
				Placeholder: "Select a snippet", MatchOnDetail: true,
				Items: []Item{
					{Separator: true, Label: "User Snippets"},
					{Label: "fori", Detail: "for loop with an index", Accept: func(KeyMods) {}},
					{Label: "iferr", Detail: "if err != nil { return err }", Accept: func(KeyMods) {}},
					{Separator: true, Label: "Go"},
					{Label: "func", Detail: "function declaration", Accept: func(KeyMods) {}},
					{Label: "main", Detail: "func main() {}", Accept: func(KeyMods) {}},
				},
			})
			r.settle()
		}},
	}
	for _, th := range goldenThemes {
		for _, tc := range cases {
			t.Run(th.name+" "+tc.name, func(t *testing.T) {
				r := newRig(t, ui.Size{Width: 720, Height: 360}, th.colors())
				tc.prepare(r)
				r.ts.MatchesGolden("quickinput_" + tc.name + "_" + th.name)
			})
		}
	}
}
