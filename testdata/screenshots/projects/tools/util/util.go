// Package util has the helpers of the program.
package util

// Name is who the program greets.
func Name() string {
	// FIXME(ann): this should come from the environment
	return "world"
}

// Double returns twice n.
func Double(n int) int {
	return n * 2
	// TODO: handle overflow
	return n + n
}
