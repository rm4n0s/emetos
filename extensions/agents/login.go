package agents

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The bounds of the text of the login.
const (
	// maxTerminalLines is how many lines of output the panel keeps; the oldest go first.
	maxTerminalLines = 2000
	// maxTerminalLine is the longest line that is kept whole.
	maxTerminalLine = 4096
)

// terminalText turns what a program writes to a terminal into lines of plain text. It is not a terminal emulator: it drops
// the escape sequences (colors, cursor movement, window titles), lets a carriage return start the line again and a backspace
// erase a character, and keeps the rest. A sequence cut by the end of a chunk is finished by the next one.
type terminalText struct {
	lines []string
	cur   []byte
	// state is 0 for text, 1 after ESC, 2 inside a CSI sequence, 3 inside an OSC sequence, 4 after ESC in an OSC (ESC \ ends it).
	state int
	cr    bool
}

// write takes a chunk of output.
func (t *terminalText) write(chunk []byte) {
	for _, b := range chunk {
		switch t.state {
		case 1:
			switch b {
			case '[':
				t.state = 2
			case ']':
				t.state = 3
			default:
				t.state = 0 // a two-byte sequence
			}
			continue
		case 2:
			if b >= 0x40 && b <= 0x7e {
				t.state = 0
			}
			continue
		case 3:
			switch b {
			case 0x07:
				t.state = 0
			case 0x1b:
				t.state = 4
			}
			continue
		case 4:
			t.state = 0
			if b != '\\' {
				t.state = 3
			}
			continue
		}
		switch b {
		case 0x1b:
			t.state = 1
		case '\r':
			t.cr = true
		case '\n':
			t.cr = false
			t.endLine()
		case '\b':
			if n := len(t.cur); n > 0 {
				_, size := utf8.DecodeLastRune(t.cur)
				t.cur = t.cur[:n-size]
			}
		case '\t':
			t.put(' ', ' ', ' ', ' ')
		default:
			if b < 0x20 || b == 0x7f {
				continue
			}
			if t.cr { // a carriage return that is not followed by a line feed: the line is written again
				t.cur, t.cr = t.cur[:0], false
			}
			t.put(b)
		}
	}
}

// put adds bytes to the current line, up to the bound.
func (t *terminalText) put(b ...byte) {
	if len(t.cur)+len(b) <= maxTerminalLine {
		t.cur = append(t.cur, b...)
	}
}

// endLine finishes the current line.
func (t *terminalText) endLine() {
	t.lines = append(t.lines, strings.ToValidUTF8(string(t.cur), "?"))
	t.cur = t.cur[:0]
	if len(t.lines) > maxTerminalLines {
		t.lines = append([]string(nil), t.lines[len(t.lines)-maxTerminalLines:]...)
	}
}

// text is the output so far, with the unfinished line at its end.
func (t *terminalText) text() string {
	out := strings.Join(t.lines, "\n")
	if len(t.cur) > 0 {
		if out != "" {
			out += "\n"
		}
		out += strings.ToValidUTF8(string(t.cur), "?")
	}
	return out
}

// loginRun is one run of the login of the program, in the panel.
type loginRun struct {
	proc *ptyProcess
	// mu guards the output, which the goroutine that reads the terminal fills.
	mu     sync.Mutex
	output terminalText
	// flushPosted says that a build of the panel is already waiting for the UI thread.
	flushPosted bool
	// finished is set on the UI thread when the program ended; status says how it went.
	finished bool
	status   string
	input    *ui.InputBoxController
	scroll   ui.ScrollHandle
}

// provideLogin gives the panel view its content.
func (a *agents) provideLogin() error {
	h, err := a.api.Views().Provide(LoginViewID, extension.ViewContent{Build: a.buildLogin})
	if err != nil {
		return err
	}
	a.loginHandle = h
	return nil
}

// loginCommand is the command that runs the login of the program in the panel.
func (a *agents) loginCommand(context.Context, any) error {
	a.startLogin()
	return nil
}

// startLogin runs `claude auth login` in a pseudo-terminal and shows the panel. A login that runs is only shown.
func (a *agents) startLogin() {
	if r := a.login; r != nil && !r.finished {
		a.loginHandle.Reveal(true)
		return
	}
	adapter := a.adapter()
	program, err := adapter.Find()
	if err != nil {
		a.api.Notifications().Warn("The claude program was not found. Install it, or set " + PathSetting + " in the settings.")
		return
	}
	if a.api.Scope().Folder == "" {
		a.api.Notifications().Warn("Open a folder first: the program runs in the folder of the workspace.")
		return
	}
	proc, err := startPty(a.ctx, program, []string{"auth", "login"}, a.api.Scope().Folder, loginEnvironment(os.Environ()))
	if err != nil {
		a.api.Log().Warn("the login did not start", "err", err)
		a.api.Notifications().Error("The login did not start: " + err.Error())
		return
	}
	run := &loginRun{proc: proc, input: ui.NewInputBoxController("")}
	if old := a.login; old != nil {
		old.input.Dispose()
	}
	a.login = run
	a.closers.Add(2)
	go func() {
		defer a.closers.Done()
		proc.pump(func(chunk []byte) {
			run.mu.Lock()
			run.output.write(chunk)
			post := !run.flushPosted
			run.flushPosted = true
			run.mu.Unlock()
			if post {
				a.api.Post(func() {
					run.mu.Lock()
					run.flushPosted = false
					run.mu.Unlock()
					run.stickToEnd()
					a.loginHandle.Refresh()
				})
			}
		})
	}()
	go func() {
		defer a.closers.Done()
		<-proc.done
		a.api.Post(func() { a.loginEnded(run) })
	}()
	a.loginHandle.Refresh()
	a.loginHandle.Reveal(true)
}

// loginEnvironment is the environment of the login: that of Emetos, with a terminal that needs no cursor movement. Nothing
// else is added: the program finds the user's configuration by itself.
func loginEnvironment(base []string) []string {
	env := make([]string, 0, len(base)+2)
	for _, kv := range base {
		if name, _, _ := strings.Cut(kv, "="); name != "TERM" && name != "NO_COLOR" {
			env = append(env, kv)
		}
	}
	return append(env, "TERM=dumb", "NO_COLOR=1")
}

// stickToEnd scrolls the output to its end, after the next build.
func (r *loginRun) stickToEnd() { r.scroll.ScrollTo(0, scrollEnd) }

// loginEnded handles the end of the login: the status of the login is checked again, and the tabs that wait for it start.
func (a *agents) loginEnded(run *loginRun) {
	run.finished = true
	run.status = "The login ended (exit status " + itoaStatus(run.proc.exit) + "). Checking the login..."
	a.loginState.Store(loginUnknown)
	a.loginHandle.Refresh()
	adapter := a.adapter()
	a.closers.Add(1)
	go func() {
		defer a.closers.Done()
		info, err := adapter.AuthStatus(a.ctx)
		a.api.Post(func() {
			switch {
			case err != nil:
				run.status = "The login could not be checked: " + errorText(err)
			case info.LoggedIn:
				a.loginState.Store(loginIn)
				run.status = "You are logged in."
				a.api.Notifications().Info("The agent is logged in.")
			default:
				a.loginState.Store(loginOut)
				run.status = "You are not logged in yet. Run the login again."
			}
			a.loginHandle.Refresh()
			if err == nil && info.LoggedIn {
				for _, t := range append([]*sessionTab(nil), a.tabs...) {
					if t.phase == phaseLoggedOut {
						t.retry()
					}
				}
			}
		})
	}()
}

// itoaStatus writes an exit status.
func itoaStatus(code int) string {
	if code < 0 {
		return "a signal"
	}
	return strconv.Itoa(code)
}

// sendLine writes a line to the program, as typed.
func (r *loginRun) sendLine(a *agents, line string) {
	if r.finished {
		return
	}
	if err := r.proc.write([]byte(line + "\n")); err != nil {
		a.api.Log().Warn("the line was not sent to the login", "err", err)
		a.api.Notifications().Warn("The line was not sent: the login has ended.")
		return
	}
	r.input.SetValue("")
}

// buildLogin is the panel view "Agents: Log in".
func (a *agents) buildLogin(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	dim := ui.TextStyle(bc, fontSize, colors.DescriptionForeground)
	r := a.login
	if r == nil {
		return widgets.NewPadding(painting.NewEdgeInsetsAll(12), widgets.NewColumn([]widgets.Widget{
			rich(bc, "Log in runs `claude auth login` here, so that the program's own flow (the browser it opens, or a code you paste back) finishes in this panel.", dim, colors),
			gap(10),
			widgets.NewAlign(ui.NewButton("Log in", ui.ButtonOpts{OnPressed: a.startLogin}), widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft}),
		}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	}
	r.mu.Lock()
	text := r.output.text()
	r.mu.Unlock()
	mono := ui.TextStyle(bc, 12, colors.Foreground).CopyWith(painting.TextStyleOpts{FontFamily: monoFamily(bc)})
	var tail []widgets.Widget
	if r.finished {
		tail = append(tail, widgets.NewText(r.status, widgets.TextOpts{Style: dim}), hgap(10),
			ui.NewButton("Log in again", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: a.startLogin}))
	} else {
		tail = append(tail,
			widgets.NewExpanded(ui.NewInputBox(nil, nil, ui.InputBoxOpts{
				Controller: r.input, Autofocus: true, Placeholder: "Type a line for the program (the code the browser shows) and press Enter",
				OnSubmit: func(line string) { r.sendLine(a, line) },
			})),
			hgap(8), ui.NewButton("Cancel", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: r.proc.stop}))
	}
	return widgets.NewColumn([]widgets.Widget{
		widgets.NewExpanded(ui.NewScrollView(widgets.NewPadding(painting.NewEdgeInsetsAll(8),
			widgets.NewText(text, widgets.TextOpts{Style: mono})), &r.scroll)),
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(8, 4, 8, 8), widgets.NewRow(tail, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}
