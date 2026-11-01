package extensions

import (
	"os"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/helper"
)

// TestMain lets the test binary play the part of the program git's askpass and editor scripts start (emetos --git-askpass, as helperMain
// answers it in the real binary), so that the tests of git typed in a terminal run the real helper.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && helper.IsHelperFlag(os.Args[1]) {
		os.Exit(helper.Main(os.Args[1:], os.Environ(), os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}
