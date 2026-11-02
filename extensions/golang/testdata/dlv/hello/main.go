// Command hello is the program the tests of the Go debugger run under dlv (build tag emetos_dlv): it has a few variables, goroutines
// and a function to step over. Its lines are the ones the tests set breakpoints on: keep them in step with debug_dlv_test.go.
package main

import (
	"fmt"
	"os"
	"strconv"
	"sync"
)

// point is a struct to show in the Variables view.
type point struct{ X, Y int }

// compute adds the squares of 1 to n.
func compute(n int) int {
	total := 0
	for i := 1; i <= n; i++ {
		total += i * i
	}
	return total
}

func main() {
	n := 5
	if len(os.Args) > 1 {
		n, _ = strconv.Atoi(os.Args[1])
	}
	p := point{X: n, Y: n * 2}
	done := make(chan struct{})
	go func() { <-done }() // a goroutine that waits, for the list of goroutines
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = compute(2)
	}()
	wg.Wait()
	result := compute(p.X)
	fmt.Println("result", result)
	close(done)
}
