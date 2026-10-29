package terminal

import (
	"slices"
	"strings"
	"testing"
	"time"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// suggestPane is a pane over a fake program that has had a session, with the suggester over the folders of suggestRig.
func suggestPane(t *testing.T, th uitest.Theme, size gui.Size, out string, tune func(*config)) (*paneRig, *suggestRig) {
	t.Helper()
	rig := newSuggestRig(t)
	r := shellPane(t, th, size, func(c *config) {
		c.sugEnabled, c.sugSelection, c.sugCdPath = true, "partial", "absolute"
		if tune != nil {
			tune(c)
		}
	}, out)
	r.env.suggester = rig.sg
	r.env.suggestEnv = func(*paneView) suggestEnv { return rig.env }
	r.pv.focused = true
	return r, rig
}

// openList triggers the list and waits for it.
func (r *paneRig) openList(t *testing.T) *sugSession {
	t.Helper()
	r.pv.suggestTrigger()
	deadline := time.Now().Add(5 * time.Second)
	for !r.pv.suggestVisible() {
		if time.Now().After(deadline) {
			t.Fatal("the list did not open")
		}
		r.settle()
		time.Sleep(5 * time.Millisecond)
	}
	r.settle()
	return r.pv.sug.sess
}

func TestGoldenSuggestList(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			var s session
			s.prompt("user@host:~/p$ ").run("ls", "a.txt  sub\r\n", 0).prompt("user@host:~/p$ ")
			r, _ := suggestPane(t, th, gui.Size{Width: 640, Height: 300}, s.String()+"git che", nil)
			sess := r.openList(t)
			if got := labelsIn(sess); got[0] != "checkout" {
				t.Fatalf("items = %q", got)
			}
			r.ts.Screenshot()
			uitest.MatchesGolden(r.ts, "pane_suggest_"+th.Name)
		})
	}
}

func TestGoldenSuggestListAboveTheCursor(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			var s session
			s.prompt("user@host:~/p$ ")
			// the prompt is on the last row, so the files are listed above it, with the details of the focused one
			r, rig := suggestPane(t, th, gui.Size{Width: 960, Height: 260}, "\r\n\r\n\r\n\r\n\r\n\r\n\r\n\r\n\r\n\r\n\r\n"+s.String()+"cat ", nil)
			sess := r.openList(t)
			// the folders of the test are made for it: the golden shows a home folder of a person
			for i := range sess.items {
				sess.items[i].detail = strings.ReplaceAll(sess.items[i].detail, rig.root, "/home/user")
			}
			sess.focus, sess.navigated = 3, true
			r.pv.suggestToggleDetails()
			r.settle()
			r.ts.Screenshot()
			uitest.MatchesGolden(r.ts, "pane_suggest_files_"+th.Name)
		})
	}
}

func TestSuggestListClosesWhenThePaneChanges(t *testing.T) {
	var s session
	s.prompt("$ ")
	tests := []struct {
		name string
		f    func(t *testing.T, r *paneRig)
	}{
		{"the view scrolls back", func(t *testing.T, r *paneRig) { r.pv.frame.Scrollback = 10; r.pv.scrollTo(3) }},
		{"the pane loses the focus", func(t *testing.T, r *paneRig) { r.pv.focusChanged(false) }},
		{"the pointer is pressed on the text", func(t *testing.T, r *paneRig) { r.pv.suggestHide() }},
		{"the program uses the alternate screen", func(t *testing.T, r *paneRig) {
			r.feed(t, "\x1b[?1049h")
			r.settle()
			r.pv.suggestObserve()
		}},
		{"the pane is disposed", func(t *testing.T, r *paneRig) { r.pv.dispose() }},
		{"Enter is typed", func(t *testing.T, r *paneRig) { r.pv.userInput([]byte("\r")) }},
		{"the setting is turned off", func(t *testing.T, r *paneRig) { r.cfg.sugEnabled = false; r.pv.configChanged() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := suggestPane(t, uitest.Themes()[0], paneSize, s.String()+"git ", nil)
			r.openList(t)
			tt.f(t, r)
			if r.pv.suggestVisible() {
				t.Errorf("the list is still shown")
			}
		})
	}
}

func TestClickOnARowAcceptsIt(t *testing.T) {
	var s session
	s.prompt("$ ")
	r, _ := suggestPane(t, uitest.Themes()[0], paneSize, s.String()+"git che", nil)
	sess := r.openList(t)
	want := sess.items[1].label
	if want != "cherry-pick" {
		t.Fatalf("the second item is %q", want)
	}
	r.pv.suggestAccept(1, false)
	deadline := time.Now().Add(2 * time.Second)
	for r.proc.input() != "rry-pick" {
		if time.Now().After(deadline) {
			t.Fatalf("the shell was sent %q, want the rest of the word", r.proc.input())
		}
		time.Sleep(time.Millisecond)
	}
	if r.pv.suggestVisible() {
		t.Errorf("the list is still shown after the item was accepted")
	}
}

func TestSuggestSessionKeepsItsFocusInView(t *testing.T) {
	var s session
	s.prompt("$ ")
	r, _ := suggestPane(t, uitest.Themes()[0], gui.Size{Width: 640, Height: 300}, s.String()+"cat ", nil)
	sess := r.openList(t)
	if len(sess.items) < 6 {
		t.Fatalf("only %d items", len(sess.items))
	}
	for i := 0; i < len(sess.items)+2; i++ {
		r.pv.suggestMove(1)
		r.settle()
		s := r.pv.sug.sess
		if s.focus < s.top || s.focus >= s.top+s.rows {
			t.Fatalf("after %d moves focus %d is outside %d..%d", i+1, s.focus, s.top, s.top+s.rows)
		}
	}
	// a page stops at the end; a step wraps
	r.pv.suggestMove(-1000)
	if r.pv.sug.sess.focus != 0 {
		t.Errorf("focus after a page up = %d", r.pv.sug.sess.focus)
	}
	r.pv.suggestMove(-1)
	if got := r.pv.sug.sess.focus; got != len(sess.items)-1 {
		t.Errorf("focus after Up on the first row = %d, want the last", got)
	}
	r.pv.suggestScroll(100)
	if s := r.pv.sug.sess; s.top != len(s.items)-s.rows {
		t.Errorf("top after the wheel = %d", s.top)
	}
	if got := labelsIn(r.pv.sug.sess); !slices.Contains(got, ".") {
		t.Errorf("the folder itself is not in the list: %q", got)
	}
}

func TestEveryKindHasAnIconThatExists(t *testing.T) {
	for kind := kindFile; kind <= kindHistory; kind++ {
		ic, ok := suggestIcons[kind]
		if !ok {
			t.Errorf("kind %d has no icon", kind)
			continue
		}
		if _, found := ui.CodiconCode(ic.icon); !found {
			t.Errorf("kind %d: the Codicon %q does not exist", kind, ic.icon)
		}
	}
}

// A drag over the text with the list open closes the list at the press and goes on selecting: the list does not take the rest of
// the drag (the widget is built again when it closes), also when the terminal scrolls sideways (step 17d.7).
func TestADragWithTheListOpenStillSelects(t *testing.T) {
	var s session
	s.prompt("$ ").run("echo alpha beta gamma", "alpha beta gamma\r\n", 0).prompt("$ ")
	r, _ := suggestPane(t, uitest.Themes()[0], paneSize, s.String()+"git che", nil)
	r.openList(t)
	// the first row is the output of the command: a drag from the "b" of beta to the last "a" of gamma
	row := 0
	for i := 0; i < r.pv.rows; i++ {
		if strings.HasPrefix(r.pv.frame.RowText(i), "alpha") {
			row = i
		}
	}
	from, to := r.cell(6, row), r.cell(15, row)
	r.ts.DragFrom(from, to.Sub(from))
	r.settle()
	if got := r.pv.selectedText(); got != "beta gamma" {
		t.Errorf("the selection is %q after a drag with the list open", got)
	}
	if r.pv.suggestVisible() {
		t.Errorf("the list is still shown after the press")
	}
}

func TestTheListFollowsTheSidewaysScroll(t *testing.T) {
	var s session
	s.prompt("$ ")
	r, _ := suggestPane(t, uitest.Themes()[0], paneSize, s.String()+"git che", func(c *config) {})
	r.pv.fixedCols = 200 // Toggle Size to Content Width on a terminal wider than its pane
	r.pv.render.MarkNeedsLayout()
	r.settle()
	r.write(t, "")
	r.openList(t)
	x := r.pv.sug.sess.x
	r.pv.scrollX(10)
	r.settle()
	if got := r.pv.sug.sess; got == nil || got.x != x-10 {
		t.Errorf("the list is at %+v after the text scrolled 10 pixels sideways from x=%v: it moves with the text", got, x)
	}
}
