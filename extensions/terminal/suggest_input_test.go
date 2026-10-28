package terminal

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// inputRig is an emulator with a shell state, fed by hand with what a shell with the integration scripts prints.
type inputRig struct {
	term  *Terminal
	em    *emulator.Emulator
	si    *shellState
	frame emulator.Frame
}

func newInputRig(t testing.TB, rows, cols int) *inputRig {
	t.Helper()
	r := &inputRig{}
	r.em = emulator.New(rows, cols, emulator.Options{Scrollback: 100, MarkCodes: shellOSCCodes, Callbacks: emulator.Callbacks{
		OSC: func(ev emulator.OSCEvent) { r.si.handle(ev) },
	}})
	r.si = newShellState(r.em, "nonce")
	r.term = &Terminal{em: r.em, si: r.si}
	return r
}

func (r *inputRig) feed(t testing.TB, s string) {
	t.Helper()
	if _, err := r.em.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
}

func (r *inputRig) input() promptInput { return readPromptInput(r.term, &r.frame) }

// The prompt as VS Code's scripts print it: A, the text of the prompt, B.
const (
	promptA = "\x1b]633;A\a"
	promptB = "\x1b]633;B\a"
	prompt  = promptA + "user@host:~$ " + promptB
)

func TestReadPromptInput(t *testing.T) {
	tests := []struct {
		name       string
		rows, cols int
		out        string
		want       string
		wantCursor int
		wantOK     bool
	}{
		{"an empty prompt", 5, 40, prompt, "", 0, true},
		{"typed text", 5, 40, prompt + "git che", "git che", 7, true},
		{"a trailing blank is kept", 5, 40, prompt + "git ", "git ", 4, true},
		{"backspace", 5, 40, prompt + "git che\b \b\b \b", "git c", 5, true},
		{"the cursor moved left in the line", 5, 40, prompt + "git che\x1b[D\x1b[D", "git che", 5, true},
		{"text after the cursor is part of the line", 5, 40, prompt + "git commit\x1b[6D", "git commit", 4, true},
		{"a line that wraps", 5, 10, prompt + "echo hello world", "echo hello world", 16, true},
		{"a line that wraps with the cursor on the first row", 5, 10, prompt + "echo hello world\x1b[8D", "echo hello world", 8, true},
		{"a suggestion of the shell in gray is not typed", 5, 40, prompt + "git che\x1b[90mckout\x1b[0m\x1b[5D", "git che", 7, true},
		{"a suggestion of the shell that is dim", 5, 40, prompt + "git che\x1b[2mckout\x1b[0m\x1b[5D", "git che", 7, true},
		{"text with other colors is typed", 5, 40, prompt + "git \x1b[31mche\x1b[0m", "git che", 7, true},
		{"wide characters", 5, 40, prompt + "echo 日本", "echo 日本", len("echo 日本"), true},
		{"a command that runs", 5, 40, prompt + "ls\r\n\x1b]633;C\a", "", 0, false},
		{"the alternate screen", 5, 40, prompt + "vim\x1b[?1049h", "", 0, false},
		{"no prompt yet", 5, 40, "just text", "", 0, false},
		{"the prompt of the next command", 5, 40, prompt + "ls\r\n\x1b]633;C\afile\r\n\x1b]633;D;0\a" + prompt + "pw", "pw", 2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newInputRig(t, tt.rows, tt.cols)
			r.feed(t, tt.out)
			in := r.input()
			if in.ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (value %q)", in.ok, tt.wantOK, in.value)
			}
			if !tt.wantOK {
				return
			}
			if in.value != tt.want || in.cursor != tt.wantCursor {
				t.Errorf("value, cursor = %q, %d; want %q, %d", in.value, in.cursor, tt.want, tt.wantCursor)
			}
		})
	}
}

func TestReadPromptInputWhereTheLineIs(t *testing.T) {
	r := newInputRig(t, 4, 40)
	r.feed(t, "one\r\ntwo\r\n"+prompt+"git ch")
	in := r.input()
	if !in.ok || in.value != "git ch" {
		t.Fatalf("got %+v", in)
	}
	if in.startX != len("user@host:~$ ") || in.cursorX != in.startX+6 || in.cursorLine != in.startLine {
		t.Errorf("start, cursor = (%d, %d), (%d, %d)", in.startX, in.startLine, in.cursorX, in.cursorLine)
	}
	// the screen scrolls; the lines are absolute, so they follow
	before := in.cursorLine
	r.feed(t, "\r\n\r\n\r\n")
	if got := r.si.Current(); got == nil || got.startLine != before {
		t.Errorf("the prompt moved to %v, want line %d", got, before)
	}
}

func TestReadPromptInputWithAMultiLineCommand(t *testing.T) {
	r := newInputRig(t, 6, 40)
	r.feed(t, prompt+"echo 'a\r\n> b")
	in := r.input()
	if !in.ok || !strings.HasPrefix(in.value, "echo 'a\n") || in.cursor != len(in.value) {
		t.Errorf("got %+v", in)
	}
}

func TestReadPromptInputIsCheapWhenIdle(t *testing.T) {
	r := newInputRig(t, 24, 80)
	r.feed(t, prompt+"git che")
	r.input() // the first read sizes the scratch frame
	if n := testing.AllocsPerRun(50, func() { r.input() }); n > 30 {
		t.Errorf("reading the line allocates %v times", n)
	}
}

func TestPrintableInput(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"a", true}, {" ", true}, {"é", true}, {"日", true}, {"/", true},
		{"\r", false}, {"\x7f", false}, {"\x1b[A", false}, {"\t", false}, {"", true},
		{strings.Repeat("a", maxInputBytes+1), false}, {"\xff", false},
	}
	for _, tt := range tests {
		if got := printableInput([]byte(tt.in)); got != tt.want {
			t.Errorf("printableInput(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
