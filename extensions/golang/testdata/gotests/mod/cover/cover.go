// Package cover has a function that its test covers in part.
package cover

// Sign says whether the number is negative, zero or positive.
func Sign(n int) string {
	if n < 0 {
		return "neg"
	}
	if n == 0 {
		return "zero"
	}
	return "pos"
}

// Unused is never called by a test.
func Unused() int {
	return 1
}
