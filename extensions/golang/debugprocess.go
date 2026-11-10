package golang

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The process picker of an attach (SPEC §10.31 "The debuggers", Go): ${command:pickProcess} lists the processes of the user,
// ${command:pickGoProcess} the ones that are Go programs, and a processId that is 0, empty or a name asks too.

// Ported from VS Code Go: extension/src/pickProcess.ts and extension/src/utils/psProcessParser.ts (Linux).

// processListTimeout ends `ps` and `go version` when they do not answer.
const processListTimeout = 20 * time.Second

// The values of processId that ask for a process (the interactive variables of the debugger).
const (
	pickProcessVariable   = "${command:pickProcess}"
	pickGoProcessVariable = "${command:pickGoProcess}"
)

// process is a row of the process list.
type process struct {
	pid int
	// name is the name of the executable (ps's comm, 15 characters at most on Linux) and cmd the command line.
	name, cmd string
}

// secondColumn is the width of the second column of the listing: ps makes a column as wide as its header, so the header is this many
// letters and the column holds what a quick pick shows of a label.
const secondColumn = 50

// psArgs are the arguments of ps: every process, with no limit of width on the last column, as the columns pid, comm and args.
func psArgs() []string {
	return []string{"axww", "-o", "pid=,comm=" + strings.Repeat("a", secondColumn) + ",args="}
}

var psLineRe = regexp.MustCompile(`^\s*([0-9]+)\s+(.{` + strconv.Itoa(secondColumn-1) + `})\s+(.*)$`)

// parsePS reads the output of ps with psArgs: the first line is the header of the table, then a line for each process.
func parsePS(out string) []process {
	var list []process
	lines := strings.Split(out, "\n")
	for i := 1; i < len(lines); i++ {
		m := psLineRe.FindStringSubmatch(strings.TrimSuffix(lines[i], "\r"))
		if m == nil {
			continue
		}
		pid, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		list = append(list, process{pid: pid, name: strings.TrimSpace(m[2]), cmd: strings.TrimSpace(m[3])})
	}
	return list
}

// goVersionRe finds the version in a line of `go version <file>...`: the file, then ": go1.22.1 ...".
var goVersionRe = regexp.MustCompile(`: go\d+\.\d+(\.\d+)?.*$`)

// parseGoVersionOutput lists the files `go version` found to be Go programs.
func parseGoVersionOutput(out string) []string {
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if loc := goVersionRe.FindStringIndex(line); loc != nil {
			files = append(files, line[:loc[0]])
		}
	}
	return files
}

// listProcesses runs ps. It fails with GoDebugProcessListFailed when ps is not there or fails.
func (g *golang) listProcesses(ctx context.Context) ([]process, error) {
	ps, err := lookProgram("ps", g.api.Scope().Folder)
	if err != nil {
		return nil, errors.NewErr("GoDebugProcessListFailed", err)
	}
	ctx, cancel := context.WithTimeout(ctx, processListTimeout)
	defer cancel()
	var out []string
	err = runProgram(ctx, cmdSpec{Program: ps, Args: psArgs(), Dir: g.api.Scope().Folder, Env: g.programEnv()}, func(l string) { out = append(out, l) })
	list := parsePS(strings.Join(out, "\n"))
	if err != nil && len(list) == 0 {
		return nil, errors.NewErr("GoDebugProcessListFailed", err)
	}
	return list, nil
}

// goProcesses are the processes whose executable is a Go program, found by `go version` on the executable of each (/proc/<pid>/exe on
// Linux, the path that ps gives as the name on macOS; the files that cannot be read are reported on its standard error and are not Go
// programs). Without a go program the list is all the processes.
func (g *golang) goProcesses(ctx context.Context, all []process) []process {
	program, err := g.goProgram()
	if err != nil {
		g.api.Notifications().Error("Failed to run \"go version\": the go program was not found. All processes are listed.")
		return all
	}
	args := []string{"version"}
	exe := map[string][]process{}
	for _, p := range all {
		path := executableOf(p)
		if path == "" {
			continue
		}
		if _, seen := exe[path]; !seen {
			args = append(args, path)
		}
		exe[path] = append(exe[path], p)
	}
	if len(args) == 1 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, processListTimeout)
	defer cancel()
	var out []string
	// go version exits with a status other than 0 when it cannot read one of the files: what it found is used
	_ = runProgram(ctx, cmdSpec{Program: program, Args: args, Dir: g.api.Scope().Folder, Env: g.programEnv()}, func(l string) { out = append(out, l) })
	var list []process
	for _, file := range parseGoVersionOutput(strings.Join(out, "\n")) {
		list = append(list, exe[file]...)
	}
	return list
}

// resolveProcessID fills in the processId of an attach in local mode when it asks for a process: 0, empty, the variable
// ${command:pickProcess} or ${command:pickGoProcess} (the interactive variables of the debugger, answered here because a command of an
// extension returns no value), or a name (the one process of that name, else the choice among them). Any other value stays for the
// substitution of the variables. It blocks on the user, so it runs off the UI thread.
func (g *golang) resolveProcessID(ctx context.Context, cfg extension.DebugConfiguration) error {
	var name string
	goOnly := false
	switch v := cfg["processId"].(type) {
	case nil:
	case float64:
		if v != 0 {
			return nil
		}
	case int:
		if v != 0 {
			return nil
		}
	case string:
		s := strings.TrimSpace(v)
		switch {
		case s == "" || s == pickProcessVariable:
		case s == pickGoProcessVariable:
			goOnly = true
		case strings.HasPrefix(s, "${"):
			return nil
		default:
			if _, isNumber := intAttribute(s); isNumber {
				return nil
			}
			name = s
		}
	default:
		return nil
	}
	all, err := g.listProcesses(ctx)
	if err != nil {
		return err
	}
	list := all
	switch {
	case goOnly:
		list = g.goProcesses(ctx, all)
	case name != "":
		var matches []process
		for _, p := range all {
			if p.name == name {
				matches = append(matches, p)
			}
		}
		if len(matches) == 1 {
			cfg["processId"] = float64(matches[0].pid)
			return nil
		}
		if len(matches) > 1 {
			list = matches
		}
	}
	pid, err := g.chooseProcess(ctx, list)
	if err != nil {
		return err
	}
	cfg["processId"] = float64(pid)
	return nil
}

// chooseProcess shows the processes in a quick pick (the name, the id and the command line, all searchable) and waits for the choice.
// It fails with GoDebugNoProcessSelected when the list is closed or has no process.
func (g *golang) chooseProcess(ctx context.Context, list []process) (int, error) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].pid < list[j].pid })
	items := make([]extension.QuickPickItem, len(list))
	for i, p := range list {
		items[i] = extension.QuickPickItem{Label: p.name, Description: strconv.Itoa(p.pid) + "  " + p.cmd}
	}
	if len(items) == 0 {
		return 0, errors.New("GoDebugNoProcessSelected", "No process selected: there is no process to attach to.")
	}
	index, err := g.pickItem(ctx, extension.QuickPickOptions{Placeholder: "Choose a process to attach to", Items: items, MatchOnDescription: true, IgnoreFocusOut: true})
	if err != nil {
		return 0, err
	}
	if index < 0 || index >= len(list) {
		return 0, errors.New("GoDebugNoProcessSelected", "No process selected.")
	}
	return list[index].pid, nil
}
