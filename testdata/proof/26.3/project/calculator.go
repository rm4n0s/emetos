package calc

import "fmt"

// Add returns the sum of a and b.
func Add(a, b int) int { return a + b }

// Subtract returns a minus b.
func Subtract(a, b int) int { return a - b }

// Divide returns a divided by b, and an error for a zero b.
func Divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, fmt.Errorf("divide %v by zero", a)
	}
	return a / b, nil
}

// FormatReport writes the total, the tax and the amount due.
func FormatReport(total, rate float64) string {
	tax := total * rate
	return fmt.Sprintf("Total: %.0f\nTax: %.2f\nDue: %.2f", total, tax, total+tax)
}
