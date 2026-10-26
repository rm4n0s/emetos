//go:build emetos_gopls

package golang

import (
	"os/exec"
	"strings"
	"testing"
)

// The tests against the real Go and gopls (SPEC §10.17, step 14.1): they only read (go env GOVERSION, go env, the path of gopls)
// and are skipped when the programs are not installed. Nothing here installs a tool.

func TestGoplsStatusItemShowsTheVersionOfTheRealGo(t *testing.T) {
	path, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not installed")
	}
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	out, err := exec.Command(path, "env", "GOVERSION").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := versionLabel(strings.TrimSpace(string(out)))
	r := newRig(t, rigOptions{realGo: true})
	r.goEditor("go")
	r.api.WaitFor("the version", func() bool { return r.item().Text == want })
	if !r.item().Visible {
		t.Error("the item is hidden for a Go file")
	}
	if _, err := lookProgram("gopls", r.folder); err != nil {
		t.Errorf("gopls is not found: %v", err)
	}
}

func TestGoplsShowGoEnvironmentRunsTheRealGoEnv(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	r := newRig(t, rigOptions{realGo: true})
	if err := r.api.Run(ShowEnvironmentCommand, nil); err != nil {
		t.Fatal(err)
	}
	ch := r.api.Channel(EnvironmentChannel)
	r.api.WaitFor("go env", func() bool {
		for _, l := range ch.Lines() {
			if strings.HasPrefix(l, "GOROOT=") {
				return true
			}
		}
		return false
	})
	r.ext.wait()
}
