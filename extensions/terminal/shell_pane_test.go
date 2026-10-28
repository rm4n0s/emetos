package terminal

import (
	"strconv"
	"strings"
	"testing"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// session builds the output of a shell that sends the sequences of shell integration: a prompt, a command, its output and its
// end (the text of the command line is read from the screen, as the pane's terminal has no secret).
type session struct{ b strings.Builder }

func (s *session) prompt(text string) *session {
	s.b.WriteString(osc("A") + text + osc("B"))
	return s
}

// run is the command being typed, run and ended with the exit status (-1 leaves it running).
func (s *session) run(command, output string, exit int) *session {
	s.b.WriteString(command + "\r\n" + osc("C") + output)
	if exit >= 0 {
		s.b.WriteString(osc("D;" + strconv.Itoa(exit)))
	}
	return s
}

func (s *session) String() string { return s.b.String() }

// shellPane shows a terminal that has had a session.
func shellPane(t *testing.T, th uitest.Theme, size gui.Size, tune func(*config), out string) *paneRig {
	t.Helper()
	r := newFakePane(t, th, size, tune)
	r.write(t, out)
	return r
}

func TestDecorationsMarkEachCommand(t *testing.T) {
	var s session
	s.prompt("$ ").run("true", "", 0).prompt("$ ").run("false", "", 1).prompt("$ ").run("echo hi", "hi\r\n", 0).prompt("$ ").run("sleep 5", "", -1)
	r := shellPane(t, uitest.Themes()[0], paneSize, nil, s.String())
	decos := r.pv.gutterDecorations(r.pv.frame.Top, r.pv.rows, nil)
	var kinds []decorationKind
	for _, d := range decos {
		kinds = append(kinds, d.kind)
	}
	// the three that ended, newest first, then the running one
	want := []decorationKind{decoSuccess, decoError, decoSuccess, decoDefault}
	if len(kinds) != 4 || kinds[0] != want[0] || kinds[1] != want[1] || kinds[2] != want[2] || kinds[3] != want[3] {
		t.Fatalf("decoration kinds = %v, want %v", kinds, want)
	}
	// the rows are those of the command lines
	text := func(row int) string { return r.pv.frame.RowText(row) }
	for _, d := range decos {
		if !strings.HasPrefix(text(d.row), "$ ") {
			t.Errorf("row %d (%q) is not a command line", d.row, text(d.row))
		}
	}
	// the setting turns them off, and so does the alternate screen
	r.cfg.decorations = "never"
	if got := r.pv.gutterDecorations(r.pv.frame.Top, r.pv.rows, nil); len(got) != 0 {
		t.Errorf("decorations with the setting never: %v", got)
	}
	r.cfg.decorations = "gutter"
	if got := r.pv.gutterDecorations(r.pv.frame.Top, r.pv.rows, nil); len(got) != 4 {
		t.Errorf("decorations with the setting gutter: %d", len(got))
	}
	r.cfg.decorations = "overviewRuler"
	if got := r.pv.gutterDecorations(r.pv.frame.Top, r.pv.rows, nil); len(got) != 0 {
		t.Errorf("the terminal has no overview ruler: %d decorations", len(got))
	}
}

func TestClickOnADecorationOpensItsMenu(t *testing.T) {
	var s session
	s.prompt("$ ").run("echo hi", "hi\r\n", 0).prompt("$ ").run("true", "", 0).prompt("$ ")
	r := shellPane(t, uitest.Themes()[0], paneSize, nil, s.String())
	var items []ui.MenuItem
	r.env.commandMenu = func(_ *paneView, it []ui.MenuItem, _ gui.Offset) { items = it }
	// the first command's line is the first row; the gutter is at the left of the first column
	a, _ := r.pv.atlases()
	d := r.pv.decorationAt(gui.Offset{Dx: 4, Dy: a.m.h/2 + a.m.h*float64(0)})
	if d == nil {
		t.Fatalf("no decoration on the first row; decorations: %+v", r.pv.gutterDecorations(r.pv.frame.Top, r.pv.rows, nil))
	}
	r.ts.TapAt(gui.Offset{Dx: 4, Dy: a.m.h / 2})
	r.settle()
	var labels []string
	for _, it := range items {
		labels = append(labels, it.Label)
		if it.Label == "Copy Output" && it.Disabled {
			t.Errorf("Copy Output is disabled for a command with output")
		}
	}
	if strings.Join(labels, "|") != "Rerun Command|Copy Command|Copy Output|Copy Output as HTML" {
		t.Fatalf("menu = %q", labels)
	}
	// a click right of the gutter is the terminal's: no decoration there
	if d := r.pv.decorationAt(gui.Offset{Dx: padLeft + 5, Dy: a.m.h / 2}); d != nil {
		t.Errorf("a decoration in the text: %+v", d)
	}
	// Rerun Command types the line and Enter
	for _, it := range items {
		if it.ID == "rerun" {
			it.OnRun()
		}
	}
	r.waitInput(t, "echo hi\r")
	// the command without output has Copy Output disabled
	cmds, _ := r.pv.shellState()
	it := r.pv.commandMenu(cmds[1])
	for _, x := range it {
		if (x.ID == "copyOutput" || x.ID == "copyHtml") && !x.Disabled {
			t.Errorf("%s enabled for a command with no output", x.ID)
		}
	}
}

func TestCommandOutputAsHTML(t *testing.T) {
	var s session
	s.prompt("$ ").run("ls", "\x1b[1;34msrc\x1b[0m a<b & \x1b[31mred\x1b[0m\r\nplain\r\n", 0).prompt("$ ")
	r := shellPane(t, uitest.Themes()[0], paneSize, nil, s.String())
	cmds, _ := r.pv.shellState()
	if len(cmds) != 1 {
		t.Fatalf("%d commands", len(cmds))
	}
	got := r.pv.outputHTML(cmds[0])
	for _, want := range []string{"<pre ", "</pre>", `font-weight:bold`, "src", "a&lt;b &amp; ", "red", "plain"} {
		if !strings.Contains(got, want) {
			t.Errorf("html lacks %q:\n%s", want, got)
		}
	}
	if text := r.term.si.output(cmds[0]); text != "src a<b & red\nplain\n" {
		t.Errorf("output = %q", text)
	}
}

func TestGoldenCommandDecorations(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			var s session
			s.prompt("user@host:~/p$ ").run("make", "building...\r\nok\r\n", 0).
				prompt("user@host:~/p$ ").run("go test ./...", "FAIL\r\n", 1).
				prompt("user@host:~/p$ ").run("git push", "fatal: The current branch x has no upstream branch.\r\n", 128).
				prompt("user@host:~/p$ ").run("sleep 30", "", -1)
			r := shellPane(t, th, gui.Size{Width: 640, Height: 260}, nil, s.String())
			// the quick fix bulb is on the failed push
			cmds, _ := r.pv.shellState()
			r.env.fixes = func(*paneView) *fixSet {
				return &fixSet{start: cmds[2].start, list: []quickFix{{id: "Git Push Set Upstream", kind: fixCommand, label: "Run: git push --set-upstream origin x"}}}
			}
			r.pv.render.MarkNeedsPaint()
			r.settle()
			r.ts.Screenshot()
			uitest.MatchesGolden(r.ts, "pane_decorations_"+th.Name)
		})
	}
}

func TestGoldenStickyScroll(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			var out strings.Builder
			for i := 1; i <= 40; i++ {
				out.WriteString("line " + strconv.Itoa(i) + " of the output\r\n")
			}
			var s session
			s.prompt("user@host:~/p$ ").run("seq 1 40", out.String(), 0).prompt("user@host:~/p$ ")
			r := shellPane(t, th, gui.Size{Width: 640, Height: 260}, nil, s.String())
			r.pv.scrollTo(6) // six lines up from the bottom: the command line is above the view
			r.settle()
			r.ts.Screenshot()
			if !r.pv.sticky.shown {
				t.Fatalf("the overlay is not shown")
			}
			uitest.MatchesGolden(r.ts, "pane_sticky_"+th.Name)
		})
	}
}

// numbered is n lines of output, "o1" ... "on".
func numbered(prefix string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString(prefix + strconv.Itoa(i) + "\r\n")
	}
	return b.String()
}

// lineOfRow is the text of a row of the view.
func lineOfRow(r *paneRig, row int) string { return r.pv.shown.RowText(row) }

func TestNavigationBetweenCommands(t *testing.T) {
	// five commands of ten lines of output each, in a view of 14 rows
	var s session
	for i := 1; i <= 5; i++ {
		s.prompt("$ ").run("cmd"+strconv.Itoa(i), numbered("out"+strconv.Itoa(i)+"-", 10), 0)
	}
	s.prompt("$ ")
	r := shellPane(t, uitest.Themes()[0], paneSize, nil, s.String())
	r.cfg.stickyOn = false // the overlay would hide the first row
	r.focus()
	// rowOf is the row of the view that shows a line, -1 for none
	rowOf := func(text string) int {
		r.settle()
		for y := 0; y < r.pv.shown.Rows; y++ {
			if lineOfRow(r, y) == text {
				return y
			}
		}
		return -1
	}
	// reaches checks that the command line is at the top of the view, or on the screen when the view is at the bottom (a line
	// near the end cannot go to the top)
	reaches := func(step string, text string) {
		t.Helper()
		row := rowOf(text)
		if row != 0 && (row <= 0 || r.pv.scroll != 0) {
			t.Fatalf("%s: %q is on row %d with the view %d lines above the bottom", step, text, row, r.pv.scroll)
		}
	}
	if r.pv.scroll != 0 {
		t.Fatalf("the view starts at the bottom, not %d", r.pv.scroll)
	}
	// Ctrl+Up from the bottom goes to the last command, and again to the one before
	for _, want := range []string{"$ cmd5", "$ cmd4", "$ cmd3", "$ cmd2", "$ cmd1"} {
		r.pv.scrollToCommand(true)
		reaches("previous", want)
	}
	// past the first it goes to the top of the buffer and stays there
	r.pv.scrollToCommand(true)
	if r.pv.scroll != r.pv.frame.Scrollback {
		t.Errorf("scroll = %d, want the top (%d)", r.pv.scroll, r.pv.frame.Scrollback)
	}
	// and back down (the first command is at the top already)
	for _, want := range []string{"$ cmd2", "$ cmd3", "$ cmd4"} {
		r.pv.scrollToCommand(false)
		reaches("next", want)
	}
	// a view that the user moved some other way starts from the view: the command above its first line
	r.pv.scrollTo(25)
	r.settle()
	first := lineOfRow(r, 0)
	r.pv.scrollToCommand(true)
	r.settle()
	if got := lineOfRow(r, 0); !strings.HasPrefix(got, "$ cmd") || got == first {
		t.Errorf("from a view that starts with %q the previous command shows %q at the top", first, got)
	}
	r.pv.scrollTo(0)
	r.pv.scrollToCommand(false) // nothing below the prompt: stays at the bottom
	if r.pv.scroll != 0 {
		t.Errorf("scroll = %d after the next command at the bottom", r.pv.scroll)
	}
}

func TestSelectToCommands(t *testing.T) {
	var s session
	for i := 1; i <= 3; i++ {
		s.prompt("$ ").run("cmd"+strconv.Itoa(i), numbered("out"+strconv.Itoa(i)+"-", 3), 0)
	}
	s.prompt("$ ")
	r := shellPane(t, uitest.Themes()[0], paneSize, nil, s.String())
	r.cfg.stickyOn = false
	r.focus()
	r.pv.selectToCommand(true)
	if got := r.pv.selectedText(); got != "$ cmd3\nout3-1\nout3-2\nout3-3" {
		t.Errorf("selected to the previous command: %q", got)
	}
	r.pv.selectToCommand(true)
	if got := r.pv.selectedText(); !strings.HasPrefix(got, "$ cmd2\nout2-1") || !strings.HasSuffix(got, "out3-3") {
		t.Errorf("selected to the one before: %q", got)
	}
	r.pv.selectToCommand(false)
	if got := r.pv.selectedText(); got != "$ cmd3\nout3-1\nout3-2\nout3-3" {
		t.Errorf("shrunk back: %q", got)
	}
	r.pv.selectToCommand(false)
	if r.pv.hasSelection() {
		t.Errorf("a selection is left after going back to where it began: %q", r.pv.selectedText())
	}
	// lines
	r.pv.selectLine(true)
	if !r.pv.hasSelection() {
		t.Errorf("select to the previous line selected nothing")
	}
}

func TestStickyLayout(t *testing.T) {
	cases := []struct {
		name     string
		session  func() string
		scroll   int
		tune     func(*config)
		wantShow bool
		wantRows int
		wantText string // text of the first row it shows
	}{
		{"the command line of a command whose output fills the view", func() string {
			var s session
			s.prompt("$ ").run("seq", numbered("n", 40), 0).prompt("$ ")
			return s.String()
		}, 0, nil, true, 1, "$ seq"},
		{"not while the command line is in the view", func() string {
			var s session
			s.prompt("$ ").run("seq", numbered("n", 3), 0).prompt("$ ")
			return s.String()
		}, 0, nil, false, 0, ""},
		{"an ignored command", func() string {
			var s session
			s.prompt("$ ").run("clear", numbered("n", 40), 0).prompt("$ ")
			return s.String()
		}, 0, nil, false, 0, ""},
		{"the setting is off", func() string {
			var s session
			s.prompt("$ ").run("seq", numbered("n", 40), 0).prompt("$ ")
			return s.String()
		}, 0, func(c *config) { c.stickyOn = false }, false, 0, ""},
		{"a running command", func() string {
			var s session
			s.prompt("$ ").run("tail -f log", numbered("n", 40), -1)
			return s.String()
		}, 0, nil, true, 1, "$ tail -f log"},
		{"a long command line of three rows shows them, to the limit", func() string {
			var s session
			s.prompt("$ ").run(strings.Repeat("x", 170), numbered("n", 40), 0).prompt("$ ")
			return s.String()
		}, 0, nil, true, 3, "$ xxxx"},
		{"the limit of lines", func() string {
			var s session
			s.prompt("$ ").run(strings.Repeat("x", 170), numbered("n", 40), 0).prompt("$ ")
			return s.String()
		}, 0, func(c *config) { c.stickyMax = 2 }, true, 2, "$ xxxx"},
		{"a multi-line prompt is shown with its command", func() string {
			var s session
			s.b.WriteString(osc("A") + "~/project main\r\n$ " + osc("B"))
			s.run("seq", numbered("n", 40), 0).prompt("$ ")
			return s.String()
		}, 0, nil, true, 2, "~/project main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := shellPane(t, uitest.Themes()[0], paneSize, tc.tune, tc.session())
			if tc.scroll > 0 {
				r.pv.scrollTo(tc.scroll)
				r.settle()
			}
			a, _ := r.pv.atlases()
			first, count, _, cmd, ok := r.pv.stickyLayout(r.pv.shown.Top, a.m.h)
			if ok != tc.wantShow {
				t.Fatalf("shown = %v, want %v (count %d)", ok, tc.wantShow, count)
			}
			if !ok {
				return
			}
			if count != tc.wantRows {
				t.Errorf("rows = %d, want %d", count, tc.wantRows)
			}
			em := r.term.Emulator()
			text, _ := em.LineText(int(first - em.FirstLine()))
			if !strings.HasPrefix(text, tc.wantText) {
				t.Errorf("first row = %q, want it to start with %q (command %q)", text, tc.wantText, cmd.text)
			}
		})
	}
}

func TestStickyScrollIsPushedUpByTheNextPrompt(t *testing.T) {
	// the output of the first command ends one row below the top of the view and its command line takes two rows: the overlay
	// slides up by a row so that it does not cover the next prompt
	var s session
	s.prompt("$ ").run(strings.Repeat("x", 100), numbered("a", 25), 0).prompt("$ ").run("two", numbered("b", 30), 0).prompt("$ ")
	r := shellPane(t, uitest.Themes()[0], paneSize, nil, s.String())
	a, _ := r.pv.atlases()
	cmds, _ := r.pv.shellState()
	if len(cmds) != 2 {
		t.Fatalf("%d commands", len(cmds))
	}
	r.pv.scrollToLine(cmds[1].top() - 1)
	r.settle()
	if r.pv.shown.Top != cmds[1].top()-1 {
		t.Fatalf("the view starts at %d, want %d", r.pv.shown.Top, cmds[1].top()-1)
	}
	_, count, shift, cmd, ok := r.pv.stickyLayout(r.pv.shown.Top, a.m.h)
	if !ok || !strings.HasPrefix(cmd.text, "xxx") {
		t.Fatalf("overlay: ok %v cmd %q", ok, cmd.text)
	}
	if count != 2 || shift != a.m.h {
		t.Errorf("count %d shift %v, want 2 rows pushed up by %v", count, shift, a.m.h)
	}
	// with the next prompt at the top, the command that owns the top is the second one, and its line is above the view
	r.pv.scrollToLine(cmds[1].top() + 5)
	r.settle()
	if _, _, _, cmd, ok := r.pv.stickyLayout(r.pv.shown.Top, a.m.h); !ok || cmd.text != "two" {
		t.Errorf("overlay: ok %v cmd %q, want the second command", ok, cmd.text)
	}
}

func TestStickyScrollClickScrollsToTheCommand(t *testing.T) {
	var s session
	s.prompt("$ ").run("seq", numbered("n", 40), 0).prompt("$ ")
	r := shellPane(t, uitest.Themes()[0], paneSize, nil, s.String())
	r.ts.Screenshot()
	if !r.pv.sticky.shown {
		t.Fatal("no overlay")
	}
	a, _ := r.pv.atlases()
	r.ts.TapAt(gui.Offset{Dx: 100, Dy: a.m.h / 2})
	r.settle()
	if got := lineOfRow(r, 0); got != "$ seq" {
		t.Errorf("after the click the top is %q, want the command line", got)
	}
	if r.pv.sticky.shown {
		t.Errorf("the overlay is shown with the command line at the top")
	}
}
