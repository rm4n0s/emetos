package language

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// threadCPU is the CPU time the calling thread has used, in user and system mode; the caller locks its goroutine to the thread.
func threadCPU(t *testing.T) time.Duration {
	t.Helper()
	var ru unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_THREAD, &ru); err != nil {
		t.Fatal(err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
