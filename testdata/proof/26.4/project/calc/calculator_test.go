package calc

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Errorf("Add(2, 3) = %d, want 5", got)
	}
}

func TestSubtract(t *testing.T) {
	if got := Subtract(5, 3); got != 2 {
		t.Errorf("Subtract(5, 3) = %d, want 2", got)
	}
}

func TestDivide(t *testing.T) {
	if _, err := Divide(1, 0); err == nil {
		t.Error("Divide(1, 0) should fail")
	}
}

func TestClamp(t *testing.T) {
	if got := Clamp(-1, 0, 10); got != 0 {
		t.Errorf("Clamp(-1, 0, 10) = %d, want 0", got)
	}
}
