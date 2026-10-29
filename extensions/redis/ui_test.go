package redis

import (
	"context"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// refreshTab is an editor tab whose Refresh builds the widget under test again, as the window does with the box of a tab.
type refreshTab struct {
	extension.EditorTab
	changed *foundation.ChangeNotifier
}

// Refresh implements extension.EditorTab.
func (t refreshTab) Refresh() {
	t.EditorTab.Refresh()
	t.changed.NotifyListeners()
}

// showKeyTab shows the key editor in a window; it builds again when the tab is refreshed.
func showKeyTab(t *testing.T, th uitest.Theme, kt *keyTab, w, h float64) *gluttertest.Tester {
	t.Helper()
	changed := foundation.NewChangeNotifier()
	kt.tab = refreshTab{kt.tab, changed}
	body := widgets.NewListenableBuilder(changed, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget { return kt.build(bc) }, nil)
	return uitest.Pump(t, th, gui.Size{Width: w, Height: h}, body)
}

// showNewKey shows the New Key form in a window.
func showNewKey(t *testing.T, th uitest.Theme, nt *newKeyTab, w, h float64) *gluttertest.Tester {
	t.Helper()
	changed := foundation.NewChangeNotifier()
	nt.tab = refreshTab{nt.tab, changed}
	body := widgets.NewListenableBuilder(changed, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget { return nt.build(bc) }, nil)
	return uitest.Pump(t, th, gui.Size{Width: w, Height: h}, body)
}

func hasText(ts *gluttertest.Tester, text string) bool {
	return ts.Exists(gluttertest.ByTextContaining(text))
}

func TestTheHashEditorDrawsItsRows(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "user:42:profile")
	ts := showKeyTab(t, uitest.Themes()[0], kt, 800, 420)
	for _, want := range []string{"user:42:profile", "Hash", "3 fields", "TTL no expiry", "Add Field", "Edit", "Remove", "Rename", "Set TTL", "Persist", "Delete", "name", "Ada", "ada@example.com"} {
		if !hasText(ts, want) {
			t.Errorf("no %q on screen", want)
		}
	}
}

func TestPressingAddFieldShowsTheFormAndSaving(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "user:42:profile")
	ts := showKeyTab(t, uitest.Themes()[0], kt, 800, 640)
	ts.Tap(gluttertest.ByTextContaining("Add Field"))
	ts.Pump()
	if kt.form == nil || !hasText(ts, "Add field") {
		t.Fatalf("no form after the button: form %v", kt.form)
	}
	ts.EnterText(gluttertest.At(gluttertest.ByType[*ui.InputBox](), 0), "city")
	ts.EnterText(gluttertest.At(gluttertest.ByType[*ui.InputBox](), 1), "Athens")
	ts.Pump()
	if kt.form.ctl[0].Value() != "city" || kt.form.ctl[1].Value() != "Athens" {
		t.Fatalf("the form holds %q, %q", kt.form.ctl[0].Value(), kt.form.ctl[1].Value())
	}
	ts.Tap(gluttertest.At(gluttertest.ByTextContaining("Save"), 0))
	r.idle(kt)
	ts.Pump()
	if kt.form != nil || !hasText(ts, "Athens") {
		t.Errorf("after Save: form %v, Athens on screen %v", kt.form != nil, hasText(ts, "Athens"))
	}
}

func TestTypingInTheStringEditorMarksTheTab(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "greeting")
	ts := showKeyTab(t, uitest.Themes()[0], kt, 800, 400)
	ts.EnterText(gluttertest.ByType[*ui.InputBox](), "!")
	ts.Pump()
	if !kt.dirty || !r.fakeTab(kt).Dirty() {
		t.Fatalf("typing did not mark the tab: editor %q", kt.editor.Value())
	}
	ts.Tap(gluttertest.At(gluttertest.ByTextContaining("Save"), 0))
	r.idle(kt)
	if got, _, _ := mustDB(t, r.store, 0).GetRange(context.Background(), "greeting", 100); !strings.HasSuffix(got, "!") && !strings.HasPrefix(got, "!") {
		t.Errorf("saved %q", got)
	}
}

func TestMessagesInPlaceOfAnEditor(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "greeting")
	ts := showKeyTab(t, uitest.Themes()[0], kt, 800, 300)
	kt.missing = true
	kt.tab.Refresh()
	ts.Pump()
	if !hasText(ts, "does not exist") {
		t.Error("no message for a missing key")
	}
	kt.missing, kt.disconnected = false, true
	kt.tab.Refresh()
	ts.Pump()
	if !hasText(ts, "connection is closed") {
		t.Error("no message for a closed connection")
	}
	kt.disconnected, kt.problem = false, "boom"
	kt.tab.Refresh()
	ts.Pump()
	if !hasText(ts, "cannot be read: boom") {
		t.Error("no message for a problem")
	}
	kt.problem, kt.info.Type = "", "TSDB-TYPE"
	kt.tab.Refresh()
	ts.Pump()
	if !hasText(ts, "does not show keys of the type") {
		t.Error("no message for a type the editor does not know")
	}
}
