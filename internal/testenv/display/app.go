//go:build emetos_display

package display

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/realinput"
)

// Options configure Start.
type Options struct {
	// Args are the arguments of emetos. A relative path is relative to the project folder.
	Args []string
	// Config are files for the configuration folder of the run ($XDG_CONFIG_HOME/emetos): the path relative to it and the
	// text, such as "settings.json".
	Config map[string]string
	// Project are files of the project folder the editor runs in (it is its working directory): the relative path and the text.
	// A nil map leaves the folder empty.
	Project map[string]string
	// Prepare, when set, is called with the project folder after Project is written and before the editor starts, to make
	// files that are not text (images) or are too many for a map.
	Prepare func(projectDir string)
	// State are files for the state folder ($XDG_STATE_HOME/emetos), with the modes Emetos wants (0600, folders 0700).
	State map[string]string
	// Env are more variables for the process (NAME=value).
	Env []string
	// KeyDelay is the pause after each key; 10 ms when zero.
	KeyDelay time.Duration
}

// An App is a running Emetos with a virtual keyboard and pointer pointed at its window.
type App struct {
	t   *testing.T
	cmd *exec.Cmd
	// Project is the project folder, the working directory of the process.
	Project string
	// Input is the virtual keyboard and pointer.
	Input *realinput.Client
	// Env are the variables of the process, which the helpers (grim) share for the socket of the compositor.
	root    string
	bin     string
	env     []string
	devPath string
	logPath string
	wayland string
	mu      sync.Mutex
	dev     net.Conn
	reader  *bufio.Reader
	window  realinput.SwayWindow
	done    chan struct{}
}

var (
	buildOnce sync.Once
	buildDir  string
	buildPath string
	buildErr  error
)

// Main is the body of TestMain for a package of display tests: it runs the tests and removes the binary they built.
func Main(m *testing.M) int {
	code := m.Run()
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
	}
	return code
}

// moduleRoot is the folder that holds go.mod, found upward from the working directory of the test.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// binary builds the debug editor once for the run, cgo-free as the editor must be.
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		if p := os.Getenv("EMETOS_DISPLAY_BINARY"); p != "" {
			buildPath = p
			return
		}
		root, err := moduleRoot()
		if err != nil {
			buildErr = err
			return
		}
		buildDir, buildErr = os.MkdirTemp("", "emetos-display-bin")
		if buildErr != nil {
			return
		}
		buildPath = filepath.Join(buildDir, "emetos")
		build := exec.Command("go", "build", "-o", buildPath, "./cmd/emetos")
		build.Dir = root
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := build.CombinedOutput(); err != nil {
			buildErr = &buildFailure{err: err, output: string(out)}
		}
	})
	if buildErr != nil {
		t.Fatalf("building emetos: %v", buildErr)
	}
	return buildPath
}

// buildFailure is the error of a build that failed, with the compiler's output.
type buildFailure struct {
	err    error
	output string
}

func (b *buildFailure) Error() string { return b.err.Error() + "\n" + b.output }

// writeFiles writes the files of a map under dir, with private modes.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// Start builds the editor (once), starts it isolated in the compositor of the environment, waits until its window is up and
// answers, and gives the keyboard focus to the window. The test is skipped when the environment has no sway compositor; it
// fails when anything else goes wrong. Everything is stopped when the test ends: the editor first (its process group), then the
// virtual devices.
func Start(t *testing.T, opts Options) *App {
	t.Helper()
	display := os.Getenv("WAYLAND_DISPLAY")
	if display == "" || os.Getenv("SWAYSOCK") == "" {
		t.Skip("no sway compositor: run with scripts/headless-wayland.sh -- go test -tags emetos_display ...")
	}
	bin := binary(t)

	// a short folder: a Unix socket path is limited to about 100 bytes
	root, err := os.MkdirTemp("", "emetos-d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	a := &App{t: t, root: root, Project: filepath.Join(root, "project"), done: make(chan struct{})}
	for _, d := range []string{"project", "home", "config", "state", "cache", "data", "run"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFiles(t, a.Project, opts.Project)
	if opts.Prepare != nil {
		opts.Prepare(a.Project)
	}
	writeFiles(t, filepath.Join(root, "config", "emetos"), opts.Config)
	writeFiles(t, filepath.Join(root, "state", "emetos"), opts.State)

	// the compositor stays reachable through a link in the private runtime folder (scripts/isolated.sh does the same)
	socket := display
	if !filepath.IsAbs(socket) {
		socket = filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), display)
	}
	a.wayland = filepath.Base(socket)
	if err := os.Symlink(socket, filepath.Join(root, "run", a.wayland)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// the virtual keyboard comes first and stays until the editor is gone: a compositor with no keyboard sends no_keymap
	a.Input, err = realinput.Connect(ctx, realinput.Options{Socket: socket, KeyDelay: opts.KeyDelay})
	if err != nil {
		t.Fatalf("realinput.Connect: %v", err)
	}
	t.Cleanup(func() {
		if err := a.Input.Close(); err != nil {
			t.Errorf("closing the virtual devices: %v", err)
		}
	})

	a.devPath = filepath.Join(root, "run", "dev.sock")
	a.logPath = filepath.Join(root, "emetos.log")
	logFile, err := os.Create(a.logPath)
	if err != nil {
		t.Fatal(err)
	}
	a.bin = bin
	cmd := exec.Command(bin, opts.Args...)
	cmd.Dir = a.Project
	a.env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + filepath.Join(root, "home"),
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"XDG_STATE_HOME=" + filepath.Join(root, "state"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
		"XDG_DATA_HOME=" + filepath.Join(root, "data"),
		"XDG_RUNTIME_DIR=" + filepath.Join(root, "run"),
		"WAYLAND_DISPLAY=" + a.wayland,
		"GLUTTER_RENDERER=" + envOr("GLUTTER_RENDERER", "gles"),
		// nothing of the user's session: no session bus (keyring, tray, portals), no X server
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/bus",
		"LANG=C.UTF-8",
	}, opts.Env...)
	cmd.Env = append(append([]string(nil), a.env...), app.DevSocketEnv+"="+a.devPath)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	// its own process group, so that the whole group can be stopped
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting emetos: %v", err)
	}
	a.cmd = cmd
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		close(a.done)
	}()
	t.Cleanup(a.stop)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the log of emetos:\n%s", a.Log())
		}
	})

	a.waitUp(ctx)
	return a
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// waitUp waits for the window to be mapped and the dev server to answer, then focuses the window.
func (a *App) waitUp(ctx context.Context) {
	a.t.Helper()
	exited := func() bool {
		select {
		case <-a.done:
			return true
		default:
			return false
		}
	}
	w, err := realinput.SwayWindowOf(ctx, a.cmd.Process.Pid)
	if err != nil {
		a.t.Fatalf("the window of emetos did not appear: %v\n%s", err, a.Log())
	}
	for {
		if exited() {
			a.t.Fatalf("emetos ended before it was up:\n%s", a.Log())
		}
		conn, err := net.DialTimeout("unix", a.devPath, time.Second)
		if err == nil {
			a.dev, a.reader = conn, bufio.NewReader(conn)
			a.t.Cleanup(func() { _ = conn.Close() })
			// the first answer comes from the UI thread: it is running
			if _, err := a.Request("dev.inputs", nil); err == nil {
				break
			}
			_ = conn.Close()
		}
		select {
		case <-ctx.Done():
			a.t.Fatalf("the dev server of emetos did not answer:\n%s", a.Log())
		case <-time.After(50 * time.Millisecond):
		}
	}
	// the window is focused by the compositor when it maps; make sure, since the keys go to the focused window
	if err := realinput.SwayCommand(ctx, "[pid="+itoa(a.cmd.Process.Pid)+"] focus"); err != nil {
		a.t.Fatalf("focusing the window: %v", err)
	}
	a.window = w
	a.RefreshWindow()
	a.Settle()
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// stop ends the editor: SIGTERM to its process group, then SIGKILL.
func (a *App) stop() {
	if a.cmd == nil || a.cmd.Process == nil {
		return
	}
	pgid := a.cmd.Process.Pid
	select {
	case <-a.done:
		return
	default:
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case <-a.done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-a.done
	}
}

// Launch runs the editor a second time with arguments, in the same isolated environment and the same folder (a user's second
// `emetos file`), and returns what it printed. The second process finds the first one through its socket, hands the files over
// and ends. It has no dev socket of its own. The test fails when it does not end within 30 seconds.
func (a *App) Launch(args ...string) string {
	a.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.bin, args...)
	cmd.Dir = a.Project
	cmd.Env = append([]string(nil), a.env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	out, err := cmd.CombinedOutput()
	if err != nil {
		a.t.Fatalf("the second launch %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// Pid is the process id of the editor.
func (a *App) Pid() int { return a.cmd.Process.Pid }

// ResidentBytes is the resident memory of the editor (VmRSS of /proc/<pid>/status).
func (a *App) ResidentBytes() int64 {
	a.t.Helper()
	data, err := os.ReadFile("/proc/" + itoa(a.Pid()) + "/status")
	if err != nil {
		a.t.Fatalf("reading the memory of the editor: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				break
			}
			kb, err := strconv.ParseInt(fields[0], 10, 64)
			if err != nil {
				a.t.Fatalf("reading VmRSS %q: %v", rest, err)
			}
			return kb << 10
		}
	}
	a.t.Fatalf("no VmRSS in /proc/%d/status", a.Pid())
	return 0
}

// MainLog is the editor's own log file (state/emetos/logs/*/main.log of the run), where Glutter's statistics go.
func (a *App) MainLog() string {
	files, err := filepath.Glob(filepath.Join(a.root, "state", "emetos", "logs", "*", "main.log"))
	if err != nil {
		return ""
	}
	var all strings.Builder
	for _, f := range files {
		if data, err := os.ReadFile(f); err == nil {
			all.Write(data)
		}
	}
	return all.String()
}

// FirstFrame is the time of the first frame the editor presented, from its log.
func (a *App) FirstFrame() (FirstFrame, bool) { return ParseFirstFrame(a.MainLog()) }

// FrameWindows are the "frames" lines the editor logged so far, when it runs with GLUTTER_FRAME_STATS=1.
func (a *App) FrameWindows() []FrameWindow { return ParseFrameStats(a.MainLog()) }

// Log is what the editor wrote to its output and error so far.
func (a *App) Log() string {
	data, err := os.ReadFile(a.logPath)
	if err != nil {
		return "(no log: " + err.Error() + ")"
	}
	return strings.TrimSpace(string(data))
}

// Request runs a command through the dev server and returns its response. The error is that of the exchange; a command that
// failed is a response with OK false.
func (a *App) Request(command string, args any) (app.DevResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	req := app.DevRequest{Command: command}
	if args != nil {
		raw, err := json.Marshal(args)
		if err != nil {
			return app.DevResponse{}, err
		}
		req.Args = raw
	}
	line, err := json.Marshal(req)
	if err != nil {
		return app.DevResponse{}, err
	}
	if err := a.dev.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return app.DevResponse{}, err
	}
	if _, err := a.dev.Write(append(line, '\n')); err != nil {
		return app.DevResponse{}, err
	}
	var resp app.DevResponse
	data, err := a.reader.ReadBytes('\n')
	if err != nil {
		return app.DevResponse{}, err
	}
	return resp, json.Unmarshal(data, &resp)
}

// Dev runs a dev command and fails the test when it does not succeed.
func (a *App) Dev(command string, args any) app.DevResponse {
	a.t.Helper()
	resp, err := a.Request(command, args)
	if err != nil {
		a.t.Fatalf("%s: %v\n%s", command, err, a.Log())
	}
	if !resp.OK {
		a.t.Fatalf("%s failed: %s (%s)", command, resp.Error, resp.Tag)
	}
	return resp
}

// Inputs lists the text fields on screen (dev.inputs).
func (a *App) Inputs() []app.DevInput {
	a.t.Helper()
	resp := a.Dev("dev.inputs", nil)
	var list []app.DevInput
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		a.t.Fatalf("dev.inputs: %v", err)
	}
	return list
}

// Focused is the text field that has the primary focus, or false when none has.
func (a *App) Focused() (app.DevInput, bool) {
	a.t.Helper()
	for _, in := range a.Inputs() {
		if in.Focused {
			return in, true
		}
	}
	return app.DevInput{}, false
}

// Previews lists the Markdown previews on screen (dev.markdown).
func (a *App) Previews() []app.DevMarkdown {
	a.t.Helper()
	resp := a.Dev("dev.markdown", nil)
	var list []app.DevMarkdown
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		a.t.Fatalf("dev.markdown: %v", err)
	}
	return list
}

// Editor is the document of the active editor and its cursor (dev.editor).
func (a *App) Editor() app.DevEditor {
	a.t.Helper()
	resp := a.Dev("dev.editor", nil)
	var ed app.DevEditor
	if err := json.Unmarshal(resp.Result, &ed); err != nil {
		a.t.Fatalf("dev.editor: %v", err)
	}
	return ed
}

// Texts lists the runs of text drawn on screen with their places (dev.texts).
func (a *App) Texts() []app.DevText {
	a.t.Helper()
	resp := a.Dev("dev.texts", nil)
	var list []app.DevText
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		a.t.Fatalf("dev.texts: %v", err)
	}
	return list
}

// Text is the run of text on screen that is exactly text, or false when there is none. When several are (a label shown twice),
// the first from the top is returned.
func (a *App) Text(text string) (app.DevText, bool) {
	a.t.Helper()
	var best app.DevText
	found := false
	for _, tx := range a.Texts() {
		if tx.Text == text && (!found || tx.Y < best.Y || (tx.Y == best.Y && tx.X < best.X)) {
			best, found = tx, true
		}
	}
	return best, found
}

// ClickText clicks the middle of the run of text on screen that is exactly text; the test fails when there is none.
func (a *App) ClickText(text string) {
	a.t.Helper()
	tx, ok := a.Text(text)
	if !ok {
		a.t.Fatalf("no text %q on screen; on screen: %s", text, a.shown())
	}
	a.Click(tx.X+tx.Width/2, tx.Y+tx.Height/2)
}

// RightClickText right-clicks the middle of the run of text that is exactly text.
func (a *App) RightClickText(text string) {
	a.t.Helper()
	tx, ok := a.Text(text)
	if !ok {
		a.t.Fatalf("no text %q on screen; on screen: %s", text, a.shown())
	}
	a.RightClick(tx.X+tx.Width/2, tx.Y+tx.Height/2)
}

// Dump lists the texts on screen with their places, for the message of a test that did not find what it looked for.
func (a *App) Dump() string {
	var b strings.Builder
	for _, tx := range a.Texts() {
		b.WriteString(quote(tx.Text) + " " + itoa(int(tx.X)) + "," + itoa(int(tx.Y)) + "\n")
	}
	return b.String()
}

// shown lists the texts on screen, for the message of a test that did not find one.
func (a *App) shown() string {
	var b strings.Builder
	for _, tx := range a.Texts() {
		b.WriteString(quote(tx.Text))
		b.WriteByte(' ')
	}
	return b.String()
}

// WaitText waits until a run of text that is exactly text is on screen, and returns it.
func (a *App) WaitText(text string) app.DevText {
	a.t.Helper()
	var found app.DevText
	a.Eventually("the text "+quote(text)+" on screen", func() (bool, string) {
		tx, ok := a.Text(text)
		found = tx
		return ok, a.shown()
	})
	return found
}

// WaitNoText waits until no run of text on screen is exactly text.
func (a *App) WaitNoText(text string) {
	a.t.Helper()
	a.Eventually("the text "+quote(text)+" to leave the screen", func() (bool, string) {
		_, ok := a.Text(text)
		return !ok, a.shown()
	})
}

// Workbench is the state of the workbench of the active tab (dev.workbench).
func (a *App) Workbench() app.DevWorkbench {
	a.t.Helper()
	resp := a.Dev("dev.workbench", nil)
	var w app.DevWorkbench
	if err := json.Unmarshal(resp.Result, &w); err != nil {
		a.t.Fatalf("dev.workbench: %v", err)
	}
	return w
}

// TryWorkbench is Workbench for a moment when the workspace may be gone (a folder is being opened): false when there is none.
func (a *App) TryWorkbench() (app.DevWorkbench, bool) {
	a.t.Helper()
	resp, err := a.Request("dev.workbench", nil)
	if err != nil {
		a.t.Fatalf("dev.workbench: %v", err)
	}
	var w app.DevWorkbench
	if !resp.OK || json.Unmarshal(resp.Result, &w) != nil {
		return app.DevWorkbench{}, false
	}
	return w, true
}

// Settle waits until what was sent to the compositor has reached the editor and been drawn: the compositor's round trip, a
// pause, and a dev command, which the UI thread runs after the frames of the events it has read.
func (a *App) Settle() {
	a.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.Input.Sync(ctx); err != nil {
		a.t.Fatalf("waiting for the compositor: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	a.Dev("dev.inputs", nil)
}

// Eventually polls check until it returns true or five seconds pass; the test fails with what check last said. It is how a
// test waits for what real input does in the editor, which handles the events a moment after the compositor sent them.
func (a *App) Eventually(what string, check func() (bool, string)) {
	a.t.Helper()
	a.EventuallyFor(5*time.Second, what, check)
}

// EventuallyFor is Eventually with the time to wait, for what a program outside the editor does (a language server that loads).
func (a *App) EventuallyFor(wait time.Duration, what string, check func() (bool, string)) {
	a.t.Helper()
	deadline := time.Now().Add(wait)
	for {
		ok, state := check()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			a.t.Fatalf("timed out waiting for %s; last state: %s", what, state)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// FocusedText is the text of the focused text field, for Eventually.
func (a *App) FocusedText() string {
	in, ok := a.Focused()
	if !ok {
		return "(no text field has the focus)"
	}
	return in.Text
}

// WaitFocusedText waits until the focused text field holds exactly text.
func (a *App) WaitFocusedText(text string) {
	a.t.Helper()
	a.Eventually("the focused text field to hold "+quote(text), func() (bool, string) {
		got := a.FocusedText()
		return got == text, quote(got)
	})
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// ---- input ----

// Type types the text into the window.
func (a *App) Type(text string) {
	a.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(text)+10)*time.Second)
	defer cancel()
	if err := a.Input.Type(ctx, text); err != nil {
		a.t.Fatalf("typing %q: %v", text, err)
	}
}

// Press presses key combinations ("ctrl+p", "Escape", "f2").
func (a *App) Press(combos ...string) {
	a.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.Input.Press(ctx, combos...); err != nil {
		a.t.Fatalf("pressing %v: %v", combos, err)
	}
}

// RefreshWindow reads where the window is from sway and tells the pointer, since a window can move.
func (a *App) RefreshWindow() {
	a.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w, err := realinput.SwayWindowOf(ctx, a.cmd.Process.Pid)
	if err != nil {
		a.t.Fatalf("finding the window: %v", err)
	}
	layout, err := realinput.SwayLayout(ctx)
	if err != nil {
		a.t.Fatalf("reading the outputs: %v", err)
	}
	a.window = w
	a.Input.SetWindow(realinput.Point{X: w.Content.X, Y: w.Content.Y}, layout)
}

// Window is the window as sway last reported it.
func (a *App) Window() realinput.SwayWindow { return a.window }

// pointer runs a pointer action at window coordinates after refreshing the window position.
func (a *App) pointer(what string, f func(ctx context.Context) error) {
	a.t.Helper()
	a.RefreshWindow()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := f(ctx); err != nil {
		a.t.Fatalf("%s: %v", what, err)
	}
	// A tap is decided when the button is released and the recognizers have had their say, a moment after the compositor sent
	// it; a user's next key comes much later than that.
	a.Settle()
}

// MoveTo moves the pointer to a position of the window, without a button.
func (a *App) MoveTo(x, y float64) {
	a.t.Helper()
	a.pointer("move", func(ctx context.Context) error { return a.Input.MoveTo(ctx, realinput.Point{X: x, Y: y}) })
}

// ClickScrollbar clicks a track of a scroll bar that fades away: a click on it is ignored unless the bar was shown by a move
// of the content a moment before, so a turn of the wheel at the same place comes first.
func (a *App) ClickScrollbar(x, y float64) {
	a.t.Helper()
	a.RefreshWindow()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	at := realinput.Point{X: x, Y: y}
	if err := a.Input.Scroll(ctx, at, 0, 1); err != nil {
		a.t.Fatalf("scroll: %v", err)
	}
	if err := a.Input.Click(ctx, at); err != nil {
		a.t.Fatalf("click: %v", err)
	}
	a.Settle()
}

// Click clicks the left button at a position of the window.
func (a *App) Click(x, y float64) {
	a.t.Helper()
	a.pointer("click", func(ctx context.Context) error { return a.Input.Click(ctx, realinput.Point{X: x, Y: y}) })
}

// ClickInput clicks the middle of a text field.
func (a *App) ClickInput(in app.DevInput) {
	a.t.Helper()
	a.Click(in.X+in.Width/2, in.Y+in.Height/2)
}

// DoubleClick double-clicks at a position of the window.
func (a *App) DoubleClick(x, y float64) {
	a.t.Helper()
	a.pointer("double click", func(ctx context.Context) error { return a.Input.DoubleClick(ctx, realinput.Point{X: x, Y: y}) })
}

// RightClick clicks the right button at a position of the window.
func (a *App) RightClick(x, y float64) {
	a.t.Helper()
	a.pointer("right click", func(ctx context.Context) error { return a.Input.RightClick(ctx, realinput.Point{X: x, Y: y}) })
}

// Scroll turns the wheel at a position of the window.
func (a *App) Scroll(x, y float64, clicksX, clicksY int) {
	a.t.Helper()
	a.pointer("scroll", func(ctx context.Context) error {
		return a.Input.Scroll(ctx, realinput.Point{X: x, Y: y}, clicksX, clicksY)
	})
}

// Drag drags the left button from one position of the window to another.
func (a *App) Drag(fromX, fromY, toX, toY float64) {
	a.t.Helper()
	a.pointer("drag", func(ctx context.Context) error {
		return a.Input.Drag(ctx, realinput.Point{X: fromX, Y: fromY}, realinput.Point{X: toX, Y: toY}, 10)
	})
}

// Screenshot saves the window as a PNG with grim and returns the path.
func (a *App) Screenshot(path string) string {
	a.t.Helper()
	a.RefreshWindow()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		a.t.Fatal(err)
	}
	c := a.window.Content
	geometry := itoa(int(c.X)) + "," + itoa(int(c.Y)) + " " + itoa(int(c.Width)) + "x" + itoa(int(c.Height))
	cmd := exec.Command("grim", "-g", geometry, path)
	cmd.Env = append(os.Environ(), "WAYLAND_DISPLAY="+os.Getenv("WAYLAND_DISPLAY"))
	if out, err := cmd.CombinedOutput(); err != nil {
		a.t.Fatalf("grim: %v\n%s", err, out)
	}
	return path
}
