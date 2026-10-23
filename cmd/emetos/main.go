// Command emetos is a code editor in the spirit of Visual Studio Code, built on
// the Glutter GUI toolkit (SPEC §1).
//
// Usage:
//
//	emetos [--log level] [folder] [file...]
//	emetos --version
//	emetos --help
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/app"
)

const usage = `Usage: emetos [options] [folder] [file...]

A folder opens as the workspace of the window; files open in the window.

Options:
  --log level   log level: debug, info, warn or error (default info)
  --version     print the version and exit
  --help        print this help and exit
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// options are the parsed command line.
type options struct {
	version bool
	help    bool
	level   slog.Level
	// paths are the folder and files named on the command line.
	paths []string
}

// parseArgs parses the command line. The error tags are LogLevelInvalid and
// ArgsInvalid (an unknown flag or a flag without its value).
func parseArgs(args []string) (options, error) {
	var o options
	var level string
	fs := flag.NewFlagSet("emetos", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&o.version, "version", false, "")
	fs.BoolVar(&o.help, "help", false, "")
	fs.StringVar(&level, "log", "info", "")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp { // -h
			o.help = true
			return o, nil
		}
		return options{}, errors.NewErr("ArgsInvalid", err)
	}
	if fs.NArg() > 0 {
		o.paths = fs.Args()
	}
	if err := o.level.UnmarshalText([]byte(strings.ToUpper(level))); err != nil {
		return options{}, errors.New("LogLevelInvalid", "the log level is not debug, info, warn or error", "level", level)
	}
	return o, nil
}

// run is main without the process: it returns the exit status.
func run(args []string, stdout, stderr io.Writer) int {
	o, err := parseArgs(args)
	if err != nil {
		complain(stderr, "emetos: "+describe(err)+"\nTry 'emetos --help'.\n")
		return 2
	}
	switch {
	case o.help:
		return say(stdout, usage)
	case o.version:
		return say(stdout, versionString()+"\n")
	}
	if err := start(o); err != nil {
		complain(stderr, "emetos: "+describe(err)+"\n")
		return 1
	}
	return 0
}

// say writes s to stdout and returns the exit status: 0, or 1 when the output
// is broken (a closed pipe), which the caller of the program should hear about.
func say(stdout io.Writer, s string) int {
	if _, err := io.WriteString(stdout, s); err != nil {
		return 1
	}
	return 0
}

// complain writes a message about a failure to stderr. When stderr itself is
// broken there is nowhere left to say so: the exit status already tells.
func complain(stderr io.Writer, s string) {
	_, _ = io.WriteString(stderr, s)
}

// start opens the log and runs the application until it exits.
func start(o options) error {
	paths, err := storage.ResolvePaths()
	if err != nil {
		return err
	}
	l, err := logs.Open(logs.Options{Dir: paths.LogsDir(), Level: o.level})
	if err != nil {
		return err
	}
	defer func() {
		if cerr := l.Close(); cerr != nil {
			complain(os.Stderr, "emetos: "+describe(cerr)+"\n")
		}
	}()
	l.Logger().Info("starting", "version", versionString(), "log", l.Path())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	folder, files, err := splitPaths(o.paths)
	if err != nil {
		return err
	}
	process := app.NewProcess(app.ProcessOptions{Paths: paths, Logs: l})
	if err := app.Run(ctx, app.Options{Process: process, Logger: l.Logger(), Folder: folder, Files: files}); err != nil {
		logError(l.Logger(), err)
		return err
	}
	l.Logger().Info("exiting")
	return nil
}

// splitPaths tells the folder from the files among the paths of the command line: the first
// folder is the workspace and later folders are ignored; the paths are made absolute. A path that does not exist is a file: opening it reports the error in the window.
func splitPaths(paths []string) (folder string, files []string, err error) {
	for _, p := range paths {
		abs, aerr := filepath.Abs(p)
		if aerr != nil {
			return "", nil, errors.NewErr("PathInvalid", aerr).SetMetadata("path", p)
		}
		if info, serr := os.Stat(abs); serr == nil && info.IsDir() {
			if folder == "" {
				folder = abs
			}
			continue
		}
		files = append(files, abs)
	}
	return folder, files, nil
}

// logError writes an error to the log as JSON, so that it can be replayed in a
// test (CLAUDE.md "Error handling").
func logError(l *slog.Logger, err error) {
	if appErr, ok := errors.FromError(err); ok {
		if j, jerr := appErr.ToJson(); jerr == nil {
			l.Error("failed", "error", j)
			return
		}
	}
	l.Error("failed", "error", err.Error())
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
