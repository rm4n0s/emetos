package resultgrid

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// exportCheckEvery is how many rows an export writes between two looks at its context.
const exportCheckEvery = 512

// Export is the result as it is shown (after the sort), ready to be written. It holds what it needs and does not follow the model, so
// it may be written from a goroutine while the grid goes on; the source must not change the rows it already has (Table does not).
type Export struct {
	src   Source
	cols  []Column
	order []int32
	n     int
}

// Export takes the result as it is shown now: its rows in the order shown. Call it on the UI thread.
func (m *Model) Export() *Export {
	return &Export{src: m.src, cols: m.cols, order: m.order, n: m.Len()}
}

// Rows is the number of rows the export writes.
func (e *Export) Rows() int { return e.n }

// row reads the row r as shown.
func (e *Export) row(r int, dst []Value) {
	clear(dst)
	i := r
	if e.order != nil {
		i = int(e.order[r])
	}
	e.src.Row(i, dst)
}

// CSVOptions configure WriteCSV.
type CSVOptions struct {
	// NoHeader leaves out the line of the titles of the columns.
	NoHeader bool
	// NullText is what NULL is written as; nothing by default, which a spreadsheet reads as an empty cell.
	NullText string
	// Comma is the separator of the cells; a comma by default.
	Comma rune
}

// WriteCSV writes the result as CSV (RFC 4180): the titles of the columns, then a line for each row, a cell with a comma, a quote
// or a line break in double quotes. Times are in ISO 8601 and binary data in hex ("0x..."). It stops when ctx ends. The errors have
// the tags ResultExportCancelled and ResultExportWriteFailed.
func (e *Export) WriteCSV(ctx context.Context, w io.Writer, opts CSVOptions) error {
	cw := csv.NewWriter(w)
	if opts.Comma != 0 {
		cw.Comma = opts.Comma
	}
	record := make([]string, len(e.cols))
	if !opts.NoHeader {
		for c, col := range e.cols {
			record[c] = col.Name
		}
		if err := cw.Write(record); err != nil {
			return errors.NewErr("ResultExportWriteFailed", err).SetMetadata("format", "csv")
		}
	}
	buf := make([]Value, len(e.cols))
	for r := range e.n {
		if r%exportCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return errors.NewErr("ResultExportCancelled", err).SetMetadata("format", "csv", "rows", r)
			}
		}
		e.row(r, buf)
		for c, v := range buf {
			if v.IsNull() {
				record[c] = opts.NullText
			} else {
				record[c] = v.Full("")
			}
		}
		if err := cw.Write(record); err != nil {
			return errors.NewErr("ResultExportWriteFailed", err).SetMetadata("format", "csv", "rows", r)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return errors.NewErr("ResultExportWriteFailed", err).SetMetadata("format", "csv")
	}
	return nil
}

// jsonKeys are the names of the columns as the keys of the objects: a name used twice gets _2, _3 and so on.
func jsonKeys(cols []Column) []string {
	seen := map[string]int{}
	keys := make([]string, len(cols))
	for i, c := range cols {
		name := c.Name
		seen[name]++
		if n := seen[name]; n > 1 {
			name = c.Name + "_" + strconv.Itoa(n)
			seen[name]++
		}
		keys[i] = name
	}
	return keys
}

// jsonString writes a string as a JSON string; the characters < > & stay as they are.
func jsonString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil { // a string always encodes
		return `""`
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// jsonValue writes a value as JSON: NULL as null, numbers and booleans as such (NaN and the infinities as strings, as JSON has no
// number for them), a JSON document as itself when it is valid, times in ISO 8601 and binary data as "0x..." text.
func jsonValue(v Value) string {
	switch v.Kind {
	case KindNull:
		return "null"
	case KindInt:
		return strconv.FormatInt(v.I, 10)
	case KindFloat:
		if math.IsNaN(v.F) || math.IsInf(v.F, 0) {
			return jsonString(formatFloat(v.F))
		}
		return strconv.FormatFloat(v.F, 'g', -1, 64)
	case KindBool:
		if v.I != 0 {
			return "true"
		}
		return "false"
	case KindJSON:
		if json.Valid([]byte(v.S)) {
			return strings.TrimSpace(v.S)
		}
		return jsonString(v.S)
	case KindTime:
		return jsonString(v.T.Format(time.RFC3339Nano))
	}
	return jsonString(v.Full(""))
}

// WriteJSON writes the result as a JSON array with an object for each row, in the order shown, with the titles of the columns as
// keys (a title used twice gets _2, _3...). It stops when ctx ends. The errors have the tags ResultExportCancelled and
// ResultExportWriteFailed.
func (e *Export) WriteJSON(ctx context.Context, w io.Writer) error {
	bw := bufio.NewWriter(w)
	keys := make([]string, len(e.cols))
	for i, k := range jsonKeys(e.cols) {
		keys[i] = jsonString(k)
	}
	write := func(s string) error {
		if _, err := bw.WriteString(s); err != nil {
			return errors.NewErr("ResultExportWriteFailed", err).SetMetadata("format", "json")
		}
		return nil
	}
	if err := write("["); err != nil {
		return err
	}
	buf := make([]Value, len(e.cols))
	for r := range e.n {
		if r%exportCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return errors.NewErr("ResultExportCancelled", err).SetMetadata("format", "json", "rows", r)
			}
		}
		e.row(r, buf)
		sep := ",\n  {"
		if r == 0 {
			sep = "\n  {"
		}
		if err := write(sep); err != nil {
			return err
		}
		for c, v := range buf {
			item := keys[c] + ": " + jsonValue(v)
			if c > 0 {
				item = ", " + item
			}
			if err := write(item); err != nil {
				return err
			}
		}
		if err := write("}"); err != nil {
			return err
		}
	}
	end := "]\n"
	if e.n > 0 {
		end = "\n]\n"
	}
	if err := write(end); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return errors.NewErr("ResultExportWriteFailed", err).SetMetadata("format", "json")
	}
	return nil
}
