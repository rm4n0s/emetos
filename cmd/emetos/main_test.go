package main

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want options
	}{
		{"no arguments", nil, options{level: slog.LevelInfo}},
		{"version", []string{"--version"}, options{version: true, level: slog.LevelInfo}},
		{"version with one dash", []string{"-version"}, options{version: true, level: slog.LevelInfo}},
		{"help", []string{"--help"}, options{help: true, level: slog.LevelInfo}},
		{"short help", []string{"-h"}, options{help: true}},
		{"log level", []string{"--log", "debug"}, options{level: slog.LevelDebug}},
		{"log level with =", []string{"--log=warn"}, options{level: slog.LevelWarn}},
		{"log level in capitals", []string{"--log", "ERROR"}, options{level: slog.LevelError}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}
			if got != tt.want {
				t.Errorf("options = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseArgsErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		tag  string
	}{
		{"unknown log level", []string{"--log", "loud"}, "LogLevelInvalid"},
		{"empty log level", []string{"--log", ""}, "LogLevelInvalid"},
		{"log without its value", []string{"--log"}, "ArgsInvalid"},
		{"unknown flag", []string{"--nope"}, "ArgsInvalid"},
		{"a positional argument", []string{"file.go"}, "ArgsInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseArgs(tt.args)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if appErr.Tag != tt.tag || !appErr.HasRoute("parseArgs") {
				t.Errorf("Tag = %q, route = %s; want tag %q through parseArgs", appErr.Tag, appErr.Route(), tt.tag)
			}
		})
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		status    int
		stdout    string // a substring
		stderr    string // a substring
		stdoutNil bool
	}{
		{"version", []string{"--version"}, 0, "emetos ", "", false},
		{"help", []string{"--help"}, 0, "--log level", "", false},
		{"short help", []string{"-h"}, 0, "Usage: emetos", "", false},
		{"unknown flag", []string{"--nope"}, 2, "", "ArgsInvalid", true},
		{"bad log level", []string{"--log", "loud"}, 2, "", "LogLevelInvalid", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(tt.args, &stdout, &stderr); got != tt.status {
				t.Errorf("status = %d, want %d (stderr: %s)", got, tt.status, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.stdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.stdout)
			}
			if tt.stdoutNil && stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.stderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.stderr)
			}
			if tt.status == 2 && !strings.Contains(stderr.String(), "emetos --help") {
				t.Errorf("stderr = %q, want a pointer to --help", stderr.String())
			}
		})
	}
}

func TestVersionString(t *testing.T) {
	got := versionString()
	if !strings.HasPrefix(got, "emetos ") || !strings.Contains(got, "go1.") || !strings.Contains(got, "/") {
		t.Errorf("versionString = %q, want the version, the Go version and the platform", got)
	}
}

// brokenWriter is an output that always fails, like a closed pipe.
type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestRunWithABrokenOutput(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--help"}} {
		if got := run(args, brokenWriter{}, &bytes.Buffer{}); got != 1 {
			t.Errorf("run(%v) with a broken stdout = %d, want 1", args, got)
		}
	}
	// A broken stderr can't be reported to; the status is still the failure's.
	if got := run([]string{"--nope"}, &bytes.Buffer{}, brokenWriter{}); got != 2 {
		t.Errorf("run with a broken stderr = %d, want 2", got)
	}
}
