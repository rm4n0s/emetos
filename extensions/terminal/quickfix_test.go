package terminal

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"testing"
)

// The cases are those of VS Code's quickFixAddon.test.ts: the same commands, outputs and exit statuses with the same expected
// actions. VS Code's test gives each fix the output through a stand-in of getOutputMatch; here the lines go through the real
// matching (anchor, offset and length), with the empty line the prompt will be on added at the end as the terminal has it.
//
// Ported from VS Code: src/vs/workbench/contrib/terminalContrib/quickFix/test/browser/quickFixAddon.test.ts

// linesOf are the logical lines of an output as the terminal reads them when the command ended: the text of the line the
// shell's prompt will be on, up to the column where the end was reported, is empty.
func linesOf(output string) []string { return append(strings.Split(output, "\n"), "") }

func labelsOf(fixes []quickFix) []string {
	var out []string
	for _, f := range fixes {
		out = append(out, f.id+" | "+f.label)
	}
	return out
}

func TestQuickFixes(t *testing.T) {
	pwshOutput := strings.Join([]string{
		"...", "", "Suggestion [General]:",
		"  The most similar commands are: python3, python3m, pamon, python3.6, rtmon, echo, pushd, etsn, pwsh, pwconv.",
		"", "Suggestion [cmd-not-found]:", "  Command 'python' not found, but can be installed with:",
		"  sudo apt install python3", "  sudo apt install python", "  sudo apt install python-minimal",
		"  You also have python3 installed, you can run 'python3' instead.'", "",
	}, "\n")
	pushOutput := "fatal: The current branch test22 has no upstream branch.\n\t\tTo push the current branch and set the remote as upstream, use\n\n\t\t\tgit push --set-upstream origin test22"
	prOutput := strings.Join([]string{
		"Total 0 (delta 0), reused 0 (delta 0), pack-reused 0", "remote:",
		"remote: Create a pull request for 'test22' on GitHub by visiting:",
		"remote:      https://github.com/meganrogge/xterm.js/pull/new/test22", "remote:",
		"To https://github.com/meganrogge/xterm.js", " * [new branch]        test22 -> test22",
		"Branch 'test22' set up to track remote branch 'test22' from 'origin'. ",
	}, "\n")
	yarn := strings.Join([]string{
		"yarn run v1.22.17", "warning ../../package.json: No license field",
		"Error: listen EADDRINUSE: address already in use 0.0.0.0:3000", "\tat Server.setupListenHandle [as _listen2] (node:net:1315:16)",
		"\tat listenInCluster (node:net:1363:12)", "\tat doListen (node:net:1501:7)", "\tat processTicksAndRejections (node:internal/process/task_queues:84:21)",
		"Emitted 'error' event on WebSocketServer instance at:", "\tat Server.emit (node:events:394:28)", "\tat emitErrorNT (node:net:1342:8)",
		"\tat processTicksAndRejections (node:internal/process/task_queues:83:21) {", "}", "error Command failed with exit code 1.",
		"info Visit https://yarnpkg.com/en/docs/cli/run for documentation about this command.",
	}, "\n")
	similarOne := "git: 'sttatus' is not a git command. See 'git --help'.\n\n\t\t\tThe most similar command is\n\t\t\tstatus"
	similarMany := "git: 'pu' is not a git command. See 'git --help'.\n\t\t\t\tThe most similar commands are\n\t\t\t\t\t\tpull\n\t\t\t\t\t\tpush"
	similarArg := "git: 'checkoutt' is not a git command. See 'git --help'.\n\t\t\t\tThe most similar commands are\n\t\t\t\t\t\tcheckout"

	cases := []struct {
		name    string
		command string
		output  string
		exit    int
		want    []string
	}{
		{"git similar, one", "git sttatus", similarOne, 1, []string{"Git Similar | Run: git status"}},
		{"git similar, another exit status", "git sttatus", similarOne, 2, []string{"Git Similar | Run: git status"}},
		{"git similar, output does not match", "git sttatus", "invalid output", 1, nil},
		{"git similar, command does not match", "gt sttatus", similarOne, 1, nil},
		{"git similar, several", "git pu", similarMany, 1, []string{"Git Similar | Run: git pull", "Git Similar | Run: git push"}},
		{"git similar passes the arguments through", "git checkoutt .", similarArg, 1, []string{"Git Similar | Run: git checkout ."}},
		{"git similar on success is nothing", "git sttatus", similarOne, 0, nil},

		{"git two dashes", "git add . -all", "error: did you mean `--all` (with two dashes)?", 1, []string{"Git Two Dashes | Run: git add . --all"}},
		{"git two dashes, another exit status", "git add . -all", "error: did you mean `--all` (with two dashes)?", 2, []string{"Git Two Dashes | Run: git add . --all"}},
		{"git two dashes, output does not match", "git add . -all", "invalid output", 1, nil},
		{"git two dashes, command does not match", "gt sttatus", "error: did you mean `--all` (with two dashes)?", 1, nil},

		{"git fast forward pull", "git checkout vnext", "Already on 'vnext' \n Your branch is behind 'origin/vnext' by 1 commit, and can be fast-forwarded.", 0,
			[]string{"Git Fast Forward Pull | Run: git pull"}},
		{"git fast forward pull, output does not match", "git checkout vnext", "invalid output", 0, nil},
		{"git fast forward pull, command does not match", "gt add", "and can be fast-forwarded.", 0, nil},
		{"git fast forward pull, exit status does not match", "git checkout vnext", "and can be fast-forwarded.", 2, nil},

		{"free port", "yarn start dev", yarn, 1, []string{"Free Port | Free port 3000"}},
		{"free port, output does not match", "yarn start dev", "invalid output", 1, nil},
		{"free port, python", "python -m http.server 8000", "OSError: [Errno 98] Address already in use\nUnable to bind 127.0.0.1:8000", 1, []string{"Free Port | Free port 8000"}},
		{"free port, a short port number does not match", "x", "listen EADDRINUSE :::80", 1, nil},

		{"git push set upstream", "git push", pushOutput, 128, []string{"Git Push Set Upstream | Run: git push --set-upstream origin test22"}},
		{"git push set upstream, another exit status", "git push", pushOutput, 2, []string{"Git Push Set Upstream | Run: git push --set-upstream origin test22"}},
		{"git push set upstream, output does not match", "git push", "invalid output", 128, nil},
		{"git push set upstream, command does not match", "git status", pushOutput, 128, nil},

		{"git create pr", "git push", prOutput, 0, []string{"Git Create Pr | Open: https://github.com/meganrogge/xterm.js/pull/new/test22"}},
		{"git create pr, output does not match", "git push", "invalid output", 0, nil},
		{"git create pr, command does not match", "git status", prOutput, 0, nil},
		{"git create pr, failure status", "git push", prOutput, 2, nil},

		{"pwsh general error", "not important", strings.Join(strings.Split(pwshOutput, "\n")[:5], "\n"), 128, []string{
			"Pwsh General Error | Run: python3", "Pwsh General Error | Run: python3m", "Pwsh General Error | Run: pamon", "Pwsh General Error | Run: python3.6",
			"Pwsh General Error | Run: rtmon", "Pwsh General Error | Run: echo", "Pwsh General Error | Run: pushd", "Pwsh General Error | Run: etsn",
			"Pwsh General Error | Run: pwsh", "Pwsh General Error | Run: pwconv",
		}},
		{"pwsh unix command not found", "not important", pwshOutput, 128, []string{
			"Pwsh Unix Command Not Found Error | Run: sudo apt install python3", "Pwsh Unix Command Not Found Error | Run: sudo apt install python",
			"Pwsh Unix Command Not Found Error | Run: sudo apt install python-minimal", "Pwsh Unix Command Not Found Error | Run: python3",
		}},
		{"pwsh output without a suggestion", "not important", "...\nnothing here", 128, nil},
		{"a command that worked has no fixes of the errors", "git sttatus", pwshOutput, 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := labelsOf(quickFixesFor(builtinFixes(), tc.command, tc.exit, linesOf(tc.output)))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("fixes:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestQuickFixDetails(t *testing.T) {
	prLines := linesOf("remote:      https://github.com/o/r/pull/new/b\nremote:\nremote:\nx\ny")
	fixes := quickFixesFor(builtinFixes(), "git push", 0, prLines)
	if len(fixes) != 1 || fixes[0].kind != fixOpen || fixes[0].uri != "https://github.com/o/r/pull/new/b" {
		t.Errorf("PR fix = %+v", fixes)
	}
	run := quickFixesFor(builtinFixes(), "git push", 128, linesOf("To push, use\n\n\tgit push --set-upstream origin feature/x"))
	if len(run) != 1 || run[0].kind != fixCommand || !run[0].execute || run[0].command != "git push --set-upstream origin feature/x" {
		t.Errorf("push fix = %+v", run)
	}
	port := quickFixesFor(builtinFixes(), "npm start", 1, linesOf("listen EADDRINUSE: address already in use :::5173"))
	if len(port) != 1 || port[0].kind != fixPort || port[0].port != "5173" || port[0].command != "npm start" {
		t.Errorf("port fix = %+v", port)
	}
	// the unix fix types its lines without running them; two fixes of one line are one
	dup := quickFixesFor([]fixOption{builtinFixes()[1], builtinFixes()[1]}, "git pull", 0, linesOf("and can be fast-forwarded"))
	if len(dup) != 1 {
		t.Errorf("duplicates = %+v", dup)
	}
}

func TestOutputMatchOf(t *testing.T) {
	lines := []string{"a", "b", "needle 1", "c", "needle 2", "d", "$ "}
	re := mustRe(t, `needle (?P<n>\d)`)
	cases := []struct {
		name string
		m    outputMatcher
		want string // the group "n", "" for no match
		read []string
	}{
		{"bottom, finds the nearest to the bottom", outputMatcher{re: re, anchor: "bottom", length: 10}, "2", []string{"a", "b", "needle 1", "c", "needle 2", "d", "$ "}},
		{"bottom, length cuts the search", outputMatcher{re: re, anchor: "bottom", length: 2}, "", nil},
		{"bottom, the lines read are the last length", outputMatcher{re: re, anchor: "bottom", length: 3}, "2", []string{"needle 2", "d", "$ "}},
		{"bottom, offset skips lines", outputMatcher{re: re, anchor: "bottom", offset: 2, length: 3}, "2", []string{"needle 1", "c", "needle 2"}},
		{"top, finds the first", outputMatcher{re: re, anchor: "top", length: 10}, "1", nil},
		{"top, offset and the last line is not read", outputMatcher{re: re, anchor: "top", offset: 3, length: 10}, "2", nil},
		{"top, the last line (the prompt) is never read", outputMatcher{re: mustRe(t, `\$`), anchor: "top", length: 10}, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := outputMatchOf(lines, tc.m)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("match = %+v, want none", got)
				}
				return
			}
			if got == nil || got.names["n"] != tc.want {
				t.Fatalf("match = %+v, want n = %s", got, tc.want)
			}
			if tc.read != nil && !reflect.DeepEqual(got.outputLines, tc.read) {
				t.Errorf("lines read = %q, want %q", got.outputLines, tc.read)
			}
		})
	}
}

func TestPortHolders(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port
	pids := portHolders("/proc", port)
	if len(pids) != 1 || pids[0] != os.Getpid() {
		t.Errorf("holders of %d = %v, want this process (%d)", port, pids, os.Getpid())
	}
	var killed []int
	n := killPort("/proc", port, func(pid int, sig syscall.Signal) error {
		killed = append(killed, pid)
		if sig != syscall.SIGTERM {
			t.Errorf("signal = %v, want SIGTERM", sig)
		}
		return nil
	})
	if n != 1 || len(killed) != 1 || killed[0] != os.Getpid() {
		t.Errorf("killPort = %d %v", n, killed)
	}
	if got := portHolders("/proc", 1); len(got) != 0 {
		t.Errorf("holders of port 1 = %v", got)
	}
	// a /proc that is not one
	if got := portHolders(filepath.Join(t.TempDir(), "none"), port); got != nil {
		t.Errorf("holders in a missing folder = %v", got)
	}
}

func mustRe(t *testing.T, expr string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(expr)
	if err != nil {
		t.Fatal(err)
	}
	return re
}
