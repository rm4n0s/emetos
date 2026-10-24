package quickinput

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
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
		{"prompt_path", func(r *rig) {
			r.ctl.Prompt(PromptOptions{Placeholder: "Enter the path of the file to save in", Value: "/home/me/project/Untitled-1.go",
				Validate: func(string) string { return "" }})
			r.settle()
		}},
		{"prompt_invalid", func(r *rig) {
			r.ctl.Prompt(PromptOptions{Placeholder: "Enter the path of the file to save in", Value: "/home/me/project/Untitled-1.go",
				Validate: func(v string) string {
					if v == "/home/me/missing/Untitled-1.go" {
						return "The folder '/home/me/missing' does not exist."
					}
					return ""
				}})
			r.ctl.SetValue("/home/me/missing/Untitled-1.go")
			r.settle()
		}},
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
				testutil.MatchesGolden(r.ts, "quickinput_"+tc.name+"_"+th.name)
			})
		}
	}
}
