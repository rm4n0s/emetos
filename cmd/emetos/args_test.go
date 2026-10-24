package main

import (
	"log/slog"
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestParseArgs(t *testing.T) {
	info := slog.LevelInfo
	tests := []struct {
		name string
		args []string
		want options
	}{
		{"no arguments", nil, options{level: info}},
		{"version", []string{"--version"}, options{version: true, level: info}},
		{"short version", []string{"-v"}, options{version: true, level: info}},
		{"help", []string{"--help"}, options{help: true, level: info}},
		{"short help", []string{"-h"}, options{help: true, level: info}},
		{"log level", []string{"--log", "debug"}, options{level: slog.LevelDebug}},
		{"log level with =", []string{"--log=warn"}, options{level: slog.LevelWarn}},
		{"log level in capitals", []string{"--log", "ERROR"}, options{level: slog.LevelError}},
		{"a file", []string{"main.go"}, options{level: info, paths: []string{"main.go"}}},
		{"flags and paths", []string{"--log", "warn", "dir", "a.go", "b.go"}, options{level: slog.LevelWarn, paths: []string{"dir", "a.go", "b.go"}}},
		{"paths around options", []string{"a.go", "-w", "b.go"}, options{level: info, wait: true, paths: []string{"a.go", "b.go"}}},

		{"new window", []string{"--new-window"}, options{level: info, newWindow: true}},
		{"short new window", []string{"-n"}, options{level: info, newWindow: true}},
		{"reuse window", []string{"--reuse-window"}, options{level: info, reuseWindow: true}},
		{"short reuse window", []string{"-r", "x"}, options{level: info, reuseWindow: true, paths: []string{"x"}}},
		{"wait", []string{"--wait", "msg"}, options{level: info, wait: true, paths: []string{"msg"}}},
		{"short wait", []string{"-w"}, options{level: info, wait: true}},
		{"repeated booleans", []string{"-w", "--wait", "-w"}, options{level: info, wait: true}},
		{"cluster", []string{"-nw", "x"}, options{level: info, newWindow: true, wait: true, paths: []string{"x"}}},
		{"cluster with a value last", []string{"-wg", "a.go:3"}, options{level: info, wait: true, gotos: []string{"a.go:3"}}},
		{"cluster with an attached value", []string{"-nwga.go:3:4"}, options{level: info, newWindow: true, wait: true, gotos: []string{"a.go:3:4"}}},

		{"goto", []string{"--goto", "a.go:3"}, options{level: info, gotos: []string{"a.go:3"}}},
		{"goto with =", []string{"--goto=a.go:3:4"}, options{level: info, gotos: []string{"a.go:3:4"}}},
		{"short goto", []string{"-g", "a.go:3"}, options{level: info, gotos: []string{"a.go:3"}}},
		{"short goto attached", []string{"-ga.go:3"}, options{level: info, gotos: []string{"a.go:3"}}},
		{"goto repeated", []string{"-g", "a.go:1", "--goto", "b.go:2"}, options{level: info, gotos: []string{"a.go:1", "b.go:2"}}},
		{"goto value that looks like an option", []string{"-g", "--odd"}, options{level: info, gotos: []string{"--odd"}}},

		{"diff", []string{"--diff", "a", "b"}, options{level: info, diff: []string{"a", "b"}}},
		{"short diff", []string{"-d", "a", "b", "c"}, options{level: info, diff: []string{"a", "b"}, paths: []string{"c"}}},
		{"diff with =", []string{"--diff=a", "b"}, options{level: info, diff: []string{"a", "b"}}},
		{"short diff attached", []string{"-da", "b"}, options{level: info, diff: []string{"a", "b"}}},
		{"merge", []string{"--merge", "p1", "p2", "base", "out"}, options{level: info, merge: []string{"p1", "p2", "base", "out"}}},
		{"short merge", []string{"-m", "1", "2", "3", "4", "5"}, options{level: info, merge: []string{"1", "2", "3", "4"}, paths: []string{"5"}}},
		{"merge with =", []string{"--merge=1", "2", "3", "4"}, options{level: info, merge: []string{"1", "2", "3", "4"}}},

		{"profile", []string{"--profile", "Work 1"}, options{level: info, profile: "Work 1"}},
		{"profile with =", []string{"--profile=my-prof_2"}, options{level: info, profile: "my-prof_2"}},

		{"end of options", []string{"--", "-x", "--wait"}, options{level: info, paths: []string{"-x", "--wait"}}},
		{"options before the end", []string{"-w", "--", "-n"}, options{level: info, wait: true, paths: []string{"-n"}}},
		{"empty end of options", []string{"--"}, options{level: info}},
		{"a path with a colon is a path", []string{"a.go:3"}, options{level: info, paths: []string{"a.go:3"}}},
		{"an empty argument is a path", []string{""}, options{level: info, paths: []string{""}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
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
		meta map[string]any
	}{
		{"unknown log level", []string{"--log", "loud"}, "LogLevelInvalid", nil},
		{"empty log level", []string{"--log", ""}, "LogLevelInvalid", nil},
		{"log without its value", []string{"--log"}, "ArgsMissingValue", map[string]any{"option": "--log"}},
		{"unknown long option", []string{"--nope"}, "ArgsUnknownOption", map[string]any{"option": "--nope"}},
		{"unknown long option with a value", []string{"--nope=1"}, "ArgsUnknownOption", map[string]any{"option": "--nope"}},
		{"unknown short option", []string{"-x"}, "ArgsUnknownOption", map[string]any{"option": "-x"}},
		{"unknown option in a cluster", []string{"-nxw"}, "ArgsUnknownOption", map[string]any{"option": "-x"}},
		{"a single dash with a long name", []string{"-version"}, "ArgsUnknownOption", nil},
		{"a boolean with a value", []string{"--wait=yes"}, "ArgsValueUnexpected", map[string]any{"option": "--wait"}},
		{"goto without its value", []string{"-g"}, "ArgsMissingValue", map[string]any{"option": "-g", "needs": 1, "got": 0}},
		{"diff with one path", []string{"--diff", "a"}, "ArgsMissingValue", map[string]any{"option": "--diff", "needs": 2, "got": 1}},
		{"diff with none", []string{"-d"}, "ArgsMissingValue", map[string]any{"needs": 2, "got": 0}},
		{"merge with three paths", []string{"--merge", "1", "2", "3"}, "ArgsMissingValue", map[string]any{"option": "--merge", "needs": 4, "got": 3}},
		{"diff twice", []string{"-d", "a", "b", "-d", "c", "d"}, "ArgsRepeated", map[string]any{"option": "-d"}},
		{"profile twice", []string{"--profile", "a", "--profile", "b"}, "ArgsRepeated", nil},
		{"log twice", []string{"--log", "info", "--log=debug"}, "ArgsRepeated", nil},
		{"new window and reuse window", []string{"-n", "-r"}, "ArgsConflict", nil},
		{"diff and merge", []string{"-d", "a", "b", "-m", "1", "2", "3", "4"}, "ArgsConflict", nil},
		{"standard input", []string{"-"}, "ArgsStdinUnsupported", nil},
		{"empty profile", []string{"--profile", ""}, "ArgsProfileInvalid", nil},
		{"profile with a slash", []string{"--profile=a/b"}, "ArgsProfileInvalid", nil},
		{"profile too long", []string{"--profile", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "ArgsProfileInvalid", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseArgs(tt.args)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if appErr.Tag != tt.tag {
				t.Errorf("Tag = %q, want %q (route %s)", appErr.Tag, tt.tag, appErr.Route())
			}
			if !appErr.HasRoute("parseArgs") {
				t.Errorf("route = %s, want it through parseArgs", appErr.Route())
			}
			for k, want := range tt.meta {
				if got := appErr.Metadata[k]; !reflect.DeepEqual(got, want) {
					t.Errorf("metadata %q = %v (%T), want %v", k, got, got, want)
				}
			}
		})
	}
}

func TestParseArgsFirstErrorWins(t *testing.T) {
	// the help of a command line with a mistake is not shown
	if _, err := parseArgs([]string{"--help", "--nope"}); err == nil {
		t.Error("no error for --help --nope")
	}
}

func TestParseGoto(t *testing.T) {
	tests := []struct {
		in           string
		path         string
		line, column int
		tag          string
	}{
		{"a.go", "a.go", 0, 0, ""},
		{"a.go:12", "a.go", 12, 0, ""},
		{"a.go:12:5", "a.go", 12, 5, ""},
		{"/abs/dir/a.go:7", "/abs/dir/a.go", 7, 0, ""},
		{"dir:with:colons/a.go:7:2", "dir:with:colons/a.go", 7, 2, ""},
		{"a.go:x", "a.go:x", 0, 0, ""},
		{"a.go:", "a.go:", 0, 0, ""},
		{"12", "12", 0, 0, ""},
		{"a.go:0", "", 0, 0, "ArgsGotoInvalid"},
		{"a.go:3:0", "", 0, 0, "ArgsGotoInvalid"},
		{"", "", 0, 0, "ArgsGotoInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			path, line, column, err := parseGoto(tt.in)
			if tt.tag != "" {
				appErr, ok := errors.FromError(err)
				if !ok || appErr.Tag != tt.tag || !appErr.HasRoute("parseGoto") {
					t.Fatalf("error = %v, want tag %s through parseGoto", err, tt.tag)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGoto: %v", err)
			}
			if path != tt.path || line != tt.line || column != tt.column {
				t.Errorf("parseGoto = %q %d %d, want %q %d %d", path, line, column, tt.path, tt.line, tt.column)
			}
		})
	}
}

func TestValidProfileName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"a", true}, {"Work 2", true}, {"a-b_c", true}, {"Ünïcode", true}, {"", false}, {"a/b", false}, {"a.b", false},
		{"tab\t", false}, {"emoji😀", false},
	}
	for _, tt := range tests {
		if got := validProfileName(tt.name); got != tt.want {
			t.Errorf("validProfileName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
