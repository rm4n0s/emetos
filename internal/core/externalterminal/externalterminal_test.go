package externalterminal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

func TestDefault(t *testing.T) {
	tests := []struct {
		name   string
		env    map[string]string
		debian bool
		want   string
	}{
		{"debian wins", map[string]string{"DESKTOP_SESSION": "gnome", "COLORTERM": "truecolor"}, true, "x-terminal-emulator"},
		{"gnome", map[string]string{"DESKTOP_SESSION": "gnome"}, false, "gnome-terminal"},
		{"gnome classic", map[string]string{"DESKTOP_SESSION": "gnome-classic"}, false, "gnome-terminal"},
		{"kde", map[string]string{"DESKTOP_SESSION": "kde-plasma"}, false, "konsole"},
		{"COLORTERM before TERM", map[string]string{"COLORTERM": "mate-terminal", "TERM": "xterm-256color"}, false, "mate-terminal"},
		{"TERM", map[string]string{"TERM": "foot"}, false, "foot"},
		{"xterm when nothing says", nil, false, "xterm"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Default(func(k string) string { return tc.env[k] }, func(p string) bool { return tc.debian && p == "/etc/debian_version" })
			if got != tc.want {
				t.Errorf("Default = %q, want %q", got, tc.want)
			}
		})
	}
}

// fakeTerminal is a program that writes its folder to the file named by the first line it is given... it takes no arguments, so it
// reads the file name from a variable.
func fakeTerminal(t *testing.T) (program, out string) {
	t.Helper()
	dir := t.TempDir()
	program = filepath.Join(dir, "fake-terminal")
	out = filepath.Join(dir, "out")
	script := "#!/bin/sh\npwd > '" + out + ".tmp'\nmv '" + out + ".tmp' '" + out + "'\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return program, out
}

func TestOpenStartsTheProgramInTheFolder(t *testing.T) {
	program, out := fakeTerminal(t)
	folder := t.TempDir()
	if err := Open(t.Context(), Options{Exec: program, Dir: folder}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(out)
		if err == nil {
			want, _ := filepath.EvalSymlinks(folder)
			got, _ := filepath.EvalSymlinks(string(data[:len(data)-1]))
			if got != want {
				t.Errorf("the terminal started in %q, want %q", got, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the program did not run")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestOpenFails(t *testing.T) {
	program, _ := fakeTerminal(t)
	notExec := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(notExec, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name  string
		ctx   context.Context
		o     Options
		route string
	}{
		{"a folder that does not exist", t.Context(), Options{Exec: program, Dir: filepath.Join(folder, "nope")}, "Open.ExternalTerminalFolderInvalid"},
		{"a file is not a folder", t.Context(), Options{Exec: program, Dir: program}, "Open.ExternalTerminalFolderInvalid"},
		{"a program that is not there", t.Context(), Options{Exec: "emetos-no-such-terminal", Dir: folder}, "Open.ExternalTerminalNotFound"},
		{"a program that cannot be run", t.Context(), Options{Exec: notExec, Dir: folder, LookPath: func(f string) (string, error) { return f, nil }}, "Open.ExternalTerminalStartFailed"},
		{"a context that ended", cancelled, Options{Exec: program, Dir: folder}, "Open.ExternalTerminalCancelled"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Open(tc.ctx, tc.o)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), tc.route)
			}
		})
	}
}
