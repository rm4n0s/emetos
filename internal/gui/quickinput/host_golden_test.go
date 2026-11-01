package quickinput

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

var goldenThemes = []struct {
	name   string
	colors func() kit.Colors
}{{"dark", kit.DarkModern}, {"light", kit.LightModern}}

func TestGoldenQuickInput(t *testing.T) {
	cases := []struct {
		name string
		// height is the height of the window; 360 when 0.
		height float64
		// prepare shows the quick input in the state of the golden.
		prepare func(r *rig)
	}{
		{"commands_line", 0, func(r *rig) { r.show(">line", ShowOptions{PreserveValue: true}) }},
		{"commands_recent", 0, func(r *rig) {
			r.history.Push("workbench.action.gotoLine")
			r.history.Push("editor.action.commentLine")
			r.show(">", ShowOptions{})
		}},
		{"commands_none", 0, func(r *rig) { r.show(">zzzz", ShowOptions{PreserveValue: true}) }},
		{"goto_line", 0, func(r *rig) { r.show(":2:3", ShowOptions{PreserveValue: true}) }},
		{"help", 0, func(r *rig) { r.show("?", ShowOptions{}) }},
		{"views", 0, func(r *rig) { r.show("view ", ShowOptions{}) }},
		{"prompt_path", 0, func(r *rig) {
			r.ctl.Prompt(PromptOptions{Placeholder: "Enter the path of the file to save in", Value: "/home/me/project/Untitled-1.go",
				Validate: func(string) string { return "" }})
			r.settle()
		}},
		{"prompt_invalid", 0, func(r *rig) {
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
		{"prompt_worktree_path", 0, func(r *rig) {
			// the path of a new worktree: the folder button after the text
			folder, _ := fonts.CodiconByName("folder")
			r.ctl.Prompt(PromptOptions{Placeholder: "Worktree path", Message: "Please provide a worktree path", Value: "/home/me/code/app.worktrees/feature-login",
				IgnoreFocusOut: true, Buttons: []PromptButton{{Icon: folder, Tooltip: "Select Worktree Destination", OnClick: func(string, func(string)) {}}}})
			r.settle()
		}},
		{"prompt_password", 0, func(r *rig) {
			r.ctl.Prompt(PromptOptions{Placeholder: "Password for \"prod\" (Enter for none)", Password: true})
			r.ctl.SetValue("hunter2-secret")
			r.settle()
		}},
		{"prompt_git_password", 0, func(r *rig) {
			r.ctl.Prompt(PromptOptions{Placeholder: "Password for 'http://ada@127.0.0.1:41233'", Message: "Git: http://127.0.0.1:41233", Password: true, IgnoreFocusOut: true})
			r.ctl.SetValue("hunter2-secret")
			r.settle()
		}},
		{"pick_host_key", 0, func(r *rig) {
			detail := `"github.com (140.82.121.4)" has fingerprint "SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU"`
			r.ctl.Pick(PickOptions{
				Placeholder: "Are you sure you want to continue connecting?", IgnoreFocusOut: true,
				Items: []Item{{Label: "Yes", Detail: detail, Accept: func(KeyMods) {}}, {Label: "No", Detail: detail, Accept: func(KeyMods) {}}},
			})
			r.settle()
		}},
		{"pick_checkout", 560, func(r *rig) {
			// the checkout picker of the Git extension: a title, commands that are always shown, then the groups of refs with their
			// descriptions and details
			noop := func(KeyMods) {}
			branchIcon, _ := fonts.CodiconByName("git-branch")
			r.ctl.Pick(PickOptions{
				Title: "Checkout", Placeholder: "Select a branch or tag to checkout",
				Items: []Item{
					{Label: "Create new branch...", Icon: fonts.CodiconAdd, AlwaysShow: true, Accept: noop},
					{Label: "Checkout detached...", Icon: fonts.CodiconDebugDisconnect, AlwaysShow: true, Accept: noop},
					{Separator: true, Label: "branches"},
					{Label: "main", Icon: branchIcon, Description: "0↓ 1↑ • 2 days ago", Detail: "Ada Example • 82428c8 • Fix the cache key of the loader", Accept: noop},
					{Label: "feature", Icon: fonts.CodiconLock, Description: "3 days ago", Detail: "Ada Example • 1f3a9be • Add the retry loop", Accept: noop},
					{Separator: true, Label: "remote branches"},
					{Label: "origin/main", Icon: fonts.CodiconCloud, Description: "2 weeks ago", Detail: "Ada Example • 5d0c7aa • Start the loader", Accept: noop},
					{Separator: true, Label: "tags"},
					{Label: "v1.0", Icon: fonts.CodiconTag, Description: "2 weeks ago", Detail: "Ada Example • 5d0c7aa • Start the loader", Accept: noop},
				},
			})
			r.settle()
		}},
		{"pick_with_separators", 0, func(r *rig) {
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
				height := tc.height
				if height == 0 {
					height = 360
				}
				r := newRig(t, ui.Size{Width: 720, Height: height}, th.colors())
				tc.prepare(r)
				testutil.MatchesGolden(r.ts, "quickinput_"+tc.name+"_"+th.name)
			})
		}
	}
}
