// Command emetos is a code editor in the spirit of Visual Studio Code, built on
// the Glutter GUI toolkit (SPEC §1).
//
// Usage:
//
//	emetos [options] [folder] [file...]
//	emetos --version
//	emetos --help
//
// The options are in SPEC §10.11 and in the usage text. A running Emetos of the same user is used when there is
// one: the arguments and the working directory are handed to it and this process ends.
package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/app"
)

const usage = `Usage: emetos [options] [folder] [file...]

A folder opens as the workspace of the window; files open in the window. When Emetos
is running already, the arguments are handed over to it and this command ends.

Options:
  -g, --goto file:line[:col]      open the file with the cursor at the line (and column); may be repeated
  -d, --diff a b                  compare two files
  -m, --merge p1 p2 base result   merge two versions of a file with their base into the result
  -w, --wait                      end only when the files that were opened are closed (for git commit)
  -n, --new-window                open in a new window
  -r, --reuse-window              open in the existing window
      --profile name              use the profile (made empty when it does not exist)
      --log level                 log level: debug, info, warn or error (default info)
  -v, --version                   print the version and exit
  -h, --help                      print this help and exit

Use -- before a path that starts with a dash.

Exit status: 0 on success, 1 on failure, 2 for a wrong command line.
`

func main() {
	os.Exit(run(os.Args[1:], defaultEnv()))
}

// Exit statuses.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// launch is a command line that opens something: the level of the log, the arguments to hand over and the working
// directory they were made against.
type launch struct {
	level slog.Level
	open  ipc.OpenArgs
	cwd   string
}

// env is what run needs of the process; tests replace it.
type env struct {
	stdout, stderr io.Writer
	getwd          func() (string, error)
	stat           func(string) (fs.FileInfo, error)
	// launch hands the command line to the running Emetos or becomes it; it returns the exit status.
	launch func(l launch, e env) int
}

// defaultEnv is the environment of the real process.
func defaultEnv() env {
	return env{stdout: os.Stdout, stderr: os.Stderr, getwd: os.Getwd, stat: os.Stat, launch: runLaunch}
}

// run is main without the process: it returns the exit status. Help, version and a wrong command line never
// reach the socket or a window.
func run(args []string, e env) int {
	o, err := parseArgs(args)
	if err != nil {
		complain(e.stderr, "emetos: "+describe(err)+"\nTry 'emetos --help'.\n")
		return exitUsage
	}
	switch {
	case o.help:
		return say(e.stdout, usage)
	case o.version:
		return say(e.stdout, versionString()+"\n")
	}
	cwd, err := e.getwd()
	if err != nil {
		complain(e.stderr, "emetos: cannot find the working directory: "+err.Error()+"\n")
		return exitFailure
	}
	open, err := buildOpen(o, cwd, e.stat)
	if err != nil {
		complain(e.stderr, "emetos: "+describe(err)+"\nTry 'emetos --help'.\n")
		return exitUsage
	}
	return e.launch(launch{level: o.level, open: open, cwd: cwd}, e)
}

// say writes s to stdout and returns the exit status: 0, or 1 when the output
// is broken (a closed pipe), which the caller of the program should hear about.
func say(stdout io.Writer, s string) int {
	if _, err := io.WriteString(stdout, s); err != nil {
		return exitFailure
	}
	return exitOK
}

// complain writes a message about a failure to stderr. When stderr itself is
// broken there is nowhere left to say so: the exit status already tells.
func complain(stderr io.Writer, s string) {
	_, _ = io.WriteString(stderr, s)
}

// buildOpen makes the arguments to hand over from the options: every path absolute against cwd, the first folder
// among the positional paths is the one to open (later folders are ignored), the other positional paths are files,
// and each --goto is a file with its position. A positional path that does not exist is a file: opening it reports
// the error in the window. The tags are PathInvalid and ArgsGotoInvalid.
func buildOpen(o options, cwd string, stat func(string) (fs.FileInfo, error)) (ipc.OpenArgs, error) {
	abs := func(p string) (string, error) {
		if filepath.IsAbs(p) {
			return filepath.Clean(p), nil
		}
		if !filepath.IsAbs(cwd) {
			return "", errors.New("PathInvalid", "a relative path needs an absolute working directory", "path", p, "cwd", cwd)
		}
		return filepath.Join(cwd, p), nil
	}
	open := ipc.OpenArgs{Wait: o.wait, NewWindow: o.newWindow, ReuseWindow: o.reuseWindow, Profile: o.profile}
	for _, p := range o.paths {
		full, err := abs(p)
		if err != nil {
			return ipc.OpenArgs{}, err
		}
		if info, serr := stat(full); serr == nil && info.IsDir() {
			if open.Folder == "" {
				open.Folder = full
			}
			continue
		}
		open.Files = append(open.Files, ipc.FileTarget{Path: full})
	}
	for _, g := range o.gotos {
		p, line, column, err := parseGoto(g)
		if err != nil {
			return ipc.OpenArgs{}, err
		}
		full, err := abs(p)
		if err != nil {
			return ipc.OpenArgs{}, err
		}
		open.Files = append(open.Files, ipc.FileTarget{Path: full, Line: line, Column: column})
	}
	for _, group := range []struct {
		from []string
		to   *[]string
	}{{o.diff, &open.Diff}, {o.merge, &open.Merge}} {
		for _, p := range group.from {
			full, err := abs(p)
			if err != nil {
				return ipc.OpenArgs{}, err
			}
			*group.to = append(*group.to, full)
		}
	}
	return open, nil
}

// runLaunch is launch for the real process: it joins the running Emetos, or becomes it. A socket that cannot be used
// (no safe folder, a path in the way) does not stop the editor: it runs without single-instance and says so.
func runLaunch(l launch, e env) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	paths, err := storage.ResolvePaths()
	if err != nil {
		complain(e.stderr, "emetos: "+describe(err)+"\n")
		return exitFailure
	}
	var srv *ipc.Server
	loc, err := ipc.Locations(paths, os.Getuid())
	if err == nil {
		var client *ipc.Client
		if srv, client, err = ipc.Join(ctx, ipc.Options{Location: loc}); err == nil && client != nil {
			defer func() { _ = client.Close() }()
			return handOver(ctx, client, l, e)
		}
	}
	if err != nil {
		complain(e.stderr, "emetos: another instance cannot be found or used, so this one runs alone: "+describe(err)+"\n")
	}
	if err := start(ctx, paths, srv, l); err != nil {
		complain(e.stderr, "emetos: "+describe(err)+"\n")
		return exitFailure
	}
	return exitOK
}

// handOver sends the command line to the running Emetos and returns the status of its reply: with --wait that
// comes when the files are closed.
func handOver(ctx context.Context, client *ipc.Client, l launch, e env) int {
	reply, err := client.Send(ctx, ipc.Request{Cwd: l.cwd, Open: &l.open})
	if err != nil {
		complain(e.stderr, "emetos: "+describe(err)+"\n")
		return exitFailure
	}
	if reply.Type == ipc.TypeError {
		complain(e.stderr, "emetos: "+reply.Message+"\n")
		return max(reply.ExitCode, exitFailure)
	}
	return reply.ExitCode
}

// start opens the log and runs the application until it exits. srv, when set, is the socket this process listens
// on: it is served while the application runs, and removed when it ends.
func start(ctx context.Context, paths storage.Paths, srv *ipc.Server, l launch) (err error) {
	var launcher *app.Launcher
	if srv != nil {
		launcher = app.NewLauncher()
		defer func() {
			launcher.Close()
			if cerr := srv.Close(); cerr != nil {
				complain(os.Stderr, "emetos: "+describe(cerr)+"\n")
			}
		}()
	}
	lg, err := logs.Open(logs.Options{Dir: paths.LogsDir(), Level: l.level})
	if err != nil {
		return err
	}
	defer func() {
		if cerr := lg.Close(); cerr != nil {
			complain(os.Stderr, "emetos: "+describe(cerr)+"\n")
		}
	}()
	lg.Logger().Info("starting", "version", versionString(), "log", lg.Path())
	defer startProfiling(lg.Logger())()
	if srv != nil {
		go func() {
			handler := func(hctx context.Context, req ipc.Request) ipc.Reply {
				lg.Logger().Info("a launch was handed over", "pid", req.PID, "cwd", req.Cwd)
				return launcher.Handle(hctx, *req.Open)
			}
			if err := srv.Serve(ctx, handler); err != nil {
				logError(lg.Logger(), err)
			}
		}()
	}
	process := app.NewProcess(app.ProcessOptions{Paths: paths, Logs: lg, WatchFiles: true, Launcher: launcher})
	if err := app.Run(ctx, app.Options{Process: process, Logger: lg.Logger(), Open: &l.open}); err != nil {
		logError(lg.Logger(), err)
		return err
	}
	lg.Logger().Info("exiting")
	return nil
}

// logError writes an error to the log with its tag, route and metadata, so that it can be
// replayed in a test (CLAUDE.md "Error handling").
func logError(l *slog.Logger, err error) {
	l.Error("failed", "err", err)
}

// describe is the one-line text of an error for the user.
func describe(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return fmt.Sprintf("%s: %s %v", appErr.Tag, appErr.Message, appErr.Metadata)
	}
	return err.Error()
}

// versionString is the version line of --version: the module version and VCS
// revision from the build, and the Go version and platform.
func versionString() string {
	version, revision := "(devel)", ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		if bi.Main.Version != "" {
			version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				revision = s.Value[:min(12, len(s.Value))]
			}
		}
	}
	line := "emetos " + version
	if revision != "" {
		line += " (" + revision + ")"
	}
	return line + " " + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH
}
