package golang

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/golang/gotest"
)

// "Go: Show Go Environment" (step 14.1).

func TestShowGoEnvironmentWritesGoEnvToTheOutputChannel(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true, settings: map[string]any{ToolsEnvVarsSetting: map[string]any{"FAKE_TOOLS_VAR": "1"}}})
	if r.api.Channel(EnvironmentChannel) != nil {
		t.Fatal("the channel is made before it is needed")
	}
	if err := r.api.Run(ShowEnvironmentCommand, nil); err != nil {
		t.Fatal(err)
	}
	ch := r.api.Channel(EnvironmentChannel)
	if ch == nil || ch.Shown() != 1 {
		t.Fatalf("channel = %v", ch)
	}
	r.api.WaitFor("go env", func() bool { return slices.Contains(ch.Lines(), "GOPATH='/fake/go'") })
	r.ext.wait()
	lines := ch.Lines()
	if lines[0] != "$ go env" || !slices.Contains(lines, "GOVERSION='go1.99.1'") || !slices.Contains(lines, "GOROOT='/fake/goroot'") {
		t.Errorf("lines = %v", lines)
	}
	// it ran `go env` in the folder, with the environment of the tools
	var env *goRun
	for _, run := range r.runs() {
		if slices.Equal(run.Args, []string{"env"}) {
			env = &run
		}
	}
	if env == nil || env.Dir != realPath(t, r.folder) || env.Env["FAKE_TOOLS_VAR"] != "1" {
		t.Errorf("run = %+v", env)
	}
	// a second run starts the channel again, and the channel is shown again
	if err := r.api.Run(ShowEnvironmentCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.ext.wait()
	if got := ch.Lines(); got[0] != "$ go env" || strings.Count(strings.Join(got, "\n"), "$ go env") != 1 || ch.Shown() != 2 {
		t.Errorf("lines after the second run = %v, shown %d", got, ch.Shown())
	}
}

func TestShowGoEnvironmentSaysWhyItFailed(t *testing.T) {
	tests := []struct {
		name string
		opts rigOptions
		want string
	}{
		{"no go program", rigOptions{}, "The go command was not found"},
		{"go env fails", rigOptions{withGo: true, env: []string{gotest.EnvEnvFail + "=go: the toolchain is broken"}}, "go: the toolchain is broken"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.opts)
			if err := r.api.Run(ShowEnvironmentCommand, nil); err != nil {
				t.Fatal(err)
			}
			ch := r.api.Channel(EnvironmentChannel)
			r.api.WaitFor("the reason", func() bool { return strings.Contains(strings.Join(ch.Lines(), "\n"), tt.want) })
			r.ext.wait()
			if tt.opts.withGo && !strings.Contains(strings.Join(ch.Lines(), "\n"), "go env failed: exit status 1") {
				t.Errorf("lines = %v", ch.Lines())
			}
		})
	}
}
