// Command fakego is a stand-in for the go command, for the tests of the Go extension (SPEC §10.17, step 14.1): it answers `go
// env`, `go env GOVERSION` and `go install pkg@version` without touching the user's GOPATH, module cache or Go environment. It
// is built by the tests (extensions/golang/gotest) and never shipped.
//
// Environment: FAKEGO_LOG, when set, is a JSONL file the program appends one record to for each run (its arguments, working
// folder, process id and group, and the variables the extension sets for the tools). FAKEGO_VERSION is the answer to `go env
// GOVERSION` (go1.99.1 by default), and FAKEGO_ENVFAIL makes `go env` fail with that text and status 1. For `go install`,
// FAKEGO_FAIL is a comma separated list of texts: a package that contains one fails with status 1 and a message on the standard
// error; FAKEGO_HANG is the same list for a package that never ends (it writes its process id and the one of a child in its
// group to FAKEGO_PIDFILE, one number per line, and waits). For `go run`, FAKEGO_RUN_STDOUT is printed to the standard output;
// then, when FAKEGO_RUN_GATE is set, the program waits until that file exists; then FAKEGO_RUN_STDERR is printed to the standard
// error, and the program ends with the status FAKEGO_RUN_EXIT (0 by default); with FAKEGO_RUN_HANG set it hangs after the output
// as a hanging install does. A hard limit of 60 seconds ends the program whatever happens.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	time.AfterFunc(60*time.Second, func() { os.Exit(3) })
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "__child" {
		time.Sleep(60 * time.Second)
		return
	}
	record(args)
	switch {
	case len(args) >= 1 && args[0] == "env":
		env(args[1:])
	case len(args) >= 2 && args[0] == "install":
		install(args[1:])
	case len(args) >= 1 && args[0] == "run":
		goRun()
	default:
		fmt.Fprintf(os.Stderr, "fakego: unsupported command %q\n", strings.Join(args, " "))
		os.Exit(2)
	}
}

// record appends the run to FAKEGO_LOG.
func record(args []string) {
	path := os.Getenv("FAKEGO_LOG")
	if path == "" {
		return
	}
	dir, _ := os.Getwd()
	vars := map[string]string{}
	for _, name := range []string{"GOPATH", "GOFLAGS", "GO111MODULE", "GOTOOLCHAIN", "GOBIN", "FAKE_TOOLS_VAR", "FAKEGO_DELAY_MS"} {
		if v, ok := os.LookupEnv(name); ok {
			vars[name] = v
		}
	}
	line, err := json.Marshal(map[string]any{"args": args, "dir": dir, "pid": os.Getpid(), "pgid": syscall.Getpgrp(), "env": vars})
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

func version() string {
	if v := os.Getenv("FAKEGO_VERSION"); v != "" {
		return v
	}
	return "go1.99.1"
}

func env(names []string) {
	if ms, err := strconv.Atoi(os.Getenv("FAKEGO_DELAY_MS")); err == nil && ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	if msg := os.Getenv("FAKEGO_ENVFAIL"); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(1)
	}
	if len(names) == 1 && names[0] == "GOVERSION" {
		fmt.Println(version())
		return
	}
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = "/fake/go"
	}
	for _, kv := range [][2]string{
		{"GOARCH", "amd64"}, {"GOEXE", ""}, {"GOOS", "linux"}, {"GOPATH", gopath}, {"GOROOT", "/fake/goroot"},
		{"GOTOOLCHAIN", firstNonEmpty(os.Getenv("GOTOOLCHAIN"), "auto")}, {"GOVERSION", version()},
	} {
		fmt.Printf("%s='%s'\n", kv[0], kv[1])
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func matches(list, pkg string) bool {
	for _, part := range strings.Split(list, ",") {
		if part != "" && strings.Contains(pkg, part) {
			return true
		}
	}
	return false
}

func install(pkgs []string) {
	for _, pkg := range pkgs {
		fmt.Printf("go: downloading %s\n", pkg)
		switch {
		case matches(os.Getenv("FAKEGO_FAIL"), pkg):
			fmt.Fprintf(os.Stderr, "go: %s: module lookup disabled by GOPROXY=off\n", pkg)
			os.Exit(1)
		case matches(os.Getenv("FAKEGO_HANG"), pkg):
			hang()
		}
		fmt.Printf("go: installed %s\n", pkg)
	}
}

// goRun is `go run`: the output, the gate, the end that the environment says.
func goRun() {
	if out := os.Getenv("FAKEGO_RUN_STDOUT"); out != "" {
		fmt.Println(out)
	}
	if gate := os.Getenv("FAKEGO_RUN_GATE"); gate != "" {
		for {
			if _, err := os.Stat(gate); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if os.Getenv("FAKEGO_RUN_HANG") != "" {
		hang()
	}
	if out := os.Getenv("FAKEGO_RUN_STDERR"); out != "" {
		fmt.Fprintln(os.Stderr, out)
	}
	if code, err := strconv.Atoi(os.Getenv("FAKEGO_RUN_EXIT")); err == nil {
		os.Exit(code)
	}
}

// hang starts a child in the process group, writes the two process ids and waits until it is killed.
func hang() {
	child := exec.Command(os.Args[0], "__child")
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "fakego: the child did not start:", err)
		os.Exit(1)
	}
	if path := os.Getenv("FAKEGO_PIDFILE"); path != "" {
		text := strconv.Itoa(os.Getpid()) + "\n" + strconv.Itoa(child.Process.Pid) + "\n"
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "fakego: the pid file was not written:", err)
			os.Exit(1)
		}
	}
	select {}
}
