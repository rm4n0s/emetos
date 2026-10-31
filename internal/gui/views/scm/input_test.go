package scm

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

var boxSize = ui.Size{Width: 320, Height: 300}

// repoWithBox adds a repository that keeps its box, with a placeholder like Git's and an accept command.
func (r *rig) repoWithBox(name string, groups ...group) *scmcore.Source {
	r.t.Helper()
	src := r.addRepo(name, groups...)
	src.Input().SetPlaceholder(`Message ({0} to commit on "main")`)
	src.SetAcceptInputCommand(scmcore.Command{ID: "git.commit", Title: "Commit"})
	return src
}

// button gives a repository an action button and lets the view show it.
func (r *rig) button(src *scmcore.Source, b scmcore.ActionButton) {
	r.t.Helper()
	src.SetActionButton(&b)
	if r.ts != nil {
		r.settle()
	}
}

// boxOf finds the text box of a source control.
func boxOf(src *scmcore.Source) gluttertest.Finder {
	return gluttertest.Descendant(gluttertest.ByKey(InputKey(src)), gluttertest.ByType[*kit.InputBox]())
}

// typeText gives the box the keyboard and types text into it (the whole text, as the input method sends it).
func (r *rig) typeText(src *scmcore.Source, text string) {
	r.t.Helper()
	r.ts.EnterText(boxOf(src), text)
	r.settle()
}

func (r *rig) press(k services.LogicalKeyboardKey, mods ...services.LogicalKeyboardKey) {
	r.t.Helper()
	for _, m := range mods {
		r.ts.SendKeyDownEvent(m)
	}
	r.ts.SendKeyEvent(k, gluttertest.KeyOpts{Physical: physicalOf(k)})
	for _, m := range slices.Backward(mods) {
		r.ts.SendKeyUpEvent(m)
	}
	r.settle()
}

// physicalOf is the physical key of the logical keys the tests press.
func physicalOf(k services.LogicalKeyboardKey) services.PhysicalKeyboardKey {
	switch k {
	case services.LogicalKeyboardKeyArrowUp:
		return services.PhysicalKeyboardKeyArrowUp
	case services.LogicalKeyboardKeyArrowDown:
		return services.PhysicalKeyboardKeyArrowDown
	case services.LogicalKeyboardKeyArrowLeft:
		return services.PhysicalKeyboardKeyArrowLeft
	case services.LogicalKeyboardKeyArrowRight:
		return services.PhysicalKeyboardKeyArrowRight
	case services.LogicalKeyboardKeyHome:
		return services.PhysicalKeyboardKeyHome
	case services.LogicalKeyboardKeyEnd:
		return services.PhysicalKeyboardKeyEnd
	case services.LogicalKeyboardKeyEnter:
		return services.PhysicalKeyboardKeyEnter
	case services.LogicalKeyboardKeyEscape:
		return services.PhysicalKeyboardKeyEscape
	case services.LogicalKeyboardKeyKeyA:
		return services.PhysicalKeyboardKeyKeyA
	case services.LogicalKeyboardKeyKeyV:
		return services.PhysicalKeyboardKeyKeyV
	case services.LogicalKeyboardKeyKeyZ:
		return services.PhysicalKeyboardKeyKeyZ
	}
	return services.PhysicalKeyboardKeyUsbReserved
}

// borderColor is the color of the border of the box of a source control.
func (r *rig) borderColor(src *scmcore.Source) ui.Color {
	r.t.Helper()
	f := gluttertest.First(gluttertest.Descendant(boxOf(src), gluttertest.ByType[*widgets.Container]()))
	d := r.ts.Widget(f).(*widgets.Container).Decoration().(*painting.BoxDecoration)
	return d.Border.(painting.Border).Top.Color
}

// boxHeightOf is the height of the box of a source control on screen.
func (r *rig) boxHeightOf(src *scmcore.Source) float64 {
	r.t.Helper()
	return r.ts.Size(boxOf(src)).Height
}

func TestInputRowsOfARepository(t *testing.T) {
	button := &scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "Commit"}, Enabled: true}
	changes := []group{{"workingTree", "Changes", []change{{path: "a.go"}}}}
	tests := []struct {
		name  string
		repos int
		setup func(r *rig, src []*scmcore.Source)
		want  []string
	}{
		{"a box and the groups", 1, nil, []string{"i:scm1", "g:scm1:workingTree", "f:scm1:workingTree:file:///work/r0/a.go"}},
		{"the box is hidden", 1, func(_ *rig, s []*scmcore.Source) { s[0].Input().SetVisible(false) },
			[]string{"g:scm1:workingTree", "f:scm1:workingTree:file:///work/r0/a.go"}},
		{"a button between the box and the groups", 1, func(_ *rig, s []*scmcore.Source) { s[0].SetActionButton(button) },
			[]string{"i:scm1", "b:scm1", "g:scm1:workingTree", "f:scm1:workingTree:file:///work/r0/a.go"}},
		{"scm.showActionButton off takes the button away", 1, func(r *rig, s []*scmcore.Source) {
			s[0].SetActionButton(button)
			r.noButton = true
			r.view.Refresh()
		}, []string{"i:scm1", "g:scm1:workingTree", "f:scm1:workingTree:file:///work/r0/a.go"}},
		{"no changes and a button: no groups", 1, func(_ *rig, s []*scmcore.Source) {
			s[0].Group("workingTree").SetResources(nil)
			s[0].SetActionButton(button)
		}, []string{"i:scm1", "b:scm1"}},
		{"no changes and no button: the groups show", 1, func(_ *rig, s []*scmcore.Source) { s[0].Group("workingTree").SetResources(nil) },
			[]string{"i:scm1", "g:scm1:workingTree"}},
		{"two repositories: a box under each header", 2, func(_ *rig, s []*scmcore.Source) { s[1].SetActionButton(button) },
			[]string{"r:scm1", "i:scm1", "g:scm1:workingTree", "f:scm1:workingTree:file:///work/r0/a.go",
				"r:scm2", "i:scm2", "b:scm2", "g:scm2:workingTree", "f:scm2:workingTree:file:///work/r1/a.go"}},
		{"two repositories, one with no changes and no button shows no groups", 2, func(_ *rig, s []*scmcore.Source) {
			s[1].Group("workingTree").SetResources(nil)
		}, []string{"r:scm1", "i:scm1", "g:scm1:workingTree", "f:scm1:workingTree:file:///work/r0/a.go", "r:scm2", "i:scm2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newBoxRig(t)
			var srcs []*scmcore.Source
			for i := range tt.repos {
				srcs = append(srcs, r.repoWithBox("r"+string(rune('0'+i)), changes...))
			}
			if tt.setup != nil {
				tt.setup(r, srcs)
			}
			r.show(goldenThemes[0].colors, boxSize)
			r.settle()
			if got := r.rowKeys(); !slices.Equal(got, tt.want) {
				t.Errorf("rows = %q\nwant   %q", got, tt.want)
			}
			r.noErrors()
		})
	}
}

// TestInputRowsAreNotRows: the list leaves the box and the button to themselves; a click does not select them and the arrow keys
// pass over them.
func TestInputRowsAreNotRows(t *testing.T) {
	r := newBoxRig(t)
	src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}, {path: "b.go"}}})
	r.button(src, scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "Commit"}, Enabled: true})
	r.show(goldenThemes[0].colors, boxSize)
	if got := r.view.List().SelectionCount(); got != 0 {
		t.Fatalf("selected rows at the start: %d", got)
	}
	// a click on the box gives it the keyboard and selects nothing
	r.ts.TapAt(ui.Offset{Dx: 100, Dy: 17}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.settle()
	if r.view.List().SelectionCount() != 0 || len(r.runs) != 0 || len(r.opens) != 0 {
		t.Errorf("a click on the box: selected %d, ran %v, opened %v", r.view.List().SelectionCount(), r.runs, r.opens)
	}
	if !r.view.InputFocused(src) {
		t.Error("a click on the box does not give it the keyboard")
	}
	if _, ok := r.view.List().Focused(); ok {
		t.Error("a row has the focus while the box has")
	}
	// the keys of the list reach the first row, not the box or the button
	r.view.List().SetFocus("g:scm1:workingTree")
	r.view.List().Perform(kit.ListActionUp)
	if k, _ := r.view.List().Focused(); k != "g:scm1:workingTree" {
		t.Errorf("Up from the first group focused %q", k)
	}
	r.view.List().Perform(kit.ListActionFirst)
	if k, _ := r.view.List().Focused(); k != "g:scm1:workingTree" {
		t.Errorf("Home focused %q", k)
	}
	r.view.List().Perform(kit.ListActionSelectAll)
	for _, k := range r.view.List().SelectedKeys() {
		if strings.HasPrefix(k, "i:") || strings.HasPrefix(k, "b:") {
			t.Errorf("select all selected %q", k)
		}
	}
	r.noErrors()
}

func TestInputTyping(t *testing.T) {
	r := newBoxRig(t)
	src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
	var seen []string
	src.Input().OnChange(func(v string) { seen = append(seen, v) })
	r.show(goldenThemes[0].colors, boxSize)

	// typing: the text reaches the model, with spaces and letters that are not ASCII
	r.typeText(src, "Fix the cache key of ünï")
	if got := src.Input().Value(); got != "Fix the cache key of ünï" {
		t.Fatalf("model text = %q", got)
	}
	if !slices.Equal(seen, []string{"Fix the cache key of ünï"}) {
		t.Errorf("OnChange saw %q", seen)
	}
	if h := src.Input().History(); !slices.Equal(h, []string{""}) {
		t.Errorf("typing wrote into the history: %q", h)
	}
	// a selection typed over
	r.ts.Pump(time.Second)              // the history of the text groups the changes that are less than half a second apart
	r.view.inputs[src].ctl.Select(4, 7) // "the"
	r.settle()
	r.ts.EnterText(boxOf(src), "Fix a cache key of ünï")
	r.settle()
	if got := src.Input().Value(); got != "Fix a cache key of ünï" {
		t.Errorf("after the replacement: %q", got)
	}
	// undo and redo with the keys of the box (the history of the text groups the changes that are less than half a second apart)
	r.ts.Pump(time.Second)
	r.press(services.LogicalKeyboardKeyKeyZ, services.LogicalKeyboardKeyControl)
	if got := src.Input().Value(); got == "Fix a cache key of ünï" {
		t.Errorf("Ctrl+Z left the text %q", got)
	}
	r.press(services.LogicalKeyboardKeyKeyZ, services.LogicalKeyboardKeyControl, services.LogicalKeyboardKeyShift)
	if got := src.Input().Value(); got != "Fix a cache key of ünï" {
		t.Errorf("Ctrl+Shift+Z gave %q", got)
	}
	// select all, then paste replaces the text
	r.ts.SetClipboardText("pasted line one\npasted line two")
	r.press(services.LogicalKeyboardKeyKeyA, services.LogicalKeyboardKeyControl)
	r.press(services.LogicalKeyboardKeyKeyV, services.LogicalKeyboardKeyControl)
	if got := src.Input().Value(); got != "pasted line one\npasted line two" {
		t.Errorf("after the paste: %q", got)
	}
	// Enter makes a new line (it does not accept: Ctrl+Enter does)
	r.press(services.LogicalKeyboardKeyEnter)
	if got := src.Input().Value(); !strings.HasSuffix(got, "\n") {
		t.Errorf("Enter did not make a new line: %q", got)
	}
	if len(r.runs) != 0 {
		t.Errorf("Enter ran %v", r.runs)
	}
	r.noErrors()
}

func TestInputFollowsTheProvider(t *testing.T) {
	r := newBoxRig(t)
	src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
	r.show(goldenThemes[0].colors, boxSize)
	r.typeText(src, "message")
	// the provider empties the box after a commit; the box follows
	src.Input().SetValue("")
	r.settle()
	if got := r.view.InputText(src); got != "" {
		t.Errorf("the box holds %q after the provider cleared it", got)
	}
	// a history move puts the caret at the start of the old message, the next one at the end
	src.Input().ShowPreviousHistoryValue()
	r.settle()
	if start, end, _ := r.view.inputs[src].ctl.Selection(); r.view.InputText(src) != "message" || start != 0 || end != 0 {
		t.Errorf("after Up: %q, caret %d-%d", r.view.InputText(src), start, end)
	}
	src.Input().SetValue("template")
	r.settle()
	if start, end, _ := r.view.inputs[src].ctl.Selection(); r.view.InputText(src) != "template" || start != len("template") || end != len("template") {
		t.Errorf("after SetValue: %q, caret %d-%d", r.view.InputText(src), start, end)
	}
	r.noErrors()
}

func TestInputPlaceholder(t *testing.T) {
	tests := []struct {
		name        string
		placeholder string
		label       string
		want        string
	}{
		{"the key of the binding", `Message ({0} to commit on "main")`, "Ctrl+Enter", `Message (Ctrl+Enter to commit on "main")`},
		{"a rebound key", `Message ({0} to commit on "dev")`, "Ctrl+Shift+Enter", `Message (Ctrl+Shift+Enter to commit on "dev")`},
		{"no label from the window is Ctrl+Enter", `Message ({0})`, "", "Message (Ctrl+Enter)"},
		{"no {0}", "Type here", "Ctrl+Enter", "Type here"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newBoxRig(t)
			src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
			r.acceptLabel = tt.label
			src.Input().SetPlaceholder(tt.placeholder)
			r.show(goldenThemes[0].colors, ui.Size{Width: 420, Height: 300})
			if !r.ts.Exists(gluttertest.ByText(tt.want)) {
				t.Errorf("no placeholder %q", tt.want)
			}
			// it goes with the first character, and comes back
			r.typeText(src, "x")
			if r.ts.Exists(gluttertest.ByText(tt.want)) {
				t.Error("the placeholder stays")
			}
			src.Input().SetValue("")
			r.settle()
			if !r.ts.Exists(gluttertest.ByText(tt.want)) {
				t.Error("the placeholder does not come back")
			}
		})
	}
	// the provider changes it while the view is open
	r := newBoxRig(t)
	src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
	r.show(goldenThemes[0].colors, ui.Size{Width: 420, Height: 300})
	src.Input().SetPlaceholder(`Message ({0} to commit on "feature")`)
	r.settle()
	if !r.ts.Exists(gluttertest.ByText(`Message (Ctrl+Enter to commit on "feature")`)) {
		t.Error("the new placeholder is not shown")
	}
}

func TestInputGrowth(t *testing.T) {
	lines := func(n int) string { return strings.TrimSuffix(strings.Repeat("line\n", n), "\n") }
	tests := []struct {
		name     string
		min, max int
		text     string
		want     int // lines
	}{
		{"one line by default", 1, 10, "", 1},
		{"a line of text", 1, 10, "hello", 1},
		{"three lines", 1, 10, lines(3), 3},
		{"the minimum holds an empty box", 4, 10, "", 4},
		{"the minimum holds a short text", 4, 10, lines(2), 4},
		{"past the minimum it grows", 2, 10, lines(5), 5},
		{"the maximum stops the growth", 1, 3, lines(8), 3},
		{"the minimum above the maximum raises the maximum", 5, 2, lines(8), 5},
		{"out of range values are clamped to 1..50", 0, 99, lines(60), 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newBoxRig(t)
			src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
			r.minLines, r.maxLines = tt.min, tt.max
			r.show(goldenThemes[0].colors, ui.Size{Width: 320, Height: 1400})
			if tt.text != "" {
				r.typeText(src, tt.text)
			}
			r.settle()
			want := float64(2 + 4 + tt.want*20) // border, padding, lines of round(13 * 1.5) px
			if got := r.boxHeightOf(src); got != want {
				t.Errorf("box height = %v, want %v for %d lines", got, want, tt.want)
			}
			if got, want := r.view.List().RowHeightAt(0), want+8; got != want {
				t.Errorf("row height = %v, want %v", got, want)
			}
			// the groups come right after the row
			if got, want := r.view.List().RowTop(1), want+8; got != want {
				t.Errorf("the next row starts at %v, want %v", got, want)
			}
			r.noErrors()
		})
	}
}

// TestInputWrapsLongLines: a long line wraps at the width of the box and the box grows with the lines that it needs; when the
// width changes the height follows.
func TestInputWrapsLongLines(t *testing.T) {
	r := newBoxRig(t)
	src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
	width := 320.0
	var setState widgets.StateSetter
	r.ts = pump(t, goldenThemes[0].colors, ui.Size{Width: 320, Height: 400}, widgets.NewStatefulBuilder(func(_ widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		setState = set
		return widgets.NewAlign(widgets.NewSizedBox(NewBody(r.view, BodyOpts{}), widgets.SizedBoxOpts{Width: foundation.Some(width)}), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})
	}))
	r.settle()
	r.typeText(src, strings.Repeat("word ", 10))
	wide := r.boxHeightOf(src)
	if wide <= 26 {
		t.Errorf("a long line did not make the box grow: %v", wide)
	}
	width = 160
	setState(func() {})
	r.settle()
	r.settle()
	if narrow := r.boxHeightOf(src); narrow <= wide {
		t.Errorf("box height at 160 px = %v, at 320 px %v", narrow, wide)
	}
	width = 320
	setState(func() {})
	r.settle()
	r.settle()
	if again := r.boxHeightOf(src); again != wide {
		t.Errorf("box height back at 320 px = %v, was %v", again, wide)
	}
	r.noErrors()
}

// TestInputSettingsWhileOpen: the number of lines and the font take effect on the view that is open.
func TestInputSettingsWhileOpen(t *testing.T) {
	r := newBoxRig(t)
	src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
	r.show(goldenThemes[0].colors, boxSize)
	if got := r.boxHeightOf(src); got != 26 {
		t.Fatalf("height = %v", got)
	}
	r.minLines = 3
	r.view.Refresh()
	r.settle()
	if got := r.boxHeightOf(src); got != 66 {
		t.Errorf("height with 3 lines = %v, want 66", got)
	}
	r.minLines = 1
	r.font = InputFont{Size: 20} // lines of 30 px
	r.view.Refresh()
	r.settle()
	if got := r.boxHeightOf(src); got != 36 {
		t.Errorf("height with the font of 20 px = %v, want 36", got)
	}
	r.font = InputFont{Mono: true, Size: 13}
	r.view.Refresh()
	r.settle()
	r.noErrors()
}

func TestInputValidation(t *testing.T) {
	r := newBoxRig(t)
	src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
	r.show(goldenThemes[0].colors, ui.Size{Width: 360, Height: 300})
	colors := goldenThemes[0].colors
	calls := 0
	src.Input().SetValidation(func(v string) *scmcore.Validation {
		calls++
		switch {
		case strings.HasPrefix(v, "E"):
			return &scmcore.Validation{Message: "an error", Level: scmcore.ValidationError}
		case strings.HasPrefix(v, "W"):
			return &scmcore.Validation{Message: "a warning", Level: scmcore.ValidationWarning}
		case strings.HasPrefix(v, "I"):
			return &scmcore.Validation{Message: "some information", Level: scmcore.ValidationInfo}
		}
		return nil
	})
	r.settle()
	tests := []struct {
		text    string
		message string
		border  ui.Color
	}{
		{"Error!", "an error", colors.InputValidationErrorBorder},
		{"Warn", "", colors.FocusBorder},
		{"Warning", "a warning", colors.InputValidationWarningBorder},
		{"Info", "some information", colors.InputValidationInfoBorder},
		{"plain", "", colors.FocusBorder},
	}
	for _, tt := range tests {
		if tt.text == "Warn" {
			continue
		}
		r.typeText(src, tt.text)
		if tt.message != "" && !r.ts.Exists(gluttertest.ByText(tt.message)) {
			t.Errorf("%q: no message %q", tt.text, tt.message)
		}
		if tt.message == "" {
			for _, m := range []string{"an error", "a warning", "some information"} {
				if r.ts.Exists(gluttertest.ByText(m)) {
					t.Errorf("%q: the message %q is shown", tt.text, m)
				}
			}
		}
		if got := r.borderColor(src); got != tt.border {
			t.Errorf("%q: border %v, want %v", tt.text, got, tt.border)
		}
		if got := r.view.ValidationShown(src); got != (tt.message != "") {
			t.Errorf("%q: ValidationShown = %v", tt.text, got)
		}
	}
	if calls == 0 {
		t.Fatal("the validation function was not called")
	}

	// Escape takes the message away, and the border keeps its color until the text changes
	r.typeText(src, "Warning again")
	if !r.view.ValidationShown(src) {
		t.Fatal("no message to clear")
	}
	r.view.ClearValidation(src)
	r.settle()
	if r.ts.Exists(gluttertest.ByText("a warning")) || r.view.ValidationShown(src) {
		t.Error("ClearValidation left the message")
	}
	if got := r.borderColor(src); got != colors.InputValidationWarningBorder {
		t.Errorf("the border after ClearValidation = %v", got)
	}
	r.typeText(src, "Warning once more")
	if !r.ts.Exists(gluttertest.ByText("a warning")) {
		t.Error("the message does not come back with the next change")
	}

	// a message shows while the box has the keyboard only
	r.view.inputs[src].ctl.Blur()
	r.settle()
	if r.ts.Exists(gluttertest.ByText("a warning")) {
		t.Error("the message stays after the box lost the keyboard")
	}
	if got := r.borderColor(src); got != colors.InputValidationWarningBorder {
		t.Errorf("the border after the blur = %v", got)
	}

	// a new validation function applies to the text that is there
	src.Input().SetValidation(func(string) *scmcore.Validation {
		return &scmcore.Validation{Message: "new rule", Level: scmcore.ValidationInfo}
	})
	r.settle()
	r.view.inputs[src].ctl.Focus()
	r.settle()
	if !r.ts.Exists(gluttertest.ByText("new rule")) {
		t.Error("a new validation function is not applied to the text")
	}
	src.Input().SetValidation(nil)
	r.settle()
	if r.ts.Exists(gluttertest.ByText("new rule")) || r.view.ValidationShown(src) {
		t.Error("removing the validation leaves the message")
	}
	r.noErrors()
}

// TestInputContextKeys: the scope of the box holds the keys of the commands of the input.
func TestInputContextKeys(t *testing.T) {
	r := newBoxRig(t)
	src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
	r.show(goldenThemes[0].colors, boxSize)
	u := r.view.inputs[src]
	keys := func() map[string]any {
		return map[string]any{
			"scmRepository":                u.scope.Get("scmRepository"),
			"scmInputIsInFirstPosition":    u.scope.Get("scmInputIsInFirstPosition"),
			"scmInputIsInLastPosition":     u.scope.Get("scmInputIsInLastPosition"),
			"scmInputHasValidationMessage": u.scope.Get("scmInputHasValidationMessage"),
			"editorHasSelection":           u.scope.Get("editorHasSelection"),
		}
	}
	check := func(what string, first, last, selection bool) {
		t.Helper()
		got := keys()
		if got["scmRepository"] != "scm1" || got["scmInputIsInFirstPosition"] != first || got["scmInputIsInLastPosition"] != last || got["editorHasSelection"] != selection {
			t.Errorf("%s: keys %v, want first %v last %v selection %v", what, got, first, last, selection)
		}
	}
	// the scope of the box is found through its focus node
	if r.scopes.ScopeOf(u.ctl.FocusNode(), nil) != u.scope {
		t.Error("the focus node of the box has no scope")
	}
	check("empty box", true, true, false)
	r.typeText(src, "one\ntwo")
	check("caret at the end", false, true, false)
	u.ctl.Select(0, 0)
	r.settle()
	check("caret at the start", true, false, false)
	u.ctl.Select(2, 2)
	r.settle()
	check("caret inside", false, false, false)
	u.ctl.Select(2, 5)
	r.settle()
	check("a selection", false, false, true)
	r.press(services.LogicalKeyboardKeyKeyA, services.LogicalKeyboardKeyControl)
	check("everything selected, the caret at the end", false, true, true)
	// Escape with some text selected collapses the selection (the key is not taken by scm.clearInput then)
	r.press(services.LogicalKeyboardKeyEscape)
	check("after Escape", false, true, false)
	if got := src.Input().Value(); got != "one\ntwo" {
		t.Errorf("Escape changed the text: %q", got)
	}
	// the repository is the focused one when the box has the keyboard
	other := r.repoWithBox("s", group{"workingTree", "Changes", []change{{path: "b.go"}}})
	r.settle()
	r.ts.EnterText(boxOf(other), "x")
	r.settle()
	if f := r.model.Focused(); f != other {
		t.Errorf("focused repository = %v after typing in its box", f.Name())
	}
	r.noErrors()
}

// TestInputFocus: the keyboard goes to the box when the view takes it and no row has it, and the provider can ask for it.
func TestInputFocus(t *testing.T) {
	r := newBoxRig(t)
	a := r.repoWithBox("a", group{"workingTree", "Changes", []change{{path: "x.go"}}})
	b := r.repoWithBox("b", group{"workingTree", "Changes", []change{{path: "y.go"}}})
	r.show(goldenThemes[0].colors, ui.Size{Width: 320, Height: 400})
	r.view.Focus()
	r.settle()
	if !r.view.InputFocused(a) || r.view.InputFocused(b) {
		t.Fatalf("Focus: a %v, b %v", r.view.InputFocused(a), r.view.InputFocused(b))
	}
	// a row with the focus keeps it
	r.view.inputs[a].ctl.Blur()
	r.view.List().SetFocus("g:scm1:workingTree")
	r.view.Focus()
	r.settle()
	if r.view.InputFocused(a) {
		t.Error("Focus took the keyboard from a focused row")
	}
	// SCMInput.Focus
	b.Input().Focus()
	r.settle()
	if !r.view.InputFocused(b) || r.reveals == 0 {
		t.Errorf("Input.Focus: focused %v, reveals %d", r.view.InputFocused(b), r.reveals)
	}
	if _, ok := r.view.List().Focused(); ok {
		t.Error("a row has the focus while the box has")
	}
	r.noErrors()
}

func TestInputDisabledAndHidden(t *testing.T) {
	r := newBoxRig(t)
	src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
	r.show(goldenThemes[0].colors, boxSize)
	src.Input().SetEnabled(false)
	r.settle()
	r.press(services.LogicalKeyboardKeyKeyX)
	if got := src.Input().Value(); got != "" {
		t.Errorf("a disabled box took text: %q", got)
	}
	src.Input().SetVisible(false)
	r.settle()
	if r.ts.Exists(boxOf(src)) {
		t.Error("the box is shown after SetVisible(false)")
	}
	src.Input().SetVisible(true)
	r.settle()
	if !r.ts.Exists(boxOf(src)) {
		t.Error("the box does not come back")
	}
	r.noErrors()
}

func TestActionButton(t *testing.T) {
	r := newBoxRig(t)
	src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
	r.button(src, scmcore.ActionButton{
		Command: scmcore.Command{ID: "git.commit", Title: "$(check) Commit", Args: map[string]any{"all": true}},
		Secondary: [][]scmcore.Command{
			{{ID: "git.commitAmend", Title: "Commit (Amend)"}, {ID: "git.commitSigned", Title: "Commit (Signed Off)"}},
			{}, {{ID: "git.commitEmpty", Title: "Commit Empty", Args: "x"}},
		},
		Enabled: true, Description: "Commit the staged changes",
	})
	r.show(goldenThemes[0].colors, boxSize)
	if !r.ts.Exists(gluttertest.ByText("Commit")) {
		t.Fatal("no button")
	}
	if got := r.view.List().RowHeightAt(1); got != 36 {
		t.Errorf("button row height = %v, want 36", got)
	}
	// a click runs the primary command with its argument
	y := r.view.List().RowTop(1) + 18
	r.ts.TapAt(ui.Offset{Dx: 100, Dy: y}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.settle()
	if len(r.runs) != 1 || r.runs[0].id != "git.commit" {
		t.Fatalf("runs = %v", r.runs)
	}
	if m, ok := r.runs[0].args.(map[string]any); !ok || m["all"] != true {
		t.Errorf("arguments = %v", r.runs[0].args)
	}
	// the arrow opens the dropdown: the groups with a separator between two of them (the empty one leaves none)
	r.ts.TapAt(ui.Offset{Dx: 320 - rowInsetRight - dropdownWidth/2, Dy: y}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.settle()
	if len(r.dropdowns) != 1 {
		t.Fatalf("dropdowns = %d", len(r.dropdowns))
	}
	var got []string
	for _, it := range r.dropdowns[0].items {
		if it.Separator {
			got = append(got, "---")
		} else {
			got = append(got, it.ID+":"+it.Label)
		}
	}
	want := []string{"git.commitAmend:Commit (Amend)", "git.commitSigned:Commit (Signed Off)", "---", "git.commitEmpty:Commit Empty"}
	if !slices.Equal(got, want) {
		t.Errorf("dropdown = %q, want %q", got, want)
	}
	if it := r.dropdowns[0].items[3]; it.Data != "x" {
		t.Errorf("argument of an item = %v", it.Data)
	}
	if at := r.dropdowns[0].at; at.Dx != rowInsetLeft || at.Dy < y {
		t.Errorf("the dropdown opens at %v, below the button's left edge", at)
	}
	if len(r.runs) != 1 {
		t.Errorf("the arrow ran %v", r.runs[1:])
	}
	// the keyboard: Tab or a click gives the button the focus, Enter runs it, Down opens the dropdown
	r.view.inputs[src].button.RequestFocus()
	r.settle()
	r.press(services.LogicalKeyboardKeyEnter)
	if len(r.runs) != 2 {
		t.Errorf("Enter on the button: runs = %v", r.runs)
	}
	r.press(services.LogicalKeyboardKeyArrowDown)
	if len(r.dropdowns) != 2 {
		t.Errorf("Down on the button: dropdowns = %d", len(r.dropdowns))
	}
	// OpenActionDropdown opens it from outside
	if !r.view.OpenActionDropdown(src) || len(r.dropdowns) != 3 {
		t.Errorf("OpenActionDropdown: dropdowns = %d", len(r.dropdowns))
	}
	r.noErrors()
}

func TestActionButtonStates(t *testing.T) {
	tests := []struct {
		name   string
		button scmcore.ActionButton
		click  int // commands run by a click
		arrow  bool
	}{
		{"disabled", scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "Commit"}}, 0, false},
		{"enabled with no dropdown", scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "Commit"}, Enabled: true}, 1, false},
		{"a button with no command", scmcore.ActionButton{Command: scmcore.Command{Title: "Nothing"}, Enabled: true}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newBoxRig(t)
			src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
			r.button(src, tt.button)
			r.show(goldenThemes[0].colors, boxSize)
			y := r.view.List().RowTop(1) + 18
			r.ts.TapAt(ui.Offset{Dx: 100, Dy: y}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
			r.settle()
			if len(r.runs) != tt.click {
				t.Errorf("a click ran %v", r.runs)
			}
		})
	}
	// the provider changes the button while the view is open
	r := newBoxRig(t)
	src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
	r.show(goldenThemes[0].colors, boxSize)
	if r.ts.Exists(gluttertest.ByKey(ActionButtonKey(src))) {
		t.Error("a button without the provider")
	}
	r.button(src, scmcore.ActionButton{Command: scmcore.Command{ID: "git.sync", Title: "$(sync) Sync Changes 1$(arrow-down) 2$(arrow-up)"}, Enabled: true})
	if !r.ts.Exists(gluttertest.ByKey(ActionButtonKey(src))) {
		t.Fatal("the button does not appear")
	}
	r.button(src, scmcore.ActionButton{Command: scmcore.Command{ID: "git.publish", Title: "Publish Branch"}, Enabled: true})
	if !r.ts.Exists(gluttertest.ByText("Publish Branch")) {
		t.Error("the button does not change")
	}
	src.SetActionButton(nil)
	r.settle()
	if r.ts.Exists(gluttertest.ByKey(ActionButtonKey(src))) {
		t.Error("the button stays after the provider removed it")
	}
	r.noErrors()
}

// TestBoxGrowthDoesNotShiftTheRowsOnScreen: a box that grows pushes the rows below it, and the rows above stay.
func TestBoxGrowthMovesTheRowsBelow(t *testing.T) {
	r := newBoxRig(t)
	a := r.repoWithBox("a", group{"workingTree", "Changes", []change{{path: "x.go"}}})
	b := r.repoWithBox("b", group{"workingTree", "Changes", []change{{path: "y.go"}}})
	r.show(goldenThemes[0].colors, ui.Size{Width: 320, Height: 500})
	rowB := "r:" + b.Key()
	before := r.view.List().RowTop(r.view.List().IndexOf(rowB))
	r.typeText(a, "1\n2\n3")
	after := r.view.List().RowTop(r.view.List().IndexOf(rowB))
	if after-before != 40 {
		t.Errorf("the second repository moved by %v, want 40 (two more lines)", after-before)
	}
	r.noErrors()
}

var _ = time.Second

// TestInputOfARemovedSourceControl: the box of a repository that goes is gone with it, and its keys with it; the others stay.
func TestInputOfARemovedSourceControl(t *testing.T) {
	r := newBoxRig(t)
	a := r.repoWithBox("a", group{"workingTree", "Changes", []change{{path: "x.go"}}})
	b := r.repoWithBox("b", group{"workingTree", "Changes", []change{{path: "y.go"}}})
	r.show(goldenThemes[0].colors, ui.Size{Width: 320, Height: 400})
	r.typeText(a, "typing in a")
	if !r.view.InputFocused(a) {
		t.Fatal("the box of a has no keyboard")
	}
	scope := r.view.inputs[a].scope
	r.model.Remove(a)
	r.settle()
	r.settle()
	if r.ts.Exists(boxOf(a)) {
		t.Error("the box of a removed repository is still on screen")
	}
	if _, ok := r.view.inputs[a]; ok {
		t.Error("the view keeps the state of a removed repository")
	}
	if scope.Get("scmRepository") != nil {
		t.Error("the scope of the removed box is alive")
	}
	r.typeText(b, "still works")
	if got := b.Input().Value(); got != "still works" {
		t.Errorf("the box of b holds %q", got)
	}
	r.view.Dispose()
	if len(r.view.inputs) != 0 {
		t.Error("Dispose keeps the boxes")
	}
	r.noErrors()
}

// TestButtonWithoutABox: a repository whose box is hidden can still have the action button (Git's git.showCommitInput off).
func TestButtonWithoutABox(t *testing.T) {
	r := newBoxRig(t)
	src := r.repoWithBox("r", group{"workingTree", "Changes", []change{{path: "a.go"}}})
	src.Input().SetVisible(false)
	r.button(src, scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "Commit"}, Enabled: true})
	r.show(goldenThemes[0].colors, boxSize)
	if got := r.rowKeys(); len(got) < 2 || got[0] != "b:scm1" {
		t.Errorf("rows %q: want the button first", got)
	}
	if !r.ts.Exists(gluttertest.ByKey(ActionButtonKey(src))) || r.ts.Exists(boxOf(src)) {
		t.Error("the button should be on screen and the box not")
	}
	r.noErrors()
}
