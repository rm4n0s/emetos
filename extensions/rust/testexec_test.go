package rust

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

// Debug (step 26.7): the configuration a request makes, the debugger it picks and the build it asks of step 25.9's provider.

// debugOf runs the Debug profile on the items and returns the configurations the pack started.
func (r *cargoRig) debugOf(include, exclude []*extension.TestItem) []extension.DebugConfiguration {
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

func TestDebugStartsTheDebuggerOnTheTestBinaryOfTheTarget(t *testing.T) {
	for _, tc := range []struct {
		name    string
		include []string
		cargo   []any
		args    []any
		filter  map[string]any
	}{
		{"one test", []string{"calc/lib/tests/adds"}, []any{"test", "--no-run", "--package=calc", "--lib"}, []any{"--exact", "tests::adds", "--nocapture"}, map[string]any{"name": "calc", "kind": "lib"}},
		{"a whole target", []string{"calc/bin calc"}, []any{"test", "--no-run", "--package=calc", "--bin=calc"}, []any{"--nocapture"}, map[string]any{"name": "calc", "kind": "bin"}},
		{"an integration test", []string{"calc/test integration/checked_divides"}, []any{"test", "--no-run", "--package=calc", "--test=integration"},
			[]any{"--exact", "checked_divides", "--nocapture"}, map[string]any{"name": "integration", "kind": "test"}},
		{"two targets debug the first", []string{"calc/lib/tests/adds", "calc/bin calc"}, []any{"test", "--no-run", "--package=calc", "--lib"}, []any{"--exact", "tests::adds", "--nocapture"},
			map[string]any{"name": "calc", "kind": "lib"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newCargoRig(t, cargoOptions{})
			r.found()
			var items []*extension.TestItem
			for _, p := range tc.include {
				items = append(items, find(t, r.ctl().Items(), p))
			}
			cfgs := r.debugOf(items, nil)
			if len(cfgs) != 1 {
				t.Fatalf("%d sessions started", len(cfgs))
			}
			c := cfgs[0]
			if c["request"] != "launch" || c["cwd"] != r.folder || !reflect.DeepEqual(c["args"], tc.args) {
				t.Errorf("configuration %+v", c)
			}
			cg, _ := c[nativedebug.CargoAttribute].(map[string]any)
			if !reflect.DeepEqual(cg["args"], tc.cargo) || !reflect.DeepEqual(cg["filter"], tc.filter) {
				t.Errorf("cargo attribute %+v", cg)
			}
			if len(tc.include) == 2 && !strings.Contains(r.note(), "one test binary") {
				t.Errorf("note %q", r.note())
			}
		})
	}
}

func TestDebugOfNothingOrOfDocumentationTestsTellsTheUser(t *testing.T) {
	r := newCargoRig(t, cargoOptions{})
	r.found()
	doc := find(t, r.ctl().Items(), "calc/doc tests")
	if cfgs := r.debugOf([]*extension.TestItem{doc}, nil); len(cfgs) != 0 || !strings.Contains(r.note(), "cannot be debugged") {
		t.Errorf("sessions %v note %q", cfgs, r.note())
	}
	lib := find(t, r.ctl().Items(), "calc/lib")
	if cfgs := r.debugOf([]*extension.TestItem{lib}, []*extension.TestItem{lib}); len(cfgs) != 0 || !strings.Contains(r.note(), "no Rust tests to debug") {
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
			r := newCargoRig(t, cargoOptions{})
			r.found()
			for _, p := range tc.progs {
				writeFile(t, r.bin, p, "#!/bin/sh\n", 0o755)
			}
			cfgs := r.debugOf([]*extension.TestItem{find(t, r.ctl().Items(), "calc/lib")}, nil)
			if len(cfgs) != 1 || cfgs[0]["type"] != tc.want {
				t.Errorf("configurations %v, want the type %s", cfgs, tc.want)
			}
		})
	}
}

func TestTheConfigurationOfADebugIsBuiltByTheProviderOfStep25_9(t *testing.T) {
	cases := append([]fakeCase(nil), listCases...)
	cases = append(cases, fakeCase{args: "test --message-format=json --no-run --package=calc --lib", file: "artifact_lib.json"})
	r := newCargoRig(t, cargoOptions{cases: cases})
	r.found()
	cfgs := r.debugOf([]*extension.TestItem{find(t, r.ctl().Items(), "calc/lib/tests/adds")}, nil)
	resolved, err := r.ext.resolveCargo(context.Background(), r.folder, cfgs[0])
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(r.folder, "target/debug/deps/calc-1"); resolved["program"] != want || resolved[nativedebug.CargoAttribute] != nil {
		t.Errorf("program %v in %v", resolved["program"], resolved)
	}
	if !reflect.DeepEqual(resolved["args"], []any{"--exact", "tests::adds", "--nocapture"}) {
		t.Errorf("args %v", resolved["args"])
	}
}
