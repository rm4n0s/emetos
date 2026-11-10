//go:build !linux

package terminal

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// procinfoTimeout ends ps and lsof when they do not answer.
const procinfoTimeout = 2 * time.Second

// processName is the name of the program of the process pid, from `ps -o comm=` (the path of the program on macOS; the name of its
// file is kept). ok is false when ps does not know the process.
func processName(pid int) (name string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), procinfoTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output() //nolint:gosec // a fixed system program
	if err != nil {
		return "", false
	}
	name = strings.TrimSpace(string(out))
	if name == "" {
		return "", false
	}
	return filepath.Base(name), true
}

// processCwd is the folder the process pid is in, from `lsof -Fn -d cwd` (macOS has no /proc). ok is false when lsof cannot tell.
func processCwd(pid int) (dir string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), procinfoTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/lsof", "-a", "-d", "cwd", "-Fn", "-p", strconv.Itoa(pid)).Output() //nolint:gosec // a fixed system program
	if err != nil {
		return "", false
	}
	return parseLsofCwd(string(out))
}

// parseLsofCwd reads the folder from the output of lsof -Fn: lines that begin with the field letter, "p<pid>", "fcwd" and "n<path>".
func parseLsofCwd(out string) (dir string, ok bool) {
	for _, line := range strings.Split(out, "\n") {
		if path, found := strings.CutPrefix(line, "n"); found && path != "" {
			return path, true
		}
	}
	return "", false
}
