package containerui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// The command line of the engine (SPEC §10.20): Build Image, Compose and Push run the `docker` (or `podman`) program, so that the
// user's credential helpers, plugins and BuildKit are the ones used. This file finds the program, builds the command lines and runs
// a process with its output handed over line by line.

// cliName is the program of the engine: Config.CLI, else docker or podman.
func (c Config) cliName() string {
	if c.CLI != "" {
		return c.CLI
	}
	if c.Kind == containerapi.KindPodman {
		return "podman"
	}
	return "docker"
}

// composeFallbackName is the stand-alone Compose program that is tried when the engine's program has no compose command.
func (c Config) composeFallbackName() string {
	if c.ComposeFallback != "" {
		return c.ComposeFallback
	}
	if c.Kind == containerapi.KindPodman {
		return "podman-compose"
	}
	return "docker-compose"
}

// lookPath finds a program on PATH.
func (c Config) lookPath(name string) (string, error) {
	if c.LookPath != nil {
		return c.LookPath(name)
	}
	return exec.LookPath(name)
}

// environ is the environment the programs run in.
func (c Config) environ() []string {
	if c.Environ != nil {
		return c.Environ()
	}
	return os.Environ()
}

// scaffoldFileName is the name of the file that describes an image: Dockerfile, or Containerfile for Podman.
func (c Config) scaffoldFileName() string {
	if c.Kind == containerapi.KindPodman {
		return "Containerfile"
	}
	return "Dockerfile"
}

// findCLI finds the program of the engine. It fails with ContainerUICLINotFound.
func (e *ext) findCLI() (string, error) {
	path, err := e.cfg.lookPath(e.cfg.cliName())
	if err != nil {
		return "", errors.NewErr("ContainerUICLINotFound", err).SetMetadata("program", e.cfg.cliName())
	}
	return path, nil
}

// cliJob is one program to run: argv[0] is the program, dir the folder it runs in ("" is the folder of Emetos), env the whole
// environment.
type cliJob struct {
	argv []string
	dir  string
	env  []string
	// name is how the program is called in the tab ("docker" for /usr/bin/docker); argv[0] when empty.
	name string
}

// text is the command line as the user would type it in a shell.
func (j cliJob) text() string {
	if j.name != "" && len(j.argv) > 0 {
		return shellJoin(append([]string{j.name}, j.argv[1:]...))
	}
	return shellJoin(j.argv)
}

// cliResult is how a run of programs ended.
type cliResult struct {
	// exit is the exit status of the last program that ran (-1 when none could be started); ok says that every program
	// ended with 0.
	exit int
	ok   bool
	// canceled says that the run was stopped (the tab was closed or the extension ended).
	canceled bool
	// err is why a program could not be started, nil otherwise.
	err error
	// tail holds the last lines of the output (stdout and stderr together), at most tailLines.
	tail []string
}

// tailLines is how many of the last lines of the output of a run are kept for the parsing of its errors.
const tailLines = 4000

// shellJoin writes a command line for a POSIX shell: an argument with anything but plain characters is put in single quotes.
func shellJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}

// shellQuote quotes one argument for a POSIX shell.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	plain := true
	for _, r := range s {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !letter && !strings.ContainsRune("_-+=.,/:@%^", r) {
			plain = false
			break
		}
	}
	if plain {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// lineSink splits what a process writes into lines and hands them over, from the goroutines that copy its pipes.
type lineSink struct {
	mu     sync.Mutex
	split  containerapi.LineSplitter
	fn     func(stderr bool, line string)
	stderr bool
	parent *lineSink
}

// Write implements io.Writer.
func (s *lineSink) Write(p []byte) (int, error) {
	root := s
	if s.parent != nil {
		root = s.parent
	}
	root.mu.Lock()
	defer root.mu.Unlock()
	stream := containerapi.StreamStdout
	if s.stderr {
		stream = containerapi.StreamStderr
	}
	err := root.split.Write(stream, p, func(st containerapi.Stream, line string) error {
		root.fn(st == containerapi.StreamStderr, line)
		return nil
	})
	return len(p), err
}

// flush hands over the part of a line that was left at the end.
func (s *lineSink) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.split.Flush(func(st containerapi.Stream, line string) error {
		s.fn(st == containerapi.StreamStderr, line)
		return nil
	})
}

// killGrace is how long a process that was asked to end has before its group is killed.
const killGrace = 3 * time.Second

// runProcess runs a program until it ends or ctx is cancelled (then its process group gets SIGTERM, and SIGKILL after killGrace),
// calling onLine for each line of its output. It returns the exit status, or -1 and an error when the program could not be
// started (ContainerUICLIFailed) or ended in a way that is no exit status.
func runProcess(ctx context.Context, job cliJob, onLine func(stderr bool, line string)) (int, error) {
	if len(job.argv) == 0 {
		return -1, errors.New("ContainerUICLIFailed", "there is no program to run")
	}
	cmd := exec.CommandContext(ctx, job.argv[0], job.argv[1:]...)
	cmd.Dir = job.dir
	cmd.Env = job.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = killGrace
	out := &lineSink{fn: onLine}
	cmd.Stdout = out
	cmd.Stderr = &lineSink{parent: out, stderr: true}
	if err := cmd.Start(); err != nil {
		return -1, errors.NewErr("ContainerUICLIFailed", err).SetMetadata("program", job.argv[0])
	}
	pid := cmd.Process.Pid
	err := cmd.Wait()
	out.flush()
	if ctx.Err() != nil {
		// the group may hold programs that did not end with the leader: no one of them may stay behind
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return -1, errors.NewErr("ContainerUICLIFailed", err).SetMetadata("program", job.argv[0])
}

// runJobs runs the jobs one after the other and stops at the first that does not end with 0. onLine is called with every line.
func runJobs(ctx context.Context, jobs []cliJob, onLine func(stderr bool, line string)) cliResult {
	res := cliResult{ok: true, exit: 0}
	var tail []string
	collect := func(stderr bool, line string) {
		if len(tail) >= tailLines {
			copy(tail, tail[tailLines/2:])
			tail = tail[:len(tail)-tailLines/2]
		}
		tail = append(tail, line)
		onLine(stderr, line)
	}
	for _, j := range jobs {
		code, err := runProcess(ctx, j, collect)
		res.exit = code
		if ctx.Err() != nil {
			res.canceled, res.ok = true, false
			break
		}
		if err != nil {
			res.err, res.ok = err, false
			break
		}
		if code != 0 {
			res.ok = false
			break
		}
	}
	res.tail = tail
	return res
}

// absPath makes a path absolute against a folder.
func absPath(folder, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(folder, p)
}

// fileURI is the URI of a path.
func fileURI(path string) string { return "file://" + path }

// pathOfURI is the path of a file URI; ok is false for anything else.
func pathOfURI(uri string) (string, bool) {
	p, ok := strings.CutPrefix(uri, "file://")
	if !ok || p == "" {
		return "", false
	}
	return p, true
}
