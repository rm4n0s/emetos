// Command errortags lists the error Tags of the repository, and the Tags that no
// test asserts. scripts/error-tags.sh runs it.
//
// Usage:
//
//	errortags [--untested] [--all] [<repository root>]
//
// Without a flag it prints every Tag, one line per place that makes it: the Tag,
// the file and line, and the function. With --untested it prints the Tags that no
// test asserts (see archtest.UntestedErrorTags), except those that
// docs/ERROR-TAGS-UNTESTED.md lists; --all keeps those in. A call whose Tag is
// not a string literal is an error in both modes. The exit status is 1 when it
// printed an untested Tag or a non-literal one, so a script can use it.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/internal/archtest"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main with its arguments and its output; it returns the exit status.
func run(args []string, stdout, stderr io.Writer) int {
	untested, all, root := false, false, "."
	for _, a := range args {
		switch a {
		case "--untested":
			untested = true
		case "--all":
			all = true
		default:
			root = a
		}
	}
	var out, errs strings.Builder
	status := report(&out, &errs, root, untested, all)
	// A write that fails (a closed pipe) is the status 2 of any other failure.
	if _, err := io.WriteString(stdout, out.String()); err != nil {
		return 2
	}
	if _, err := io.WriteString(stderr, errs.String()); err != nil {
		return 2
	}
	return status
}

// report writes what run prints to out and errs and returns the exit status.
func report(out, errs *strings.Builder, root string, untested, all bool) int {
	var sites []archtest.TagSite
	var bad []archtest.Violation
	var err error
	excepted := map[string]string{}
	if untested {
		sites, bad, err = archtest.UntestedErrorTags(root)
		if err == nil {
			excepted, err = archtest.ReadUntestedList(filepath.Join(root, "docs", "ERROR-TAGS-UNTESTED.md"))
		}
	} else {
		sites, bad, err = archtest.ErrorTags(root)
	}
	if err != nil {
		fmt.Fprintln(errs, "error-tags:", err)
		return 2
	}
	status := 0
	for _, s := range sites {
		if _, listed := excepted[s.Tag]; listed && !all {
			continue
		}
		fmt.Fprintf(out, "%s\t%s:%d\t%s\n", s.Tag, s.File, s.Line, s.Func)
		if untested {
			status = 1
		}
	}
	for _, v := range bad {
		fmt.Fprintf(errs, "error-tags: %s:%d: the Tag is not a string literal: %s\n", v.File, v.Line, v.Detail)
		status = 1
	}
	return status
}
