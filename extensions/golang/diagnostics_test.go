package golang

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang/gotest"
)

// go vet and the lint tool on save (step 14.3) with the fake go and the fake lint tools.

// lintRun is a record of the fake lint tool.
type lintRun struct {
	Tool string   `json:"tool"`
	Args []string `json:"args"`
	Dir  string   `json:"dir"`
	Pid  int      `json:"pid"`
	Pgid int      `json:"pgid"`
}

// lintRuns are the records of the runs of the fake lint tools.
func (r *rig) lintRuns() []lintRun {
	r.t.Helper()
	data, err := os.ReadFile(r.lintLog)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		r.t.Fatal(err)
	}
	var out []lintRun
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var run lintRun
		if err := json.Unmarshal([]byte(line), &run); err != nil {
			r.t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, run)
	}
	return out
}

// vetRuns are the runs of the fake go that were `go vet`.
func (r *rig) vetRuns() []goRun {
	var out []goRun
	for _, run := range r.runs() {
		if len(run.Args) > 0 && run.Args[0] == "vet" {
			out = append(out, run)
		}
	}
	return out
}

// diagRig is a rig with the fake go and the fake lint tools whose saves are debounced for a few milliseconds.
func diagRig(t *testing.T, env ...string) *rig {
	t.Helper()
	r := newRig(t, rigOptions{withGo: true, withLint: true, env: env})
	r.ext.debounce = 10 * time.Millisecond
	return r
}

// pkgFile makes a Go file in a folder of the workspace and returns its path.
func (r *rig) pkgFile(rel, text string) string {
	r.t.Helper()
	path := filepath.Join(r.folder, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		r.t.Fatal(err)
	}
	return path
}

// save tells the extension that the document was saved.
func (r *rig) save(path string) {
	r.t.Helper()
	r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: fileURI(path), Version: 2})
	r.api.Pump()
}

// settled waits until nothing waits for the debounce and no run goes on.
func (r *rig) settled() {
	r.t.Helper()
	r.api.WaitFor("the runs to end", func() bool { return len(r.ext.timers) == 0 && len(r.ext.diagRuns) == 0 })
}

// markerList is the markers of an owner as "file:line:col message [code] (source, severity)" lines, sorted.
func (r *rig) markerList(owner string) []string {
	var out []string
	for uri, ms := range r.api.Markers(owner) {
		for _, m := range ms {
			sev := map[extension.Severity]string{extension.SeverityError: "error", extension.SeverityWarning: "warning"}[m.Severity]
			text := strings.TrimPrefix(uri, "file://"+r.folder+"/") + ":" + itoa(m.StartLine) + ":" + itoa(m.StartColumn) + " " + m.Message
			if m.Code != "" {
				text += " [" + m.Code + "]"
			}
			out = append(out, text+" ("+m.Source+", "+sev+")")
		}
	}
	slices.Sort(out)
	return out
}

func TestVetOnSaveFillsTheProblemsOfThePackage(t *testing.T) {
	r := diagRig(t, gotest.EnvVetOut+"=# example.com/p\n./a.go:7:2: fmt.Printf format %d has arg \"x\" of wrong type string\n./a.go:9: unreachable code")
	a := r.pkgFile("p/a.go", "package p\n")
	r.save(a)
	r.settled()
	want := []string{
		`p/a.go:7:2 fmt.Printf format %d has arg "x" of wrong type string (go vet, warning)`,
		"p/a.go:9:1 unreachable code (go vet, warning)",
	}
	if got := r.markerList(VetMarkerOwner); !slices.Equal(got, want) {
		t.Errorf("markers = %q, want %q", got, want)
	}
	runs := r.vetRuns()
	if len(runs) != 1 || !slices.Equal(runs[0].Args, []string{"vet", "."}) || runs[0].Dir != filepath.Dir(a) {
		t.Errorf("runs = %+v", runs)
	}
	// the code is fixed and saved: the markers of the package go
	t.Setenv(gotest.EnvVetOut, "")
	r.save(a)
	r.settled()
	if got := r.markerList(VetMarkerOwner); len(got) != 0 {
		t.Errorf("markers after the fix = %q", got)
	}
	// nothing else was linted by go vet's owner
	if got := r.markerList(LintMarkerOwner); len(got) != 0 {
		t.Errorf("lint markers = %q", got)
	}
}

func TestAPackageReplacesOnlyItsOwnMarkers(t *testing.T) {
	r := diagRig(t, gotest.EnvVetOut+"=./a.go:1:1: problem in a")
	a := r.pkgFile("pa/a.go", "package pa\n")
	b := r.pkgFile("pb/b.go", "package pb\n")
	r.save(a)
	r.settled()
	t.Setenv(gotest.EnvVetOut, "./b.go:2:3: problem in b")
	r.save(b)
	r.settled()
	want := []string{"pa/a.go:1:1 problem in a (go vet, warning)", "pb/b.go:2:3 problem in b (go vet, warning)"}
	if got := r.markerList(VetMarkerOwner); !slices.Equal(got, want) {
		t.Fatalf("markers = %q, want %q", got, want)
	}
	// package a is vetted clean: its markers go, b's stay
	t.Setenv(gotest.EnvVetOut, "")
	r.save(a)
	r.settled()
	if got := r.markerList(VetMarkerOwner); !slices.Equal(got, want[1:]) {
		t.Errorf("markers = %q, want %q", got, want[1:])
	}
}

func TestTheScopesOfVetAndLintOnSave(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		vet      []string
		lint     []string
	}{
		{"the defaults: the package", nil, []string{"vet", "."}, []string{"."}},
		{"the workspace", map[string]any{VetOnSaveSetting: "workspace", LintOnSaveSetting: "workspace"}, []string{"vet", "./..."}, []string{"./..."}},
		{"a file for the lint tool", map[string]any{LintOnSaveSetting: "file"}, []string{"vet", "."}, []string{"a.go"}},
		{"flags", map[string]any{VetFlagsSetting: []string{"-printf=false"}, LintFlagsSetting: []string{"-checks=all"}}, []string{"vet", "-printf=false", "."}, []string{"-checks=all", "."}},
		{"vet off", map[string]any{VetOnSaveSetting: "off"}, nil, []string{"."}},
		{"lint off", map[string]any{LintOnSaveSetting: "off"}, []string{"vet", "."}, nil},
		{"both off", map[string]any{VetOnSaveSetting: "off", LintOnSaveSetting: "off"}, nil, nil},
		{"vet cannot lint a file", map[string]any{VetOnSaveSetting: "file"}, nil, []string{"."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := diagRig(t)
			for k, v := range tt.settings {
				r.api.SetSetting(k, v)
			}
			a := r.pkgFile("p/a.go", "package p\n")
			r.save(a)
			r.settled()
			vet, lint := r.vetRuns(), r.lintRuns()
			dir := filepath.Dir(a)
			if tt.settings[VetOnSaveSetting] == "workspace" {
				dir = r.folder
			}
			if tt.vet == nil && len(vet) != 0 || tt.vet != nil && (len(vet) != 1 || !slices.Equal(vet[0].Args, tt.vet) || vet[0].Dir != dir) {
				t.Errorf("vet runs = %+v, want %v in %s", vet, tt.vet, dir)
			}
			ldir := filepath.Dir(a)
			if tt.settings[LintOnSaveSetting] == "workspace" {
				ldir = r.folder
			}
			if tt.lint == nil && len(lint) != 0 || tt.lint != nil && (len(lint) != 1 || lint[0].Tool != "staticcheck" || !slices.Equal(lint[0].Args, tt.lint) || lint[0].Dir != ldir) {
				t.Errorf("lint runs = %+v, want %v in %s", lint, tt.lint, ldir)
			}
		})
	}
}

func TestOnlyASavedGoFileOfTheWorkspaceIsChecked(t *testing.T) {
	r := diagRig(t)
	mod := r.pkgFile("go.mod", "module m\n")
	readme := r.pkgFile("README.md", "# x\n")
	outside := filepath.Join(t.TempDir(), "x.go")
	if err := os.WriteFile(outside, []byte("package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{mod, readme, outside} {
		r.save(path)
	}
	r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: "untitled:1"})
	r.api.Pump()
	time.Sleep(60 * time.Millisecond)
	r.settled()
	if len(r.vetRuns()) != 0 || len(r.lintRuns()) != 0 {
		t.Errorf("vet %v lint %v", r.vetRuns(), r.lintRuns())
	}
	// no folder: nothing is checked either
	r2 := newRig(t, rigOptions{withGo: true, withLint: true, noFolder: true})
	r2.ext.debounce = 10 * time.Millisecond
	r2.save(filepath.Join(t.TempDir(), "y.go"))
	time.Sleep(60 * time.Millisecond)
	r2.settled()
	if len(r2.vetRuns()) != 0 {
		t.Errorf("vet ran with no folder: %v", r2.vetRuns())
	}
}

func TestSavesWithinTheDebounceMakeOneRun(t *testing.T) {
	r := diagRig(t)
	r.ext.debounce = 150 * time.Millisecond
	a := r.pkgFile("p/a.go", "package p\n")
	b := r.pkgFile("p/b.go", "package p\n")
	for i := 0; i < 4; i++ {
		r.save(a)
		r.save(b)
		time.Sleep(10 * time.Millisecond)
	}
	if len(r.ext.timers) != 2 {
		t.Errorf("%d timers: one for vet and one for lint of the package", len(r.ext.timers))
	}
	r.settled()
	if len(r.vetRuns()) != 1 || len(r.lintRuns()) != 1 {
		t.Errorf("vet runs %d, lint runs %d", len(r.vetRuns()), len(r.lintRuns()))
	}
}

func TestTheLintToolsAndWhatTheyPrint(t *testing.T) {
	tests := []struct {
		tool string
		out  string
		args []string
		want []string
	}{
		{"staticcheck", "a.go:12:2: this value of err is never used (SA4006)", []string{"."},
			[]string{"p/a.go:12:2 this value of err is never used [SA4006] (staticcheck, warning)"}},
		{"golangci-lint", "a.go:5:9: Error return value of `os.Remove` is not checked (errcheck)", []string{"run", "--print-issued-lines=false", "--color=never", "."},
			[]string{"p/a.go:5:9 Error return value of `os.Remove` is not checked [errcheck] (golangci-lint, warning)"}},
		{"golangci-lint-v2", "a.go:5:9: Error return value is not checked (errcheck)", []string{"run", "--output.text.print-issued-lines=false", "--color=never", "."},
			[]string{"p/a.go:5:9 Error return value is not checked [errcheck] (golangci-lint-v2, warning)"}},
		{"revive", "a.go:4:6: [exported] exported function Foo should have comment or be unexported", []string{"-formatter", "unix", "."},
			[]string{"p/a.go:4:6 exported function Foo should have comment or be unexported [exported] (revive, warning)"}},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			r := diagRig(t, gotest.EnvLintOut+"="+tt.out)
			r.api.SetSetting(LintToolSetting, tt.tool)
			r.api.SetSetting(VetOnSaveSetting, "off")
			a := r.pkgFile("p/a.go", "package p\n")
			r.save(a)
			r.settled()
			runs := r.lintRuns()
			wantProgram := strings.TrimSuffix(tt.tool, "-v2")
			if len(runs) != 1 || runs[0].Tool != wantProgram || !slices.Equal(runs[0].Args, tt.args) || runs[0].Dir != filepath.Dir(a) {
				t.Errorf("runs = %+v, want %s %v", runs, wantProgram, tt.args)
			}
			if got := r.markerList(LintMarkerOwner); !slices.Equal(got, tt.want) {
				t.Errorf("markers = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestALintToolFromAlternateToolsAndAMissingOne(t *testing.T) {
	// go.alternateTools names the program, and it is used when PATH has none
	r := newRig(t, rigOptions{withGo: true, env: []string{gotest.EnvLintOut + "=a.go:1:1: from the alternate (X1)"}})
	r.ext.debounce = 10 * time.Millisecond
	r.api.SetSetting(VetOnSaveSetting, "off")
	r.api.SetSetting(AlternateToolsSetting, map[string]any{"staticcheck": filepath.Join(fakeLintDir, "staticcheck")})
	a := r.pkgFile("p/a.go", "package p\n")
	r.save(a)
	r.settled()
	if got := r.markerList(LintMarkerOwner); len(got) != 1 || !strings.Contains(got[0], "from the alternate") {
		t.Errorf("markers = %q", got)
	}

	// no staticcheck anywhere: one notification with Install, once, however many saves
	r = newRig(t, rigOptions{withGo: true})
	r.ext.debounce = 10 * time.Millisecond
	r.api.SetSetting(VetOnSaveSetting, "off")
	a = r.pkgFile("p/a.go", "package p\n")
	for i := 0; i < 3; i++ {
		r.save(a)
		r.settled()
	}
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "warn" || !strings.HasPrefix(notes[0].Message, "staticcheck was not found.") || !slices.Equal(notes[0].Actions, []string{"Install"}) {
		t.Errorf("notes = %+v", notes)
	}
	// a tool the extension does not install: the message says what to do, with no button
	r.api.SetSetting(LintToolSetting, "revive")
	r.save(a)
	r.settled()
	r.save(a)
	r.settled()
	notes = r.api.Notes()
	if len(notes) != 2 || notes[1].Message != "revive was not found. Install it, or choose another tool with go.lintTool." || len(notes[1].Actions) != 0 {
		t.Errorf("notes = %+v", notes)
	}
}

func TestANewSaveCancelsTheRunThatIsStillGoingAndItsGroupEnds(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "gate")
	r := diagRig(t, gotest.EnvLintGate+"="+gate, gotest.EnvLintOut+"=a.go:3:1: late finding (SA1)")
	r.api.SetSetting(VetOnSaveSetting, "off")
	a := r.pkgFile("p/a.go", "package p\n")
	r.save(a)
	r.api.WaitFor("the first run to start", func() bool { return len(r.lintRuns()) == 1 })
	first := r.lintRuns()[0]
	r.save(a)
	r.api.WaitFor("the second run to start", func() bool { return len(r.lintRuns()) == 2 })
	// the first run was cancelled: its program and its group are gone while the second waits for the gate
	requireGone(t, "the first lint run", first.Pgid)
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.settled()
	if got := r.markerList(LintMarkerOwner); !slices.Equal(got, []string{"p/a.go:3:1 late finding [SA1] (staticcheck, warning)"}) {
		t.Errorf("markers = %q", got)
	}
}

func TestALintRunThatHangsEndsWithTheActivation(t *testing.T) {
	r := diagRig(t, gotest.EnvLintHang+"=1")
	r.api.SetSetting(VetOnSaveSetting, "off")
	r.save(r.pkgFile("p/a.go", "package p\n"))
	r.api.WaitFor("the run to start", func() bool { return len(r.lintRuns()) == 1 })
	run := r.lintRuns()[0]
	r.cancel()
	r.ext.wait()
	requireGone(t, "the lint tool", run.Pgid)
	if !gone(run.Pid) {
		t.Error("the lint tool is still there")
	}
}

func TestAToolThatFailsWithNoPlaceKeepsTheMarkersAndIsToldInTheOutput(t *testing.T) {
	r := diagRig(t, gotest.EnvVetOut+"=./a.go:1:1: a problem")
	r.api.SetSetting(LintOnSaveSetting, "off")
	a := r.pkgFile("p/a.go", "package p\n")
	r.save(a)
	r.settled()
	if len(r.markerList(VetMarkerOwner)) != 1 {
		t.Fatal("no marker to keep")
	}
	t.Setenv(gotest.EnvVetOut, "go: go.mod file not found in current directory or any parent directory")
	r.save(a)
	r.settled()
	if got := r.markerList(VetMarkerOwner); len(got) != 1 {
		t.Errorf("markers = %q: a run that did not work cleared them", got)
	}
	ch := r.api.Channel(EnvironmentChannel)
	if ch == nil || !strings.Contains(strings.Join(ch.Lines(), "\n"), "go.mod file not found") {
		t.Errorf("the Output channel has %v", ch)
	}
	// on save the user is not told with a notification
	if len(r.api.Notes()) != 0 {
		t.Errorf("notes = %+v", r.api.Notes())
	}
}

func TestTheCommandsToVetAndLint(t *testing.T) {
	r := diagRig(t)
	pkg := r.pkgFile("p/a.go", "package p\n")
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: fileURI(pkg), LanguageID: "go"})
	r.api.Pump()
	for _, id := range []string{VetPackageCommand, LintPackageCommand} {
		if err := r.api.Run(id, nil); err != nil {
			t.Fatal(err)
		}
		r.settled()
	}
	notes := r.api.Notes()
	if len(notes) != 2 || notes[0].Message != "go vet: no problems found in the package p." || notes[1].Message != "staticcheck: no problems found in the package p." {
		t.Errorf("notes = %+v", notes)
	}
	for _, id := range []string{VetWorkspaceCommand, LintWorkspaceCommand} {
		if err := r.api.Run(id, nil); err != nil {
			t.Fatal(err)
		}
		r.settled()
	}
	vet, lint := r.vetRuns(), r.lintRuns()
	if len(vet) != 2 || !slices.Equal(vet[1].Args, []string{"vet", "./..."}) || vet[1].Dir != r.folder || len(lint) != 2 || !slices.Equal(lint[1].Args, []string{"./..."}) {
		t.Errorf("vet %+v lint %+v", vet, lint)
	}
	if n := r.api.Notes(); len(n) != 4 || n[3].Message != "staticcheck: no problems found in the workspace." {
		t.Errorf("notes = %+v", n)
	}
	// a command run that fails is shown
	t.Setenv(gotest.EnvVetOut, "go: no go.mod")
	if err := r.api.Run(VetPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settled()
	if sev, msg := r.lastNote(); sev != "error" || msg != "go vet failed: go: no go.mod" {
		t.Errorf("note %s %q", sev, msg)
	}
}

func TestTheVetCommandsWithoutAFileOrAFolder(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true, noFolder: true})
	for _, id := range []string{VetPackageCommand, LintWorkspaceCommand} {
		if err := r.api.Run(id, nil); err != nil {
			t.Fatal(err)
		}
		if _, msg := r.lastNote(); !strings.HasPrefix(msg, "This command needs a") {
			t.Errorf("%s: note %q", id, msg)
		}
	}
	if len(r.vetRuns()) != 0 {
		t.Error("vet ran")
	}
}

func TestAMissingGoProgramIsToldOnceOnSaveAndAlwaysForACommand(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.ext.debounce = 10 * time.Millisecond
	r.api.SetSetting(LintOnSaveSetting, "off")
	a := r.pkgFile("p/a.go", "package p\n")
	for i := 0; i < 3; i++ {
		r.save(a)
		r.settled()
	}
	const msg = "The go command was not found. Install Go, or set go.alternateTools in the settings."
	if notes := r.api.Notes(); len(notes) != 1 || notes[0].Severity != "error" || notes[0].Message != msg {
		t.Fatalf("notes = %+v", notes)
	}
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: fileURI(a), LanguageID: "go"})
	r.api.Pump()
	if err := r.api.Run(VetPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	if notes := r.api.Notes(); len(notes) != 2 {
		t.Errorf("notes = %+v: a command is told again", notes)
	}
}
