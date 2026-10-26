// Command fakelint is a stand-in for a Go lint tool (staticcheck, golangci-lint, revive), for the tests of the Go extension (SPEC
// §10.17, step 14.3): it is built under the name of the tool (extensions/golang/gotest) and prints what the environment says, so no
// real linter is needed and none is installed.
//
// Environment: FAKELINT_LOG, when set, is a JSONL file the program appends a record to (its name, arguments and working folder);
// FAKELINT_OUT is printed to the standard output and FAKELINT_EXIT is the status it ends with (1 when it printed something, else
// 0); FAKELINT_GATE is a file it waits for before it prints; FAKELINT_HANG makes it hang after it started, with a child in its
// process group. A hard limit of 60 seconds ends the program whatever happens.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func main() {
	time.AfterFunc(60*time.Second, func() { os.Exit(3) })
	if len(os.Args) > 1 && os.Args[1] == "__child" {
		time.Sleep(60 * time.Second)
		return
	}
	record()
	if os.Getenv("FAKELINT_HANG") != "" {
		child := exec.Command(os.Args[0], "__child")
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "fakelint:", err)
			os.Exit(2)
		}
		select {}
	}
	for gate := os.Getenv("FAKELINT_GATE"); gate != ""; {
		if _, err := os.Stat(gate); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	out := os.Getenv("FAKELINT_OUT")
	if out != "" {
		fmt.Println(out)
	}
	if code, err := strconv.Atoi(os.Getenv("FAKELINT_EXIT")); err == nil {
		os.Exit(code)
	}
	if out != "" {
		os.Exit(1)
	}
}

func record() {
	path := os.Getenv("FAKELINT_LOG")
	if path == "" {
		return
	}
	dir, _ := os.Getwd()
	line, err := json.Marshal(map[string]any{"tool": filepath.Base(os.Args[0]), "args": os.Args[1:], "dir": dir, "pid": os.Getpid(), "pgid": syscall.Getpgrp()})
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(line, '\n'))
}
