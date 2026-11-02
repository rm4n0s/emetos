package python

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The process picker of an attach (SPEC §10.31 "The debuggers", Python): `${command:pickProcess}` lists the processes of the user, the
// Python ones first, and the choice is the processId of the configuration.

// Ported from vscode-python-debugger: src/extension/debugger/attachQuickPick/provider.ts and psProcessParser.ts (Linux).

// processListTimeout ends `ps` when it does not answer, and maxPSOutput bounds what is read of it.
const (
	processListTimeout = 20 * time.Second
	maxPSOutput        = 16 << 20
)

// pickProcessVariable is the value of processId that asks for a process (the interactive variable of the debugger).
const pickProcessVariable = "${command:pickProcess}"

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

// sortProcesses orders the list as the Python Debugger does: the processes whose name starts with "python" first, by command line,
// then the rest by name, ignoring the case.
func sortProcesses(list []process) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		ap, bp := strings.HasPrefix(a.name, "python"), strings.HasPrefix(b.name, "python")
		switch {
		case ap && !bp:
			return true
		case bp && !ap:
			return false
		case ap:
			return strings.ToLower(a.cmd) < strings.ToLower(b.cmd)
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})
}

// listProcesses runs ps. It fails with PythonDebugProcessListFailed when ps is not there or fails.
func (p *python) listProcesses(ctx context.Context) ([]process, error) {
	ps, err := exec.LookPath("ps")
	if err != nil {
		return nil, errors.NewErr("PythonDebugProcessListFailed", err)
	}
	ctx, cancel := context.WithTimeout(ctx, processListTimeout)
	defer cancel()
	cmd := exec.Command(ps, psArgs()...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, left: maxPSOutput}
	if err := cmd.Start(); err != nil {
		return nil, errors.NewErr("PythonDebugProcessListFailed", err)
	}
	pgid := cmd.Process.Pid
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		case <-done:
		}
	}()
	waitErr := cmd.Wait()
	close(done)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	list := parsePS(out.String())
	if waitErr != nil && len(list) == 0 {
		return nil, errors.NewErr("PythonDebugProcessListFailed", waitErr)
	}
	return list, nil
}

// limitedWriter writes at most left bytes and drops the rest.
type limitedWriter struct {
	w    io.Writer
	left int
}

// Write implements io.Writer.
func (l *limitedWriter) Write(b []byte) (int, error) {
	n := len(b)
	if n > l.left {
		b = b[:l.left]
	}
	l.left -= len(b)
	if _, err := l.w.Write(b); err != nil {
		return 0, err
	}
	return n, nil
}

// resolveProcessID fills in the processId of an attach when it is the variable ${command:pickProcess} (answered here because a command
// of an extension returns no value). Any other value stays for the substitution of the variables. It blocks on the user, so it runs off
// the UI thread.
func (p *python) resolveProcessID(ctx context.Context, cfg extension.DebugConfiguration) error {
	if s, ok := cfg["processId"].(string); !ok || strings.TrimSpace(s) != pickProcessVariable {
		return nil
	}
	all, err := p.listProcesses(ctx)
	if err != nil {
		return err
	}
	pid, err := p.chooseProcess(ctx, all)
	if err != nil {
		return err
	}
	cfg["processId"] = float64(pid)
	return nil
}

// chooseProcess shows the processes in a quick pick (the name, the id and the command line, all searchable) and waits for the choice.
// It fails with PythonDebugNoProcessSelected when the list is closed or has no process.
func (p *python) chooseProcess(ctx context.Context, list []process) (int, error) {
	sortProcesses(list)
	items := make([]extension.QuickPickItem, len(list))
	for i, pr := range list {
		items[i] = extension.QuickPickItem{Label: pr.name, Description: strconv.Itoa(pr.pid) + "  " + pr.cmd}
	}
	if len(items) == 0 {
		return 0, errors.New("PythonDebugNoProcessSelected", "No process selected: there is no process to attach to.")
	}
	index, err := p.pickItem(ctx, extension.QuickPickOptions{Placeholder: "Select the process to attach to", Items: items, MatchOnDescription: true, IgnoreFocusOut: true})
	if err != nil {
		return 0, err
	}
	if index < 0 || index >= len(list) {
		return 0, errors.New("PythonDebugNoProcessSelected", "No process selected.")
	}
	return list[index].pid, nil
}
