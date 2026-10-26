package proof

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Errorf("Add(2, 3) = %d, want 5", got)
	}
}

func TestDivide(t *testing.T) {
	got, err := Divide(7, 2)
	if err != nil {
		t.Fatal(err)
	}
	// 7 / 2 is 3 in integer arithmetic: this expectation is wrong on purpose
	if got != 4 {
		t.Errorf("Divide(7, 2) = %d, want 4", got)
	}
}

func TestDivideByZero(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		if _, err := Divide(1, 0); err == nil {
			t.Error("no error for a zero divisor")
		}
	})
}

func BenchmarkAdd(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Add(i, i)
	}
}
