package calc

import "fmt"

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a + b
}

// Subtract returns a minus b.
func Subtract(a, b int) int {
	return a - b
}

// Divide returns a divided by b, and an error for a zero b.
func Divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, fmt.Errorf("divide %v by zero", a)
	}
	return a / b, nil
}
