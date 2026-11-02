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
	got, err := Divide(5, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2.5 {
		t.Errorf("Divide(5, 2) = %v, want 2.5", got)
	}
}

func TestFormatReport(t *testing.T) {
	got := FormatReport(12, 0.2)
	want := "Total: 12\nTax: 2.40\nDue: 14.40"
	if got != want {
		t.Errorf("FormatReport(12, 0.2) = %q", got)
	}
}

func TestSquareRoot(t *testing.T) {
	t.Skip("not implemented yet")
}
