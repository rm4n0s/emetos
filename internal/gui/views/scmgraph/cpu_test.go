package scmgraph

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// threadCPU is the processor time the calling thread has used. A budget of a frame is the work of the UI thread, so a test of it
// counts that and not the wall clock, which a busy machine stretches (the caller holds its thread with runtime.LockOSThread).
func threadCPU(t testing.TB) time.Duration {
	t.Helper()
	var ru unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_THREAD, &ru); err != nil {
		t.Fatalf("Getrusage: %v", err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
