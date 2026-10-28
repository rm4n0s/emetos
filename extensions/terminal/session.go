package terminal

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/pty"
	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// The sizes of the flow control (SPEC §10.25 "The process" and "Speed"): the program's output is read in chunks of readChunk
// bytes; reading pauses when more than highWater bytes wait to be parsed and goes on when fewer than lowWater do (VS Code's
// flow control constants: pause above 100,000 unprocessed characters, resume below 5,000).
const (
	readChunk = 64 << 10
	highWater = 100_000
	lowWater  = 5_000
)

// process is the program of a terminal. *pty.Process is one; the tests have others.
type process interface {
	io.Reader
	io.Writer
	// Resize sets the size of the terminal of the program.
	Resize(rows, cols int) error
	// Close ends the program the way closing a terminal does (SIGHUP, then SIGKILL after the grace time) and closes the
	// terminal, so that a Read that waits returns io.EOF.
	Close()
	// Signal sends a signal to the program's group.
	Signal(sig syscall.Signal)
	// Done is closed when the program and its group are gone, and ExitCode is its status then (-1 for a signal).
	Done() <-chan struct{}
	ExitCode() int
	Pid() int
}

// foregrounder is a process that knows its foreground process group (*pty.Process).
type foregrounder interface{ ForegroundPid() (int, error) }

// starter starts the program of a terminal.
type starter func(ctx context.Context, opts pty.Options) (process, error)

// ptyStarter starts a real program in a pseudo-terminal.
func ptyStarter(ctx context.Context, opts pty.Options) (process, error) {
	p, err := pty.Start(ctx, opts)
	if err != nil {
		return nil, err // the error is the package's own: its tag and route stay
	}
	return p, nil
}

// terminalOpts say how a terminal starts.
type terminalOpts struct {
	id      int
	profile profile
	dir     string
	env     []string
	rows    int
	cols    int
	// scrollback is the number of lines of scrollback.
	scrollback int
	version    string
	start      starter
	log        *slog.Logger
	// grace is how long a program has to end after SIGHUP before it is killed; 0 is 5 seconds.
	grace time.Duration
	// obs receives the events of the terminal (SPEC §10.25 "Events and requests"); nil is none.
	obs *observer
	// interval is how long the output is gathered before it is published; 0 is outputInterval.
	interval time.Duration
	// snapshotLimit is the most a snapshot's data may be; 0 is maxSnapshotBytes.
	snapshotLimit int
	// nonce is the secret the shell integration scripts print with the command line (inject.go); "" believes no command line.
	nonce string
	// injected says that the shell integration scripts were injected into the shell (the tab's hover says so).
	injected bool
}

// Terminal is one terminal: the program in its pseudo-terminal, the emulator that turns its output into a screen, and what a
// tab shows about it (SPEC §10.25). It does not know where it is shown. Three goroutines belong to it: one reads the output
// of the program in chunks and one parses them (it owns the emulator; the user interface takes a snapshot of the rows that
// changed once a frame), and one writes what the user types and what the emulator answers. Everything but the widget's own
// state is safe to use from any goroutine.
type Terminal struct {
	id   int
	log  *slog.Logger
	prof profile
	dir  string
	em   *emulator.Emulator
	proc process

	// flow control: unparsed bytes were read and wait for the parser.
	flowMu   sync.Mutex
	flowCond *sync.Cond
	unparsed int

	// input waits to be written to the program.
	inMu   sync.Mutex
	inBuf  []byte
	inWake chan struct{}

	mu       sync.Mutex
	name     string // set by the user (rename); empty
	title    string // the title the program set with an escape sequence
	process  string // the name of the foreground process
	cwd      string // the folder the shell reported
	icon     string
	color    string
	bellAt   time.Time
	exited   bool
	exitCode int
	rows     int
	cols     int

	// changed says that the screen changed since the widget last took a snapshot; onFrame and onMeta are told (from any
	// goroutine) that the screen, or what the tab shows, changed.
	changed atomic.Bool
	onFrame atomic.Pointer[func()]
	onMeta  atomic.Pointer[func()]
	closed  atomic.Bool

	// exitedCh is closed when the program is gone and its last output was parsed.
	exitedCh chan struct{}
	wg       sync.WaitGroup

	// si follows the shell integration (shellint.go); injected says the scripts were injected, shellName is the kind of shell
	si        *shellState
	injected  bool
	shellName string

	// the stream of the output for the observer (stream.go): outMu holds the parser's write and the bytes that wait to be
	// published together, so that a snapshot is the screen after exactly the output of the events up to its seq.
	obs      *observer
	snapMax  int
	outMu    sync.Mutex
	outBuf   []byte
	outTail  []byte
	outSeq   uint64
	outTimer *time.Timer
}

// chunk is a piece of the program's output.
type chunk struct {
	buf []byte
}

var chunkPool = sync.Pool{New: func() any { b := make([]byte, readChunk); return &b }}

// newTerminal starts the program and the goroutines of a terminal. The error is that of pty.Start.
func newTerminal(ctx context.Context, o terminalOpts) (*Terminal, error) {
	if o.rows <= 0 {
		o.rows = 24
	}
	if o.cols <= 0 {
		o.cols = 80
	}
	if o.start == nil {
		o.start = ptyStarter
	}
	if o.log == nil {
		o.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.obs != nil && o.interval > 0 {
		o.obs.interval = o.interval
	}
	if o.snapshotLimit <= 0 {
		o.snapshotLimit = maxSnapshotBytes
	}
	t := &Terminal{id: o.id, log: o.log.With("terminal", o.id), prof: o.profile, dir: o.dir, rows: o.rows, cols: o.cols,
		inWake: make(chan struct{}, 1), exitedCh: make(chan struct{}), icon: o.profile.icon, obs: o.obs, snapMax: o.snapshotLimit,
		injected: o.injected, shellName: shellNameOf(o.profile.path)}
	t.flowCond = sync.NewCond(&t.flowMu)
	t.em = emulator.New(o.rows, o.cols, emulator.Options{
		Scrollback: o.scrollback, Name: "Emetos", Version: o.version, MarkCodes: shellOSCCodes,
		Callbacks: emulator.Callbacks{
			OSC:   func(ev emulator.OSCEvent) { t.si.handle(ev) },
			Reply: func(p []byte) { t.Send(p) },
			Title: func(s string) {
				t.mu.Lock()
				t.title = s
				t.mu.Unlock()
				t.fireMeta()
				if t.obs != nil && t.obs.title != nil {
					t.obs.title(s)
				}
			},
			Bell: func() {
				t.mu.Lock()
				t.bellAt = time.Now()
				t.mu.Unlock()
				t.fireMeta()
			},
			Cwd: func(_, path string) {
				t.mu.Lock()
				t.cwd = path
				t.mu.Unlock()
				t.fireMeta()
			},
			PaletteChanged: func() { t.markChanged() },
		},
	})
	t.si = newShellState(t.em, o.nonce)
	t.si.onCwd = func(dir string) {
		t.mu.Lock()
		t.cwd = dir
		t.mu.Unlock()
		t.fireMeta()
	}
	t.si.onChange = t.markChanged
	grace := o.grace
	proc, err := o.start(ctx, pty.Options{
		Program: o.profile.path, Args: o.profile.args, Dir: o.dir, Env: o.env, Rows: o.rows, Cols: o.cols, Grace: grace,
	})
	if err != nil {
		return nil, err // pty.Start's own error: its tag and route stay
	}
	t.proc = proc
	t.wg.Add(4)
	chunks := make(chan chunk, 8)
	go t.readLoop(chunks)
	go t.parseLoop(chunks)
	go t.writeLoop()
	go t.watchProcessName()
	return t, nil
}

// ShellIntegration is the state of the shell integration of the terminal (shellint.go).
func (t *Terminal) ShellIntegration() *shellState { return t.si }

// ShellName is the kind of shell of the terminal: the base name of its program ("bash", "zsh", "fish", "pwsh", "sh"...).
func (t *Terminal) ShellName() string { return t.shellName }

// shellNameOf is the kind of shell of a program: its base name in lower case, without ".exe"; Windows PowerShell counts as pwsh.
func shellNameOf(path string) string {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(path)), ".exe")
	if name == "powershell" {
		return shellPwsh
	}
	return name
}

// ID is the number of the terminal in its window tab; the first is 1.
func (t *Terminal) ID() int { return t.id }

// Emulator is the emulator of the terminal; the widget takes its snapshots.
func (t *Terminal) Emulator() *emulator.Emulator { return t.em }

// Pid is the process ID of the program, 0 for a terminal whose program is gone.
func (t *Terminal) Pid() int {
	if t.proc == nil {
		return 0
	}
	return t.proc.Pid()
}

// SetOnFrame sets the function that is told, from any goroutine, that the screen changed; nil takes it away. The function
// must not block. It is told once, and again only after TakeChanged.
func (t *Terminal) SetOnFrame(f func()) {
	if f == nil {
		t.onFrame.Store(nil)
		return
	}
	t.onFrame.Store(&f)
	if t.changed.Load() {
		f()
	}
}

// SetOnMeta sets the function that is told, from any goroutine, that the title, the folder, the bell or the state of the
// terminal changed; nil takes it away.
func (t *Terminal) SetOnMeta(f func()) {
	if f == nil {
		t.onMeta.Store(nil)
		return
	}
	t.onMeta.Store(&f)
}

func (t *Terminal) fireMeta() {
	if f := t.onMeta.Load(); f != nil {
		(*f)()
	}
}

// markChanged records that the screen changed and tells the widget the first time after the last snapshot: the widget is told
// at most once a frame, however much output arrives.
func (t *Terminal) markChanged() {
	if t.changed.CompareAndSwap(false, true) {
		if f := t.onFrame.Load(); f != nil {
			(*f)()
		}
	}
}

// TakeChanged clears the flag the parser sets, before the widget takes its snapshot, so that output that arrives after the
// snapshot tells the widget again.
func (t *Terminal) TakeChanged() { t.changed.Store(false) }

// readLoop reads the output of the program in chunks of readChunk bytes, with flow control.
func (t *Terminal) readLoop(chunks chan<- chunk) {
	defer t.wg.Done()
	defer close(chunks)
	for {
		t.flowMu.Lock()
		if t.unparsed > highWater {
			// paused: reading goes on when the parser has caught up to below lowWater
			for t.unparsed >= lowWater && !t.closed.Load() {
				t.flowCond.Wait()
			}
		}
		t.flowMu.Unlock()
		bp := chunkPool.Get().(*[]byte)
		n, err := t.proc.Read(*bp)
		if n > 0 {
			t.flowMu.Lock()
			t.unparsed += n
			t.flowMu.Unlock()
			chunks <- chunk{buf: (*bp)[:n]}
		} else {
			chunkPool.Put(bp)
		}
		if err != nil {
			if err != io.EOF {
				t.log.Debug("the terminal cannot be read", "err", err)
			}
			return
		}
	}
}

// parseLoop gives the chunks to the emulator, which this goroutine owns, and tells the widget the screen changed.
func (t *Terminal) parseLoop(chunks <-chan chunk) {
	defer t.wg.Done()
	for c := range chunks {
		t.feed(c.buf)
		n := len(c.buf)
		full := c.buf[:cap(c.buf)]
		chunkPool.Put(&full)
		t.flowMu.Lock()
		t.unparsed -= n
		if t.unparsed < lowWater {
			t.flowCond.Broadcast()
		}
		t.flowMu.Unlock()
		t.markChanged()
	}
	t.finish()
}

// finish records the end of the program: its status once it is gone (the terminal is closed when it does not end soon, as a
// background process of another session may hold it open).
func (t *Terminal) finish() {
	select {
	case <-t.proc.Done():
	case <-time.After(2 * time.Second):
		t.proc.Close()
		<-t.proc.Done()
	}
	t.flushOutput(true)
	t.mu.Lock()
	t.exited, t.exitCode = true, t.proc.ExitCode()
	t.mu.Unlock()
	close(t.exitedCh)
	t.markChanged()
	t.fireMeta()
}

// Send writes bytes to the program, as typed on its terminal. It does not block: a writer goroutine does the writing, so that
// a program that does not read its input never stops the parser or the user interface.
func (t *Terminal) Send(p []byte) {
	if len(p) == 0 || t.closed.Load() {
		return
	}
	t.inMu.Lock()
	t.inBuf = append(t.inBuf, p...)
	t.inMu.Unlock()
	select {
	case t.inWake <- struct{}{}:
	default:
	}
}

// writeLoop writes the queued input to the program.
func (t *Terminal) writeLoop() {
	defer t.wg.Done()
	var out []byte
	for {
		select {
		case <-t.inWake:
		case <-t.proc.Done():
			return
		case <-t.exitedCh:
			return
		}
		for {
			t.inMu.Lock()
			out, t.inBuf = t.inBuf, out[:0]
			t.inMu.Unlock()
			if len(out) == 0 {
				break
			}
			for len(out) > 0 {
				n, err := t.proc.Write(out)
				if err != nil {
					t.log.Debug("the terminal cannot be written", "err", err)
					return
				}
				out = out[n:]
			}
			out = out[:0]
		}
	}
}

// watchProcessName polls the name of the foreground process of the terminal (the title ${process}).
func (t *Terminal) watchProcessName() {
	defer t.wg.Done()
	fg, ok := t.proc.(foregrounder)
	if !ok {
		return
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		t.refreshProcessName(fg)
		select {
		case <-tick.C:
		case <-t.exitedCh:
			return
		case <-t.proc.Done():
			return
		}
	}
}

// refreshProcessName reads the name of the foreground process from /proc once.
func (t *Terminal) refreshProcessName(fg foregrounder) {
	pgid, err := fg.ForegroundPid()
	if err != nil || pgid <= 0 {
		return
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pgid) + "/comm")
	if err != nil {
		return
	}
	name := strings.TrimSpace(string(data))
	t.mu.Lock()
	changed := name != "" && name != t.process
	if changed {
		t.process = name
	}
	t.mu.Unlock()
	if changed {
		t.fireMeta()
	}
}

// Resize sets the size of the terminal in cells: the emulator reflows its lines and the program learns the new size.
func (t *Terminal) Resize(cols, rows int) {
	cols, rows = max(cols, 1), max(rows, 1)
	t.mu.Lock()
	same := t.cols == cols && t.rows == rows
	t.cols, t.rows = cols, rows
	exited := t.exited
	t.mu.Unlock()
	if same {
		return
	}
	t.resizeEmulator(rows, cols)
	if !exited {
		if err := t.proc.Resize(rows, cols); err != nil {
			t.log.Debug("the terminal cannot be resized", "err", err)
		}
	}
	t.markChanged()
}

// Size is the size of the terminal in cells.
func (t *Terminal) Size() (cols, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cols, t.rows
}

// Kill ends the terminal the way closing it does: SIGHUP to the program's group, SIGKILL after the grace time. It returns at
// once; Wait tells when the terminal is gone.
func (t *Terminal) Kill() {
	if t.closed.Swap(true) {
		return
	}
	t.proc.Close()
	t.flowMu.Lock()
	t.flowCond.Broadcast()
	t.flowMu.Unlock()
}

// Exited is closed when the program is gone and its output was parsed.
func (t *Terminal) Exited() <-chan struct{} { return t.exitedCh }

// ExitCode is the exit status of the program and whether it ended.
func (t *Terminal) ExitCode() (code int, exited bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exitCode, t.exited
}

// Wait waits until the goroutines of the terminal are done; the tests use it to leave nothing running.
func (t *Terminal) Wait() { t.wg.Wait() }

// Name is the name the user gave the terminal ("" for none).
func (t *Terminal) Name() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.name
}

// SetName sets the name of the terminal.
func (t *Terminal) SetName(name string) {
	t.mu.Lock()
	t.name = name
	t.mu.Unlock()
	t.fireMeta()
}

// meta is what a tab shows about a terminal.
type meta struct {
	name, title, process, cwd, icon, color string
	bellAt                                 time.Time
	exited                                 bool
	exitCode                               int
}

// Meta reads what a tab shows.
func (t *Terminal) Meta() meta {
	t.mu.Lock()
	defer t.mu.Unlock()
	return meta{name: t.name, title: t.title, process: t.process, cwd: t.cwd, icon: t.icon, color: t.color,
		bellAt: t.bellAt, exited: t.exited, exitCode: t.exitCode}
}

// Cwd is the working folder of the terminal: the one the shell reported, else the one it started in.
func (t *Terminal) Cwd() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cwd != "" {
		return t.cwd
	}
	return t.dir
}

// InitialDir is the folder the terminal started in.
func (t *Terminal) InitialDir() string { return t.dir }

// SpeculativeCwd is the folder the shell is in as well as can be known: the one it reported (OSC 7, shell integration), else
// the one the system says its process is in (/proc/<pid>/cwd), else the one it started in (VS Code's getSpeculativeCwd).
func (t *Terminal) SpeculativeCwd() string {
	t.mu.Lock()
	reported := t.cwd
	t.mu.Unlock()
	if reported != "" {
		return reported
	}
	if pid := t.Pid(); pid > 0 {
		if dir, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd"); err == nil && filepath.IsAbs(dir) {
			return dir
		}
	}
	return t.dir
}

// splitDir is the folder a split terminal starts in (terminal.integrated.splitCwd): "inherited" the folder of the terminal it
// is split from, "initial" the folder that one started in, "workspaceRoot" none (the start folder of the settings).
func splitDir(mode string, from *Terminal) string {
	switch mode {
	case "workspaceRoot":
		return ""
	case "initial":
		return from.InitialDir()
	}
	return from.SpeculativeCwd()
}

// Profile is the profile the terminal started with.
func (t *Terminal) Profile() profile { return t.prof }

// unparsedNow is the number of bytes read and not parsed yet.
func (t *Terminal) unparsedNow() int {
	t.flowMu.Lock()
	defer t.flowMu.Unlock()
	return t.unparsed
}
