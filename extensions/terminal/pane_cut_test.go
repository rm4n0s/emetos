package terminal

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// The prompt the cut tests type at: the prompt text is 13 cells, so the command line starts at column 13 of row 0.
const cutPromptCols = 13

// sel is a selection from (col0, row0) to (col1, row1), both cells included, on a terminal with no scrollback yet.
func sel(col0, row0, col1, row1 int) selection {
	return selection{active: true, anchor: pos{line: int64(row0), col: col0}, head: pos{line: int64(row1), col: col1}}
}

func TestPlanCut(t *testing.T) {
	p := cutPromptCols
	tests := []struct {
		name   string
		size   gui.Size // zero: paneSize
		out    string
		sel    selection
		want   cutPlan
		wantOK bool
	}{
		{"a word at the end, the cursor after it", gui.Size{}, prompt + "echo hello", sel(p+5, 0, p+9, 0), cutPlan{move: 0, del: 5, text: "hello"}, true},
		{"a word before the cursor", gui.Size{}, prompt + "echo hello", sel(p, 0, p+3, 0), cutPlan{move: -6, del: 4, text: "echo"}, true},
		{"a word after the cursor", gui.Size{}, prompt + "echo hello\x1b[10D", sel(p+5, 0, p+9, 0), cutPlan{move: 10, del: 5, text: "hello"}, true},
		{"the cursor in the selection", gui.Size{}, prompt + "echo hello\x1b[3D", sel(p+5, 0, p+9, 0), cutPlan{move: 3, del: 5, text: "hello"}, true},
		{"the selected word reversed", gui.Size{}, prompt + "echo hello", sel(p+9, 0, p+5, 0), cutPlan{move: 0, del: 5, text: "hello"}, true},
		{"a selection past the end of the line stops at its last character", gui.Size{}, prompt + "echo hello", sel(p+5, 0, p+20, 0), cutPlan{move: 0, del: 5, text: "hello"}, true},
		{"a wide character takes one deletion", gui.Size{}, prompt + "echo 日本 x", sel(p+5, 0, p+8, 0), cutPlan{move: -2, del: 2, text: "日本"}, true},
		{"the right half of a wide character selects it", gui.Size{}, prompt + "echo 日本", sel(p+6, 0, p+6, 0), cutPlan{move: -1, del: 1, text: "日"}, true},
		{"a line that wraps onto the next row", gui.Size{Width: 200, Height: 200}, prompt + "echo hello world", sel(p+5, 0, 4, 1), cutPlan{}, true},
		{"the prompt is not the command line", gui.Size{}, prompt + "echo hello", sel(p-3, 0, p+3, 0), cutPlan{}, false},
		{"output above the prompt", gui.Size{}, "some output\r\n" + prompt + "echo", sel(0, 0, 3, 0), cutPlan{}, false},
		{"no shell integration", gui.Size{}, "user@host:~$ echo hello", sel(p+5, 0, p+9, 0), cutPlan{}, false},
		{"a command that runs", gui.Size{}, prompt + "sleep 9\r\n\x1b]633;C\a", sel(p, 0, p+4, 0), cutPlan{}, false},
		{"a full-screen program", gui.Size{}, prompt + "vim\x1b[?1049hsome text", sel(0, 0, 3, 0), cutPlan{}, false},
		{"a command of several lines", gui.Size{}, prompt + "echo \\\r\n> two", sel(2, 1, 4, 1), cutPlan{}, false},
		{"no selection", gui.Size{}, prompt + "echo hello", selection{}, cutPlan{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := paneSize
			if tt.size != (gui.Size{}) {
				size = tt.size
			}
			r := newFakePane(t, uitest.Themes()[0], size, nil)
			r.write(t, tt.out)
			want := tt.want
			if tt.name == "a line that wraps onto the next row" {
				// "hello world" wraps where the pane ends; the selection runs from "hello" to the fifth cell of the next row
				cols := r.pv.cols
				if cols <= p+5 || cols > p+16 {
					t.Skipf("the pane has %d columns, the line does not wrap after its fifth character", cols)
				}
				end := cols - p + 4 // characters of the line up to (4, 1), counted from its start
				want = cutPlan{move: end + 1 - 16, del: end + 1 - 5, text: "echo hello world"[5 : end+1]}
			}
			r.pv.setSelection(tt.sel)
			got, ok := r.pv.planCut()
			if ok != tt.wantOK {
				t.Fatalf("planCut ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != want {
				t.Errorf("planCut = %+v, want %+v", got, want)
			}
		})
	}
}

func TestCutCopiesTheSelectionAndDeletesItFromTheCommandLine(t *testing.T) {
	p := cutPromptCols
	tests := []struct {
		name     string
		out      string
		sel      selection
		wantText string
		wantSent string
	}{
		{"a word before the cursor", prompt + "echo hello", sel(p, 0, p+4, 0), "echo ", "\x1b[D\x1b[D\x1b[D\x1b[D\x1b[D\x7f\x7f\x7f\x7f\x7f"},
		{"a word after the cursor", prompt + "echo hello\x1b[10D", sel(p+5, 0, p+9, 0), "hello", "\x1b[C\x1b[C\x1b[C\x1b[C\x1b[C\x1b[C\x1b[C\x1b[C\x1b[C\x1b[C\x7f\x7f\x7f\x7f\x7f"},
		{"the cursor keys of a shell in application mode", prompt + "\x1b[?1hecho hello", sel(p, 0, p+4, 0), "echo ", "\x1bOD\x1bOD\x1bOD\x1bOD\x1bOD\x7f\x7f\x7f\x7f\x7f"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
			r.write(t, tt.out)
			r.pv.setSelection(tt.sel)
			r.pv.cutSelection()
			r.settle()
			if got := r.waitInput(t, tt.wantSent); got != tt.wantSent {
				t.Errorf("the shell received %q, want %q", got, tt.wantSent)
			}
			if text, ok := r.ts.ClipboardText(); !ok || text != tt.wantText {
				t.Errorf("the clipboard has %q (%v), want %q", text, ok, tt.wantText)
			}
			if r.pv.hasSelection() {
				t.Error("the selection is still there after Cut")
			}
		})
	}
}

func TestCutThatDoesNotApplyDoesNothing(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	r.write(t, "some output\r\n"+prompt+"echo")
	r.ts.SetClipboardText("before")
	r.pv.setSelection(sel(0, 0, 3, 0))
	r.pv.cutSelection()
	r.settle()
	time.Sleep(50 * time.Millisecond)
	if got := r.proc.input(); got != "" {
		t.Errorf("the shell received %q", got)
	}
	if text, _ := r.ts.ClipboardText(); text != "before" {
		t.Errorf("the clipboard has %q, want it unchanged", text)
	}
}

// menuItemsShown are which of the labels a menu on screen shows.
func menuItemsShown(ts *gluttertest.Tester, labels ...string) map[string]bool {
	out := map[string]bool{}
	for _, l := range labels {
		out[l] = ts.Exists(gluttertest.ByText(l))
	}
	return out
}

// secondaryTap presses and releases the secondary button of the mouse at a place.
func secondaryTap(ts *gluttertest.Tester, at gui.Offset) {
	ts.TapAt(at, gluttertest.GestureOpts{Kind: gui.PointerDeviceKindMouse, Buttons: foundation.Some(2)})
}

// The right click opens the menu of the terminal wherever the terminal is: the panel, an editor tab and a window tab (the user,
// 2026-11-17, saw no menu in the last two: only the panel hosted it). A window tab's menu has no Split, Move, Rename, Icon or
// Color.
func TestTheRightClickOpensTheMenuWhereverTheTerminalIs(t *testing.T) {
	labels := []string{"Cut", "Copy", "Paste", "Select All", "Clear", "Split Terminal", "Move Terminal into Panel",
		"Move Terminal into Editor Area", "Rename...", "Kill Terminal"}
	size := gui.Size{Width: 800, Height: 500}
	middle := gui.Offset{Dx: 200, Dy: 150}
	tests := []struct {
		name string
		// open shows the terminal and returns the tester and a function that runs what the extension posted
		open func(t *testing.T) (*gluttertest.Tester, func())
		want []string
	}{
		{"the panel", func(t *testing.T) (*gluttertest.Tester, func()) {
			r := newPanelRig(t, uitest.Themes()[0], size)
			r.waitShows(t, "$")
			return r.ts, r.settle
		}, []string{"Cut", "Copy", "Paste", "Select All", "Clear", "Split Terminal", "Move Terminal into Editor Area", "Rename...", "Kill Terminal"}},
		{"an editor tab", func(t *testing.T) (*gluttertest.Tester, func()) {
			r := newExtRig(t, "")
			r.newTerminalReady()
			r.run(CmdMoveToEditor, nil)
			tabs := openTabs(r)
			if len(tabs) != 1 {
				t.Fatalf("%d editor tabs, want 1", len(tabs))
			}
			ts := uitest.Pump(t, uitest.Themes()[0], size, widgets.NewBuilder(tabs[0].Content.Build))
			return ts, func() {
				for range 4 {
					r.api.Pump()
					ts.FlushPosted()
					ts.Pump(20 * time.Millisecond)
				}
			}
		}, []string{"Cut", "Copy", "Paste", "Select All", "Clear", "Split Terminal", "Move Terminal into Panel", "Rename...", "Kill Terminal"}},
		{"a window tab", func(t *testing.T) (*gluttertest.Tester, func()) {
			r := newWtRig(t, "")
			r.ready()
			ts := uitest.Pump(t, uitest.Themes()[0], size, widgets.NewBuilder(r.wt.Content.Build))
			return ts, func() {
				for range 4 {
					r.api.Pump()
					ts.FlushPosted()
					ts.Pump(20 * time.Millisecond)
				}
			}
		}, []string{"Cut", "Copy", "Paste", "Select All", "Clear", "Kill Terminal"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, settle := tt.open(t)
			settle()
			if shown := menuItemsShown(ts, "Kill Terminal"); shown["Kill Terminal"] {
				t.Fatal("the menu is open before the right click")
			}
			secondaryTap(ts, middle)
			settle()
			want := map[string]bool{}
			for _, l := range labels {
				want[l] = false
			}
			for _, l := range tt.want {
				want[l] = true
			}
			got := menuItemsShown(ts, labels...)
			for _, l := range labels {
				if got[l] != want[l] {
					t.Errorf("the menu shows %q = %v, want %v", l, got[l], want[l])
				}
			}
		})
	}
}
