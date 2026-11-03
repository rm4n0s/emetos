package report

import (
	"fmt"
	"strings"
)

// FormatReport writes the total, the tax and the amount due.
func FormatReport(total, rate float64) string {
	tax := total * rate
	return fmt.Sprintf("Total: %.0f\nTax: %.2f\nDue: %.2f", total, tax, total+tax)
}

// Percent writes a share as a percentage.
func Percent(part, whole float64) string {
	if whole == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", part/whole*100)
}

// Summary joins the lines of a report.
func Summary(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}
