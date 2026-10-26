// Command lsptest-fakeserver is the fake language server as a program (SPEC §9.12 "Tests"): it speaks the
// protocol on its standard input and output, behaves as the script given with -script says, and exits with
// status 0 after shutdown and exit, 1 after exit without shutdown, 3 when it is told to crash, and 2 when
// it cannot start. The harness of package lsptest builds it and starts it in its own process group.
package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

// stdio joins the standard input and output into the stream the server serves.
type stdio struct {
	io.Reader
	io.Writer
}

func main() { os.Exit(run()) }

func run() int {
	scriptPath := flag.String("script", "", "the JSON script the server follows")
	flag.Parse()
	script := lsptest.Script{}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if *scriptPath != "" {
		s, err := lsptest.LoadScript(*scriptPath)
		if err != nil {
			log.Error("cannot load the script", "err", err)
			return 2
		}
		script = s
	}
	srv := lsptest.NewServer(script, func() { os.Exit(3) })
	if err := srv.Serve(stdio{os.Stdin, os.Stdout}); err != nil {
		log.Error("the fake server stopped", "err", err)
		return 2
	}
	if !srv.Shutdown() && srv.Count("exit") > 0 {
		fmt.Fprintln(os.Stderr, "exit without shutdown")
		return 1
	}
	return 0
}
