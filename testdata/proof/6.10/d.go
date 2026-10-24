package main

import "fmt"

// Counter counts up from zero.
type Counter struct {
	n int
}

// Next gives the next number.
func (c *Counter) Next() int {
	c.n++
	return c.n
}

func demo() {
	var c Counter
	for range 3 {
		fmt.Println(c.Next())
	}
}
