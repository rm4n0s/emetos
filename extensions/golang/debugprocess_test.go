package golang

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// The process picker of an attach (step 25.7) over a fake ps and the fake go. Nothing here lists the processes of the machine.

// psLine is a row of the listing ps makes with psArgs: the pid, the name in a column of 50 characters, the command line.
func psLine(pid int, name, cmd string) string {
	return fmt.Sprintf("%5d %-*s %s", pid, secondColumn-1, name, cmd)
}

// psListing is the output of ps: the header of the table, then the rows.
func psListing(rows ...string) string {
	return fmt.Sprintf("    %s\n", strings.Repeat("a", secondColumn)) + strings.Join(rows, "\n") + "\n"
}

// fakePS puts a ps that prints the listing (or fails with the status) first in PATH, with the fake go after it.
func fakePS(t *testing.T, listing string, status int) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ps.out"), []byte(listing), 0o600); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\n# only builtins: PATH holds nothing else\nwhile IFS= read -r line; do printf '%%s\\n' \"$line\"; done < \"${0%%/*}/ps.out\"\nexit %d\n", status)
	ps := filepath.Join(dir, "ps")
	if err := os.WriteFile(ps, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	// A goroutine of an earlier test that forks while the script is still open for writing gives the child a copy of the file
	// descriptor until it execs, and running the script then fails with "text file busy". The script is run once here until it no
	// longer is, so the picker's own run of it never meets that (running it only prints the listing).
	for i := 0; ; i++ {
		err := exec.Command(ps).Run()
		if !errors.Is(err, syscall.ETXTBSY) {
			break
		}
		if i == 100 {
			t.Fatalf("the fake ps stays busy: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+fakeGoDir)
}

func TestParsePS(t *testing.T) {
	long := strings.Repeat("x", 60)
	tests := []struct {
		name, in string
		want     []process
	}{
		{"the header is skipped", psListing(psLine(1, "init", "/sbin/init splash"), psLine(22, "bash", "-bash")),
			[]process{{1, "init", "/sbin/init splash"}, {22, "bash", "-bash"}}},
		{"a command line with no arguments", psListing(psLine(7, "kthreadd", "")), []process{{7, "kthreadd", ""}}},
		{"a name of 15 characters and a long command line", psListing(psLine(9, "systemd-journal", "/usr/lib/systemd/systemd-journald "+long)),
			[]process{{9, "systemd-journal", "/usr/lib/systemd/systemd-journald " + long}}},
		{"lines that are not rows", psListing("garbage", "", psLine(3, "ok", "run")), []process{{3, "ok", "run"}}},
		{"nothing", "", nil},
		{"only the header", psListing(), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePS(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("processes = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseGoVersionOutput(t *testing.T) {
	in := "/proc/10/exe: go1.22.1\n/proc/11/exe: go1.21\n/proc/12/exe: go1.23.4 X:nocoverageredesign\n/proc/13/exe: could not read Go build info\nnoise\n"
	want := []string{"/proc/10/exe", "/proc/11/exe", "/proc/12/exe"}
	if got := parseGoVersionOutput(in); !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

// pickLabels are the processes of the last quick pick as "name pid", in the order shown.
func pickLabels(r *rig) []string {
	var out []string
	for _, it := range r.api.LastPick().Options().Items {
		pid, _, _ := strings.Cut(it.Description, " ")
		out = append(out, it.Label+" "+pid)
	}
	return out
}

func TestTheProcessPickerAnswersTheProcessId(t *testing.T) {
	listing := psListing(
		psLine(30, "nginx", "nginx: worker"),
		psLine(10, "myapp", "/opt/myapp --serve"),
		psLine(20, "bash", "-bash"),
		psLine(40, "myapp", "/opt/myapp --other"),
		psLine(50, "solo", "/opt/solo"),
	)
	tests := []struct {
		name string
		// processID is the attribute; a nil one is missing from the configuration
		processID any
		// shown are the processes the picker lists, in order, or nil when it is not shown; choose is the item chosen
		shown  []string
		choose int
		want   any
	}{
		{"0", float64(0), []string{"myapp 10", "bash 20", "nginx 30", "myapp 40", "solo 50"}, 2, float64(30)},
		{"missing", nil, []string{"myapp 10", "bash 20", "nginx 30", "myapp 40", "solo 50"}, 0, float64(10)},
		{"empty", "", []string{"myapp 10", "bash 20", "nginx 30", "myapp 40", "solo 50"}, 4, float64(50)},
		{"the variable pickProcess", "${command:pickProcess}", []string{"myapp 10", "bash 20", "nginx 30", "myapp 40", "solo 50"}, 1, float64(20)},
		{"a name of one process", "solo", nil, 0, float64(50)},
		{"a name of two processes", "myapp", []string{"myapp 10", "myapp 40"}, 1, float64(40)},
		{"a name of no process", "ghost", []string{"myapp 10", "bash 20", "nginx 30", "myapp 40", "solo 50"}, 3, float64(40)},
		{"the variable pickGoProcess", "${command:pickGoProcess}", []string{"myapp 10", "myapp 40"}, 0, float64(10)},
		{"a number", float64(77), nil, 0, float64(77)},
		{"a number as text is left for the substitution", "77", nil, 0, "77"},
		{"another variable is left for the substitution", "${input:pid}", nil, 0, "${input:pid}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := debugRig(t, rigOptions{withGo: true})
			fakePS(t, listing, 0)
			t.Setenv("FAKEGO_VERSION_FILES", executableFor(10, "myapp", "/opt/myapp --serve")+","+executableFor(40, "myapp", "/opt/myapp --other"))
			in := cfg{"name": "A", "request": "attach", "mode": "local"}
			if tt.processID != nil {
				in["processId"] = tt.processID
			}
			ch := r.resolveAsync(r.folder, in)
			if tt.shown != nil {
				r.api.WaitFor("the picker", func() bool { return r.api.LastPick() != nil })
				if got := pickLabels(r); !reflect.DeepEqual(got, tt.shown) {
					t.Errorf("picker = %v, want %v", got, tt.shown)
				}
				p := r.api.LastPick()
				if !p.Options().MatchOnDescription || p.Options().Placeholder != "Choose a process to attach to" {
					t.Errorf("options = %+v", p.Options())
				}
				p.Choose(tt.choose)
			}
			got := r.wait(ch)
			if got.err != nil {
				t.Fatal(got.err)
			}
			if got.cfg["processId"] != tt.want {
				t.Errorf("processId = %#v, want %#v", got.cfg["processId"], tt.want)
			}
			if tt.shown == nil && len(r.api.Picks()) != 0 {
				t.Error("a picker was shown")
			}
		})
	}
}

func TestTheDetailOfAProcessIsItsCommandLine(t *testing.T) {
	r, _ := debugRig(t, rigOptions{withGo: true})
	fakePS(t, psListing(psLine(10, "myapp", "/opt/myapp --serve -v")), 0)
	ch := r.resolveAsync(r.folder, cfg{"request": "attach", "mode": "local", "processId": float64(0)})
	r.api.WaitFor("the picker", func() bool { return r.api.LastPick() != nil })
	items := r.api.LastPick().Options().Items
	if len(items) != 1 || items[0].Label != "myapp" || items[0].Description != "10  /opt/myapp --serve -v" {
		t.Errorf("items = %+v", items)
	}
	r.api.LastPick().Choose(0)
	if got := r.wait(ch); got.err != nil || got.cfg["processId"] != float64(10) {
		t.Errorf("answer = %+v", got)
	}
}

func TestTheProcessPickerFailsWithoutAChoiceOrAList(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T)
		// closePick closes the picker with no choice
		closePick bool
		route     string
	}{
		{"the list is closed", func(t *testing.T) { fakePS(t, psListing(psLine(10, "a", "b")), 0) }, true, "chooseProcess.GoDebugNoProcessSelected"},
		{"there is no process", func(t *testing.T) { fakePS(t, psListing(), 0) }, false, "chooseProcess.GoDebugNoProcessSelected"},
		{"ps is not there", func(t *testing.T) { t.Setenv("PATH", emptyPath) }, false, "listProcesses.GoDebugProcessListFailed"},
		{"ps fails", func(t *testing.T) { fakePS(t, "", 3) }, false, "listProcesses.GoDebugProcessListFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := debugRig(t, rigOptions{withGo: true})
			tt.setup(t)
			ch := r.resolveAsync(r.folder, cfg{"request": "attach", "mode": "local", "processId": float64(0)})
			if tt.closePick {
				r.api.WaitFor("the picker", func() bool { return r.api.LastPick() != nil })
				r.api.LastPick().Close()
			}
			got := r.wait(ch)
			appErr, ok := errors.FromError(got.err)
			if !ok || !appErr.HasRoute(tt.route) {
				t.Errorf("err = %v, want the route %s", got.err, tt.route)
			}
		})
	}
}

func TestPickGoProcessWithoutGoListsEveryProcess(t *testing.T) {
	// go.alternateTools names a go that is not there
	r, _ := debugRig(t, rigOptions{settings: map[string]any{AlternateToolsSetting: map[string]any{"dlv": fakeDlv(t), "go": "/no/such/go"}}})
	fakePS(t, psListing(psLine(10, "a", "b"), psLine(20, "c", "d")), 0)
	ch := r.resolveAsync(r.folder, cfg{"request": "attach", "mode": "local", "processId": "${command:pickGoProcess}"})
	r.api.WaitFor("the picker", func() bool { return r.api.LastPick() != nil })
	if got := pickLabels(r); !reflect.DeepEqual(got, []string{"a 10", "c 20"}) {
		t.Errorf("picker = %v", got)
	}
	r.api.LastPick().Choose(1)
	if got := r.wait(ch); got.err != nil || got.cfg["processId"] != float64(20) {
		t.Errorf("answer = %+v", got)
	}
	if sev, msg := r.lastNote(); sev != "error" || !strings.Contains(msg, `Failed to run "go version"`) {
		t.Errorf("note = %s %q", sev, msg)
	}
}

// executableFor is the file that the picker asks `go version` about for a process: /proc/<pid>/exe on Linux, the path of the
// program on macOS.
func executableFor(pid int, name, cmd string) string {
	return executableOf(process{pid: pid, name: name, cmd: cmd})
}
