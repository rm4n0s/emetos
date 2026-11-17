package markdown

import (
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// The text of the page of the preview is selectable and copied (the user, 2026-11-17: "on markdown preview I couldn't click
// on text to copy it").

const selectionDoc = "# Title\n\nThe first paragraph has words.\n\nThe second paragraph has more words.\n\n- one\n- two\n"

// rightClick presses and releases the secondary button of the mouse at a place.
func rightClick(ts *gluttertest.Tester, at gui.Offset) {
	ts.TapAt(at, gluttertest.GestureOpts{Kind: gui.PointerDeviceKindMouse, Buttons: foundation.Some(2)})
	ts.Pump()
}

// runMenuItem runs the item of the open context menu that shows the label.
func runMenuItem(t *testing.T, ts *gluttertest.Tester, label string) {
	t.Helper()
	f := gluttertest.ByText(label)
	if !ts.Exists(f) {
		t.Fatalf("the context menu has no %q", label)
	}
	ts.Tap(f)
	ts.Pump()
	ts.Pump()
}

// box is where the text a finder finds is on screen.
type box struct{ left, top, right, midY float64 }

func boxOf(ts *gluttertest.Tester, f gluttertest.Finder) box {
	a, b := ts.TopLeft(f), ts.BottomRight(f)
	return box{left: a.Dx, top: a.Dy, right: b.Dx, midY: (a.Dy + b.Dy) / 2}
}

// ctrlKey presses a key with Ctrl held.
func ctrlKey(ts *gluttertest.Tester, key services.LogicalKeyboardKey) {
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyControlLeft)
	ts.SendKeyEvent(key)
	ts.SendKeyUpEvent(services.LogicalKeyboardKeyControlLeft)
	ts.Pump()
}

// dragOver drags the mouse with the primary button from one place to another.
func dragOver(ts *gluttertest.Tester, from, to gui.Offset) {
	g := ts.StartGesture(from, gluttertest.GestureOpts{Kind: gui.PointerDeviceKindMouse})
	ts.Pump()
	for i := 1; i <= 8; i++ {
		f := float64(i) / 8
		g.MoveTo(gui.Offset{Dx: from.Dx + (to.Dx-from.Dx)*f, Dy: from.Dy + (to.Dy-from.Dy)*f})
		ts.Pump()
	}
	g.Up()
	ts.Pump()
}

func TestTheTextOfThePreviewIsSelectedAndCopied(t *testing.T) {
	tests := []struct {
		name string
		// selectText makes the selection on the preview
		selectText func(t *testing.T, ts *gluttertest.Tester)
		want       []string // what the copied text contains
		wantNot    []string // and does not
	}{
		{"a drag over a paragraph", func(t *testing.T, ts *gluttertest.Tester) {
			r := boxOf(ts, textOf("The first paragraph"))
			dragOver(ts, gui.Offset{Dx: r.left + 1, Dy: r.midY}, gui.Offset{Dx: r.right - 1, Dy: r.midY})
		}, []string{"first paragraph"}, []string{"Title", "second"}},
		{"a drag from a paragraph into the next", func(t *testing.T, ts *gluttertest.Tester) {
			a, b := boxOf(ts, textOf("The first paragraph")), boxOf(ts, textOf("The second paragraph"))
			dragOver(ts, gui.Offset{Dx: a.left + 1, Dy: a.midY}, gui.Offset{Dx: b.left + 60, Dy: b.midY})
		}, []string{"first paragraph has words.\nThe sec"}, []string{"Title"}},
		{"Select All in the context menu", func(t *testing.T, ts *gluttertest.Tester) {
			rightClick(ts, ts.Center(textOf("The first paragraph")))
			runMenuItem(t, ts, "Select All")
		}, []string{"Title", "first paragraph", "second paragraph has more words."}, nil},
		{"a list is copied without its markers, an item on each line", func(t *testing.T, ts *gluttertest.Tester) {
			rightClick(ts, ts.Center(textOf("The first paragraph")))
			runMenuItem(t, ts, "Select All")
		}, []string{"Title\nThe first paragraph has words.\nThe second paragraph has more words.\none\ntwo"}, []string{"•", "1."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, _ := pumpPreview(t, selectionDoc, Options{})
			ts.SetClipboardText("nothing yet")
			tt.selectText(t, ts)
			rightClick(ts, ts.Center(textOf("The second paragraph")))
			runMenuItem(t, ts, "Copy")
			got, ok := ts.ClipboardText()
			if !ok {
				t.Fatal("the clipboard has no text")
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("the copied text %q does not have %q", got, w)
				}
			}
			for _, w := range tt.wantNot {
				if strings.Contains(got, w) {
					t.Errorf("the copied text %q has %q", got, w)
				}
			}
		})
	}
}

func TestCopyIsDisabledWithNoSelection(t *testing.T) {
	ts, _ := pumpPreview(t, selectionDoc, Options{})
	ts.SetClipboardText("before")
	rightClick(ts, ts.Center(textOf("The first paragraph")))
	if !ts.Exists(gluttertest.ByText("Select All")) {
		t.Fatal("the right click opened no menu")
	}
	ts.Tap(gluttertest.ByText("Copy"))
	ts.Pump()
	if got, _ := ts.ClipboardText(); got != "before" {
		t.Errorf("Copy with nothing selected put %q on the clipboard", got)
	}
}

// The keys: the selection's focus takes Ctrl+C and Ctrl+A through the shortcuts of the app (WidgetsApp's text editing
// shortcuts; the window's dispatcher leaves Ctrl+A to it, see internal/gui/app).
func TestCtrlAAndCtrlCSelectAndCopyThePage(t *testing.T) {
	ts, _ := pumpPreview(t, selectionDoc, Options{})
	r := boxOf(ts, textOf("The first paragraph"))
	dragOver(ts, gui.Offset{Dx: r.left + 1, Dy: r.midY}, gui.Offset{Dx: r.left + 40, Dy: r.midY})
	ctrlKey(ts, services.LogicalKeyboardKeyKeyA)
	ctrlKey(ts, services.LogicalKeyboardKeyKeyC)
	got, _ := ts.ClipboardText()
	if !strings.Contains(got, "Title") || !strings.Contains(got, "second paragraph") {
		t.Errorf("after Ctrl+A and Ctrl+C the clipboard has %q", got)
	}
}

func TestTheSmallLooksAreNotSelectable(t *testing.T) {
	for _, opts := range []Options{{Hover: true}, {Flow: true}, {Compact: true}} {
		ts := uitest.Pump(t, uitest.Themes()[0], previewSize, NewView(selectionDoc, opts))
		ts.Pump()
		if ts.Exists(gluttertest.ByType[*widgets.SelectableRegion]()) {
			t.Errorf("the text of %+v is in a selectable region", opts)
		}
	}
}
