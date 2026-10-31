package gitscm

import (
	"os"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/gitscm/helper"
)

func TestMain(m *testing.M) {
	gittest.MaybeRunFakeGit()
	// The scripts of the askpass and editor programs start this test binary with the flags of Emetos's own: it plays the part of the
	// program cmd/emetos is, so that the tests run the real helper with the real git.
	if len(os.Args) > 1 && helper.IsHelperFlag(os.Args[1]) {
		os.Exit(helper.Main(os.Args[1:], os.Environ(), os.Stdin, os.Stdout, os.Stderr))
	}
	// Nothing of a test reaches the user's runtime folder: a test that forgets to give its own gets this one.
	dir, err := os.MkdirTemp("", "gr")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("XDG_RUNTIME_DIR", dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
