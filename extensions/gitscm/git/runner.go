package git

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// Default limits (SPEC §10.30 "Running git").
const (
	// DefaultMaxOutput is the most standard output that Run keeps: 64 MiB.
	DefaultMaxOutput = 64 << 20
	// DefaultKillGrace is the time between SIGTERM and SIGKILL of a cancelled command.
	DefaultKillGrace = 3 * time.Second
	// StderrKept is the length of the error output kept in a Result and in the metadata of an error: the last 2 KiB.
	StderrKept = 2 << 10
	// stderrClassified is the length of the error output that the classification of failures reads.
	stderrClassified = 32 << 10
	// stdoutClassified is the length of the standard output that the classification reads (merge and stash print CONFLICT
	// lines there).
	stdoutClassified = 1 << 20
)

// Logger receives the lines of the Git channel of the Output panel (SPEC §10.30 "Output"), without the final newline:
// "> git status -z -uall [34ms]", and the error output of a command. The userinfo of URLs is replaced by *** before.
type Logger func(line string)

// Config configures a Runner.
type Config struct {
	// Path is the git program (Find returns it).
	Path string
	// Version is the version of that git; it is only used by callers that branch on it.
	Version Version
	// Env is the base environment ("KEY=VALUE"). Nil means the environment of the process.
	Env []string
	// Extra are variables added to the environment of every command: the askpass and editor variables (GIT_ASKPASS,
	// SSH_ASKPASS, SSH_ASKPASS_REQUIRE, GIT_EDITOR, EMETOS_GIT_SOCKET, EMETOS_GIT_TOKEN) that the extension gives.
	Extra []string
	// ExtraFor, when set, gives more variables for a command: it is called, from the goroutine of the command, with the git command
	// ("fetch") and whether it is a background command, and its entries follow Extra. The extension starts the server of the askpass
	// and editor programs on the first command that can ask something, and gives it the socket and the token here (SPEC §10.30 "Passwords
	// and editors"); a background command never prompts and gets nothing.
	ExtraFor func(ctx context.Context, name string, background bool) []string
	// Logger gets the Git channel's lines; may be nil.
	Logger Logger
	// CommandsToLog lists the commands (git.commandsToLog) whose standard output is logged too, besides the command line and
	// the error output that are always logged.
	CommandsToLog []string
	// KillGrace is the time a cancelled command has between SIGTERM and SIGKILL; zero means DefaultKillGrace.
	KillGrace time.Duration
	// MaxOutput is the most standard output Run keeps; zero means DefaultMaxOutput.
	MaxOutput int64
	// Trust says whether git may be started in a repository (TrustedBy makes it from the store of the user's answers). A command
	// whose working directory, -C folder, --git-dir, --work-tree, GIT_DIR or GIT_WORK_TREE is in a repository that it does not trust is
	// refused with GitRepositoryNotTrusted before any process starts (SPEC §10.30 "Trusting a repository", D37). Nil trusts no
	// repository: only commands outside every repository run.
	Trust func(Location) bool
	// Now is the clock the durations of the log are measured with; nil means time.Now (a test gives a fake clock).
	Now func() time.Time
}

// Runner starts git commands. It holds no state besides its Config and is safe for concurrent use.
type Runner struct {
	cfg Config
}

// NewRunner makes a Runner.
func NewRunner(cfg Config) *Runner {
	if cfg.KillGrace <= 0 {
		cfg.KillGrace = DefaultKillGrace
	}
	if cfg.MaxOutput <= 0 {
		cfg.MaxOutput = DefaultMaxOutput
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Runner{cfg: cfg}
}

// Path returns the git program of the Runner.
func (r *Runner) Path() string { return r.cfg.Path }

// Version returns the version of the git of the Runner, as given in its Config.
func (r *Runner) Version() Version { return r.cfg.Version }

func (r *Runner) base() []string {
	if r.cfg.Env != nil {
		return r.cfg.Env
	}
	return os.Environ()
}

// Command is one git command.
type Command struct {
	// Dir is the working directory (the repository). Empty means the root of the file system, outside any repository.
	Dir string
	// Args are the arguments after the program name, the command first (or "-c key=value" pairs before it): ["status", "-z",
	// "-uall"]. Never a shell line.
	Args []string
	// Paths are the paths the command works on. They go after "--" and, past MaxPathsOnTheCommandLine of them, for the
	// commands that can read them (add, rm, reset, restore, checkout, commit, stash), through --pathspec-from-file=-
	// --pathspec-file-nul on the standard input; Stdin must then be nil.
	Paths []string
	// Stdin is the standard input; nil means none (/dev/null).
	Stdin io.Reader
	// Background marks the commands that Emetos runs by itself (status, a refresh after a file change, blame, the graph, the
	// timeline, the originals of the quick diff): they get -c core.fsmonitor=false, -c core.pager=cat, GIT_OPTIONAL_LOCKS=0
	// and --no-ext-diff --no-textconv where the command has them.
	Background bool
	// Env are variables added last to the environment of this command.
	Env []string
	// AllowExit lists the exit statuses, besides 0, that are not failures for this command (1 for "git diff --quiet",
	// "check-ignore", "merge-base").
	AllowExit []int
	// Progress, when set, receives the progress lines git prints on its error output (the command must be given --progress): they are
	// taken out of the error output, which keeps git's messages. It is called from a goroutine of the Runner.
	Progress func(Progress)
}

// Result is what a command left.
type Result struct {
	// ExitCode is the exit status; -1 when git was ended by a signal.
	ExitCode int
	// Stdout is the standard output kept by Run; RunStream leaves it empty.
	Stdout []byte
	// Stderr is the last 2 KiB of the error output.
	Stderr string
	// Duration is how long the command ran, by the clock of the Config.
	Duration time.Duration
}

// ErrStopStream is what a consumer of RunStream returns to stop the command without an error: git is ended (SIGTERM to its
// process group) and RunStream returns nil. It is a sentinel (a struct literal), so the route of an error never starts here.
var ErrStopStream = &errors.Error{Tag: "GitStreamStopped"}

// Run runs the command and returns its output, kept up to the limit of the Config (64 MiB; a command that prints more is
// stopped with the error GitOutputTooLarge). A failure is classified into the tags of VS Code's GitErrorCodes
// (classify) or is GitCommandFailed; the Result is returned with the error, so that the caller can read the output.
// Cancelling ctx sends SIGTERM to the whole process group and, after KillGrace, SIGKILL; the error has the tag GitCancelled.
//
// Ported from VS Code: extensions/git/src/git.ts (Git.exec, _exec, spawn).
func (r *Runner) Run(ctx context.Context, c Command) (Result, error) {
	return r.execute(ctx, c, nil)
}

// RunStream runs the command and hands its standard output to consume as a stream. When consume returns ErrStopStream the
// command is ended and RunStream returns nil; when it returns another error the command is ended and that error is
// returned untouched; when it returns nil the rest of the output is read and dropped. The Result has no Stdout.
func (r *Runner) RunStream(ctx context.Context, c Command, consume func(stdout io.Reader) error) (Result, error) {
	return r.execute(ctx, c, consume)
}

func (r *Runner) execute(ctx context.Context, c Command, consume func(io.Reader) error) (Result, error) {
	name, idx := subcommand(c.Args)
	if loc, bad := r.untrustedRepository(c); bad {
		return Result{}, errors.New("GitRepositoryNotTrusted", "git is not started in a repository that the user did not trust",
			"root", loc.Root, "args", maskArgs(c.Args))
	}
	argv, pathInput := arguments(name, idx, c)
	if pathInput != nil && c.Stdin != nil {
		return Result{}, errors.New("GitStdinConflict", "the paths of the command and its input both need the standard input",
			"args", maskArgs(c.Args), "paths", len(c.Paths))
	}
	stdin := c.Stdin
	if pathInput != nil {
		stdin = bytes.NewReader(pathInput)
	}
	dir := c.Dir
	if dir == "" {
		dir = rootDir()
	}

	// A command that prints too much is cancelled through this context.
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()

	xc := exec.CommandContext(runCtx, r.cfg.Path, argv...)
	xc.Dir = dir
	xc.Env = r.environ(ctx, name, c)
	xc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// git starts helpers (hooks, credential helpers, ssh); the process group is the unit that is stopped.
	xc.Cancel = func() error { return signalGroup(xc, syscall.SIGTERM) }
	xc.WaitDelay = r.cfg.KillGrace
	if stdin != nil {
		xc.Stdin = stdin
	}
	stderr := &tailBuffer{max: stderrClassified}
	xc.Stderr = stderr
	var progress *progressWriter
	if c.Progress != nil {
		progress = &progressWriter{next: stderr, progress: c.Progress}
		xc.Stderr = progress
	}
	var out *limitBuffer
	var pipe io.ReadCloser
	if consume == nil {
		out = &limitBuffer{max: r.cfg.MaxOutput, stop: stopRun}
		xc.Stdout = out
	} else {
		var err error
		pipe, err = xc.StdoutPipe()
		if err != nil {
			return Result{}, errors.NewErr("GitStartFailed", err).SetMetadata("args", maskArgs(c.Args), "dir", dir)
		}
	}

	start := r.cfg.Now()
	if err := xc.Start(); err != nil {
		if notExist(err) {
			if _, statErr := os.Stat(r.cfg.Path); notExist(statErr) {
				return Result{}, errors.NewErr("GitNotFound", err).SetMetadata("path", r.cfg.Path, "args", maskArgs(c.Args))
			}
			return Result{}, errors.NewErr("GitDirMissing", err).SetMetadata("dir", dir, "args", maskArgs(c.Args))
		}
		return Result{}, errors.NewErr("GitStartFailed", err).SetMetadata("path", r.cfg.Path, "args", maskArgs(c.Args))
	}

	var consumeErr error
	stopped := false
	var escalate *time.Timer
	if consume != nil {
		consumeErr = consume(pipe)
		if consumeErr != nil {
			stopped = true
			_ = signalGroup(xc, syscall.SIGTERM)
			escalate = time.AfterFunc(r.cfg.KillGrace, func() { _ = signalGroup(xc, syscall.SIGKILL) })
		}
		// The rest of the output is read and dropped, so that git is never blocked on a full pipe.
		_, _ = io.Copy(io.Discard, pipe)
	}
	waitErr := xc.Wait()
	if progress != nil {
		progress.flush()
	}
	if escalate != nil {
		escalate.Stop()
	}
	// Whatever is left in the group (a child that ignored SIGTERM, a helper holding the pipes) is ended.
	if runCtx.Err() != nil || stopped {
		_ = signalGroup(xc, syscall.SIGKILL)
	}
	duration := r.cfg.Now().Sub(start)

	res := Result{Duration: duration, Stderr: lastBytes(stderr.String(), StderrKept)}
	if xc.ProcessState != nil {
		res.ExitCode = xc.ProcessState.ExitCode()
	}
	if out != nil {
		res.Stdout = out.Bytes()
	}
	cancelled := ctx.Err() != nil && (res.ExitCode != 0 || waitErr != nil)
	r.logCommand(name, c.Args, res, stderr, cancelled || stopped)

	switch {
	case consumeErr == ErrStopStream:
		return res, nil
	case out != nil && out.exceeded:
		return res, errors.New("GitOutputTooLarge", "the output of git is larger than the limit",
			"args", maskArgs(c.Args), "limit", r.cfg.MaxOutput)
	case cancelled:
		// A consumer that fails because the pipe closed under the cancellation reports the cancellation.
		return res, errors.NewErr("GitCancelled", ctx.Err()).SetMetadata("args", maskArgs(c.Args), "dir", dir)
	case consumeErr != nil:
		return res, consumeErr
	}
	if waitErr != nil {
		if _, isExit := waitErr.(*exec.ExitError); !isExit && !errors.Is(waitErr, exec.ErrWaitDelay) {
			return res, errors.NewErr("GitCommandFailed", waitErr).SetMetadata("args", maskArgs(c.Args), "dir", dir)
		}
	}
	if res.ExitCode == 0 {
		return res, classifySuccess(name, c.Args, string(res.Stdout), stderr.String())
	}
	for _, ok := range c.AllowExit {
		if res.ExitCode == ok {
			return res, nil
		}
	}
	stdout := res.Stdout
	if len(stdout) > stdoutClassified {
		stdout = stdout[:stdoutClassified]
	}
	return res, classify(name, c.Args, res.ExitCode, string(stdout), stderr.String())
}

// signalGroup sends a signal to the process group of the command, which Setpgid made equal to its pid.
func signalGroup(xc *exec.Cmd, sig syscall.Signal) error {
	if xc.Process == nil {
		return nil
	}
	err := syscall.Kill(-xc.Process.Pid, sig)
	if err == syscall.ESRCH {
		return os.ErrProcessDone
	}
	return err
}

// logCommand writes the Git channel's lines of a finished command.
func (r *Runner) logCommand(name string, args []string, res Result, stderr *tailBuffer, cancelled bool) {
	if r.cfg.Logger == nil {
		return
	}
	line := "> git " + strings.Join(maskArgs(args), " ") + " [" + formatMillis(res.Duration) + "]"
	if cancelled {
		line += " (cancelled)"
	}
	r.cfg.Logger(line)
	if len(res.Stdout) > 0 && r.logsStdout(name) {
		r.cfg.Logger(MaskURLs(strings.TrimRight(string(res.Stdout), "\n")))
	}
	if s := strings.TrimRight(stderr.String(), "\n"); s != "" {
		r.cfg.Logger(MaskURLs(lastBytes(s, stderrClassified)))
	}
}

func (r *Runner) logsStdout(name string) bool {
	for _, n := range r.cfg.CommandsToLog {
		if n == name {
			return true
		}
	}
	return false
}

func formatMillis(d time.Duration) string {
	return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
}

// urlUserinfo matches the userinfo of a URL ("https://user:secret@host"): the scheme, then everything up to the "@" that
// precedes the host.
var urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]*@`)

// MaskURLs replaces the userinfo of every URL in s by *** ("https://user:secret@host/x" becomes "https://***@host/x"), so
// that a password or a token in a URL is never logged (SPEC §10.30 "Output").
func MaskURLs(s string) string {
	return urlUserinfo.ReplaceAllString(s, "${1}***@")
}

// maskArgs returns the arguments with the userinfo of URLs masked; it is what logs and error metadata show.
func maskArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = MaskURLs(a)
	}
	return out
}

// lastBytes returns the last n bytes of s, cut on a character boundary.
func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2*t.max {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buf
	if len(b) > t.max {
		b = b[len(b)-t.max:]
	}
	return string(b)
}

// limitBuffer keeps the output up to max bytes; past it, it calls stop (which ends the command) and fails the write.
type limitBuffer struct {
	max      int64
	stop     func()
	buf      bytes.Buffer
	exceeded bool
}

func (l *limitBuffer) Write(p []byte) (int, error) {
	if l.exceeded {
		return 0, io.ErrShortWrite
	}
	if int64(l.buf.Len())+int64(len(p)) > l.max {
		l.exceeded = true
		l.stop()
		return 0, io.ErrShortWrite
	}
	return l.buf.Write(p)
}

func (l *limitBuffer) Bytes() []byte { return l.buf.Bytes() }
