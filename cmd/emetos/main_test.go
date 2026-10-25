package main

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
)

// testEnv is an environment that starts nothing: launch records the command line it was given and gives status.
type testEnv struct {
	env
	stdout, stderr bytes.Buffer
	launched       []launch
	status         int
}

func newTestEnv(t *testing.T, cwd string) *testEnv {
	t.Helper()
	te := &testEnv{}
	te.env = env{
		stdout: &te.stdout, stderr: &te.stderr,
		getwd: func() (string, error) { return cwd, nil },
		stat:  os.Stat,
		launch: func(l launch, _ env) int {
			te.launched = append(te.launched, l)
			return te.status
		},
	}
	return te
}

func TestRun(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		status int
		stdout string // a substring
		stderr string // a substring
	}{
		{"version", []string{"--version"}, 0, "emetos ", ""},
		{"short version", []string{"-v"}, 0, "emetos ", ""},
		{"help", []string{"--help"}, 0, "--goto file:line[:col]", ""},
		{"short help", []string{"-h"}, 0, "Usage: emetos", ""},
		{"help lists every option", []string{"-h"}, 0, "--new-window", ""},
		{"unknown option", []string{"--nope"}, 2, "", "ArgsUnknownOption"},
		{"bad log level", []string{"--log", "loud"}, 2, "", "LogLevelInvalid"},
		{"missing value", []string{"--diff", "a"}, 2, "", "ArgsMissingValue"},
		{"bad goto", []string{"-g", "a.go:0"}, 2, "", "ArgsGotoInvalid"},
		{"standard input", []string{"-"}, 2, "", "ArgsStdinUnsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			te := newTestEnv(t, t.TempDir())
			if got := run(tt.args, te.env); got != tt.status {
				t.Errorf("status = %d, want %d (stderr: %s)", got, tt.status, te.stderr.String())
			}
			if !strings.Contains(te.stdout.String(), tt.stdout) {
				t.Errorf("stdout = %q, want it to contain %q", te.stdout.String(), tt.stdout)
			}
			if tt.status == 2 && te.stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", te.stdout.String())
			}
			if !strings.Contains(te.stderr.String(), tt.stderr) {
				t.Errorf("stderr = %q, want it to contain %q", te.stderr.String(), tt.stderr)
			}
			if tt.status == 2 && !strings.Contains(te.stderr.String(), "emetos --help") {
				t.Errorf("stderr = %q, want a pointer to --help", te.stderr.String())
			}
			if len(te.launched) != 0 {
				t.Errorf("nothing starts for %v, but launch was called with %+v", tt.args, te.launched)
			}
		})
	}
}

func TestRunLaunches(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		args   []string
		status int
		want   launch
	}{
		{"no arguments", nil, 0, launch{level: slog.LevelInfo, cwd: dir}},
		{"a folder and a file", []string{"sub", "a.go"}, 0, launch{level: slog.LevelInfo, cwd: dir, open: ipc.OpenArgs{
			Folder: filepath.Join(dir, "sub"), Files: []ipc.FileTarget{{Path: file}}}}},
		{"the status of the launch is the status of the command", []string{"-w", "a.go"}, 7, launch{level: slog.LevelInfo, cwd: dir, open: ipc.OpenArgs{
			Wait: true, Files: []ipc.FileTarget{{Path: file}}}}},
		{"debug log", []string{"--log", "debug"}, 0, launch{level: slog.LevelDebug, cwd: dir}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			te := newTestEnv(t, dir)
			te.status = tt.status
			if got := run(tt.args, te.env); got != tt.status {
				t.Errorf("status = %d, want %d", got, tt.status)
			}
			if len(te.launched) != 1 || !reflect.DeepEqual(te.launched[0], tt.want) {
				t.Errorf("launched %+v, want %+v", te.launched, tt.want)
			}
		})
	}
}

func TestRunNeedsTheWorkingDirectory(t *testing.T) {
	te := newTestEnv(t, "")
	te.getwd = func() (string, error) { return "", fs.ErrNotExist }
	if got := run([]string{"a.go"}, te.env); got != 1 || !strings.Contains(te.stderr.String(), "working directory") {
		t.Errorf("status %d, stderr %q", got, te.stderr.String())
	}
}

func TestRunWithARelativeWorkingDirectory(t *testing.T) {
	te := newTestEnv(t, "relative/dir")
	if got := run([]string{"a.go"}, te.env); got != 2 || !strings.Contains(te.stderr.String(), "PathInvalid") {
		t.Errorf("status %d, stderr %q", got, te.stderr.String())
	}
}

func TestVersionString(t *testing.T) {
	got := versionString()
	if !strings.HasPrefix(got, "emetos ") || !strings.Contains(got, "go1.") || !strings.Contains(got, "/") {
		t.Errorf("versionString = %q, want the version, the Go version and the platform", got)
	}
}

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name                  string
		mainVersion, revision string
		want                  string
	}{
		{"installed release", "v0.1.0", "", "emetos v0.1.0 go1.27.3 linux/amd64"},
		{"tagged checkout with revision", "v0.1.0", "0123456789ab", "emetos v0.1.0 (0123456789ab) go1.27.3 linux/amd64"},
		{"checkout without a version", "(devel)", "", "emetos " + devVersion + " go1.27.3 linux/amd64"},
		{"no build information", "", "", "emetos " + devVersion + " go1.27.3 linux/amd64"},
		{"pseudo version", "v0.1.1-0.20261025120000-abcdef123456+dirty", "", "emetos v0.1.1-0.20261025120000-abcdef123456+dirty go1.27.3 linux/amd64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatVersion(tt.mainVersion, tt.revision, "go1.27.3", "linux", "amd64"); got != tt.want {
				t.Errorf("formatVersion = %q, want %q", got, tt.want)
			}
		})
	}
}

// brokenWriter is an output that always fails, like a closed pipe.
type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestRunWithABrokenOutput(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--help"}} {
		te := newTestEnv(t, t.TempDir())
		te.env.stdout = brokenWriter{}
		if got := run(args, te.env); got != 1 {
			t.Errorf("run(%v) with a broken stdout = %d, want 1", args, got)
		}
	}
	// A broken stderr can't be reported to; the status is still the failure's.
	te := newTestEnv(t, t.TempDir())
	te.env.stderr = brokenWriter{}
	if got := run([]string{"--nope"}, te.env); got != 2 {
		t.Errorf("run with a broken stderr = %d, want 2", got)
	}
}

func TestBuildOpen(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	for _, d := range []string{"sub", "sub2"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(dir, "missing.go")
	tests := []struct {
		name string
		o    options
		cwd  string
		want ipc.OpenArgs
		tag  string
	}{
		{name: "nothing", cwd: dir},
		{name: "the first folder and the files", cwd: "/", o: options{paths: []string{file, dir, other, missing}},
			want: ipc.OpenArgs{Folder: dir, Files: []ipc.FileTarget{{Path: file}, {Path: missing}}}},
		{name: "relative paths", cwd: dir, o: options{paths: []string{"a.go", "sub", "sub2", "./nope/../b.go"}},
			want: ipc.OpenArgs{Folder: filepath.Join(dir, "sub"), Files: []ipc.FileTarget{{Path: file}, {Path: filepath.Join(dir, "b.go")}}}},
		{name: "goto", cwd: dir, o: options{paths: []string{"a.go"}, gotos: []string{"a.go:3:4", "b.go:9", "c.go"}},
			want: ipc.OpenArgs{Files: []ipc.FileTarget{{Path: file}, {Path: file, Line: 3, Column: 4},
				{Path: filepath.Join(dir, "b.go"), Line: 9}, {Path: filepath.Join(dir, "c.go")}}}},
		{name: "goto with an absolute path", cwd: "/elsewhere", o: options{gotos: []string{file + ":2"}},
			want: ipc.OpenArgs{Files: []ipc.FileTarget{{Path: file, Line: 2}}}},
		{name: "diff and merge", cwd: dir, o: options{diff: []string{"a", "/b"}},
			want: ipc.OpenArgs{Diff: []string{filepath.Join(dir, "a"), "/b"}}},
		{name: "merge", cwd: dir, o: options{merge: []string{"1", "2", "3", "4"}},
			want: ipc.OpenArgs{Merge: []string{filepath.Join(dir, "1"), filepath.Join(dir, "2"), filepath.Join(dir, "3"), filepath.Join(dir, "4")}}},
		{name: "flags", cwd: dir, o: options{wait: true, newWindow: true, profile: "p"},
			want: ipc.OpenArgs{Wait: true, NewWindow: true, Profile: "p"}},
		{name: "reuse window", cwd: dir, o: options{reuseWindow: true}, want: ipc.OpenArgs{ReuseWindow: true}},
		{name: "relative path without a working directory", cwd: "rel", o: options{paths: []string{"a"}}, tag: "PathInvalid"},
		{name: "relative goto without a working directory", cwd: "", o: options{gotos: []string{"a:1"}}, tag: "PathInvalid"},
		{name: "relative diff without a working directory", cwd: "", o: options{diff: []string{"a", "b"}}, tag: "PathInvalid"},
		{name: "bad goto", cwd: dir, o: options{gotos: []string{"a:0"}}, tag: "ArgsGotoInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildOpen(tt.o, tt.cwd, os.Stat)
			if tt.tag != "" {
				appErr, ok := errors.FromError(err)
				if !ok || appErr.Tag != tt.tag || !appErr.HasRoute("buildOpen") {
					t.Fatalf("error = %v, want tag %s through buildOpen", err, tt.tag)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildOpen: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("open = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestHandOver(t *testing.T) {
	tests := []struct {
		name   string
		reply  ipc.Reply
		status int
		stderr string
	}{
		{"ok", ipc.OK(), 0, ""},
		{"done", ipc.Done(0), 0, ""},
		{"done with a code", ipc.Done(3), 3, ""},
		{"an error", ipc.Failed("The diff editor is not available yet."), 1, "The diff editor is not available yet."},
		{"an error with a code", ipc.Reply{Version: ipc.Version, Type: ipc.TypeError, ExitCode: 4, Message: "no"}, 4, "no"},
		{"an error with the code 0 still fails", ipc.Reply{Version: ipc.Version, Type: ipc.TypeError, Message: "no"}, 1, "no"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := storagetest.Isolate(t)
			loc, err := ipc.Locations(paths, os.Getuid())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			srv, _, err := ipc.Join(ctx, ipc.Options{Location: loc})
			if err != nil || srv == nil {
				t.Fatalf("Join: %v", err)
			}
			var mu sync.Mutex
			var got ipc.Request
			served := make(chan error, 1)
			go func() {
				served <- srv.Serve(ctx, func(_ context.Context, req ipc.Request) ipc.Reply {
					mu.Lock()
					got = req
					mu.Unlock()
					return tt.reply
				})
			}()
			defer func() {
				cancel()
				<-served
			}()
			_, client, err := ipc.Join(ctx, ipc.Options{Location: loc})
			if err != nil || client == nil {
				t.Fatalf("second Join: %v", err)
			}
			defer func() { _ = client.Close() }()
			te := newTestEnv(t, "/work")
			l := launch{cwd: "/work", open: ipc.OpenArgs{Folder: "/work/p", Wait: true}}
			if status := handOver(ctx, client, l, te.env); status != tt.status {
				t.Errorf("status = %d, want %d", status, tt.status)
			}
			if !strings.Contains(te.stderr.String(), tt.stderr) || (tt.stderr == "" && te.stderr.Len() != 0) {
				t.Errorf("stderr = %q, want %q", te.stderr.String(), tt.stderr)
			}
			mu.Lock()
			defer mu.Unlock()
			if got.Cwd != "/work" || got.Open == nil || got.Open.Folder != "/work/p" {
				t.Errorf("the instance received %+v", got)
			}
		})
	}
}

func TestHandOverWithoutAnInstance(t *testing.T) {
	paths := storagetest.Isolate(t)
	loc, err := ipc.Locations(paths, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	// a server that goes away before it answers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, _, err := ipc.Join(ctx, ipc.Options{Location: loc})
	if err != nil || srv == nil {
		t.Fatalf("Join: %v", err)
	}
	served := make(chan error, 1)
	go func() {
		served <- srv.Serve(ctx, func(hctx context.Context, _ ipc.Request) ipc.Reply {
			cancel() // the instance ends while it handles the request
			<-hctx.Done()
			return ipc.OK()
		})
	}()
	client, err := ipc.Dial(context.Background(), ipc.Options{Location: loc})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	te := newTestEnv(t, "/work")
	status := handOver(context.Background(), client, launch{cwd: "/work"}, te.env)
	<-served
	// the instance may answer while it shuts down; either way the client ends without hanging
	if status != 0 && !strings.Contains(te.stderr.String(), "emetos:") {
		t.Errorf("status %d, stderr %q", status, te.stderr.String())
	}
}
