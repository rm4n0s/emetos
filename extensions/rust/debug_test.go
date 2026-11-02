package rust

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

// The Rust part of the native debuggers over the fake API (SPEC §10.31 "The debuggers", C, C++ and Rust; step 25.9): the targets of
// `cargo metadata`, the build of a configuration with the attribute `cargo`, and the executable cargo names. The cargo of these tests
// is a script that prints what the real one prints (the fixtures below); the real cargo and the real debuggers are used by the tests
// with the tags emetos_gdb and emetos_lldbdap (debug_native_test.go).

type cfg = extension.DebugConfiguration

// fakeCargo puts a cargo first in PATH: a script that prints the files listed in out (stdout) and err (stderr) and exits with status,
// and writes its arguments, its folder and the variables CARGO_HOME and CARGO_TARGET_DIR to args.log. The script is run once here: a
// goroutine of an earlier test that forks while the script is open for writing gives the child a copy of the file descriptor until it
// execs, and running the script then fails with "text file busy".
func fakeCargo(t *testing.T, r *rig, stdout, stderr string, status int) string {
	t.Helper()
	writeFile(t, r.bin, "cargo.out", endLine(stdout), 0o644)
	writeFile(t, r.bin, "cargo.err", endLine(stderr), 0o644)
	body := "#!/bin/sh\nd=\"${0%/*}\"\nprintf '%s|%s|%s|%s\\n' \"$*\" \"$PWD\" \"$CARGO_HOME\" \"$CARGO_TARGET_DIR\" >> \"$d/cargo.log\"\n" +
		"while IFS= read -r line; do printf '%s\\n' \"$line\"; done < \"$d/cargo.out\"\n" +
		"while IFS= read -r line; do printf '%s\\n' \"$line\" >&2; done < \"$d/cargo.err\"\nexit " + strconv.Itoa(status) + "\n"
	p := writeFile(t, r.bin, "cargo", body, 0o755)
	for i := 0; ; i++ {
		err := exec.Command(p).Run()
		if !errors.Is(err, syscall.ETXTBSY) {
			break
		}
		if i == 100 {
			t.Fatalf("the fake cargo stays busy: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.Remove(filepath.Join(r.bin, "cargo.log")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return p
}

// cargoLog lists the calls the fake cargo logged: arguments, folder, CARGO_HOME and CARGO_TARGET_DIR.
func cargoLog(t *testing.T, r *rig) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.bin, "cargo.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// resolve runs the second resolution of the Rust provider of the type.
func (r *rig) resolveCargo(c cfg) (cfg, error) {
	r.t.Helper()
	return r.api.ResolveDebugConfigurationSubstituted(context.Background(), nativedebug.TypeGDB, r.folder, c)
}

// artifactLine is a line of the JSON cargo prints for a compiled artifact.
func artifactLine(exe, name string, kinds string, test bool) string {
	exeJSON := "null"
	if exe != "" {
		exeJSON = `"` + exe + `"`
	}
	testJSON := "false"
	if test {
		testJSON = "true"
	}
	return `{"reason":"compiler-artifact","package_id":"p 0.1.0","target":{"kind":[` + kinds + `],"crate_types":["bin"],"name":"` + name + `"},"profile":{"test":` + testJSON + `},"executable":` + exeJSON + `}`
}

func TestTheRustManifestAllowsBreakpointsInRust(t *testing.T) {
	m := New().Manifest()
	if !slices.ContainsFunc(m.Contributes.Breakpoints, func(b extension.BreakpointSpec) bool { return b.Language == "rust" }) {
		t.Errorf("breakpoints = %v", m.Contributes.Breakpoints)
	}
	if len(m.Contributes.Debuggers) != 0 {
		t.Errorf("the Rust extension declares debug types, which belong to the C and C++ extension: %v", m.Contributes.Debuggers)
	}
}

func TestTheRustProvidersAreAddedToTheNativeTypes(t *testing.T) {
	r := newRig(t, false)
	write := func(typ string, trigger extension.DebugProviderTrigger) int {
		// the dynamic provider lists targets, which needs a Cargo.toml and cargo: with none it gives nothing and no failure
		list, err := r.api.ProvideDebugConfigurations(context.Background(), typ, r.folder, trigger)
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		return len(list)
	}
	for _, typ := range []string{nativedebug.TypeGDB, nativedebug.TypeLLDB, nativedebug.TypeLLDBOld} {
		if n := write(typ, extension.DebugTriggerDynamic); n != 0 {
			t.Errorf("%s: %d configurations without cargo", typ, n)
		}
	}
	// the type of the old name has no dynamic provider: asking it is not a failure either
}

func metadata(packages ...string) string {
	return `{"packages":[` + strings.Join(packages, ",") + `],"workspace_root":"/w"}`
}

func pkg(name string, targets ...string) string {
	return `{"name":"` + name + `","targets":[` + strings.Join(targets, ",") + `]}`
}

func tgt(name, kinds string, test bool) string {
	t := "false"
	if test {
		t = "true"
	}
	return `{"name":"` + name + `","kind":[` + kinds + `],"crate_types":["bin"],"test":` + t + `}`
}

func TestTheTargetsOfCargoMetadataAreTheDynamicConfigurations(t *testing.T) {
	tests := []struct {
		name string
		out  string
		// want lists the names, in order, and the cargo arguments of each
		want [][2]string
	}{
		{"a binary with unit tests", metadata(pkg("app", tgt("app", `"bin"`, true))), [][2]string{
			{"Debug executable 'app'", "build --bin=app --package=app"},
			{"Debug unit tests in executable 'app'", "test --no-run --bin=app --package=app"},
		}},
		{"a library, a binary and an integration test", metadata(pkg("app", tgt("app", `"lib"`, true), tgt("app", `"bin"`, false), tgt("api", `"test"`, true))), [][2]string{
			{"Debug executable 'app'", "build --bin=app --package=app"},
			{"Debug unit tests in library 'app'", "test --no-run --lib --package=app"},
			{"Debug integration test 'api'", "test --no-run --test=api --package=app"},
		}},
		{"examples and benchmarks", metadata(pkg("app", tgt("demo", `"example"`, false), tgt("speed", `"bench"`, true), tgt("build-script-build", `"custom-build"`, false))), [][2]string{
			{"Debug example 'demo'", "build --example=demo --package=app"},
		}},
		{"a workspace names the package", metadata(pkg("a", tgt("a", `"bin"`, false)), pkg("b", tgt("b", `"bin"`, false))), [][2]string{
			{"Debug executable 'a' (a)", "build --bin=a --package=a"},
			{"Debug executable 'b' (b)", "build --bin=b --package=b"},
		}},
		{"a procedural macro library", metadata(pkg("m", tgt("m", `"proc-macro"`, true))), [][2]string{
			{"Debug unit tests in library 'm'", "test --no-run --lib --package=m"},
		}},
		{"a library without tests", metadata(pkg("l", tgt("l", `"lib"`, false))), nil},
		{"no package", metadata(), nil},
	}
	for _, tt := range tests {
		for _, typ := range []string{nativedebug.TypeGDB, nativedebug.TypeLLDB} {
			t.Run(tt.name+" "+typ, func(t *testing.T) {
				r := newRig(t, false)
				fakeCargo(t, r, tt.out, "", 0)
				list, err := r.api.ProvideDebugConfigurations(context.Background(), typ, "", extension.DebugTriggerDynamic)
				if err != nil {
					t.Fatal(err)
				}
				if tt.out != "" && len(cargoLog(t, r)) != 1 {
					t.Fatalf("cargo ran %v", cargoLog(t, r))
				}
				var got [][2]string
				for _, c := range list {
					args := c["cargo"].(map[string]any)["args"].([]any)
					var words []string
					for _, a := range args {
						words = append(words, a.(string))
					}
					got = append(got, [2]string{c["name"].(string), strings.Join(words, " ")})
					if c["type"] != typ || c["request"] != "launch" || c["cwd"] != "${workspaceFolder}" {
						t.Errorf("configuration = %v", c)
					}
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("configurations = %q, want %q", got, tt.want)
				}
				if len(cargoLog(t, r)) == 1 && !strings.HasPrefix(cargoLog(t, r)[0], "metadata --no-deps --format-version 1|") {
					t.Errorf("cargo ran %v", cargoLog(t, r))
				}
			})
		}
	}
}

func TestTheFilterOfATargetNamesItsKind(t *testing.T) {
	r := newRig(t, false)
	fakeCargo(t, r, metadata(pkg("app", tgt("app", `"rlib"`, true))), "", 0)
	list, err := r.api.ProvideDebugConfigurations(context.Background(), nativedebug.TypeGDB, "", extension.DebugTriggerDynamic)
	if err != nil || len(list) != 1 {
		t.Fatalf("%v, %v", list, err)
	}
	if filter := list[0]["cargo"].(map[string]any)["filter"]; !reflect.DeepEqual(filter, map[string]any{"name": "app", "kind": "rlib"}) {
		t.Errorf("filter = %v", filter)
	}
}

func TestFolderWithoutCargoTomlHasNoTargetsAndRunsNoCargo(t *testing.T) {
	r := newRig(t, false)
	fakeCargo(t, r, metadata(pkg("app", tgt("app", `"bin"`, false))), "", 0)
	if err := os.Remove(filepath.Join(r.folder, "Cargo.toml")); err != nil {
		t.Fatal(err)
	}
	list, err := r.api.ProvideDebugConfigurations(context.Background(), nativedebug.TypeGDB, "", extension.DebugTriggerDynamic)
	if err != nil || len(list) != 0 || len(cargoLog(t, r)) != 0 {
		t.Errorf("configurations = %v, %v, cargo ran %v", list, err, cargoLog(t, r))
	}
}

func TestACargoThatFailsGivesNoTargetsAndNoFailure(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		status int
		hasBin bool
	}{
		{"cargo fails", "", 1, true},
		{"cargo prints no JSON", "not json", 0, true},
		{"cargo is not there", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, false)
			if tt.hasBin {
				fakeCargo(t, r, tt.stdout, "error: could not parse manifest", tt.status)
			}
			list, err := r.api.ProvideDebugConfigurations(context.Background(), nativedebug.TypeGDB, "", extension.DebugTriggerDynamic)
			if err != nil || len(list) != 0 {
				t.Errorf("configurations = %v, %v", list, err)
			}
		})
	}
}

func TestReadMetadataFailures(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		status int
		hasBin bool
		route  string
	}{
		{"cargo fails", "", 1, true, "readMetadata.RustDebugMetadataFailed"},
		{"cargo prints no JSON", "not json", 0, true, "readMetadata.RustDebugMetadataFailed"},
		{"cargo is not there", "", 0, false, "findCargo.RustDebugCargoNotFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, false)
			if tt.hasBin {
				fakeCargo(t, r, tt.stdout, "error: could not parse manifest", tt.status)
			}
			_, err := readMetadata(context.Background(), r.folder, nil)
			wantRoute(t, err, tt.route)
			if tt.hasBin && tt.status != 0 && !strings.Contains(err.Error(), "could not parse manifest") {
				t.Errorf("error = %v, want cargo's words", err)
			}
		})
	}
}

func TestParseCargoOutput(t *testing.T) {
	tests := []struct {
		name      string
		out       string
		artifacts []artifact
		problems  []string
	}{
		{"an executable", artifactLine("/t/debug/app", "app", `"bin"`, false) + "\n", []artifact{{"/t/debug/app", "app", []string{"bin"}}}, nil},
		{"a library has no executable", artifactLine("", "lib", `"lib"`, false) + "\n", nil, nil},
		{"a build script is left out", artifactLine("/t/build/build-script-build", "build-script-build", `"custom-build"`, false) + "\n", nil, nil},
		{"lines that are not messages", "   Compiling app\n{broken\n\n[1,2]\n" + artifactLine("/t/x", "x", `"test"`, true) + "\n", []artifact{{"/t/x", "x", []string{"test"}}}, nil},
		{"errors of the compiler", `{"reason":"compiler-message","message":{"level":"error","rendered":"error[E0425]: cannot find value\n --> src/main.rs:2\n"}}` + "\n" +
			`{"reason":"compiler-message","message":{"level":"warning","rendered":"warning: unused"}}` + "\n" + `{"reason":"build-finished","success":false}` + "\n",
			nil, []string{"error[E0425]: cannot find value\n --> src/main.rs:2"}},
		{"nothing", "", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arts, problems := parseCargoOutput([]byte(tt.out))
			if !reflect.DeepEqual(arts, tt.artifacts) || !reflect.DeepEqual(problems, tt.problems) {
				t.Errorf("artifacts = %+v, problems = %q", arts, problems)
			}
		})
	}
}

func TestChooseArtifact(t *testing.T) {
	bin := artifact{"/t/app", "app", []string{"bin"}}
	unit := artifact{"/t/app-hash", "app", []string{"bin"}}
	lib := artifact{"/t/libtests", "mylib", []string{"lib"}}
	api := artifact{"/t/api", "api", []string{"test"}}
	tests := []struct {
		name       string
		list       []artifact
		filterName string
		kind       string
		want       string
		route      string
	}{
		{"one executable", []artifact{bin}, "", "", "/t/app", ""},
		{"the name picks one", []artifact{lib, api}, "api", "", "/t/api", ""},
		{"the kind picks one", []artifact{lib, api}, "", "lib", "/t/libtests", ""},
		{"name and kind", []artifact{bin, lib, api}, "app", "bin", "/t/app", ""},
		{"nothing was built", nil, "", "", "", "chooseArtifact.RustDebugNoExecutable"},
		{"the filter matches nothing", []artifact{bin}, "other", "", "", "chooseArtifact.RustDebugNoExecutable"},
		{"two are left", []artifact{bin, unit}, "app", "bin", "", "chooseArtifact.RustDebugManyExecutables"},
		{"several with no filter", []artifact{lib, api}, "", "", "", "chooseArtifact.RustDebugManyExecutables"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := chooseArtifact(tt.list, tt.filterName, tt.kind)
			if tt.route != "" {
				wantRoute(t, err, tt.route)
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("executable = %q, %v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestWithMessageFormat(t *testing.T) {
	tests := []struct{ in, want []string }{
		{[]string{"build"}, []string{"build", "--message-format=json"}},
		{[]string{"build", "--bin=app"}, []string{"build", "--message-format=json", "--bin=app"}},
		{[]string{"test", "--no-run", "--", "x"}, []string{"test", "--message-format=json", "--no-run", "--", "x"}},
		{[]string{"build", "--message-format=json-render-diagnostics"}, []string{"build", "--message-format=json-render-diagnostics"}},
		{[]string{"build", "--message-format", "json"}, []string{"build", "--message-format", "json"}},
		{[]string{"run", "--", "--message-format=x"}, []string{"run", "--message-format=json", "--", "--message-format=x"}},
	}
	for _, tt := range tests {
		if got := withMessageFormat(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("withMessageFormat(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestReadCargoRequestFailures(t *testing.T) {
	tests := []struct {
		name string
		in   any
	}{
		{"not an object", "build"},
		{"no arguments", map[string]any{}},
		{"empty arguments", map[string]any{"args": []any{}}},
		{"arguments that are not texts", map[string]any{"args": []any{"build", 7.0}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readCargoRequest(tt.in)
			wantRoute(t, err, "readCargoRequest.RustDebugCargoInvalid")
		})
	}
	req, err := readCargoRequest(map[string]any{"args": []any{"build"}, "cwd": "sub", "env": map[string]any{"A": "1", "B": 2.0}, "filter": map[string]any{"name": "n", "kind": "k"}})
	if err != nil || !slices.Equal(req.args, []string{"build"}) || req.cwd != "sub" || !reflect.DeepEqual(req.env, map[string]string{"A": "1"}) ||
		req.filter.name != "n" || req.filter.kind != "k" {
		t.Errorf("request = %+v, %v", req, err)
	}
}

func TestACargoConfigurationIsBuiltAndGetsItsProgram(t *testing.T) {
	exe := "/target/debug/app"
	tests := []struct {
		name string
		in   cfg
		out  string
		// cargo is the command line the fake cargo saw, and dir the folder it ran in ("$folder" is the folder of the tab)
		cargo, dir string
		want       cfg
	}{
		{"a build", cfg{"request": "launch", "cargo": map[string]any{"args": []any{"build", "--bin=app"}}},
			artifactLine(exe, "app", `"bin"`, false) + "\n", "build --message-format=json --bin=app", "$folder",
			cfg{"request": "launch", "program": exe}},
		{"the environment and the folder of cargo", cfg{"request": "launch", "name": "x", "cargo": map[string]any{
			"args": []any{"test", "--no-run"}, "cwd": "sub", "env": map[string]any{"CARGO_HOME": "/h", "CARGO_TARGET_DIR": "/t"}, "filter": map[string]any{"name": "app", "kind": "bin"}}},
			artifactLine("/t/other", "other", `"bin"`, true) + "\n" + artifactLine("/t/app-1", "app", `"bin"`, true) + "\n", "test --message-format=json --no-run", "$folder/sub",
			cfg{"request": "launch", "name": "x", "program": "/t/app-1"}},
		{"a program that is named is not built", cfg{"request": "launch", "program": "/x/app", "cargo": map[string]any{"args": []any{"build"}}},
			"", "", "", cfg{"request": "launch", "program": "/x/app"}},
		{"no cargo attribute", cfg{"request": "launch", "program": "/x/app"}, "", "", "", cfg{"request": "launch", "program": "/x/app"}},
		{"an attach is left alone", cfg{"request": "attach", "cargo": map[string]any{"args": []any{"build"}}}, "", "", "",
			cfg{"request": "attach", "cargo": map[string]any{"args": []any{"build"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, false)
			fakeCargo(t, r, tt.out, "", 0)
			if tt.dir == "$folder/sub" {
				if err := os.MkdirAll(filepath.Join(r.folder, "sub"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			got, err := r.resolveCargo(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("configuration = %v, want %v", got, tt.want)
			}
			log := cargoLog(t, r)
			if tt.cargo == "" {
				if len(log) != 0 {
					t.Errorf("cargo ran: %v", log)
				}
				return
			}
			want := tt.cargo + "|" + strings.ReplaceAll(tt.dir, "$folder", r.folder)
			if len(log) != 1 || !strings.HasPrefix(log[0], want+"|") {
				t.Errorf("cargo ran %q, want %q", log, want)
			}
			if strings.Contains(tt.name, "environment") && !strings.HasSuffix(log[0], "|/h|/t") {
				t.Errorf("the environment of cargo = %q", log[0])
			}
		})
	}
}

func TestTheOutputOfCargoIsInTheDebugConsole(t *testing.T) {
	r := newRig(t, false)
	fakeCargo(t, r, artifactLine("/t/app", "app", `"bin"`, false)+"\n", "", 0)
	if _, err := r.resolveCargo(cfg{"request": "launch", "cargo": map[string]any{"args": []any{"build", "--bin=app"}}}); err != nil {
		t.Fatal(err)
	}
	if got := r.api.DebugConsoleText(); got != "> cargo build --bin=app\n" {
		t.Errorf("console = %q", got)
	}
}

func TestTheCargoFailures(t *testing.T) {
	compilerError := `{"reason":"compiler-message","message":{"level":"error","rendered":"error[E0425]: cannot find value x\n"}}` + "\n"
	tests := []struct {
		name   string
		in     cfg
		out    string
		stderr string
		status int
		noBin  bool
		route  string
		text   string
	}{
		{"cargo is not installed", cfg{"request": "launch", "cargo": map[string]any{"args": []any{"build"}}}, "", "", 0, true, "findCargo.RustDebugCargoNotFound", "cargo was not found"},
		{"cargo arguments that are not valid", cfg{"request": "launch", "cargo": map[string]any{}}, "", "", 0, false, "readCargoRequest.RustDebugCargoInvalid", "needs \"args\""},
		{"a build that fails shows the errors of the compiler", cfg{"request": "launch", "cargo": map[string]any{"args": []any{"build"}}}, compilerError, "error: could not compile `app`", 101, false,
			"resolveCargo.RustDebugCargoFailed", "cannot find value x"},
		{"a build that fails with no message of the compiler shows stderr", cfg{"request": "launch", "cargo": map[string]any{"args": []any{"build"}}}, "", "error: no such command", 101, false,
			"resolveCargo.RustDebugCargoFailed", "no such command"},
		{"a build of nothing", cfg{"request": "launch", "cargo": map[string]any{"args": []any{"build", "--lib"}}}, artifactLine("", "lib", `"lib"`, false) + "\n", "", 0, false,
			"chooseArtifact.RustDebugNoExecutable", "cargo built no executable"},
		{"several executables", cfg{"request": "launch", "cargo": map[string]any{"args": []any{"build"}}}, artifactLine("/a", "a", `"bin"`, false) + "\n" + artifactLine("/b", "b", `"bin"`, false) + "\n",
			"", 0, false, "chooseArtifact.RustDebugManyExecutables", "more than one executable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, false)
			if !tt.noBin {
				fakeCargo(t, r, tt.out, tt.stderr, tt.status)
			}
			got, err := r.resolveCargo(tt.in)
			if got != nil || err == nil {
				t.Fatalf("configuration = %v, err = %v", got, err)
			}
			wantRoute(t, err, tt.route)
			if !strings.Contains(err.Error(), tt.text) {
				t.Errorf("error = %v, want %q", err, tt.text)
			}
		})
	}
}

func TestACargoThatCannotBeStartedFails(t *testing.T) {
	r := newRig(t, false)
	p := writeFile(t, r.bin, "cargo", "not a program", 0o755)
	_, _, err := runCargo(context.Background(), p, []string{"build"}, r.folder, nil)
	wantRoute(t, err, "runCargo.RustDebugCargoFailed")
}

func TestACargoThatRunsTooLongIsKilled(t *testing.T) {
	r := newRig(t, false)
	p := writeFile(t, r.bin, "cargo", "#!/bin/sh\nexec sleep 30\n", 0o755)
	for i := 0; ; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		start := time.Now()
		_, _, err := runCargo(ctx, p, nil, r.folder, nil)
		cancel()
		if errors.Is(err, syscall.ETXTBSY) && i < 100 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if err == nil || time.Since(start) > 10*time.Second {
			t.Fatalf("err = %v after %v", err, time.Since(start))
		}
		break
	}
}

func TestFindCargoTomlLooksInTheFoldersAbove(t *testing.T) {
	base := t.TempDir()
	writeFile(t, base, "Cargo.toml", "", 0o644)
	deep := filepath.Join(base, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if !findCargoToml(deep) {
		t.Error("the Cargo.toml above is not found")
	}
}

// endLine ends a text with a line feed: the script reads lines, and a last line without one is lost.
func endLine(s string) string {
	if s != "" && !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}
