package terminal

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/pty"
	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// updating tells whether -update-goldens (the flag of gluttertest) was given: it also rewrites the recording of bash.
func updating() bool {
	f := flag.Lookup("update-goldens")
	return f != nil && f.Value.String() == "true"
}

// The recorded output of the shell integration scripts (SPEC §10.25 "Shell integration"): bash's is recorded from the real
// shell with the real script (testdata/shellint/bash.raw, rewritten by -update-goldens, with the commands it detected in
// bash.golden.json). The scripts of zsh, fish and PowerShell cannot run here (those shells are not installed on the
// development machine), so their streams are assembled by hand from the printf lines of the scripts, in the order the scripts
// print them (shellIntegration-rc.zsh: __vsc_precmd and __vsc_preexec; shellIntegration.fish: the fish_prompt, fish_preexec,
// fish_postexec and fish_cancel handlers; shellIntegration.ps1: Prompt and PSConsoleHostReadLine). Every stream is fed whole and
// cut in two at every byte, and must give the same commands.

// found is what a test compares: a command as the state detected it.
type found struct {
	Text    string `json:"text"`
	Exit    int    `json:"exit"`
	HasExit bool   `json:"hasExit"`
	Output  string `json:"output"`
	Cwd     string `json:"cwd"`
	Conf    string `json:"confidence"`
}

// replay feeds the chunks to a fresh emulator with the state and returns the commands it found.
func replay(t testing.TB, rows, cols int, nonce string, chunks ...string) (cmds []found, st *shellState, em *emulator.Emulator) {
	t.Helper()
	var s *shellState
	em = emulator.New(rows, cols, emulator.Options{Scrollback: 1000, MarkCodes: shellOSCCodes,
		Callbacks: emulator.Callbacks{OSC: func(ev emulator.OSCEvent) { s.handle(ev) }}})
	s = newShellState(em, nonce)
	for _, c := range chunks {
		if _, err := em.Write([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range s.Commands() {
		cmds = append(cmds, found{Text: v.text, Exit: v.exit, HasExit: v.hasExit, Output: s.output(v), Cwd: v.cwd, Conf: v.confidence})
	}
	return cmds, s, em
}

func osc(body string) string { return "\x1b]633;" + body + "\a" }

func TestShellSequencesOfTheScripts(t *testing.T) {
	const n = "N0"
	prompt := func(cwd, text string) string { return osc("A") + text + osc("B") }
	zsh := osc("P;HasRichCommandDetection=True") +
		osc("E;;"+n) + osc("C") + osc("D") + osc("P;Cwd=/home/u") + prompt("/home/u", "u@h % ") +
		"ls -la\r\n" + osc("E;ls -la;"+n) + osc("C") + "total 0\r\nfile\r\n" + osc("D;0") + osc("P;Cwd=/home/u") + prompt("/home/u", "u@h % ") +
		"cd /tmp\r\n" + osc("E;cd /tmp;"+n) + osc("C") + osc("D;0") + osc("P;Cwd=/tmp") + prompt("/tmp", "u@h % ") +
		"false\r\n" + osc("E;false;"+n) + osc("C") + osc("D;1") + osc("P;Cwd=/tmp") + prompt("/tmp", "u@h % ") +
		"echo a;b\r\n" + osc(`E;echo a\x3bb;`+n) + osc("C") + "a\r\nb: command not found\r\n" + osc("D;127") + osc("P;Cwd=/tmp") + prompt("/tmp", "u@h % ") +
		"\r\n" + osc("E;;"+n) + osc("C") + osc("D") + osc("P;Cwd=/tmp") + prompt("/tmp", "u@h % ")
	fish := osc("P;HasRichCommandDetection=True") + osc("P;Cwd=/home/u") + prompt("", "u@h ~> ") +
		"ls\r\n" + osc("E;ls;"+n) + osc("C") + "a  b\r\n" + osc("D;0") + osc("P;Cwd=/home/u") + prompt("", "u@h ~> ") +
		"cd /tmp\r\n" + osc("E;cd /tmp;"+n) + osc("C") + osc("D;0") + osc("P;Cwd=/tmp") + prompt("", "u@h /tmp> ") +
		"\r\n" + osc("P;Cwd=/tmp") + osc("E;;"+n) + osc("C") + osc("D") + prompt("", "u@h /tmp> ") +
		"false\r\n" + osc("E;false;"+n) + osc("C") + osc("D;1") + osc("P;Cwd=/tmp") + prompt("", "u@h /tmp> ")
	pwsh := osc("P;HasRichCommandDetection=True") + osc("P;IsWindows=False") +
		osc("A") + osc("P;Cwd=/home/u") + "PS /home/u> " + osc("B") +
		"Get-ChildItem\r\n" + osc("E;Get-ChildItem;"+n) + osc("C") + "Mode  Name\r\n----  ----\r\n" +
		osc("D;0") + osc("A") + osc("P;Cwd=/home/u") + "PS /home/u> " + osc("B") +
		"\r\n" + osc("E;;"+n) + osc("C") + osc("D") + osc("A") + osc("P;Cwd=/home/u") + "PS /home/u> " + osc("B") +
		"Write-Error x\r\n" + osc("E;Write-Error x;"+n) + osc("C") + "x\r\n" + osc("D;1") + osc("A") + osc("P;Cwd=/home/u") + "PS /home/u> " + osc("B")
	// OSC 133 (FinalTerm): the same four letters, with options after A and the exit status after D
	final := "\x1b]133;D\a\x1b]133;A\a$ \x1b]133;B\a" + "make\r\n" + "\x1b]133;C\a" + "built\r\n" + "\x1b]133;D;2;aid=7\a\x1b]133;A;k=s\a$ \x1b]133;B\a"

	cases := []struct {
		name   string
		stream string
		nonce  string
		want   []found
	}{
		{"zsh", zsh, n, []found{
			{"ls -la", 0, true, "total 0\nfile\n", "/home/u", confHigh},
			{"cd /tmp", 0, true, "", "/home/u", confHigh},
			{"false", 1, true, "", "/tmp", confHigh},
			{"echo a;b", 127, true, "a\nb: command not found\n", "/tmp", confHigh},
		}},
		{"fish", fish, n, []found{
			{"ls", 0, true, "a  b\n", "/home/u", confHigh},
			{"cd /tmp", 0, true, "", "/home/u", confHigh},
			{"false", 1, true, "", "/tmp", confHigh},
		}},
		{"pwsh", pwsh, n, []found{
			{"Get-ChildItem", 0, true, "Mode  Name\n----  ----\n", "/home/u", confHigh},
			{"Write-Error x", 1, true, "x\n", "/home/u", confHigh},
		}},
		{"OSC 133 has no command line: it is read from the screen", final, n, []found{
			{"make", 2, true, "built\n", "", confMedium},
		}},
		{"a command line with the wrong secret is not believed", strings.ReplaceAll(zsh, "E;ls -la;N0", "E;rm -rf /;WRONG"), n, []found{
			{"ls -la", 0, true, "total 0\nfile\n", "/home/u", confMedium},
			{"cd /tmp", 0, true, "", "/home/u", confHigh},
			{"false", 1, true, "", "/tmp", confHigh},
			{"echo a;b", 127, true, "a\nb: command not found\n", "/tmp", confHigh},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, _ := replay(t, 24, 80, tc.nonce, tc.stream)
			if tc.name == "a command line with the wrong secret is not believed" {
				// the line is read from the screen: what the test types is on it
				for i := range got {
					if got[i].Conf == confLow || got[i].Conf == confMedium {
						got[i].Conf = confMedium
					}
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("commands:\n got %+v\nwant %+v", got, tc.want)
			}
			// the same stream cut in two at every byte
			for cut := 1; cut < len(tc.stream); cut += 7 {
				again, _, _ := replay(t, 24, 80, tc.nonce, tc.stream[:cut], tc.stream[cut:])
				if tc.name == "a command line with the wrong secret is not believed" {
					continue
				}
				if !reflect.DeepEqual(again, got) {
					t.Fatalf("cut at %d: %+v, want %+v", cut, again, got)
				}
			}
		})
	}
}

func TestShellSequenceEdgeCases(t *testing.T) {
	const n = "N0"
	cases := []struct {
		name   string
		stream string
		want   []string // the commands' texts
		active bool
		cwd    string
	}{
		{"nothing is no integration", "hello\r\n", nil, false, ""},
		{"a prompt alone", osc("A") + "$ " + osc("B"), nil, true, ""},
		{"D without a prompt is ignored", osc("D;0") + osc("D;1"), nil, true, ""},
		{"C without a command start is ignored", osc("C") + "x\r\n" + osc("D;0"), nil, true, ""},
		{"a redrawn prompt replaces the first", osc("A") + "$ " + osc("B") + "\r" + osc("A") + "$ " + osc("B") + "ab\r\n" + osc("E;ab;"+n) + osc("C") + osc("D;0"), []string{"ab"}, true, ""},
		{"a property of the folder", osc("P;Cwd=/a/b\\x3bc"), nil, true, "/a/b;c"},
		{"OSC 1337 CurrentDir", "\x1b]1337;CurrentDir=/x/y\a", nil, true, "/x/y"},
		{"a backslash in the line", osc("A") + "$ " + osc("B") + osc(`E;echo a\\b;`+n) + osc("C") + osc("D;0"), []string{`echo a\b`}, true, ""},
		{"a hex byte in the line", osc("A") + "$ " + osc("B") + osc(`E;echo \xe2\x82\xac;`+n) + osc("C") + osc("D;0"), []string{"echo €"}, true, ""},
		{"the alternate screen has no commands", "\x1b[?1049h" + osc("A") + "$ " + osc("B") + osc("E;x;"+n) + osc("C") + osc("D;0"), nil, false, ""},
		{"an unknown letter", osc("Z;1") + osc("F") + osc("G") + osc("H") + osc("I"), nil, false, ""},
		{"a status that is not a number", osc("A") + "$ " + osc("B") + osc("E;x;"+n) + osc("C") + osc("D;abc"), []string{"x"}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, st, _ := replay(t, 24, 80, n, tc.stream)
			var texts []string
			for _, c := range got {
				texts = append(texts, c.Text)
			}
			if !reflect.DeepEqual(texts, tc.want) {
				t.Errorf("commands = %q, want %q", texts, tc.want)
			}
			if st.Active() != tc.active {
				t.Errorf("Active = %v, want %v", st.Active(), tc.active)
			}
			if st.Cwd() != tc.cwd {
				t.Errorf("Cwd = %q, want %q", st.Cwd(), tc.cwd)
			}
		})
	}
}

func TestUnescapeValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"}, {`a\\b`, `a\b`}, {`a\x3bb`, "a;b"}, {`\x0a`, "\n"}, {`\xe2\x82\xac`, "€"}, {`trailing\`, `trailing\`},
		{`\x3`, `\x3`}, {`\xZZ`, `\xZZ`}, {`\\x41`, `\x41`}, {`\xff`, "�"}, {``, ``},
	}
	for _, tc := range cases {
		if got := unescapeValue(tc.in); got != tc.want {
			t.Errorf("unescapeValue(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// tee is a process that keeps what the program wrote, to record it.
type tee struct {
	process
	mu  sync.Mutex
	buf []byte
}

func (t *tee) Read(p []byte) (int, error) {
	n, err := t.process.Read(p)
	t.mu.Lock()
	t.buf = append(t.buf, p[:n]...)
	t.mu.Unlock()
	return n, err
}

// TestRecordedBash records the real bash with the real script (under -update-goldens) and replays the recording: whole, and
// cut into pieces of every size.
func TestRecordedBash(t *testing.T) {
	raw := filepath.Join("testdata", "shellint", "bash.raw")
	golden := filepath.Join("testdata", "shellint", "bash.golden.json")
	if updating() {
		recordBash(t, raw, golden)
	}
	data, err := os.ReadFile(raw)
	if err != nil {
		t.Skipf("no recording (run with -update-goldens): %v", err)
	}
	wantJSON, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	var want []found
	if err := json.Unmarshal(wantJSON, &want); err != nil {
		t.Fatal(err)
	}
	got, _, _ := replay(t, 24, 80, "testnonce", string(data))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commands:\n got %+v\nwant %+v", got, want)
	}
	for _, size := range []int{1, 2, 3, 5, 13, 64, 1000} {
		var chunks []string
		for i := 0; i < len(data); i += size {
			chunks = append(chunks, string(data[i:min(i+size, len(data))]))
		}
		again, _, _ := replay(t, 24, 80, "testnonce", chunks...)
		if !reflect.DeepEqual(again, want) {
			t.Fatalf("pieces of %d bytes: %+v", size, again)
		}
	}
}

// recordBash runs a session in the real bash and writes what it printed and what the state detected.
func recordBash(t *testing.T, raw, golden string) {
	t.Helper()
	var rec *tee
	b := startBashWith(t, 24, 80, func(ctx context.Context, opts pty.Options) (process, error) {
		p, err := ptyStarter(ctx, opts)
		if err != nil {
			return nil, err
		}
		rec = &tee{process: p}
		return rec, nil
	})
	b.waitCommands(t, 0)
	done := 0
	for _, line := range []string{"echo hello", "false", "", "printf 'two\\nlines\\n'", "cd /tmp", "echo 'semi;colon' \\\\ back"} {
		if line != "" {
			done++
		}
		b.run(t, line, done)
	}
	var cmds []found
	for _, v := range b.si.Commands() {
		cmds = append(cmds, found{Text: v.text, Exit: v.exit, HasExit: v.hasExit, Output: b.si.output(v), Cwd: "", Conf: v.confidence})
	}
	// the folder is the one of the test: replace it so that the golden does not depend on it
	if err := os.MkdirAll(filepath.Dir(raw), 0o755); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	data := append([]byte(nil), rec.buf...)
	rec.mu.Unlock()
	home := b.home
	dir := b.dir
	data = []byte(strings.ReplaceAll(strings.ReplaceAll(string(data), dir, "/work"), home, "/home/test"))
	if err := os.WriteFile(raw, data, 0o644); err != nil {
		t.Fatal(err)
	}
	replayed, _, _ := replay(t, 24, 80, "testnonce", string(data))
	for i := range cmds {
		if i < len(replayed) {
			cmds[i].Cwd = replayed[i].Cwd
		}
	}
	out, err := json.MarshalIndent(cmds, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(golden, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
