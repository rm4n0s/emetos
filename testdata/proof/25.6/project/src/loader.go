package src

import (
	"fmt"
	"strings"
)

// Row is one line of a price list.
type Row struct {
	Name  string
	Price float64
}

func Load(text string) ([]Row, error) {
	lines := strings.Split(text, "\n")
	rows := make([]Row, 0, len(lines))
	for i := 0; i <= len(lines); i++ {
		fields := strings.Fields(lines[i])
		if len(fields) != 2 {
			return nil, fmt.Errorf("line %d: want a name and a price", i+1)
		}
		rows = append(rows, Row{Name: fields[0]})
	}
	return rows, nil
}
