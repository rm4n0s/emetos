package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// signalHelperEnv makes the test binary run as the helper process of TestSignalContext instead of the tests.
const signalHelperEnv = "EMETOS_SIGNAL_HELPER"

// init runs the helper process of TestSignalContext when the test binary is started as one. The display tests have a TestMain
// of their own, so the helper starts from an init function.
func init() {
	if os.Getenv(signalHelperEnv) == "" {
		return
	}
	ctx, stop := signalContext(context.Background())
	defer stop()
	fmt.Println("ready")
	<-ctx.Done()
	time.Sleep(200 * time.Millisecond) // the handler is taken away by the goroutine of signalContext meanwhile
	fmt.Println("first")
	// the application that is stuck while it ends: only a second signal can end the process
	time.Sleep(time.Minute)
	os.Exit(3)
}

func TestSignalContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
	}{
		{"SIGTERM", syscall.SIGTERM},
		{"SIGINT", syscall.SIGINT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), signalHelperEnv+"=1")
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			lines := bufio.NewScanner(out)
			want := func(text string) {
				t.Helper()
				if !lines.Scan() || lines.Text() != text {
					t.Fatalf("the helper said %q (%v), want %q", lines.Text(), lines.Err(), text)
				}
			}
			want("ready")
			if err := cmd.Process.Signal(tc.sig); err != nil {
				t.Fatal(err)
			}
			want("first") // the first signal ends the context, not the process
			if err := cmd.Process.Signal(tc.sig); err != nil {
				t.Fatal(err)
			}
			err = cmd.Wait()
			ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if err == nil || !ok || !ws.Signaled() || ws.Signal() != tc.sig {
				t.Errorf("the second signal ended the helper with %v (%v), want it killed by %v", err, cmd.ProcessState, tc.sig)
			}
		})
	}
}
