package emulator

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/pty"
)

var updateGoldens = flag.Bool("update-goldens", false, "rewrite the golden files of the recorded programs")

// session runs a real program in a pseudo-terminal made by the test and feeds its output to an emulator, the way the terminal
// extension does. Nothing here touches the user's home folder or configuration: the program gets a temporary HOME and a
// clean environment. Every process it starts is stopped by the cleanup.
type session struct {
	t       *testing.T
	p       *pty.Process
	e       *Emulator
	mu      sync.Mutex
	raw     bytes.Buffer
	lastOut atomic.Int64
	readEnd chan struct{}
	replies atomic.Int64
}

// cleanEnv is the environment of a recorded program: a terminal that is xterm-256color, a temporary home and nothing else of
// the user's.
func cleanEnv(home string, extra ...string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"TERM=xterm-256color",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"SHELL=/bin/sh",
		"USER=emetos-test",
		"LOGNAME=emetos-test",
		"TMPDIR=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
		"XDG_DATA_HOME=" + filepath.Join(home, "data"),
		"XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
		"XDG_STATE_HOME=" + filepath.Join(home, "state"),
	}
	return append(env, extra...)
}

// recordProgram starts the program under timeout(1) (--foreground keeps it in the foreground group of the terminal) and
// skips the test when it is not installed.
func recordProgram(t *testing.T, rows, cols int, dir string, env []string, program string, args ...string) *session {
	t.Helper()
	if _, err := exec.LookPath(program); err != nil {
		t.Skipf("%s is not installed", program)
	}
	timeout, err := exec.LookPath("timeout")
	if err != nil {
		t.Skip("timeout is not installed")
	}
	s := &session{t: t, readEnd: make(chan struct{})}
	s.e = New(rows, cols, Options{Scrollback: 200, Callbacks: Callbacks{Reply: func(b []byte) {
		s.replies.Add(1)
		if s.p != nil {
			_, _ = s.p.Write(b)
		}
	}}})
	p, err := pty.Start(t.Context(), pty.Options{
		Program: timeout,
		Args:    append([]string{"--foreground", "-k", "2", "40", program}, args...),
		Dir:     dir,
		Env:     env,
		Rows:    rows,
		Cols:    cols,
		Grace:   500 * time.Millisecond,
	})
	if err != nil {
		t.Skipf("cannot run %s in a pseudo-terminal: %v", program, err)
	}
	s.mu.Lock()
	s.p = p
	s.mu.Unlock()
	t.Cleanup(func() {
		p.Close()
		select {
		case <-p.Done():
		case <-time.After(10 * time.Second):
			t.Errorf("%s did not end", program)
		}
		<-s.readEnd
	})
	go func() {
		defer close(s.readEnd)
		buf := make([]byte, 64<<10)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.raw.Write(buf[:n])
				s.mu.Unlock()
				if _, werr := s.e.Write(buf[:n]); werr != nil {
					return
				}
				s.lastOut.Store(time.Now().UnixNano())
			}
			if err != nil {
				return
			}
		}
	}()
	return s
}

// screenText is the text of the screen, rows joined with newlines.
func (s *session) screenText() string {
	var f Frame
	s.e.ReadLines(s.e.Scrollback(), s.e.rows, &f)
	rows := make([]string, f.Rows)
	for y := range rows {
		rows[y] = f.RowText(y)
	}
	return strings.Join(rows, "\n")
}

// waitFor waits until the screen shows text.
func (s *session) waitFor(text string, timeout time.Duration) {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(s.screenText(), text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.t.Fatalf("the screen never showed %q; it shows:\n%s", text, s.screenText())
}

// send types bytes.
func (s *session) send(text string) {
	s.t.Helper()
	if _, err := s.p.Write([]byte(text)); err != nil {
		s.t.Fatalf("send %q: %v", text, err)
	}
}

// settle waits for the program to answer what was just sent (up to 2 seconds, since some keys draw nothing) and then until it
// has written nothing for quiet.
func (s *session) settle(quiet time.Duration) {
	start := time.Now()
	for time.Since(start) < 2*time.Second && time.Unix(0, s.lastOut.Load()).Before(start) {
		time.Sleep(5 * time.Millisecond)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		last := time.Unix(0, s.lastOut.Load())
		if time.Since(last) > quiet {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitExit waits for the program to end.
func (s *session) waitExit(timeout time.Duration) {
	s.t.Helper()
	select {
	case <-s.p.Done():
	case <-time.After(timeout):
		s.t.Fatalf("the program did not end; the screen shows:\n%s", s.screenText())
	}
	<-s.readEnd
}

// describeStyle writes a style in a few words for the golden files.
func describeStyle(f *Frame, st Style) string {
	var parts []string
	col := func(name string, c Color) {
		switch c.Kind() {
		case ColorIndexed:
			parts = append(parts, fmt.Sprintf("%s=%d", name, c.Index()))
		case ColorRGB:
			r, g, b := c.RGB()
			parts = append(parts, fmt.Sprintf("%s=#%02x%02x%02x", name, r, g, b))
		}
	}
	col("fg", st.Fg)
	col("bg", st.Bg)
	col("ul", st.UnderlineColor)
	for _, a := range []struct {
		bit  Attr
		name string
	}{{AttrBold, "bold"}, {AttrDim, "dim"}, {AttrItalic, "italic"}, {AttrBlink, "blink"}, {AttrInverse, "inverse"},
		{AttrInvisible, "invisible"}, {AttrStrike, "strike"}, {AttrOverline, "overline"}} {
		if st.Attrs&a.bit != 0 {
			parts = append(parts, a.name)
		}
	}
	if st.Underline != UnderlineNone {
		parts = append(parts, "underline"+strconv.Itoa(int(st.Underline)))
	}
	if st.Link != 0 {
		parts = append(parts, "link="+f.LinkOf(st).URL)
	}
	return strings.Join(parts, " ")
}

// goldenOf writes the cells of the screen as text: the rows, then the runs of cells that have a style.
func goldenOf(e *Emulator) string {
	var f Frame
	e.ReadLines(e.Scrollback(), e.rows, &f)
	var sb strings.Builder
	var snap Frame
	e.Snapshot(&snap)
	fmt.Fprintf(&sb, "size %dx%d cursor (%d,%d) alt=%v\n", f.Cols, f.Rows, snap.Cursor.X, snap.Cursor.Y, snap.Modes.AltScreen)
	sb.WriteString("--- text\n")
	for y := 0; y < f.Rows; y++ {
		fmt.Fprintf(&sb, "%2d|%s\n", y, f.RowText(y))
	}
	sb.WriteString("--- styles\n")
	for y := 0; y < f.Rows; y++ {
		x := 0
		for x < f.Cols {
			c := f.Cell(y, x)
			desc := describeStyle(&f, f.StyleOf(c))
			x0 := x
			for x < f.Cols && describeStyle(&f, f.StyleOf(f.Cell(y, x))) == desc {
				x++
			}
			if desc != "" {
				fmt.Fprintf(&sb, "%2d %d-%d %s\n", y, x0, x-1, desc)
			}
			_ = c
		}
	}
	return sb.String()
}

// matchGolden compares the screen with testdata/programs/<name>.golden, rewriting the file under -update-goldens.
func matchGolden(t *testing.T, name string, s *session) {
	t.Helper()
	got := goldenOf(s.e)
	path := filepath.Join("testdata", "programs", name+".golden")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		// The bytes the program wrote are kept too, so that TestReplayRecordings checks the same screen without the program.
		s.mu.Lock()
		raw := append([]byte(nil), s.raw.Bytes()...)
		s.mu.Unlock()
		if err := os.WriteFile(filepath.Join("testdata", "programs", name+".raw"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no golden file (run with -update-goldens): %v\n%s", err, got)
	}
	if string(want) != got {
		t.Errorf("the screen differs from %s:\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

// numberedFile writes a text file of numbered lines in dir.
func numberedFile(t *testing.T, dir, name string, lines int) string {
	t.Helper()
	var sb strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&sb, "line %d of the file with some text to read\n", i)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRecordedPrograms draws the output of real programs to the screen they show and compares the cells with a golden
// file. The programs run in a pseudo-terminal of the test, in a temporary home with TERM=xterm-256color; a program that is not
// installed skips its test. (vim, less, git and tmux give the same screen every time; htop does not, so its test checks the
// layout.)
func TestRecordedPrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("real programs are run in the full run")
	}
	t.Run("vim", func(t *testing.T) {
		home := t.TempDir()
		numberedFile(t, home, "text.txt", 60)
		s := recordProgram(t, 24, 80, home, cleanEnv(home), "vim", "-u", "NONE", "-U", "NONE", "-N", "-n", "-i", "NONE", "--noplugin", "-c", "set ttimeout ttimeoutlen=20", "text.txt")
		s.waitFor("line 1 of", 10*time.Second)
		s.send(":set number ruler\r")
		s.waitFor("  1 line 1", 10*time.Second)
		s.send("5Gcwchanged\x1b")
		s.waitFor("changed", 10*time.Second)
		s.send("/line 3[0-9]\r")
		s.settle(300 * time.Millisecond)
		s.send("\x06") // Ctrl+F, scrolls a page with a scroll region
		s.settle(300 * time.Millisecond)
		s.send("ggOinserted above\x1b")
		s.settle(300 * time.Millisecond)
		matchGolden(t, "vim", s)
		if !s.e.Modes().AltScreen {
			t.Error("vim does not use the alternate screen")
		}
		s.send(":q!\r")
		s.waitExit(10 * time.Second)
	})
	t.Run("less", func(t *testing.T) {
		home := t.TempDir()
		numberedFile(t, home, "text.txt", 100)
		s := recordProgram(t, 24, 80, home, cleanEnv(home, "LESSHISTFILE=-", "LESS="), "less", "-N", "-R", "text.txt")
		s.waitFor("line 1 of", 10*time.Second)
		s.send(" ")
		s.waitFor("line 25 of", 10*time.Second)
		s.send("/line 6[0-9]\r")
		s.waitFor("line 60 of", 10*time.Second)
		s.settle(300 * time.Millisecond)
		matchGolden(t, "less", s)
		s.send("q")
		s.waitExit(10 * time.Second)
	})
	t.Run("git log", func(t *testing.T) {
		home := t.TempDir()
		repo := filepath.Join(home, "repo")
		genv := append(cleanEnv(home), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		git := func(date string, args ...string) {
			t.Helper()
			cmd := exec.CommandContext(t.Context(), "git", args...)
			cmd.Dir = repo
			cmd.Env = append(genv, "GIT_AUTHOR_NAME=A U Thor", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_COMMITTER_NAME=A U Thor",
				"GIT_COMMITTER_EMAIL=a@example.com", "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Skipf("git %v: %v\n%s", args, err, out)
			}
		}
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git is not installed")
		}
		git("2024-01-01T10:00:00Z", "init", "-q", "-b", "main")
		for i, msg := range []string{"first commit", "second commit", "third commit with a longer message"} {
			if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte(strings.Repeat("x\n", i+1)), 0o644); err != nil {
				t.Fatal(err)
			}
			git(fmt.Sprintf("2024-01-0%dT10:00:00Z", i+1), "add", "f.txt")
			git(fmt.Sprintf("2024-01-0%dT10:00:00Z", i+1), "commit", "-q", "-m", msg)
		}
		git("2024-01-04T10:00:00Z", "tag", "v1")
		git("2024-01-04T10:00:00Z", "branch", "feature", "HEAD~1")
		s := recordProgram(t, 24, 80, repo, genv, "git", "--no-pager", "log", "--graph", "--decorate", "--color=always", "--stat", "--date=short")
		s.waitExit(15 * time.Second)
		matchGolden(t, "gitlog", s)
	})
	t.Run("tmux", func(t *testing.T) {
		if _, err := exec.LookPath("tmux"); err != nil {
			t.Skip("tmux is not installed")
		}
		home := t.TempDir()
		conf := filepath.Join(home, "tmux.conf")
		cfg := "set -g status-left '[#S] '\nset -g status-right ''\nset -g automatic-rename off\nset -g allow-rename off\n" +
			"set -g default-command \"sh -c 'printf \\\"pane two\\\\n\\\"; exec cat'\"\n"
		if err := os.WriteFile(conf, []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		sock := "emetos-test-" + strconv.Itoa(os.Getpid())
		env := cleanEnv(home, "TMUX_TMPDIR="+home)
		t.Cleanup(func() {
			cmd := exec.Command("tmux", "-L", sock, "kill-server")
			cmd.Env = env
			_ = cmd.Run()
		})
		s := recordProgram(t, 24, 80, home, env, "tmux", "-L", sock, "-f", conf, "new-session", "-s", "test", "-n", "main",
			"sh -c 'printf \"pane one\\n\"; exec cat'")
		s.waitFor("pane one", 10*time.Second)
		s.waitFor("[test]", 10*time.Second)
		s.send("\x02%") // Ctrl+B %: split into two panes
		s.waitFor("pane two", 10*time.Second)
		s.settle(500 * time.Millisecond)
		matchGolden(t, "tmux", s)
		if s.replies.Load() == 0 {
			t.Log("tmux asked the terminal nothing")
		}
		kill := exec.Command("tmux", "-L", sock, "kill-server")
		kill.Env = env
		if out, err := kill.CombinedOutput(); err != nil {
			t.Errorf("kill-server: %v\n%s", err, out)
		}
		s.waitExit(10 * time.Second)
	})
	t.Run("htop", func(t *testing.T) {
		home := t.TempDir()
		s := recordProgram(t, 30, 100, home, cleanEnv(home), "htop", "-d", "10")
		s.waitFor("F10", 15*time.Second)
		s.waitFor("PID", 15*time.Second)
		time.Sleep(1500 * time.Millisecond) // htop redraws every second: it is never quiet
		text := s.screenText()
		for _, want := range []string{"Tasks:", "PID", "USER", "Command", "Quit"} {
			if !strings.Contains(text, want) {
				t.Errorf("htop's screen lacks %q:\n%s", want, text)
			}
		}
		rows := strings.Split(text, "\n")
		if !strings.Contains(rows[len(rows)-1], "F10") {
			t.Errorf("the function key bar is not on the last row: %q", rows[len(rows)-1])
		}
		// The colors of the meters and of the header row reach the cells.
		var f Frame
		s.e.ReadLines(s.e.Scrollback(), s.e.rows, &f)
		colored := 0
		for y := 0; y < f.Rows; y++ {
			for x := 0; x < f.Cols; x++ {
				if st := f.StyleOf(f.Cell(y, x)); st.Fg != 0 || st.Bg != 0 {
					colored++
				}
			}
		}
		if colored < 200 {
			t.Errorf("only %d colored cells", colored)
		}
		s.send("q")
		s.waitExit(10 * time.Second)
	})
}

// TestReplayRecordings feeds the bytes recorded by TestRecordedPrograms (testdata/programs/*.raw, written by -update-goldens) to
// a new emulator, whole and cut in different ways, and compares the cells with the golden file. It needs none of the programs.
func TestReplayRecordings(t *testing.T) {
	raws, err := filepath.Glob(filepath.Join("testdata", "programs", "*.raw"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raws) == 0 {
		t.Skip("no recordings (run TestRecordedPrograms with -update-goldens)")
	}
	for _, path := range raws {
		name := strings.TrimSuffix(filepath.Base(path), ".raw")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(strings.TrimSuffix(path, ".raw") + ".golden")
			if err != nil {
				t.Fatal(err)
			}
			var rows, cols int
			if _, err := fmt.Sscanf(string(want), "size %dx%d", &cols, &rows); err != nil {
				t.Fatalf("golden header: %v", err)
			}
			for _, chunk := range []int{len(raw), 4096, 100, 7, 1} {
				e := New(rows, cols, Options{Scrollback: 200, Callbacks: Callbacks{Reply: func([]byte) {}}})
				for off := 0; off < len(raw); off += chunk {
					if _, err := e.Write(raw[off:min(off+chunk, len(raw))]); err != nil {
						t.Fatal(err)
					}
				}
				if got := goldenOf(e); got != string(want) {
					t.Fatalf("chunks of %d: the screen differs from the golden file:\n--- got\n%s\n--- want\n%s", chunk, got, want)
				}
				if msg := checkInvariants(e); msg != "" {
					t.Fatalf("chunks of %d: %s", chunk, msg)
				}
			}
		})
	}
}
