package keybindingsui

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

const userFile = `[{ "key": "ctrl+alt+d", "command": "editor.action.copyLinesDownAction" }]`

func TestTheTableShowsEveryCommand(t *testing.T) {
	r := newRig(t, rigOpts{userText: userFile})
	want := []string{
		"editor.action.addCommentLine|ctrl+k ctrl+c",
		"editor.action.clipboardCopyAction|ctrl+c",
		"editor.action.copyLinesDownAction|ctrl+alt+d",
		"editor.action.copyLinesDownAction|shift+alt+down",
		"editor.action.copyLinesUpAction|shift+alt+up",
		"editor.action.clipboardCutAction|ctrl+x",
		"other.cut|ctrl+x",
		"workbench.action.openSettings|",
		"workbench.action.showCommands|ctrl+shift+p",
		"cursorLeft|",
	}
	got := r.shown()
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows\n got %q\nwant %q", got, want)
	}
	// the header
	for _, name := range []string{"Command", "Keybinding", "When", "Source"} {
		if !r.ts.Exists(gluttertest.ByText(name)) {
			t.Errorf("no column %q", name)
		}
	}
	// a user row says so, a command without a title shows its id
	if !r.ts.Exists(gluttertest.Descendant(r.rowFinder("editor.action.copyLinesDownAction", "ctrl+alt+d"), gluttertest.ByText("User"))) {
		t.Error("the user row has no source")
	}
	if !r.ts.Exists(gluttertest.Descendant(r.rowFinder("cursorLeft", ""), gluttertest.ByText("cursorLeft"))) {
		t.Error("the command without a title shows no id")
	}
	if !r.ts.Exists(gluttertest.Descendant(r.rowFinder("workbench.action.showCommands", ""), gluttertest.ByText("View: Show All Commands"))) {
		t.Error("the category is not shown")
	}
	r.noErrors()
}

func TestSearchNarrowsTheRows(t *testing.T) {
	r := newRig(t, rigOpts{userText: userFile})
	r.search("copy line")
	got := r.shown()
	want := []string{"editor.action.copyLinesDownAction|ctrl+alt+d", "editor.action.copyLinesDownAction|shift+alt+down", "editor.action.copyLinesUpAction|shift+alt+up"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows %q, want %q", got, want)
	}
	// a search that finds nothing leaves the header alone
	r.search("zzzzz")
	if got := r.shown(); len(got) != 0 {
		t.Errorf("rows %q", got)
	}
	if !r.ts.Exists(gluttertest.ByText("Command")) {
		t.Error("the header went")
	}
	// the clear button empties the search
	r.search("copy")
	r.ts.Tap(gluttertest.ByKey(foundation.NewValueKey("keybindingsui-clear")))
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
	if r.ctl.Query() != "" || len(r.shown()) != 10 {
		t.Errorf("query %q, %d rows", r.ctl.Query(), len(r.shown()))
	}
	if last := r.states[len(r.states)-1]; last.Query != "" {
		t.Errorf("the state %+v", last)
	}
	r.noErrors()
}

func TestSearchByFilters(t *testing.T) {
	r := newRig(t, rigOpts{userText: userFile})
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"@source:user", []string{"editor.action.copyLinesDownAction|ctrl+alt+d"}},
		{`"ctrl+x"`, []string{"editor.action.clipboardCutAction|ctrl+x", "other.cut|ctrl+x"}},
		{`"ctrl+k ctrl+c"`, []string{"editor.action.addCommentLine|ctrl+k ctrl+c"}},
		{"@command:editor.action.clipboardCopyAction", []string{"editor.action.clipboardCopyAction|ctrl+c"}},
	} {
		r.search(tc.query)
		if got := r.shown(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: rows %q, want %q", tc.query, got, tc.want)
		}
	}
	r.noErrors()
}

func TestSearchHighlightsWhatMatched(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.search("line up")
	row := r.rowFinder("editor.action.copyLinesUpAction", "")
	// the matching words are in their own bold spans
	texts := r.ts.Widgets(gluttertest.Descendant(row, gluttertest.ByType[*widgetsRichText]()))
	if len(texts) == 0 {
		t.Fatal("no highlighted text in the row")
	}
	r.noErrors()
}

func TestRecordKeysFillsTheSearch(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.ts.Tap(gluttertest.ByKey(foundation.NewValueKey("keybindingsui-record")))
	r.ts.Pump(time.Millisecond)
	if !r.ctl.Recording() || r.recorder == nil || r.records != 1 {
		t.Fatalf("recording %v, recorder %v", r.ctl.Recording(), r.recorder)
	}
	// ctrl+k ctrl+c, as the keys are pressed
	r.press(letter('k'), ctrl)
	if r.ctl.Query() != `"ctrl+k"` {
		t.Errorf("query %q", r.ctl.Query())
	}
	r.press(letter('c'), ctrl)
	if r.ctl.Query() != `"ctrl+k ctrl+c"` {
		t.Errorf("query %q", r.ctl.Query())
	}
	if got := r.shown(); !reflect.DeepEqual(got, []string{"editor.action.addCommentLine|ctrl+k ctrl+c"}) {
		t.Errorf("rows %q", got)
	}
	// a third chord starts over
	r.press(letter('x'), ctrl)
	if r.ctl.Query() != `"ctrl+x"` {
		t.Errorf("query %q", r.ctl.Query())
	}
	// Enter is a key here, the second chord
	r.press(enter)
	if r.ctl.Query() != `"ctrl+x enter"` {
		t.Errorf("query %q", r.ctl.Query())
	}
	// Escape ends the recording and leaves what it found
	r.press(esc)
	if r.ctl.Recording() || r.recorder != nil || r.ctl.Query() != `"ctrl+x enter"` {
		t.Errorf("recording %v recorder %v query %q", r.ctl.Recording(), r.recorder, r.ctl.Query())
	}
	r.noErrors()
}

func TestRecordKeysEndsWhenTheEditorGoes(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.ctl.RecordSearchKeys()
	r.ts.Pump(time.Millisecond)
	if r.recorder == nil {
		t.Fatal("not recording")
	}
	r.ts.PumpWidget(kit.NewTheme(kit.DarkModern(), nilBox()))
	if r.recorder != nil {
		t.Error("the recording mode is left on")
	}
	r.noErrors()
}

func TestDefineRecordsAKeybinding(t *testing.T) {
	r := newRig(t, rigOpts{userText: userFile})
	r.tapRow("editor.action.copyLinesUpAction", "")
	r.ctl.Define(false)
	r.ts.Pump(time.Millisecond)
	if r.recorder == nil || !r.ts.Exists(gluttertest.ByText(DefineMessage)) {
		t.Fatal("the box is not shown")
	}
	// modifiers alone are shown as they are pressed
	r.ts.SendKeyDownEvent(ctrl)
	r.ts.Pump(time.Millisecond)
	if got := textOfKey(r, "keybindingsui-define-input"); got != "ctrl" {
		t.Errorf("after Ctrl: %q", got)
	}
	r.ts.SendKeyDownEvent(shift)
	r.ts.SendKeyDownEvent(letter('9'))
	r.ts.SendKeyUpEvent(letter('9'))
	r.ts.SendKeyUpEvent(shift)
	r.ts.SendKeyUpEvent(ctrl)
	r.ts.Pump(time.Millisecond)
	if got := textOfKey(r, "keybindingsui-define-input"); got != "ctrl+shift+9" {
		t.Errorf("the keys %q", got)
	}
	for _, cap := range []string{"Ctrl", "Shift", "9"} {
		if !r.ts.Exists(gluttertest.Descendant(gluttertest.ByKey(foundation.NewValueKey("keybindingsui-define")), gluttertest.ByText(cap))) {
			t.Errorf("no key cap %q", cap)
		}
	}
	// no command has it
	if r.ts.Exists(gluttertest.ByText(ExistingText(1))) {
		t.Error("a conflict is shown")
	}
	r.press(enter)
	if want := []string{"change editor.action.copyLinesUpAction ctrl+shift+9"}; !reflect.DeepEqual(r.calls, want) {
		t.Errorf("calls %q, want %q", r.calls, want)
	}
	if r.recorder != nil || r.ts.Exists(gluttertest.ByText(DefineMessage)) {
		t.Error("the box stays")
	}
	r.noErrors()
}

func TestDefineTwoChordsAndRestart(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.tapRow("editor.action.copyLinesUpAction", "")
	r.ctl.Define(true)
	r.ts.Pump(time.Millisecond)
	r.press(letter('k'), ctrl)
	r.press(letter('m'), ctrl)
	if got := textOfKey(r, "keybindingsui-define-input"); got != "ctrl+k ctrl+m" {
		t.Errorf("the keys %q", got)
	}
	if !r.ts.Exists(gluttertest.ByText(ChordTo)) {
		t.Error("no \"chord to\"")
	}
	// the third chord starts over
	r.press(letter('j'), ctrl)
	if got := textOfKey(r, "keybindingsui-define-input"); got != "ctrl+j" {
		t.Errorf("the keys %q", got)
	}
	r.press(enter)
	if want := []string{"add editor.action.copyLinesUpAction ctrl+j"}; !reflect.DeepEqual(r.calls, want) {
		t.Errorf("calls %q, want %q", r.calls, want)
	}
	r.noErrors()
}

func TestDefineEscapeClearsThenCancels(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.tapRow("editor.action.copyLinesUpAction", "")
	r.ctl.Define(false)
	r.ts.Pump(time.Millisecond)
	r.press(letter('9'), ctrl)
	r.press(esc)
	if got := textOfKey(r, "keybindingsui-define-input"); got != "" || r.recorder == nil {
		t.Errorf("the first Escape: keys %q, recording %v", got, r.recorder != nil)
	}
	r.press(esc)
	if r.recorder != nil || r.ts.Exists(gluttertest.ByText(DefineMessage)) || len(r.calls) != 0 {
		t.Errorf("the second Escape: recording %v calls %q", r.recorder != nil, r.calls)
	}
	// Enter with nothing recorded changes nothing
	r.ctl.Define(false)
	r.ts.Pump(time.Millisecond)
	r.press(enter)
	if len(r.calls) != 0 || r.recorder != nil {
		t.Errorf("calls %q recording %v", r.calls, r.recorder != nil)
	}
	// the same key as the row has changes nothing
	r.ctl.Define(false)
	r.ts.Pump(time.Millisecond)
	r.press(letter('p'), ctrl, shift)
	r.press(enter)
	r.ts.Pump(time.Millisecond)
	_ = alt
	r.noErrors()
}

func TestDefineClickOutsideCancels(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.tapRow("editor.action.copyLinesUpAction", "")
	r.ctl.Define(false)
	r.ts.Pump(time.Millisecond)
	r.ts.TapAt(ui.Offset{Dx: 10, Dy: 500}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.ts.Pump(time.Millisecond)
	if r.recorder != nil || r.ts.Exists(gluttertest.ByText(DefineMessage)) {
		t.Error("the box stays")
	}
	r.noErrors()
}

func TestTheEditButtonOfARowDefinesIt(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.tapRow("editor.action.copyLinesUpAction", "")
	buttons := r.ts.Widgets(gluttertest.Descendant(r.rowFinder("editor.action.copyLinesUpAction", ""), gluttertest.ByType[*kit.IconButton]()))
	if len(buttons) != 1 || buttons[0].(*kit.IconButton).Tooltip() != "Change Keybinding (Enter)" {
		t.Fatalf("buttons %v", buttons)
	}
	r.ts.Tap(gluttertest.Descendant(r.rowFinder("editor.action.copyLinesUpAction", ""), gluttertest.ByType[*kit.IconButton]()))
	r.ts.Pump(time.Millisecond)
	if !r.ts.Exists(gluttertest.ByText(DefineMessage)) {
		t.Error("the box is not shown")
	}
	// a command without a key has the plus
	r.press(esc)
	r.press(esc)
	r.tapRow("cursorLeft", "")
	b := r.ts.Widgets(gluttertest.Descendant(r.rowFinder("cursorLeft", ""), gluttertest.ByType[*kit.IconButton]()))
	if len(b) != 1 || !strings.HasPrefix(b[0].(*kit.IconButton).Tooltip(), "Add Keybinding") {
		t.Errorf("buttons %v", b)
	}
	// only the row that is selected has it
	if r.ts.Exists(gluttertest.Descendant(r.rowFinder("editor.action.copyLinesUpAction", ""), gluttertest.ByType[*kit.IconButton]())) {
		t.Error("a row that is not selected has the button")
	}
	r.noErrors()
}

func TestADoubleClickDefinesTheRow(t *testing.T) {
	r := newRig(t, rigOpts{})
	at := r.ts.Center(r.rowFinder("editor.action.copyLinesUpAction", ""))
	mouse := gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse}
	r.ts.TapAt(at, mouse)
	r.ts.TapAt(at, mouse)
	r.ts.Pump(time.Millisecond)
	if !r.ts.Exists(gluttertest.ByText(DefineMessage)) {
		t.Error("the box is not shown")
	}
	r.noErrors()
}

func TestDefineWarnsOfTheCommandsThatHaveTheKeys(t *testing.T) {
	r := newRig(t, rigOpts{userText: userFile})
	r.tapRow("editor.action.copyLinesUpAction", "")
	r.ctl.Define(false)
	r.ts.Pump(time.Millisecond)
	r.press(letter('x'), ctrl)
	if !r.ts.Exists(gluttertest.ByText(ExistingText(2))) {
		t.Fatal("the box does not say that 2 commands have the keys")
	}
	for _, name := range []string{"Cut (textInputFocus)", "Cut Other (editorFocus)"} {
		if !r.ts.Exists(gluttertest.ByText("- " + name)) {
			t.Errorf("%q is not listed", name)
		}
	}
	// the keys of a single command say "1 existing command"
	r.press(esc)
	r.press(letter('c'), ctrl)
	if !r.ts.Exists(gluttertest.ByText(ExistingText(1))) {
		t.Error("the box does not say that 1 command has the keys")
	}
	// a click on the line searches for the keys
	r.ts.Tap(gluttertest.ByText(ExistingText(1)))
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
	if r.ctl.Query() != `"Ctrl+C"` || r.recorder != nil {
		t.Errorf("query %q, recording %v", r.ctl.Query(), r.recorder != nil)
	}
	if got := r.shown(); !reflect.DeepEqual(got, []string{"editor.action.clipboardCopyAction|ctrl+c"}) {
		t.Errorf("rows %q", got)
	}
	r.noErrors()
}

func TestConflictDialog(t *testing.T) {
	for _, tc := range []struct {
		name   string
		button string
		want   []string
	}{
		{"keep both", KeepBoth, []string{"change editor.action.copyLinesUpAction ctrl+x"}},
		{"overwrite", Overwrite, []string{"change editor.action.copyLinesUpAction ctrl+x overwrite:editor.action.clipboardCutAction overwrite:other.cut"}},
		{"cancel", Cancel, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, rigOpts{})
			r.tapRow("editor.action.copyLinesUpAction", "")
			r.ctl.Define(false)
			r.ts.Pump(time.Millisecond)
			r.press(letter('x'), ctrl)
			r.press(enter)
			r.ts.Pump(time.Millisecond)
			if !r.dialogs.IsShowing() {
				t.Fatal("no dialog")
			}
			spec, _ := r.dialogs.Current()
			if spec.Message != ConflictsTitle || !strings.Contains(spec.Detail, "Cut (textInputFocus)") || !strings.Contains(spec.Detail, "Cut Other (editorFocus)") ||
				!slices.Equal(spec.Buttons, []string{KeepBoth, Overwrite, Cancel}) {
				t.Errorf("dialog %+v", spec)
			}
			if len(r.calls) != 0 {
				t.Errorf("the keybinding was made before the answer: %q", r.calls)
			}
			r.ts.Tap(gluttertest.ByText(tc.button))
			r.ts.Pump(time.Millisecond)
			r.ts.Pump(time.Millisecond)
			if !reflect.DeepEqual(r.calls, tc.want) {
				t.Errorf("calls %q, want %q", r.calls, tc.want)
			}
			if r.dialogs.IsShowing() {
				t.Error("the dialog stays")
			}
			r.noErrors()
		})
	}
}

func TestADialogIsNotShownWithoutConflicts(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.tapRow("editor.action.copyLinesUpAction", "")
	r.ctl.Define(false)
	r.ts.Pump(time.Millisecond)
	r.press(letter('8'), ctrl)
	r.press(enter)
	if r.dialogs.IsShowing() || len(r.calls) != 1 {
		t.Errorf("dialog %v calls %q", r.dialogs.IsShowing(), r.calls)
	}
	r.noErrors()
}

// The table refuses rows with the same id; NewModel never makes them, so the test gives the editor
// a model of two rows that are the same.
func TestATableOfRowsThatRepeatIsReported(t *testing.T) {
	r := newRig(t, rigOpts{})
	row := keybinding.Row{ID: "same", Command: "editor.action.clipboardCopyAction", Label: "Copy"}
	r.model = keybinding.NewModelOfRows([]keybinding.Row{row, row})
	for _, f := range r.changed {
		f()
	}
	r.ts.Pump(time.Millisecond)
	if len(r.errs) == 0 {
		t.Fatal("no error was reported")
	}
	appErr, ok := errors.FromError(r.errs[0])
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", r.errs[0], r.errs[0])
	}
	if !appErr.HasRoute("editorState.refresh->ListViewModel.SetItems.ListItemKeyDuplicated") || appErr.Tag != "ListItemKeyDuplicated" {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestAFailedEditIsReported(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.failing = errFailed
	r.tapRow("editor.action.copyLinesUpAction", "")
	r.ctl.Define(false)
	r.ts.Pump(time.Millisecond)
	r.press(letter('8'), ctrl)
	r.press(enter)
	if len(r.errs) != 1 {
		t.Fatalf("errors %v", r.errs)
	}
	r.errs = nil
	r.ctl.Remove()
	if len(r.errs) != 1 {
		t.Errorf("errors %v", r.errs)
	}
	r.errs = nil
	r.noErrors()
}

func TestRemoveAndReset(t *testing.T) {
	r := newRig(t, rigOpts{userText: userFile})
	// a row without a key has nothing to remove
	r.tapRow("cursorLeft", "")
	r.ctl.Remove()
	if len(r.calls) != 0 {
		t.Errorf("calls %q", r.calls)
	}
	r.tapRow("editor.action.copyLinesDownAction", "shift+alt+down")
	r.ctl.Remove()
	r.tapRow("editor.action.copyLinesDownAction", "ctrl+alt+d")
	r.ctl.Reset()
	if want := []string{"remove editor.action.copyLinesDownAction shift+alt+down", "reset editor.action.copyLinesDownAction"}; !reflect.DeepEqual(r.calls, want) {
		t.Errorf("calls %q, want %q", r.calls, want)
	}
	r.noErrors()
}

func TestTheContextMenuOfARow(t *testing.T) {
	r := newRig(t, rigOpts{userText: userFile})
	rightClick := func(command, key string) []kit.MenuItem {
		r.ts.TapAt(r.ts.Center(r.rowFinder(command, key)), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)})
		r.ts.Pump(time.Millisecond)
		r.ts.Pump(time.Millisecond)
		menus := r.ts.Widgets(gluttertest.ByType[*kit.Menu]())
		if len(menus) != 1 {
			t.Fatalf("%d menus", len(menus))
		}
		return menus[0].(*kit.Menu).Items()
	}
	labels := func(items []kit.MenuItem) []string {
		var out []string
		for _, it := range items {
			if it.Separator {
				out = append(out, "-")
			} else if it.Disabled {
				out = append(out, "("+it.Label+")")
			} else {
				out = append(out, it.Label)
			}
		}
		return out
	}
	items := rightClick("editor.action.copyLinesDownAction", "shift+alt+down")
	want := []string{"Copy", "Copy Command ID", "Copy Command Title", "-", "Change Keybinding...", "Add Keybinding...", "-", "Remove Keybinding", "(Reset Keybinding)", "-", "Change When Expression", "-", "Show Same Keybindings"}
	if got := labels(items); !reflect.DeepEqual(got, want) {
		t.Errorf("menu of a built-in keybinding\n got %q\nwant %q", got, want)
	}
	if items[4].Keybinding != "Enter" {
		t.Errorf("Change Keybinding has the key %q", items[4].Keybinding)
	}
	r.press(esc)
	// a user keybinding can be reset
	items = rightClick("editor.action.copyLinesDownAction", "ctrl+alt+d")
	if got := labels(items); got[8] != "Reset Keybinding" {
		t.Errorf("menu of a user keybinding %q", got)
	}
	r.press(esc)
	// a command without a key can only be given one
	items = rightClick("cursorLeft", "")
	want = []string{"Copy", "Copy Command ID", "(Copy Command Title)", "-", "Add Keybinding...", "-", "(Remove Keybinding)", "(Reset Keybinding)", "-", "(Change When Expression)", "-", "(Show Same Keybindings)"}
	if got := labels(items); !reflect.DeepEqual(got, want) {
		t.Errorf("menu of a command without a key\n got %q\nwant %q", got, want)
	}
	r.press(esc)
	r.noErrors()
}

func TestTheContextMenuRuns(t *testing.T) {
	r := newRig(t, rigOpts{userText: userFile})
	choose := func(command, key, label string) {
		r.ts.TapAt(r.ts.Center(r.rowFinder(command, key)), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)})
		r.ts.Pump(time.Millisecond)
		r.ts.Pump(time.Millisecond)
		r.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Menu](), gluttertest.ByText(label)))
		r.ts.Pump(time.Millisecond)
		r.ts.Pump(time.Millisecond)
	}
	choose("editor.action.copyLinesUpAction", "", "Copy")
	choose("editor.action.copyLinesUpAction", "", "Copy Command ID")
	choose("editor.action.copyLinesUpAction", "", "Copy Command Title")
	want := []string{"{\n  \"key\": \"shift+alt+up\",\n  \"command\": \"editor.action.copyLinesUpAction\",\n  \"when\": \"editorTextFocus && !editorReadonly\"\n}", "editor.action.copyLinesUpAction", "Copy Line Up"}
	if !reflect.DeepEqual(r.copied, want) {
		t.Errorf("copied %q, want %q", r.copied, want)
	}
	choose("editor.action.copyLinesUpAction", "", "Remove Keybinding")
	choose("editor.action.copyLinesDownAction", "ctrl+alt+d", "Reset Keybinding")
	if want := []string{"remove editor.action.copyLinesUpAction shift+alt+up", "reset editor.action.copyLinesDownAction"}; !reflect.DeepEqual(r.calls, want) {
		t.Errorf("calls %q, want %q", r.calls, want)
	}
	choose("editor.action.clipboardCopyAction", "", "Show Same Keybindings")
	if r.ctl.Query() != `"Ctrl+C"` {
		t.Errorf("query %q", r.ctl.Query())
	}
	choose("editor.action.clipboardCopyAction", "", "Add Keybinding...")
	if !r.ts.Exists(gluttertest.ByText(DefineMessage)) {
		t.Error("the box is not shown")
	}
	r.press(esc)
	r.press(esc)
	choose("editor.action.clipboardCopyAction", "", "Change Keybinding...")
	if !r.ts.Exists(gluttertest.ByText(DefineMessage)) {
		t.Error("the box is not shown")
	}
	r.noErrors()
}

func TestTheWhenClauseIsEdited(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.tapRow("editor.action.copyLinesUpAction", "")
	r.ctl.DefineWhen()
	r.ts.Pump(time.Millisecond)
	input := gluttertest.ByKey(foundation.NewValueKey("keybindingsui-when"))
	if !r.ts.Exists(input) {
		t.Fatal("no box")
	}
	if r.key("whenFocus") != true {
		t.Error("whenFocus is not set")
	}
	// a text that is not a when-clause says so and keeps the box
	r.typeIn(input, "editorFocus &&")
	r.press(enter)
	if len(r.calls) != 0 || !r.ts.Exists(input) {
		t.Fatalf("calls %q, box %v", r.calls, r.ts.Exists(input))
	}
	if !r.ts.Exists(gluttertest.ByTextContaining("Not a when-clause")) {
		t.Error("the error is not shown")
	}
	r.typeIn(input, "editorFocus && !editorReadonly")
	r.press(enter)
	if want := []string{"when editor.action.copyLinesUpAction editorFocus && !editorReadonly"}; !reflect.DeepEqual(r.calls, want) {
		t.Errorf("calls %q, want %q", r.calls, want)
	}
	if r.ts.Exists(input) {
		t.Error("the box stays")
	}
	// Escape gives it up
	r.calls = nil
	r.ctl.DefineWhen()
	r.ts.Pump(time.Millisecond)
	r.typeIn(input, "isLinux")
	r.press(esc)
	if len(r.calls) != 0 || r.ts.Exists(input) {
		t.Errorf("calls %q, box %v", r.calls, r.ts.Exists(input))
	}
	// a row without a key has no when-clause to edit
	r.tapRow("cursorLeft", "")
	r.ctl.DefineWhen()
	r.ts.Pump(time.Millisecond)
	if r.ts.Exists(input) {
		t.Error("a box for a row without a key")
	}
	r.noErrors()
}

func TestTheContextKeys(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.ctl.FocusSearch()
	r.ts.Pump(time.Millisecond)
	for k, want := range map[string]any{"inKeybindings": true, "inKeybindingsSearch": true, "inputFocus": true, "keybindingsSearchHasValue": false, "keybindingFocus": nil} {
		if got := r.key(k); got != want {
			t.Errorf("search focused: %s = %v, want %v", k, got, want)
		}
	}
	r.search("copy")
	if r.key("keybindingsSearchHasValue") != true {
		t.Error("keybindingsSearchHasValue is not set")
	}
	r.ctl.FocusRows()
	r.ts.Pump(time.Millisecond)
	for k, want := range map[string]any{"inKeybindings": true, "keybindingFocus": true, "inKeybindingsSearch": nil, "inputFocus": nil} {
		if got := r.key(k); got != want {
			t.Errorf("rows focused: %s = %v, want %v", k, got, want)
		}
	}
	r.noErrors()
}

func TestTheOpenJSONButton(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.ts.Tap(gluttertest.ByKey(foundation.NewValueKey("keybindingsui-open-json")))
	if r.opened != 1 {
		t.Errorf("opened %d", r.opened)
	}
	r.noErrors()
}

func TestTheViewStateIsKeptAndRestored(t *testing.T) {
	r := newRig(t, rigOpts{userText: userFile})
	r.tapRow("editor.action.copyLinesDownAction", "ctrl+alt+d")
	if got := r.ctl.ViewState(); got.Command != "editor.action.copyLinesDownAction" || got.Key != "ctrl+alt+d" {
		t.Errorf("view state %+v", got)
	}
	last := r.states[len(r.states)-1]
	if last.Command != "editor.action.copyLinesDownAction" || last.Key != "ctrl+alt+d" {
		t.Errorf("reported %+v", last)
	}
	r.search("copy line")
	if got := r.ctl.ViewState(); got.Query != "copy line" {
		t.Errorf("view state %+v", got)
	}
	// the editor comes back with the search and the row
	b := newRig(t, rigOpts{userText: userFile, state: editorgroup.KeybindingsViewState{Query: "copy line", Command: "editor.action.copyLinesDownAction", Key: "ctrl+alt+d"}})
	b.ts.Pump(time.Millisecond)
	b.ts.Pump(time.Millisecond)
	if b.ctl.Query() != "copy line" {
		t.Errorf("query %q", b.ctl.Query())
	}
	if row, ok := b.ctl.Selected(); !ok || row.Command != "editor.action.copyLinesDownAction" || row.Key != "ctrl+alt+d" {
		t.Errorf("selected %+v %v", row, ok)
	}
	if len(b.shown()) != 3 {
		t.Errorf("rows %q", b.shown())
	}
	r.noErrors()
	b.noErrors()
}

func TestTheTableFollowsTheModel(t *testing.T) {
	r := newRig(t, rigOpts{})
	if r.has("editor.action.copyLinesDownAction", "ctrl+alt+d") {
		t.Fatal("the row is there already")
	}
	r.reselectAfter = func() {
		r.setUser(userFile)
		r.reload()
	}
	r.tapRow("editor.action.copyLinesDownAction", "shift+alt+down")
	r.ctl.Define(false)
	r.ts.Pump(time.Millisecond)
	r.press(letter('d'), ctrl, alt)
	r.press(enter)
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
	if !r.has("editor.action.copyLinesDownAction", "ctrl+alt+d") {
		t.Errorf("the new row is not shown: %q", r.shown())
	}
	// and it is the selected one
	if row, ok := r.ctl.Selected(); !ok || row.Key != "ctrl+alt+d" {
		t.Errorf("selected %+v", row)
	}
	r.noErrors()
}
