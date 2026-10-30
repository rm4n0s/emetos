package git

import (
	"os"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

func TestMain(m *testing.M) {
	gittest.MaybeRunFakeGit()
	os.Exit(m.Run())
}
