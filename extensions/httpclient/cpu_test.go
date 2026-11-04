package httpclient

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// threadCPU is the processor time the calling OS thread has used. The frame-budget tests lock the test goroutine (the UI thread) to its
// thread and measure this instead of wall time, so that a loaded machine does not count as a slow frame.
func threadCPU(t testing.TB) time.Duration {
	t.Helper()
	var ru unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_THREAD, &ru); err != nil {
		t.Fatalf("Getrusage: %v", err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
