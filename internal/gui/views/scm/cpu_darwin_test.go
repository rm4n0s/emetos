package scm

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// threadCPU is the processor time the calling OS thread has used (macOS has no RUSAGE_THREAD; its thread clock says the same).
// The frame-budget tests lock the test goroutine (the UI thread) to its thread and measure this instead of wall time.
func threadCPU(t testing.TB) time.Duration {
	t.Helper()
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &ts); err != nil {
		t.Fatalf("ClockGettime: %v", err)
	}
	return time.Duration(ts.Nano())
}
