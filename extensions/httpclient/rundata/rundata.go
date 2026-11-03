package rundata

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// The limits of a data file: a larger one is refused, not cut.
const (
	// MaxBytes is the most a data file may hold.
	MaxBytes = 16 << 20
	// MaxRows is the most rows (iterations) a data file may have.
	MaxRows = 10000
)

// Table is the content of a data file.
type Table struct {
	// Columns are the names of the variables, in the order of the file (a JSON file: sorted by name).
	Columns []string
	// Rows have one entry for each column the row has a value for (a JSON object that lacks a key lacks the entry).
	Rows []map[string]string
}

// Load reads the data file at path; its extension (.csv or .json) tells the format. It fails with HttpRunDataRead, HttpRunDataTooLarge,
// HttpRunDataFormat and the errors of Parse.
func Load(ctx context.Context, path string) (Table, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv", ".json":
	default:
		return Table{}, errors.New("HttpRunDataFormat", "a data file is a .csv file with a header row or a .json file with an array of objects", "path", path)
	}
	if err := ctx.Err(); err != nil {
		return Table{}, errors.NewErr("HttpRunDataRead", err).SetMetadata("path", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return Table{}, errors.NewErr("HttpRunDataRead", err).SetMetadata("path", path)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return Table{}, errors.NewErr("HttpRunDataRead", err).SetMetadata("path", path)
	}
	if len(data) > MaxBytes {
		return Table{}, errors.New("HttpRunDataTooLarge", "a data file may hold at most 16 MiB", "path", path)
	}
	return Parse(filepath.Base(path), data)
}

// Parse reads the text of a data file named name (its extension tells the format). It fails with HttpRunDataFormat and the errors of
// ParseCSV and ParseJSON.
func Parse(name string, data []byte) (Table, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".csv":
		return ParseCSV(data)
	case ".json":
		return ParseJSON(data)
	}
	return Table{}, errors.New("HttpRunDataFormat", "a data file is a .csv file with a header row or a .json file with an array of objects", "name", name)
}

// ParseCSV reads a CSV file: the first row names the columns, each other row is an iteration. Fields may be quoted (with "" for a quote,
// and holding commas and line breaks), and a byte order mark at the start is ignored. A row with another number of fields than the header
// is an error. It fails with HttpRunDataCSVInvalid, HttpRunDataNoHeader, HttpRunDataColumnInvalid, HttpRunDataNoRows and
// HttpRunDataTooManyRows.
func ParseCSV(data []byte) (Table, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = 0 // the first row decides
	head, err := r.Read()
	if errors.Is(err, io.EOF) {
		return Table{}, errors.New("HttpRunDataNoHeader", "the CSV file is empty: its first row must name the columns")
	}
	if err != nil {
		return Table{}, errors.NewErr("HttpRunDataCSVInvalid", err).SetMetadata("line", csvLine(err))
	}
	cols := make([]string, len(head))
	seen := map[string]bool{}
	for i, h := range head {
		h = strings.TrimSpace(h)
		switch {
		case h == "":
			return Table{}, errors.New("HttpRunDataColumnInvalid", "a column of the header row has no name", "column", i+1)
		case seen[h]:
			return Table{}, errors.New("HttpRunDataColumnInvalid", "a column name is in the header row twice", "column", i+1, "name", h)
		}
		seen[h] = true
		cols[i] = h
	}
	t := Table{Columns: cols}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Table{}, errors.NewErr("HttpRunDataCSVInvalid", err).SetMetadata("line", csvLine(err))
		}
		if len(t.Rows) == MaxRows {
			return Table{}, errors.New("HttpRunDataTooManyRows", "a data file may have at most 10000 rows", "rows", MaxRows)
		}
		row := make(map[string]string, len(cols))
		for i, c := range cols {
			row[c] = rec[i]
		}
		t.Rows = append(t.Rows, row)
	}
	if len(t.Rows) == 0 {
		return Table{}, errors.New("HttpRunDataNoRows", "the data file has no rows besides the header")
	}
	return t, nil
}

// csvLine is the line a CSV error is about, 0 when it has none.
func csvLine(err error) int {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		return pe.Line
	}
	return 0
}

// ParseJSON reads a JSON array of objects: each object is an iteration and its keys are variables. It fails with HttpRunDataJSONInvalid,
// HttpRunDataJSONShape, HttpRunDataNoRows and HttpRunDataTooManyRows.
func ParseJSON(data []byte) (Table, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var items []any
	if err := dec.Decode(&items); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) {
			return Table{}, errors.New("HttpRunDataJSONShape", "a JSON data file is an array of objects", "found", te.Value)
		}
		return Table{}, errors.NewErr("HttpRunDataJSONInvalid", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Table{}, errors.New("HttpRunDataJSONInvalid", "there is more text after the array")
	}
	if len(items) > MaxRows {
		return Table{}, errors.New("HttpRunDataTooManyRows", "a data file may have at most 10000 rows", "rows", MaxRows)
	}
	t := Table{}
	seen := map[string]bool{}
	for i, it := range items {
		obj, ok := it.(map[string]any)
		if !ok {
			return Table{}, errors.New("HttpRunDataJSONShape", "every item of a JSON data file is an object", "item", i+1)
		}
		row := make(map[string]string, len(obj))
		for k, v := range obj {
			row[k] = valueText(v)
			if !seen[k] {
				seen[k] = true
				t.Columns = append(t.Columns, k)
			}
		}
		t.Rows = append(t.Rows, row)
	}
	if len(t.Rows) == 0 {
		return Table{}, errors.New("HttpRunDataNoRows", "the data file has no rows")
	}
	sort.Strings(t.Columns)
	return t, nil
}

// valueText is the text of a JSON value.
func valueText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case json.Number:
		return x.String()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
