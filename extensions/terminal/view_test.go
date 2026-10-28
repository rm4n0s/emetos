package terminal

import (
	"context"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/pty"
)

// paneRig shows one terminal in a window of the tests.
type paneRig struct {
	ts   *gluttertest.Tester
	pv   *paneView
	term *Terminal
	proc *fakeProcess
	cfg  *config
	env  *paneEnv
}

// newFakePane shows a terminal whose program is a fake one: the test writes its output with write.
func newFakePane(t *testing.T, th uitest.Theme, size gui.Size, tune func(*config)) *paneRig {
	t.Helper()
	return newFakePaneT(t, th, size, tune)
}

// newFakePaneT is newFakePane for tests and benchmarks.
func newFakePaneT(t testing.TB, th uitest.Theme, size gui.Size, tune func(*config)) *paneRig {
	t.Helper()
	cfg := testConfig()
	if tune != nil {
		tune(cfg)
	}
	proc := newFakeProcess()
	term, err := newTerminal(t.Context(), terminalOpts{id: 1, profile: profile{name: "fake", path: "fake"}, scrollback: cfg.scrollback,
		start: func(context.Context, pty.Options) (process, error) { return proc, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Kill(); term.Wait() })
	box := &postBox{}
	env := &paneEnv{cfg: func() *config { return cfg }, fonts: &fontCache{}, post: box.Post, clipboardCtx: context.Background()}
	pv := newPaneView(env, term)
	ts := uitest.Pump(t, th, size, paneWidget(pv))
	box.attach(ts.Binding.Post)
	r := &paneRig{ts: ts, pv: pv, term: term, proc: proc, cfg: cfg, env: env}
	r.settle()
	return r
}

// write gives the terminal output as the program's and waits until it is parsed.
func (r *paneRig) write(t *testing.T, text string) { r.writeB(t, text) }

// writeB is write for tests and benchmarks.
func (r *paneRig) writeB(t testing.TB, text string) {
	t.Helper()
	r.feed(t, text)
	time.Sleep(20 * time.Millisecond)
	r.settle()
}

// feed gives the terminal output as the program's and waits until it is parsed; no frame is drawn.
func (r *paneRig) feed(t testing.TB, text string) {
	t.Helper()
	r.proc.out <- []byte(text)
	deadline := time.Now().Add(5 * time.Second)
	for r.term.unparsedNow() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the output was not parsed")
		}
		time.Sleep(time.Millisecond)
	}
}

// settle runs the frames the changes ask for.
func (r *paneRig) settle() {
	for i := 0; i < 4; i++ {
		r.ts.FlushPosted()
		r.ts.Pump(20 * time.Millisecond)
	}
}

func TestGoldenAPromptWithColors(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newFakePane(t, th, gui.Size{Width: 640, Height: 260}, nil)
			r.write(t, "user@host:~/project$ ls --color\r\n"+
				"\x1b[1;34msrc\x1b[0m  \x1b[1;34mdocs\x1b[0m  \x1b[0;32mrun.sh\x1b[0m  README.md  \x1b[31;1merror.log\x1b[0m\r\n"+
				"\x1b[4munderline\x1b[0m \x1b[3mitalic\x1b[0m \x1b[1mbold\x1b[0m \x1b[7minverse\x1b[0m \x1b[9mstrike\x1b[0m \x1b[2mdim\x1b[0m\r\n"+
				"\x1b[38;2;255;128;0mtruecolor\x1b[0m \x1b[48;5;25m 256 \x1b[0m \x1b[41;37m red bg \x1b[0m\r\n"+
				"┌──────┬──────┐ ╭──╮ █▓▒░ ▀▄▌▐ ⠁⠃⠇\r\n│ box  │ draw │ ╰──╯ ╔═╦═╗\r\n└──────┴──────┘ ║ ║ ║\r\n"+
				"user@host:~/project$ ")
			r.ts.Screenshot()
			uitest.MatchesGolden(r.ts, "pane_prompt_"+th.Name)
		})
	}
}

func TestGoldenASelectionTheAlternateScreenAndTheCursors(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newFakePane(t, th, gui.Size{Width: 640, Height: 200}, nil)
			r.write(t, "alpha beta gamma\r\nsecond line here\r\nthird\r\n$ ")
			r.pv.setSelection(selection{active: true, anchor: pos{r.pv.frame.Top, 6}, head: pos{r.pv.frame.Top + 1, 5}})
			r.settle()
			r.ts.Screenshot()
			uitest.MatchesGolden(r.ts, "pane_selection_"+th.Name)

			alt := newFakePane(t, th, gui.Size{Width: 640, Height: 200}, nil)
			alt.write(t, "main screen\r\n\x1b[?1049h\x1b[2J\x1b[1;1H\x1b[7m ALT SCREEN \x1b[0m\x1b[3;5Hinside the alternate screen\x1b[?25l")
			alt.ts.Screenshot()
			uitest.MatchesGolden(alt.ts, "pane_alt_"+th.Name)

			styles := newFakePane(t, th, gui.Size{Width: 640, Height: 200}, func(c *config) { c.cursorStyle, c.cursorInactive = "line", "underline"; c.cursorWidth = 2 })
			styles.write(t, "a line cursor, an underline when inactive")
			styles.ts.Screenshot()
			uitest.MatchesGolden(styles.ts, "pane_cursor_"+th.Name)
		})
	}
}
