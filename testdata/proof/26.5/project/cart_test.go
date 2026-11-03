package shop

import "testing"

func TestTotal(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if got := Total(nil); got != 0 {
			t.Errorf("Total(nil) = %d, want 0", got)
		}
	})
	for _, tc := range []struct {
		name   string
		prices []int
		want   int
	}{{"one item", []int{5}, 5}, {"three items", []int{1, 2, 3}, 6}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Total(tc.prices); got != tc.want {
				t.Errorf("Total(%v) = %d, want %d", tc.prices, got, tc.want)
			}
		})
	}
}

func TestDiscount(t *testing.T) {
	if got := Discount(100, 0); got != 100 {
		t.Errorf("Discount(100, 0) = %d, want 100", got)
	}
	// 15% of 200 is 30: this expectation is wrong on purpose
	if got := Discount(200, 15); got != 175 {
		t.Errorf("Discount(200, 15) = %d, want 175", got)
	}
}

func TestLabel(t *testing.T) {
	if got := Label(10); got != "total" {
		t.Errorf("Label(10) = %q, want total", got)
	}
}
