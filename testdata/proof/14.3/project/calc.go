// Package proof is a tiny package for the proof of step 14.3: one test passes and one fails.
package proof

import "errors"

// Add returns the sum of a and b.
func Add(a, b int) int { return a + b }

// Divide returns a divided by b; dividing by zero is an error.
func Divide(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("divide by zero")
	}
	return a / b, nil
}
