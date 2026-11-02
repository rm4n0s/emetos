// Command attach is the program the attach test of the Go debugger debugs (build tag emetos_dlv): it prints its process id, lets any
// process trace it (the Yama restriction of the kernel would let only its parent), and calls work again and again until it is killed.
package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// prSetPtracer is PR_SET_PTRACER of prctl(2); the argument -1 is PR_SET_PTRACER_ANY.
const prSetPtracer = 0x59616d61

func work(round int) int {
	return round * 2
}

func main() {
	// a kernel without this option, or one that forbids tracing, makes dlv's attach fail, and the test says so
	_, _, _ = syscall.RawSyscall(syscall.SYS_PRCTL, prSetPtracer, ^uintptr(0), 0)
	fmt.Println("pid", os.Getpid())
	deadline := time.Now().Add(3 * time.Minute)
	for round := 0; time.Now().Before(deadline); round++ {
		_ = work(round)
		time.Sleep(20 * time.Millisecond)
	}
}
