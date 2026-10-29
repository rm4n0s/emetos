package containerapitest

import (
	"strconv"
	"strings"
)

// scriptOutput is what the little shell of the fake makes of a command: the lines a container prints when it starts, the
// exit status, and whether it goes on printing "tick" every moment (a script with "while true").
type scriptOutput struct {
	lines []scriptLine
	exit  int
	ticks bool
}

// scriptLine is one line of output.
type scriptLine struct {
	stderr bool
	text   string
}

// runScript interprets a command the way the tests use containers: ["sh", "-c", "echo out; echo err >&2; exit 3"] runs the
// echo, sleep, exit and "while true" parts of the script; ["echo", "hi"], ["true"] and ["false"] are known; anything else
// fails with status 127 and a message on stderr. It is not a shell: the fake needs only enough to make output and exit
// codes that a test can check.
func runScript(cmd []string) scriptOutput {
	if len(cmd) == 0 {
		return scriptOutput{}
	}
	switch cmd[0] {
	case "sh", "/bin/sh", "bash", "/bin/bash":
		if len(cmd) >= 3 && cmd[1] == "-c" {
			return runSh(cmd[2])
		}
		return scriptOutput{}
	case "echo":
		return scriptOutput{lines: []scriptLine{{text: strings.Join(cmd[1:], " ")}}}
	case "true", "sleep":
		return scriptOutput{}
	case "false":
		return scriptOutput{exit: 1}
	}
	return scriptOutput{exit: 127, lines: []scriptLine{{stderr: true, text: cmd[0] + ": not found"}}}
}

// runSh interprets a script of parts separated by ";".
func runSh(script string) scriptOutput {
	var out scriptOutput
	for _, part := range strings.Split(script, ";") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "echo "):
			text := strings.TrimSpace(strings.TrimPrefix(part, "echo "))
			stderr := false
			if strings.HasSuffix(text, ">&2") {
				stderr = true
				text = strings.TrimSpace(strings.TrimSuffix(text, ">&2"))
			}
			text = strings.Trim(text, `"'`)
			out.lines = append(out.lines, scriptLine{stderr: stderr, text: text})
		case strings.HasPrefix(part, "exit "):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(part, "exit "))); err == nil {
				out.exit = n
			}
			return out
		case strings.HasPrefix(part, "while true"):
			out.ticks = true
		}
	}
	return out
}
