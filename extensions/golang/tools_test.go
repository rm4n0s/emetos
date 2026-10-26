package golang

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang/gotest"
)

// "Go: Install/Update Tools" and the missing tool (step 14.1), with the fake go program: nothing here installs a thing.

// installs are the runs of the fake go that were installs.
func (r *rig) installs() []goRun {
	var out []goRun
	for _, run := range r.runs() {
		if len(run.Args) > 0 && run.Args[0] == "install" {
			out = append(out, run)
		}
	}
	return out
}

// finished waits for the run of the tools to end.
func (r *rig) finished() *installRun {
	r.t.Helper()
	r.api.WaitFor("the tools run to end", func() bool { return r.ext.tools.run != nil && !r.ext.tools.run.running })
	return r.ext.tools.run
}

// states lists the states of the tools of the run by name.
func states(run *installRun) map[string]int {
	out := map[string]int{}
	for _, row := range run.rows {
		out[row.tool.Name] = row.state
	}
	return out
}

// output is the output of the run.
func (run *installRun) text() string {
	run.mu.Lock()
	defer run.mu.Unlock()
	return strings.Join(run.output, "\n")
}

func TestInstallToolsRunsGoInstallForEachToolOneByOne(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true})
	if err := r.api.Run(InstallToolsCommand, nil); err != nil {
		t.Fatal(err)
	}
	view := r.api.View(ToolsViewID)
	if len(view.Reveals) != 1 || !view.Reveals[0] {
		t.Errorf("the panel was revealed %v", view.Reveals)
	}
	run := r.finished()
	installs := r.installs()
	var targets []string
	for _, in := range installs {
		targets = append(targets, in.Args[1])
	}
	want := []string{"golang.org/x/tools/gopls@latest", "github.com/go-delve/delve/cmd/dlv@latest", "honnef.co/go/tools/cmd/staticcheck@latest"}
	if !slices.Equal(targets, want) {
		t.Fatalf("installed %v, want %v", targets, want)
	}
	// each in a folder of its own that is not the project's (the go.mod of the project must not matter), with modules on
	for _, in := range installs {
		if in.Dir == realPath(t, r.folder) || !strings.Contains(in.Dir, "emetos-go-tools-") || in.Env["GO111MODULE"] != "on" {
			t.Errorf("install = %+v", in)
		}
		if _, err := os.Stat(in.Dir); !os.IsNotExist(err) {
			t.Errorf("the folder %s of the install was not removed (%v)", in.Dir, err)
		}
		if _, ok := in.Env["GOPATH"]; ok {
			t.Errorf("GOPATH was set without go.toolsGopath: %v", in.Env)
		}
	}
	st := states(run)
	for _, name := range []string{"gopls", "dlv", "staticcheck"} {
		if st[name] != toolInstalled {
			t.Errorf("%s: state %d", name, st[name])
		}
	}
	// the output of the programs streams into the panel, with the command before each
	text := run.text()
	for _, want := range []string{"$ go install golang.org/x/tools/gopls@latest", "go: downloading honnef.co/go/tools/cmd/staticcheck@latest", "dlv is installed."} {
		if !strings.Contains(text, want) {
			t.Errorf("the output does not have %q:\n%s", want, text)
		}
	}
	if !strings.HasPrefix(run.summary, "Installed: gopls, dlv, staticcheck.") {
		t.Errorf("summary = %q", run.summary)
	}
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "info" || !strings.Contains(notes[0].Message, "gopls, dlv, staticcheck") {
		t.Errorf("notes = %+v", notes)
	}
	if refreshs, _, _ := view.Snapshot(); refreshs < 4 {
		t.Errorf("the panel was built %d times", refreshs)
	}
}

func TestInstallToolsCanInstallSomeOfThem(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true})
	if err := r.api.Run(InstallToolsCommand, map[string]any{"tools": []any{"dlv"}}); err != nil {
		t.Fatal(err)
	}
	run := r.finished()
	if len(run.rows) != 1 || run.rows[0].tool.Name != "dlv" || len(r.installs()) != 1 {
		t.Errorf("rows %+v, installs %+v", run.rows, r.installs())
	}
	err := r.api.Run(InstallToolsCommand, map[string]any{"tools": []any{"gopls", "no-such-tool"}})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("golang.installToolsCommand.ToolUnknown") {
		t.Errorf("err = %v", err)
	}
	if len(r.installs()) != 1 {
		t.Error("a list with an unknown tool installed something")
	}
}

func TestInstallToolsUsesTheSettings(t *testing.T) {
	gopath := filepath.Join(t.TempDir(), "gopath")
	r := newRig(t, rigOptions{withGo: true, settings: map[string]any{
		ToolsGopathSetting:  gopath,
		ToolsEnvVarsSetting: map[string]any{"FAKE_TOOLS_VAR": "yes", "GOFLAGS": "-modcacherw"},
	}})
	if err := r.api.Run(InstallToolsCommand, map[string]any{"tools": []any{"gopls"}}); err != nil {
		t.Fatal(err)
	}
	r.finished()
	in := r.installs()
	if len(in) != 1 || in[0].Env["GOPATH"] != gopath || in[0].Env["GOBIN"] != gopath+"/bin" || in[0].Env["FAKE_TOOLS_VAR"] != "yes" || in[0].Env["GOFLAGS"] != "-modcacherw" {
		t.Errorf("install = %+v", in)
	}
}

func TestInstallToolsUsesTheGoProgramOfTheToolsManagement(t *testing.T) {
	r := newRig(t, rigOptions{})
	// no go in PATH: the installs run with the program of go.toolsManagement.go
	if err := r.api.Run(InstallToolsCommand, map[string]any{"tools": []any{"gopls"}}); err != nil {
		t.Fatal(err)
	}
	if notes := r.api.Notes(); len(notes) != 1 || notes[0].Severity != "error" || !strings.Contains(notes[0].Message, "go.toolsManagement.go") {
		t.Fatalf("notes = %+v", notes)
	}
	if r.ext.tools.run != nil {
		t.Error("a run started without a go program")
	}
	r.api.SetSetting(ToolsManagementGoSetting, filepath.Join(fakeGoDir, "go"))
	if err := r.api.Run(InstallToolsCommand, map[string]any{"tools": []any{"gopls"}}); err != nil {
		t.Fatal(err)
	}
	r.finished()
	if len(r.installs()) != 1 {
		t.Errorf("installs = %+v", r.installs())
	}
}

func TestAFailedInstallIsShownAndTheOthersGoOn(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true, env: []string{gotest.EnvFail + "=delve"}})
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerMissing})
	// the missing-tool notification is not what this test is about
	before := len(r.api.Notes())
	if err := r.api.Run(InstallToolsCommand, nil); err != nil {
		t.Fatal(err)
	}
	run := r.finished()
	st := states(run)
	if st["gopls"] != toolInstalled || st["dlv"] != toolFailed || st["staticcheck"] != toolInstalled {
		t.Errorf("states = %v", st)
	}
	if !strings.Contains(run.text(), "module lookup disabled") || !strings.Contains(run.text(), "dlv: ") {
		t.Errorf("output:\n%s", run.text())
	}
	notes := r.api.Notes()[before:]
	if len(notes) != 1 || notes[0].Severity != "error" || !strings.Contains(notes[0].Message, "Could not install: dlv.") || !slices.Equal(notes[0].Actions, []string{"Show Output"}) {
		t.Fatalf("notes = %+v", notes)
	}
	view := r.api.View(ToolsViewID)
	revealed := len(view.Reveals)
	r.api.RunNoteAction(before+0, "Show Output")
	if len(view.Reveals) != revealed+1 {
		t.Error("Show Output did not reveal the panel")
	}
	// gopls was installed: the server that was missing is started again
	if r.api.ServerRestarts(ServerID) != 1 {
		t.Errorf("restarts = %d, want the missing server started again", r.api.ServerRestarts(ServerID))
	}
}

func TestInstallingGoplsDoesNotRestartAServerThatRuns(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true})
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerRunning})
	if err := r.api.Run(InstallToolsCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.finished()
	if r.api.ServerRestarts(ServerID) != 0 {
		t.Error("a running server was restarted")
	}
}

func TestACancelledInstallEndsTheProgramsAndLeavesTheRestAlone(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pids")
	r := newRig(t, rigOptions{withGo: true, env: []string{gotest.EnvHang + "=delve", gotest.EnvPidfile + "=" + pidfile}})
	if err := r.api.Run(InstallToolsCommand, nil); err != nil {
		t.Fatal(err)
	}
	pid, child := pidsOf(t, pidfile)
	r.api.Pump()
	run := r.ext.tools.run
	if !run.running || states(run)["dlv"] != toolInstalling || states(run)["gopls"] != toolInstalled {
		t.Fatalf("states = %v running %v", states(run), run.running)
	}
	// a second command while it runs only shows the panel
	view := r.api.View(ToolsViewID)
	revealed := len(view.Reveals)
	if err := r.api.Run(InstallToolsCommand, nil); err != nil {
		t.Fatal(err)
	}
	if len(view.Reveals) != revealed+1 || r.ext.tools.run != run || len(r.installs()) != 2 {
		t.Errorf("reveals %d, same run %v, installs %d", len(view.Reveals), r.ext.tools.run == run, len(r.installs()))
	}
	run.cancel() // the Cancel button
	run = r.finished()
	st := states(run)
	if st["gopls"] != toolInstalled || st["dlv"] != toolSkipped || st["staticcheck"] != toolSkipped {
		t.Errorf("states = %v", st)
	}
	requireGone(t, "the install", pid)
	for _, p := range []int{pid, child} {
		deadline := 0
		for !gone(p) && deadline < 1500 {
			deadline++
			sleepMS(10)
		}
		if !gone(p) {
			t.Errorf("process %d is still there", p)
		}
	}
	if len(r.installs()) != 2 {
		t.Errorf("the tool after the cancelled one was started: %d installs", len(r.installs()))
	}
	notes := r.api.Notes()
	if last := notes[len(notes)-1]; last.Severity != "info" || !strings.Contains(last.Message, "cancelled") || run.summary != "Cancelled." {
		t.Errorf("notes = %+v, summary %q", notes, run.summary)
	}
	// the panel offers a new run
	if err := r.api.Run(InstallToolsCommand, map[string]any{"tools": []any{"gopls"}}); err != nil {
		t.Fatal(err)
	}
	if r.finished() == run {
		t.Error("no new run")
	}
}

func TestTheEndOfTheActivationKillsAnInstall(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pids")
	r := newRig(t, rigOptions{withGo: true, env: []string{gotest.EnvHang + "=gopls", gotest.EnvPidfile + "=" + pidfile}})
	if err := r.api.Run(InstallToolsCommand, nil); err != nil {
		t.Fatal(err)
	}
	pid, child := pidsOf(t, pidfile)
	r.cancel()
	r.ext.wait()
	for _, p := range []int{pid, child} {
		for i := 0; !gone(p) && i < 1500; i++ {
			sleepMS(10)
		}
		if !gone(p) {
			t.Errorf("process %d outlived the activation", p)
		}
	}
	// the folder of the install is removed too
	for _, in := range r.installs() {
		if _, err := os.Stat(in.Dir); !os.IsNotExist(err) {
			t.Errorf("the folder %s is still there (%v)", in.Dir, err)
		}
	}
}

func TestAMissingToolIsToldOnceWithAnInstallButton(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true})
	// gopls: the host tells the state, the extension tells the user
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerMissing})
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "warn" || !slices.Equal(notes[0].Actions, []string{"Install"}) ||
		!strings.Contains(notes[0].Message, "gopls was not found") || !strings.Contains(notes[0].Message, "go.goplsPath") {
		t.Fatalf("notes = %+v", notes)
	}
	// and not again: not for a restart that finds it missing again, not for another change of state
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerStarting})
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerMissing})
	if len(r.api.Notes()) != 1 {
		t.Errorf("notes = %+v", r.api.Notes())
	}
	// Install installs that tool only
	r.api.RunNoteAction(0, "Install")
	run := r.finished()
	if len(run.rows) != 1 || run.rows[0].tool.Name != "gopls" || len(r.installs()) != 1 || r.installs()[0].Args[1] != "golang.org/x/tools/gopls@latest" {
		t.Errorf("rows %+v installs %+v", run.rows, r.installs())
	}
	// the other tools say what they are for
	for _, name := range []string{"dlv", "staticcheck"} {
		tl, _ := toolByName(name)
		r.ext.promptMissingTool(tl)
		r.ext.promptMissingTool(tl)
	}
	notes = r.api.Notes()
	var messages []string
	for _, n := range notes[len(notes)-2:] {
		messages = append(messages, n.Message)
	}
	if len(notes) != 4 || messages[0] != "dlv was not found. It debugs Go programs." || messages[1] != "staticcheck was not found. It checks Go code for mistakes." {
		t.Errorf("notes = %+v", notes)
	}
}

func TestTheToolsAndTheirStatesAreDescribed(t *testing.T) {
	// the states of a tool have an icon and a word each
	seen := map[string]bool{}
	for _, s := range []int{toolPending, toolInstalling, toolInstalled, toolFailed, toolSkipped} {
		icon, word := stateIcon(s), stateText(s)
		if icon == "" || word == "" || seen[icon+word] {
			t.Errorf("state %d: %q %q", s, icon, word)
		}
		seen[icon+word] = true
	}
	if _, ok := toolByName("nothing"); ok {
		t.Error("a tool that does not exist was found")
	}
	var names []string
	for _, tl := range allTools() {
		names = append(names, tl.Name)
		if tl.Package == "" || tl.Version == "" || tl.Purpose == "" {
			t.Errorf("tool %+v", tl)
		}
	}
	if !slices.Equal(names, []string{"gopls", "dlv", "staticcheck"}) {
		t.Errorf("tools = %v", names)
	}
}

func TestInstallToolsFailsWhenThereIsNoFolderToInstallIn(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true})
	// a TMPDIR that is a file: the folder of the install cannot be made
	file := filepath.Join(t.TempDir(), "not-a-folder")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", file)
	if err := r.api.Run(InstallToolsCommand, nil); err != nil {
		t.Fatal(err)
	}
	run := r.finished()
	for name, st := range states(run) {
		if st != toolFailed {
			t.Errorf("%s: state %d, want failed", name, st)
		}
	}
	if !strings.Contains(run.text(), "The folder to install in cannot be made") || len(r.installs()) != 0 || !strings.HasPrefix(run.summary, "Could not install: gopls, dlv, staticcheck.") {
		t.Errorf("output %q, installs %d, summary %q", run.text(), len(r.installs()), run.summary)
	}
}
