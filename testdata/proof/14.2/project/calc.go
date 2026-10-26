package calc

import "errors"

type Calculator struct {
	Precision int
}

// Divide divides a by b.
func (c *Calculator) Divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	return a / b, nil
}
