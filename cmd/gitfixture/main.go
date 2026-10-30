// Command gitfixture builds the deterministic Git repositories that the proofs and the measurements of the Source Control
// milestone use (docs/IMPLEMENTATION.md step 24.1, SPEC §10.30 "Tests"):
//
//	gitfixture small|conflict|history|many <dir> [-files N]
//
// It runs the real git in an environment made from scratch (its own HOME and configuration, the test identity Ada Example
// <ada@example.com>, fixed dates), so nothing of the user's git configuration, credentials, keys or repositories is read or
// written, and two runs give the same hashes. scripts/git-fixture.sh runs it. The kinds are described at gittest.Fixture.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// failure is what the harness panics with when a step fails; run turns it into an exit status.
type failure struct{ message string }

// cliTB is the gittest.TB of the program: a failure ends the run, a skip too (git missing), and the temporary folders are
// removed at the end.
type cliTB struct {
	stderr   io.Writer
	dirs     []string
	cleanups []func()
}

func (c *cliTB) Helper() {}

func (c *cliTB) Fatalf(format string, args ...any) {
	panic(failure{fmt.Sprintf(format, args...)})
}

func (c *cliTB) Skipf(format string, args ...any) {
	panic(failure{fmt.Sprintf(format, args...)})
}

func (c *cliTB) Logf(format string, args ...any) {
	if _, err := fmt.Fprintf(c.stderr, format+"\n", args...); err != nil {
		panic(failure{err.Error()})
	}
}

func (c *cliTB) Cleanup(f func()) { c.cleanups = append(c.cleanups, f) }

func (c *cliTB) TempDir() string {
	dir, err := os.MkdirTemp("", "gitfixture-")
	if err != nil {
		panic(failure{err.Error()})
	}
	c.dirs = append(c.dirs, dir)
	return dir
}

// finish runs the cleanups in reverse order and removes the temporary folders.
func (c *cliTB) finish() {
	for i := len(c.cleanups) - 1; i >= 0; i-- {
		c.cleanups[i]()
	}
	for _, d := range c.dirs {
		if err := os.RemoveAll(d); err != nil {
			_, _ = fmt.Fprintf(c.stderr, "gitfixture: removing %s: %v\n", d, err)
		}
	}
}

// run builds the fixture the arguments name and prints its kind, folder, HEAD and manifest hash; it returns the exit status.
func run(args []string, stdout, stderr io.Writer) (status int) {
	fs := flag.NewFlagSet("gitfixture", flag.ContinueOnError)
	fs.SetOutput(stderr)
	files := fs.Int("files", 0, "the number of changed files of the fixture many (default 100000)")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "usage: gitfixture [-files N] <%s> <dir>\n", strings.Join(gittest.FixtureKinds, "|"))
	}
	// The flags may come after the two names too.
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return 64
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	if len(positional) != 2 {
		fs.Usage()
		return 64
	}
	kind, dir := positional[0], positional[1]
	known := false
	for _, k := range gittest.FixtureKinds {
		known = known || k == kind
	}
	if !known {
		fs.Usage()
		return 64
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		_, _ = fmt.Fprintf(stderr, "gitfixture: %s is not empty\n", dir)
		return 1
	}

	tb := &cliTB{stderr: stderr}
	defer tb.finish()
	defer func() {
		if r := recover(); r != nil {
			f, ok := r.(failure)
			if !ok {
				panic(r)
			}
			_, _ = fmt.Fprintf(stderr, "gitfixture: %s\n", f.message)
			status = 1
		}
	}()
	repo := gittest.Fixture(tb, kind, dir, gittest.FixtureOptions{Files: *files})
	manifest := gittest.Manifest(tb, repo.Dir)
	head := "(no commit)"
	if out, _, err := repo.GitErr("rev-parse", "HEAD"); err == nil {
		head = out
	}
	if _, err := fmt.Fprintf(stdout, "%s\t%s\tHEAD %s\tmanifest %s\n", kind, repo.Dir, head, manifest); err != nil {
		return 1
	}
	return 0
}
