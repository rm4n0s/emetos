package language

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// threadCPU is the CPU time the calling thread has used (macOS has no RUSAGE_THREAD; its thread clock says the same); the caller
// locks its goroutine to the thread.
func threadCPU(t *testing.T) time.Duration {
	t.Helper()
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &ts); err != nil {
		t.Fatal(err)
	}
	return time.Duration(ts.Nano())
}
