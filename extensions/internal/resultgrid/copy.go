package resultgrid

import "strings"

// tsvField writes a value for a TSV text: as it is, or in double quotes (doubled inside) when it has a tab, a line break or a
// quote, the way a spreadsheet writes and reads it.
func tsvField(s string) string {
	if !strings.ContainsAny(s, "\t\n\r\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// text is the whole text of the value at a place of a row that was read.
func (m *Model) text(v Value) string { return v.Full(m.opts.NullText) }

// CopyCell is the whole text of one cell, not cut and not quoted.
func (m *Model) CopyCell(r, c int) string {
	if !m.cellOK(r, c) {
		return ""
	}
	return m.text(m.Value(r, c))
}

// copyRect writes the cells of the rectangle as TSV: tabs between the cells of a row, a line break between rows, with the titles of
// the columns first when header is set. A rectangle of one cell is its text as it is.
func (m *Model) copyRect(r0, c0, r1, c1 int, header bool) string {
	if r0 == r1 && c0 == c1 && !header {
		return m.text(m.Value(r0, c0))
	}
	var b strings.Builder
	if header {
		for c := c0; c <= c1; c++ {
			if c > c0 {
				b.WriteByte('\t')
			}
			b.WriteString(tsvField(m.cols[c].Name))
		}
		b.WriteByte('\n')
	}
	buf := make([]Value, len(m.cols))
	for r := r0; r <= r1; r++ {
		m.Row(r, buf)
		for c := c0; c <= c1; c++ {
			if c > c0 {
				b.WriteByte('\t')
			}
			b.WriteString(tsvField(m.text(buf[c])))
		}
		if r < r1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// CopyRow is the cells of a row as TSV, with the titles of the columns first when header is set.
func (m *Model) CopyRow(r int, header bool) string {
	if r < 0 || r >= m.Len() || len(m.cols) == 0 {
		return ""
	}
	return m.copyRect(r, 0, r, len(m.cols)-1, header)
}

// CopyColumn is the values of a column, one for each row in the order shown, with its title first when header is set.
func (m *Model) CopyColumn(c int, header bool) string {
	if c < 0 || c >= len(m.cols) || m.Len() == 0 {
		return ""
	}
	if m.Len() == 1 && !header {
		return m.text(m.Value(0, c))
	}
	return m.copyRect(0, c, m.Len()-1, c, header)
}

// CopySelection is the selected cells as TSV; with header the titles of the columns are first. It is empty when nothing is selected.
func (m *Model) CopySelection(header bool) string {
	r0, c0, r1, c1, ok := m.sel.Bounds(m.Len(), len(m.cols))
	if !ok {
		return ""
	}
	return m.copyRect(r0, c0, r1, c1, header)
}

// CopyAll is the whole result as TSV.
func (m *Model) CopyAll(header bool) string {
	if m.Len() == 0 || len(m.cols) == 0 {
		return ""
	}
	return m.copyRect(0, 0, m.Len()-1, len(m.cols)-1, header)
}
