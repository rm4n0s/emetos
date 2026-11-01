package langserver

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDefaultVersion(t *testing.T) {
	tests := []struct {
		out  string
		want string
		ok   bool
	}{
		{"rust-analyzer 1.78.0 (9b00956e5 2024-04-29)", "1.78.0", true},
		{"clangd version 18.1.8 (Fedora 18.1.8-1.fc40)\nFeatures: linux+grpc", "18.1.8", true},
		{"5.4.5", "5.4.5", true},
		{"typescript-language-server 4.3.3-rc.1", "4.3.3-rc.1", true},
		{"openjdk version \"21.0.2\" 2024-01-16", "21.0.2", true},
		{"no version here, just 7", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := DefaultVersion(tt.out)
		if got != tt.want || ok != tt.ok {
			t.Errorf("DefaultVersion(%q) = %q, %v; want %q, %v", tt.out, got, ok, tt.want, tt.ok)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.10", "1.9", 1},
		{"1.9", "1.10", -1},
		{"1.2", "1.2.0", 0},
		{"21.0.2", "21", 1},
		{"17.0.9", "21", -1},
		{"v2.0.0", "1.99.99", 1},
		{"4.3.3-rc.1", "4.3.3", 0},
		{"0.2.0-nightly", "0.3", -1},
		{"x", "0", 0},
	}
	for _, tt := range tests {
		if got := CompareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestQueryVersion(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name   string
		script string
		check  VersionCheck
		want   string
		route  string
	}{
		{"the version on the standard output", "echo 'tool 2.5.1'", VersionCheck{Args: []string{"--version"}}, "2.5.1", ""},
		{"the version on the standard error", "echo 'tool 2.5.1' >&2", VersionCheck{}, "2.5.1", ""},
		{"the version of a program that exits with 1", "echo 'tool 3.0.0'; exit 1", VersionCheck{}, "3.0.0", ""},
		{"a parser of the pack", "echo 'build=42 version=7.1'", VersionCheck{Parse: func(s string) (string, bool) {
			_, v, ok := strings.Cut(s, "version=")
			return strings.TrimSpace(v), ok
		}}, "7.1", ""},
		{"a program that prints no version", "echo 'hello'", VersionCheck{}, "", "QueryVersion.ServerVersionUnreadable"},
		{"a program that fails", "echo 'boom'; exit 3", VersionCheck{}, "", "QueryVersion.ServerVersionFailed"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, dir, "prog"+strconv.Itoa(i), "#!/bin/sh\n"+tt.script+"\n", 0o755)
			got, err := QueryVersion(context.Background(), path, tt.check, dir, nil)
			if tt.route != "" {
				if err == nil {
					t.Fatalf("QueryVersion = %q, want the failure %s", got, tt.route)
				}
				wantRoute(t, err, tt.route)
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("QueryVersion = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestQueryVersionOfAProgramThatCannotRun(t *testing.T) {
	_, err := QueryVersion(context.Background(), filepath.Join(t.TempDir(), "missing"), VersionCheck{}, "", nil)
	wantRoute(t, err, "QueryVersion.ServerVersionFailed")
}

func TestQueryVersionGivesTheArgumentsTheFolderAndTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "prog", "#!/bin/sh\necho \"v9.8 $1 $2 $(pwd) $TOOL_MARK\"\n", 0o755)
	want := "9.8"
	got, err := QueryVersion(context.Background(), path, VersionCheck{Args: []string{"-a", "-b"}, Parse: func(s string) (string, bool) {
		f := strings.Fields(s)
		if len(f) == 5 && f[1] == "-a" && f[2] == "-b" && f[3] == mustReal(t, dir) && f[4] == "marked" {
			return want, true
		}
		return "", false
	}}, dir, []string{"TOOL_MARK=marked", "PATH=" + os.Getenv("PATH")})
	if err != nil || got != want {
		t.Errorf("QueryVersion = %q, %v", got, err)
	}
}

func mustReal(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestQueryVersionKillsAProgramThatDoesNotAnswerAndWhatItStarted(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	path := writeFile(t, dir, "slow", "#!/bin/sh\nsleep 60 &\necho $! > "+pidFile+"\nwait\n", 0o755)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := QueryVersion(ctx, path, VersionCheck{}, dir, nil)
	wantRoute(t, err, "QueryVersion.ServerVersionFailed")
	if time.Since(start) > 10*time.Second {
		t.Errorf("the question took %v", time.Since(start))
	}
	data, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(string(data)))
	if perr != nil {
		t.Fatal(perr)
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the process %d the program started is still running", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestQueryVersionBoundsWhatItReads(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "chatty", "#!/bin/sh\nhead -c 3000000 /dev/zero | tr '\\0' 'a'\necho\necho 'v1.2.3'\n", 0o755)
	_, err := QueryVersion(context.Background(), path, VersionCheck{}, dir, nil)
	// the version came after the bound: it is not read, and the program is not blocked
	wantRoute(t, err, "QueryVersion.ServerVersionUnreadable")
}
