package mergeeditor

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

var goldenThemes = []struct {
	name   string
	theme  string
	colors func() kit.Colors
}{
	{"dark", "Dark Modern", kit.DarkModern},
	{"light", "Light Modern", kit.LightModern},
}

// TestMergeEditorGolden shows a merge with two conflicts and one change that was taken without asking, in both
// themes: the incoming side on the left, the current side on the right, the result below with the conflicts outlined.
func TestMergeEditorGolden(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			inputs, _ := goMerge()
			r := newRig(t, rigConfig{inputs: inputs, size: ui.Size{Width: 1000, Height: 840}, colors: th.colors(), highlight: th.theme})
			r.settle()
			testutil.MatchesGolden(r.ts, "merge_"+th.name)
		})
	}
}

// TestMergeEditorResolvedGolden shows the same merge after the first conflict got both sides (current, then incoming)
// from its two checkboxes, with the cursor in the result on the second one: the first is handled and outlined in
// gray, its checkboxes are checked (the incoming one with two marks), the lines it put in the result are colored, and
// the second conflict is the focused one, with the thicker border.
func TestMergeEditorResolvedGolden(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			inputs, _ := goMerge()
			r := newRig(t, rigConfig{inputs: inputs, size: ui.Size{Width: 1000, Height: 840}, colors: th.colors(), highlight: th.theme})
			r.model.Toggle(0, diff.Input2)
			r.model.Toggle(0, diff.Input1)
			r.model.GoToConflict(true)
			r.settle()
			testutil.MatchesGolden(r.ts, "merge_resolved_"+th.name)
		})
	}
}
