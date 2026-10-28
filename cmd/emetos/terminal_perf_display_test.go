//go:build emetos_display

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The speed of the terminal (step 17d.2, SPEC §14 "Terminal output", "Terminal frame" and "Terminal typing"): the tests of this
// file run the release build with real input in the compositor of the run and print what they measured. They are not part of the
// default run of scripts/realinput.sh; scripts/terminal-perf.sh runs them (with --desktop on the GPU of the desktop).

const (
	// perfBigLines of about 100 bytes make the 100 MB file of the budget.
	perfBigBytes = 100 << 20
	// perfFrames is how many full screens the file of the frame budget has.
	perfFrames = 6000
	// perfCols and perfRows are the size of the terminal of the budgets.
	perfCols, perfRows = 160, 45
)

// perfMeasureScript runs a command and prints how long it took in milliseconds, as the shell measures it.
const perfMeasureScript = `s=$(date +%s%N)
cat "$1"
e=$(date +%s%N)
echo CATMS=$(( (e-s)/1000000 ))
`

// writeBigFile writes a text file of about 100 MB that ends with a line of END.
func writeBigFile(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriterSize(f, 1<<20)
	words := strings.Fields("the quick brown fox jumps over a lazy dog while terminals draw glyphs from an atlas and scroll faster than the eye can follow 0123456789 error warning info")
	written, n := 0, 0
	for written < perfBigBytes {
		var line strings.Builder
		fmt.Fprintf(&line, "2026-10-28T12:%02d:%02d.%03d INFO [worker-%d] ", n/60%60, n%60, n%1000, n%16)
		for i := 0; i < 8+n%12; i++ {
			line.WriteString(words[(n*7+i*13)%len(words)])
			line.WriteByte(' ')
		}
		line.WriteByte('\n')
		c, err := w.WriteString(line.String())
		if err != nil {
			t.Fatal(err)
		}
		written += c
		n++
	}
	if _, err := w.WriteString("END\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// writeFramesFile writes perfFrames screens of 160 x 45 cells that all change from one to the next, followed by END.
func writeFramesFile(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriterSize(f, 1<<20)
	put := func(s string) {
		if _, err := w.WriteString(s); err != nil {
			t.Fatal(err)
		}
	}
	for frame := range perfFrames {
		put("\x1b[H")
		for y := range perfRows {
			for x := 0; x < perfCols; x += 8 {
				put("\x1b[3" + string(rune('1'+(x/8+y+frame)%6)) + "m")
				var run [8]byte
				for i := range run {
					run[i] = byte('a' + (x+i+y+frame)%26)
				}
				put(string(run[:]))
			}
			put("\x1b[0m")
			if y < perfRows-1 {
				put("\r\n")
			}
		}
	}
	put("\x1b[0m\x1b[H\x1b[2JEND\n")
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// startPerfTerminal starts the editor with the terminal made as large as the budgets say (160 x 45 cells or more): the panel
// maximized, the side bar hidden. The window of the run should be 1920 x 1080 (scripts/terminal-perf.sh).
func startPerfTerminal(t *testing.T, project map[string]string, prepare func(dir string)) *display.App {
	t.Helper()
	files := map[string]string{"app.go": "package main\n", "measure.sh": perfMeasureScript}
	for k, v := range project {
		files[k] = v
	}
	a := display.Start(t, display.Options{
		Args: []string{".", "app.go"}, Project: files, Config: terminalConfig, Prepare: prepare,
		Env: []string{"GLUTTER_FRAME_STATS=1"},
	})
	a.ResizeWindow(1920, 1080)
	a.Press("ctrl+b")
	a.Press("ctrl+grave")
	waitTerminals(t, a, 1)
	waitPrompt(t, a, 0)
	a.Press("ctrl+shift+p")
	a.Type("Toggle Maximized Panel")
	a.Press("Return")
	a.Eventually("a terminal of 160 x 45 cells", func() (bool, string) {
		l := a.Terminals()
		return len(l) == 1 && l[0].Cols >= perfCols && l[0].Rows >= perfRows, fmt.Sprintf("%+v", l)
	})
	// the frames of the start and of the maximize (the first draw of the window, the glyphs of a new size) are logged now, so
	// that the windows of the run itself hold nothing else
	settleFrameWindows(a)
	return a
}

// perfWindows are the frame statistics logged from a start on: the window after before, until the last one.
func perfWindows(a *display.App, before int) display.FrameSummary {
	return display.Summarize(a.FrameWindows()[before:])
}

// settleFrameWindows lets the window of statistics that holds the end of the run be logged: Glutter logs one once a second, and
// only while it draws frames.
func settleFrameWindows(a *display.App) {
	a.Settle()
	for range 3 {
		a.MoveTo(5, 5)
		time.Sleep(700 * time.Millisecond)
		a.MoveTo(9, 9)
	}
}

func logSummary(t *testing.T, what string, s display.FrameSummary) {
	t.Helper()
	t.Logf("PERF %s: %d frames, the UI thread %v on average and %v at worst, the renderer %v and %v, %d frames over 16 ms",
		what, s.Frames, s.BuildAvg, s.BuildMax, s.RasterAvg, s.RasterMax, s.Over16)
}

// catMillis reads the CATMS=<n> line of the screen.
func catMillis(text string) (int, bool) {
	for _, line := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(line, "CATMS="); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			return n, err == nil
		}
	}
	return 0, false
}

// TestPerfTerminalOutput: `cat` of a 100 MB text file in a terminal of 160 x 45 cells (SPEC §14 "Terminal output").
func TestPerfTerminalOutput(t *testing.T) {
	a := startPerfTerminal(t, nil, func(dir string) { writeBigFile(t, filepath.Join(dir, "big.txt")) })
	cols, rows := a.Terminals()[0].Cols, a.Terminals()[0].Rows
	before := len(a.FrameWindows())
	start := time.Now()
	t.Logf("the command is typed at %s", start.Format("15:04:05.000"))
	a.Type("bash measure.sh big.txt")
	a.Press("Return")
	var ms int
	a.EventuallyFor(60*time.Second, "cat to finish", func() (bool, string) {
		n, ok := catMillis(termText(a, 0))
		ms = n
		return ok, termText(a, 0)
	})
	total := time.Since(start)
	settleFrameWindows(a)
	t.Logf("PERF terminal output: cat of %d MB in a terminal of %d x %d: %d ms by the shell's clock, the screen showed the end %v after the command", perfBigBytes>>20, cols, rows, ms, total.Round(time.Millisecond))
	logSummary(t, "terminal output", perfWindows(a, before))
	for i, w := range a.FrameWindows()[before:] {
		t.Logf("window %d: %+v", i, w)
	}
	for _, line := range strings.Split(a.MainLog(), "\n") {
		if strings.Contains(line, "slow frame") {
			t.Log(line)
		}
	}
}

// TestPerfTerminalFrames: a full screen of 160 x 45 cells that changes in every frame (SPEC §14 "Terminal frame").
func TestPerfTerminalFrames(t *testing.T) {
	a := startPerfTerminal(t, nil, func(dir string) { writeFramesFile(t, filepath.Join(dir, "frames.txt")) })
	before := len(a.FrameWindows())
	a.Type("bash measure.sh frames.txt")
	a.Press("Return")
	var ms int
	a.EventuallyFor(60*time.Second, "the frames to be shown", func() (bool, string) {
		n, ok := catMillis(termText(a, 0))
		ms = n
		return ok, termText(a, 0)
	})
	settleFrameWindows(a)
	t.Logf("PERF terminal frames: %d full screens of %d x %d cells shown in %d ms", perfFrames, perfCols, perfRows, ms)
	logSummary(t, "terminal frames", perfWindows(a, before))
}

// TestPerfTerminalTyping: keys at a shell prompt (SPEC §14 "Terminal typing"): the frames the echo makes.
func TestPerfTerminalTyping(t *testing.T) {
	a := startPerfTerminal(t, nil, nil)
	before := len(a.FrameWindows())
	text := strings.Repeat("the quick brown fox ", 15)
	a.Type(text)
	start := time.Now()
	a.Eventually("the echo", func() (bool, string) {
		return strings.Contains(termText(a, 0), strings.TrimSpace(text[len(text)-20:])), termText(a, 0)
	})
	t.Logf("PERF terminal typing: %d characters typed, the last one shown %v after the end of the typing", len(text), time.Since(start).Round(time.Millisecond))
	settleFrameWindows(a)
	logSummary(t, "terminal typing", perfWindows(a, before))
}

// TestPerfTerminalFoot is the same cat in foot, in the compositor of the run, for comparison (foot is installed on the
// development machine; Ghostty is not). It runs at the size of the budget and at the size Emetos's terminal has in the
// maximized panel of a window of 1920 x 1080.
func TestPerfTerminalFoot(t *testing.T) {
	if _, err := exec.LookPath("foot"); err != nil {
		t.Skip("foot is not installed")
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no compositor")
	}
	dir := t.TempDir()
	writeBigFile(t, filepath.Join(dir, "big.txt"))
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, size := range []string{"160x45", "231x52"} {
		out := filepath.Join(dir, "out-"+size+".txt")
		script := "s=$(date +%s%N)\ncat big.txt\ne=$(date +%s%N)\n{ echo CATMS=$(( (e-s)/1000000 )); stty size; } > " + out + "\n"
		if err := os.WriteFile(filepath.Join(dir, "foot.sh"), []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		// foot gets the window of the size it asks for; stty tells the size of its terminal
		cmd := exec.Command("foot", "--config=/dev/null", "--window-size-chars="+size, "-e", "bash", "--norc", "--noprofile", "foot.sh")
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
			"XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "XDG_RUNTIME_DIR=" + os.Getenv("XDG_RUNTIME_DIR"),
			"WAYLAND_DISPLAY=" + os.Getenv("WAYLAND_DISPLAY"), "LANG=C.UTF-8", "TERM=xterm-256color"}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		deadline := time.Now().Add(90 * time.Second)
		for {
			data, _ := os.ReadFile(out)
			if ms, ok := catMillis(string(data)); ok && strings.Contains(string(data), "\n") && afterLine(string(data), "CATMS=") != "" {
				t.Logf("PERF foot: cat of %d MB: %d ms by the shell's clock; asked for %s, the size of its terminal (rows columns): %s", perfBigBytes>>20, ms, size, strings.TrimSpace(afterLine(string(data), "CATMS=")))
				break
			}
			if time.Now().After(deadline) {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				<-done
				t.Fatalf("foot did not finish: %q", data)
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
}

// afterLine is the text after the line that starts with prefix.
func afterLine(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, prefix) && i+1 < len(lines) {
			return strings.Join(lines[i+1:], " ")
		}
	}
	return ""
}
