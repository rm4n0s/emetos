package langserver

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

	"github.com/rm4n0s/errors"
)

// VersionCheck says how to ask a program for its version.
type VersionCheck struct {
	// Args are the arguments that make the program print its version and exit (--version); nil for a program that has no way, which
	// is then never asked.
	Args []string
	// Parse finds the version in what the program printed; DefaultVersion when nil.
	Parse func(output string) (string, bool)
	// Min is the oldest version accepted ("1.2"); a program that is older fails the start with ServerTooOld. Empty accepts any. A
	// version that could not be read is not refused.
	Min string
}

// versionTimeout ends a program that does not answer --version, and maxVersionOutput bounds what is read of it.
const (
	versionTimeout   = 15 * time.Second
	maxVersionOutput = 64 * 1024
)

var versionPattern = regexp.MustCompile(`\d+(?:\.\d+)+(?:[-+][0-9A-Za-z.-]+)?`)

// DefaultVersion finds the first dotted number in the output: "rust-analyzer 1.78.0 (9b00956e5 2024-04-29)" gives "1.78.0".
func DefaultVersion(output string) (string, bool) {
	v := versionPattern.FindString(output)
	return v, v != ""
}

// QueryVersion runs the program with the arguments of the check, in the folder and with the environment given (nil is Emetos's), in
// a process group of its own that is killed when ctx ends or after versionTimeout, and returns the version it printed. A program
// that could not be run fails with ServerVersionFailed; one that printed no version, with ServerVersionUnreadable.
func QueryVersion(ctx context.Context, program string, vc VersionCheck, dir string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	cmd := exec.Command(program, vc.Args...)
	cmd.Dir, cmd.Env = dir, env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// one pipe for both streams, made here so that Wait does not wait for the processes that hold its other end
	pr, pw, err := os.Pipe()
	if err != nil {
		return "", errors.NewErr("ServerVersionFailed", err).SetMetadata("program", program)
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return "", errors.NewErr("ServerVersionFailed", err).SetMetadata("program", program)
	}
	_ = pw.Close()
	pgid := cmd.Process.Pid
	var out bytes.Buffer
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		_, _ = io.Copy(&out, io.LimitReader(pr, maxVersionOutput))
		_, _ = io.Copy(io.Discard, pr)
	}()
	waited := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		case <-waited:
		}
	}()
	waitErr := cmd.Wait()
	close(waited)
	_ = syscall.Kill(-pgid, syscall.SIGKILL) // what the program left behind
	readers.Wait()
	_ = pr.Close()

	parse := vc.Parse
	if parse == nil {
		parse = DefaultVersion
	}
	text := strings.ToValidUTF8(out.String(), "?")
	if v, ok := parse(text); ok {
		return v, nil
	}
	if ctx.Err() != nil {
		return "", errors.NewErr("ServerVersionFailed", ctx.Err()).SetMetadata("program", program, "output", firstLine(text))
	}
	if waitErr != nil {
		return "", errors.NewErr("ServerVersionFailed", waitErr).SetMetadata("program", program, "output", firstLine(text))
	}
	return "", errors.New("ServerVersionUnreadable", program+" printed no version", "program", program, "output", firstLine(text))
}

// firstLine is the first line of the text that is not empty, cut short.
func firstLine(text string) string {
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if len(l) > 200 {
				l = l[:200]
			}
			return l
		}
	}
	return ""
}

// CompareVersions compares two versions by their numbers ("1.10" is newer than "1.9"): -1, 0 or 1. A part after the numbers
// ("-nightly") is ignored, a missing number is 0 ("1.2" equals "1.2.0").
func CompareVersions(a, b string) int {
	pa, pb := versionNumbers(a), versionNumbers(b)
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

// versionNumbers are the leading dotted numbers of a version.
func versionNumbers(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out []int
	for _, part := range strings.Split(v, ".") {
		end := 0
		for end < len(part) && part[end] >= '0' && part[end] <= '9' {
			end++
		}
		if end == 0 {
			break
		}
		n, err := strconv.Atoi(part[:end])
		if err != nil {
			break
		}
		out = append(out, n)
		if end < len(part) {
			break
		}
	}
	return out
}
