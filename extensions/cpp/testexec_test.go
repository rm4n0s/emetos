package cpp

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

// Debug (step 26.9): the configuration a request makes and the debugger it picks.

// debugOf runs the Debug profile on the items and returns the configurations the pack started.
func (r *ctestRig) debugOf(include, exclude []*extension.TestItem) []extension.DebugConfiguration {
	r.t.Helper()
	n := len(r.api.DebugStarts())
	if _, err := r.profile(extension.TestKindDebug).Run(context.Background(), include, exclude, false); err != nil {
		r.t.Fatal(err)
	}
	r.api.Pump()
	r.ext.wait()
	var out []extension.DebugConfiguration
	for _, s := range r.api.DebugStarts()[n:] {
		out = append(out, s.Target.Config)
	}
	return out
}

func TestDebugStartsTheDebuggerOnTheCommandOfTheTest(t *testing.T) {
	r := newCTestRig(t, ctestOptions{})
	build := filepath.Join(r.folder, "build")
	for _, tc := range []struct {
		name    string
		include []string
		program string
		args    []any
		cwd     string
	}{
		{"a program", []string{"adds"}, filepath.Join(build, "adds"), []any{}, build},
		{"a program with arguments", []string{"divides"}, filepath.Join(build, "divides"), []any{"5", "2"}, build},
		{"a working directory of its own", []string{"math.lists"}, filepath.Join(build, "adds"), []any{}, filepath.Join(r.folder, "src")},
		{"two tests debug the first", []string{"divides", "adds"}, filepath.Join(build, "divides"), []any{"5", "2"}, build},
		{"everything debugs the first", nil, filepath.Join(build, "adds"), []any{}, build},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfgs := r.debugOf(r.items(tc.include...), nil)
			if len(cfgs) != 1 {
				t.Fatalf("%d sessions started", len(cfgs))
			}
			c := cfgs[0]
			if c["request"] != "launch" || c["program"] != tc.program || !reflect.DeepEqual(c["args"], tc.args) || c["cwd"] != tc.cwd || c["env"] != nil {
				t.Errorf("configuration %+v", c)
			}
			if many := len(tc.include) != 1; many && !strings.Contains(r.note(), "one test") {
				t.Errorf("note %q", r.note())
			}
		})
	}
}

func TestDebugGivesTheTestItsEnvironmentAndFindsItsProgramInThePath(t *testing.T) {
	show := `{"kind":"ctestInfo","version":{"major":1,"minor":0},"backtraceGraph":{"commands":[],"files":[],"nodes":[]},"tests":[
{"name":"env","command":["/x/prog","a b"],"properties":[{"name":"ENVIRONMENT","value":["A=1","B=two words"]}]},
{"name":"viash","command":["sed","-n"],"properties":[]},{"name":"nocmd","properties":[]}]}`
	r := newCTestRig(t, ctestOptions{fixtures: map[string]string{"show.json": show}})
	cfgs := r.debugOf(r.items("env"), nil)
	if len(cfgs) != 1 || !reflect.DeepEqual(cfgs[0]["env"], []any{"A=1", "B=two words"}) || !reflect.DeepEqual(cfgs[0]["args"], []any{"a b"}) {
		t.Errorf("configurations %+v", cfgs)
	}
	cfgs = r.debugOf(r.items("viash"), nil)
	if len(cfgs) != 1 || cfgs[0]["program"] != filepath.Join(r.bin, "sed") || cfgs[0]["cwd"] != filepath.Join(r.folder, "build") {
		t.Errorf("configurations %+v", cfgs)
	}
	if cfgs := r.debugOf(r.items("nocmd"), nil); len(cfgs) != 0 || !strings.Contains(r.note(), "has no command to debug") {
		t.Errorf("sessions %v note %q", cfgs, r.note())
	}
	all := r.items("env", "viash", "nocmd")
	if cfgs := r.debugOf(all, all); len(cfgs) != 0 || !strings.Contains(r.note(), "no CTest tests to debug") {
		t.Errorf("sessions %v note %q", cfgs, r.note())
	}
}

func TestTheDebuggerIsGdbThenLldbDapThenGdbAgain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		progs []string
		want  string
	}{
		{"gdb", []string{"gdb", "lldb-dap"}, nativedebug.TypeGDB},
		{"only lldb-dap", []string{"lldb-dap"}, nativedebug.TypeLLDB},
		{"neither", nil, nativedebug.TypeGDB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newCTestRig(t, ctestOptions{})
			for _, p := range tc.progs {
				writeFile(t, r.bin, p, "#!/bin/sh\n", 0o755)
			}
			cfgs := r.debugOf(r.items("adds"), nil)
			if len(cfgs) != 1 || cfgs[0]["type"] != tc.want {
				t.Errorf("configurations %v, want the type %s", cfgs, tc.want)
			}
		})
	}
}
