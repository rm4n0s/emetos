package sql

import (
	"bufio"
	"context"
	"encoding/hex"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// The formats a result is exported in.
const (
	formatCSV  = "csv"
	formatJSON = "json"
	formatSQL  = "sql"
)

// exportTimeout bounds the writing of an export.
const exportTimeout = 10 * time.Minute

// export is the command Export Result: it writes the result set that the panel shows, as it is shown (sorted as the grid is), to a file
// the user chooses in the desktop's save dialog. The argument is "csv", "json" or "sql"; without one the user is asked.
func (q *queries) export(args any) {
	format, _ := args.(string)
	if format == "" {
		if m, ok := args.(map[string]any); ok {
			format, _ = m["format"].(string)
		}
	}
	if format == "" {
		q.askFormat()
		return
	}
	q.exportAs(format)
}

// askFormat asks which format to export in.
func (q *queries) askFormat() {
	formats := []string{formatCSV, formatJSON, formatSQL}
	err := q.api.QuickPick().Show(extension.QuickPickOptions{
		Placeholder: "Export the result as",
		Items: []extension.QuickPickItem{
			{Label: "$(file) CSV", Description: "a comma-separated file, with the titles of the columns"},
			{Label: "$(json) JSON", Description: "an array with an object for each row"},
			{Label: "$(database) SQL INSERT statements", Description: "one INSERT for each row"},
		},
	}, func(i int) {
		if i >= 0 && i < len(formats) {
			q.exportAs(formats[i])
		}
	})
	if err != nil {
		q.api.Log().Warn("the formats cannot be listed", "err", err)
	}
}

// shownSet is the result set the panel shows, with the run it belongs to.
func (q *queries) shownSet() (*queryEditor, *runRecord, *setRecord) {
	ed := q.shown
	if ed == nil || ed.shown == nil {
		return nil, nil, nil
	}
	rec := ed.shown
	if rec.tab < 0 || rec.tab >= len(rec.sets) {
		return nil, nil, nil
	}
	return ed, rec, rec.sets[rec.tab]
}

// exportAs writes the shown result set in a format.
func (q *queries) exportAs(format string) {
	ed, rec, sr := q.shownSet()
	if sr == nil || sr.set.Table.Len() == 0 {
		q.api.Notifications().Info("There is no result with rows to export. Run a query first.")
		return
	}
	grid := q.gridOf(ed, rec, sr)
	exp := grid.Model().Export()
	d, ok := dialect.ByID(rec.engine)
	if !ok {
		q.api.Notifications().Error("Cannot export: the engine of the result is unknown.")
		return
	}
	switch format {
	case formatCSV, formatJSON:
		q.save(exp, format, d, "")
	case formatSQL:
		guess := tableOf(rec.steps[sr.step].res.Statement.Text)
		err := q.api.InputBox().Show(extension.InputBoxOptions{
			Prompt: "Table for the INSERT statements (schema.table)", Value: guess,
			Validate: func(v string) string {
				if strings.TrimSpace(v) == "" {
					return "A table name is needed."
				}
				return ""
			},
		}, func(table string, ok bool) {
			if ok {
				q.save(exp, formatSQL, d, strings.TrimSpace(table))
			}
		})
		if err != nil {
			q.api.Log().Warn("the table name cannot be asked", "err", err)
			q.api.Notifications().Error("Cannot ask for the table: " + err.Error())
		}
	default:
		q.api.Notifications().Error("Unknown export format " + strconv.Quote(format) + ".")
	}
}

// save asks where to write the file, then writes it off the UI thread.
func (q *queries) save(exp *resultgrid.Export, format string, d dialect.Dialect, table string) {
	api := q.api
	name := map[string]string{formatCSV: "result.csv", formatJSON: "result.json", formatSQL: "result.sql"}[format]
	filter := map[string]extension.FileFilter{
		formatCSV:  {Name: "CSV files", Patterns: []string{"*.csv", "*.CSV"}},
		formatJSON: {Name: "JSON files", Patterns: []string{"*.json", "*.JSON"}},
		formatSQL:  {Name: "SQL files", Patterns: []string{"*.sql", "*.SQL"}},
	}[format]
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), exportTimeout)
		defer cancel()
		path, err := api.FileChooser().Save(ctx, extension.FileChooserOptions{Title: "Export Result", AcceptLabel: "Export", Name: name, Filters: []extension.FileFilter{filter}})
		if err == nil && path != "" {
			err = writeExport(ctx, path, format, d, table, exp)
		}
		api.Post(func() {
			switch {
			case err != nil:
				api.Log().Warn("a result cannot be exported", "err", err)
				api.Notifications().Error("Cannot export the result: " + err.Error())
			case path != "":
				api.Notifications().Info("Exported " + plural(exp.Rows(), "row") + " to " + path + ".")
			}
		})
	}()
}

// writeExport writes the export to the file, in the format. The file is removed when the writing fails half way. The errors have the
// tags SQLExportFileFailed and SQLExportWriteFailed, or those of the writers of the grid.
func writeExport(ctx context.Context, path, format string, d dialect.Dialect, table string, exp *resultgrid.Export) (err error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errors.NewErr("SQLExportFileFailed", err).SetMetadata("path", path)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = errors.NewErr("SQLExportFileFailed", cerr).SetMetadata("path", path)
		}
		if err != nil {
			if rerr := os.Remove(path); rerr != nil && !os.IsNotExist(rerr) {
				err = errors.NewErr("SQLExportFileFailed", rerr).SetMetadata("path", path, "first", err.Error())
			}
		}
	}()
	switch format {
	case formatCSV:
		return exp.WriteCSV(ctx, f, resultgrid.CSVOptions{})
	case formatJSON:
		return exp.WriteJSON(ctx, f)
	}
	return WriteInserts(ctx, f, d, table, exp)
}

// tableRE finds the table of a simple SELECT: the name after the first FROM.
var tableRE = regexp.MustCompile("(?is)\\bfrom\\s+((?:[\\w$]+|\"[^\"]+\"|`[^`]+`|\\[[^\\]]+\\])(?:\\s*\\.\\s*(?:[\\w$]+|\"[^\"]+\"|`[^`]+`|\\[[^\\]]+\\]))*)")

// dotSpaceRE is the white space around the dots of a qualified name.
var dotSpaceRE = regexp.MustCompile(`\s*\.\s*`)

// tableOf guesses the table a SELECT reads, for the name the INSERTs start with; "table_name" when it is not plain.
func tableOf(stmt string) string {
	if m := tableRE.FindStringSubmatch(stmt); m != nil {
		return dotSpaceRE.ReplaceAllString(m[1], ".")
	}
	return "table_name"
}

// WriteInserts writes the result as one INSERT statement for each row, with the columns of the result, the values written as the
// engine of d reads them (strings quoted, NULL, numbers, hex for binary data). table is the name of the table, written as a person
// types it: schema.table, with quotes if a name needs them; a name with no quotes is quoted by d. It stops when ctx ends. The errors
// have the tag SQLExportWriteFailed, or ResultExportCancelled.
func WriteInserts(ctx context.Context, w io.Writer, d dialect.Dialect, table string, exp *resultgrid.Export) error {
	bw := bufio.NewWriter(w)
	cols := make([]string, len(exp.Columns()))
	for i, c := range exp.Columns() {
		cols[i] = d.Quote(c.Name)
	}
	head := "INSERT INTO " + quoteTable(d, table) + " (" + strings.Join(cols, ", ") + ") VALUES ("
	var b strings.Builder
	err := exp.Each(ctx, func(row []resultgrid.Value) error {
		b.Reset()
		b.WriteString(head)
		for i, v := range row {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(insertLiteral(d, v))
		}
		b.WriteString(");\n")
		if _, err := bw.WriteString(b.String()); err != nil {
			return errors.NewErr("SQLExportWriteFailed", err).SetMetadata("format", "sql")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return errors.NewErr("SQLExportWriteFailed", err).SetMetadata("format", "sql")
	}
	return nil
}

// quoteTable quotes the parts of a table name that are not quoted already.
func quoteTable(d dialect.Dialect, table string) string {
	if strings.ContainsAny(table, "\"`[") {
		return table
	}
	parts := strings.Split(table, ".")
	for i, p := range parts {
		parts[i] = d.Quote(strings.TrimSpace(p))
	}
	return strings.Join(parts, ".")
}

// insertLiteral writes a value as a literal of the engine's SQL.
func insertLiteral(d dialect.Dialect, v resultgrid.Value) string {
	switch v.Kind {
	case resultgrid.KindNull:
		return "NULL"
	case resultgrid.KindInt:
		return strconv.FormatInt(v.I, 10)
	case resultgrid.KindFloat:
		if math.IsNaN(v.F) || math.IsInf(v.F, 0) {
			return d.QuoteString(strconv.FormatFloat(v.F, 'g', -1, 64))
		}
		return strconv.FormatFloat(v.F, 'g', -1, 64)
	case resultgrid.KindBool:
		if d.ID() == "postgres" || d.ID() == "mysql" {
			return map[bool]string{true: "TRUE", false: "FALSE"}[v.I != 0]
		}
		return strconv.FormatInt(v.I, 10)
	case resultgrid.KindBytes:
		h := hex.EncodeToString(v.B)
		switch d.ID() {
		case "postgres":
			return "'\\x" + h + "'"
		case "sqlserver":
			return "0x" + h
		}
		return "X'" + h + "'"
	case resultgrid.KindTime:
		layout := "2006-01-02 15:04:05.999999999"
		if d.ID() == "postgres" || d.ID() == "sqlserver" {
			layout += "-07:00"
		}
		return d.QuoteString(v.T.Format(layout))
	}
	return d.QuoteString(strings.ReplaceAll(v.S, "\x00", ""))
}
