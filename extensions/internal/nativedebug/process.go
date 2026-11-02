package nativedebug

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

// The process picker of an attach (SPEC §10.31 "The debuggers"): ${command:pickProcess} lists the processes of the machine and the
// choice is the pid of the configuration.

// Ported from vscode-python-debugger: src/extension/debugger/attachQuickPick/psProcessParser.ts (Linux), as the Python extension does.

// processListTimeout ends `ps` when it does not answer, and maxPSOutput bounds what is read of it.
const (
	processListTimeout = 20 * time.Second
	maxPSOutput        = 16 << 20
)

// process is a row of the process list.
type process struct {
	pid int
	// name is the name of the executable (ps's comm) and cmd the command line.
	name, cmd string
}

// secondColumn is the width of the second column of the listing: ps makes a column as wide as its header, so the header is this many
// letters and the column holds the name of the executable.
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

// sortProcesses orders the list by the name of the executable, ignoring the case, then by pid.
func sortProcesses(list []process) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := strings.ToLower(list[i].name), strings.ToLower(list[j].name)
		if a != b {
			return a < b
		}
		return list[i].pid < list[j].pid
	})
}

// listProcesses runs ps. It fails with NativeDebugProcessListFailed when ps is not there or fails.
func listProcesses(ctx context.Context) ([]process, error) {
	ps, err := exec.LookPath("ps")
	if err != nil {
		return nil, errors.NewErr("NativeDebugProcessListFailed", err)
	}
	ctx, cancel := context.WithTimeout(ctx, processListTimeout)
	defer cancel()
	cmd := exec.Command(ps, psArgs()...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, left: maxPSOutput}
	if err := cmd.Start(); err != nil {
		return nil, errors.NewErr("NativeDebugProcessListFailed", err)
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
		return nil, errors.NewErr("NativeDebugProcessListFailed", waitErr)
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

// pickProcess lists the processes in a quick pick (the name, the id and the command line, all searchable) and waits for the choice. It
// fails with NativeDebugNoProcessSelected when the list is closed or has no process, with NativeDebugPickFailed when the host has no
// quick pick. It blocks on the user, so it runs off the UI thread.
func (d *Debugger) pickProcess(ctx context.Context) (int, error) {
	list, err := listProcesses(ctx)
	if err != nil {
		return 0, err
	}
	sortProcesses(list)
	if len(list) == 0 {
		return 0, errors.New("NativeDebugNoProcessSelected", "No process selected: there is no process to attach to.")
	}
	items := make([]extension.QuickPickItem, len(list))
	for i, pr := range list {
		items[i] = extension.QuickPickItem{Label: pr.name, Description: strconv.Itoa(pr.pid) + "  " + pr.cmd}
	}
	ch := make(chan int, 1)
	opts := extension.QuickPickOptions{Placeholder: "Select the process to attach to", Items: items, MatchOnDescription: true, IgnoreFocusOut: true}
	if err := d.api.QuickPick().Show(opts, func(i int) { ch <- i }); err != nil {
		return 0, errors.NewErr("NativeDebugPickFailed", err)
	}
	select {
	case i := <-ch:
		if i < 0 || i >= len(list) {
			return 0, errors.New("NativeDebugNoProcessSelected", "No process selected.")
		}
		return list[i].pid, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
