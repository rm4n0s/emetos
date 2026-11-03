package report

import "testing"

func TestFormatReport(t *testing.T) {
	want := "Total: 12\nTax: 2.40\nDue: 14.40"
	if got := FormatReport(12, 0.2); got != want {
		t.Errorf("FormatReport(12, 0.2) = %q, want %q", got, want)
	}
}
