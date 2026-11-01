// Command daptest-fakeadapter is the fake debug adapter as a program (SPEC §10.31 "Tests"): it follows the script given with
// -script, over its standard input and output, or over a TCP port or a Unix socket it listens on. The harness of package daptest
// builds it and starts it in its own process group.
//
// Flags:
//
//	-script file            the JSON lines script (see daptest.ParseScript)
//	-listen none|tcp|unix   none (the default) speaks on standard input and output; tcp listens on 127.0.0.1 and a port the
//	                        system picks; unix listens on the socket -unix names. Either serves one connection and exits.
//	-unix path              the Unix socket
//	-announce               print a line with the port before accepting (tcp)
//	-announce-format fmt    the line, with %d for the port (default "DAP server listening at: 127.0.0.1:%d")
//	-exit-before-listen N   exit with status N before listening (N >= 0)
//
// The exit status is that of the script's exit or crash step, 0 otherwise, and 2 when the program cannot start.
package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
)

// stdio joins the standard input and output into the stream the adapter serves.
type stdio struct {
	io.Reader
	io.Writer
}

func (stdio) Close() error { return nil }

func main() { os.Exit(run()) }

func run() int {
	scriptPath := flag.String("script", "", "the JSON lines script the adapter follows")
	listen := flag.String("listen", "none", "none, tcp or unix")
	unixPath := flag.String("unix", "", "the Unix socket to listen on")
	announce := flag.Bool("announce", false, "print the port before accepting")
	format := flag.String("announce-format", "DAP server listening at: 127.0.0.1:%d", "the line to print, with %d for the port")
	exitBefore := flag.Int("exit-before-listen", -1, "exit with this status before listening")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if *exitBefore >= 0 {
		return *exitBefore
	}
	var script daptest.Script
	if *scriptPath != "" {
		s, err := daptest.LoadScript(*scriptPath)
		if err != nil {
			log.Error("cannot load the script", "err", err)
			return 2
		}
		script = s
	}
	srv := daptest.NewServer(script, daptest.ServerOptions{Crash: func(code int) { os.Exit(code) }, Stderr: os.Stderr})
	var stream io.ReadWriteCloser = stdio{os.Stdin, os.Stdout}
	switch *listen {
	case "none":
	case "tcp", "unix":
		var l net.Listener
		var err error
		if *listen == "tcp" {
			l, err = net.Listen("tcp", "127.0.0.1:0")
		} else {
			l, err = net.Listen("unix", *unixPath)
		}
		if err != nil {
			log.Error("cannot listen", "err", err)
			return 2
		}
		if *announce && *listen == "tcp" {
			port := l.Addr().(*net.TCPAddr).Port
			fmt.Printf(*format+"\n", port)
		}
		conn, err := l.Accept()
		if cerr := l.Close(); cerr != nil {
			log.Error("cannot close the listener", "err", cerr)
		}
		if err != nil {
			log.Error("cannot accept", "err", err)
			return 2
		}
		stream = conn
	default:
		log.Error("unknown -listen", "value", *listen)
		return 2
	}
	if err := srv.Serve(stream); err != nil {
		log.Error("the fake adapter stopped", "err", err)
		return 2
	}
	for _, v := range srv.Violations() {
		fmt.Fprintln(os.Stderr, "violation:", v)
	}
	return srv.ExitCode()
}
