package claudetest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestBuild(t *testing.T) {
	dir := t.TempDir()
	path, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(path); err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("the built program %s is not an executable file: %v", path, err)
	}
}

func TestBuildFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plain-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// a plain file where the folder of the output should be
	_, err := Build(file)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute("Build.FakeBuildFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
