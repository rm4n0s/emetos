// Package shop prices a cart.
package shop

// Total adds up the prices of the items.
func Total(prices []int) int {
	sum := 0
	for _, p := range prices {
		sum += p
	}
	return sum
}

// Discount takes a percentage off a total.
func Discount(total, percent int) int {
	if percent <= 0 {
		return total
	}
	if percent > 100 {
		return 0
	}
	return total - total*percent/100
}

// Label names a total for the receipt.
func Label(total int) string {
	if total == 0 {
		return "empty"
	}
	return "total"
}
