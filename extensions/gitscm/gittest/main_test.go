package gittest

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	MaybeRunFakeGit()
	os.Exit(m.Run())
}
