package sql

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	mssql "github.com/microsoft/go-mssqldb"

	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// binaryTypes are the type names (capitals) of the columns whose bytes are data and not text.
var binaryTypes = map[string]bool{
	"BLOB": true, "TINYBLOB": true, "MEDIUMBLOB": true, "LONGBLOB": true, "BINARY": true, "VARBINARY": true, "BIT": true, "GEOMETRY": true,
	"BYTEA": true, "IMAGE": true, "ROWVERSION": true,
}

// convert turns what a driver gave for a column into a value of the grid. The drivers differ: pgx gives text for most types and
// bytes for bytea and JSON; the MySQL driver gives bytes for text, decimals and blobs alike, so the name of the column's type says
// which; modernc's SQLite gives bytes only for blobs; go-mssqldb gives bytes for decimals and for a GUID's sixteen bytes.
func convert(engine, typeName string, v any) resultgrid.Value {
	switch x := v.(type) {
	case nil:
		return resultgrid.Null()
	case int64:
		return resultgrid.Int(x)
	case int:
		return resultgrid.Int(int64(x))
	case uint64:
		if x > 1<<63-1 {
			return resultgrid.Text(formatUnsigned(x))
		}
		return resultgrid.Int(int64(x))
	case float64:
		return resultgrid.Float(x)
	case float32:
		return resultgrid.Float(float64(x))
	case bool:
		return resultgrid.Bool(x)
	case time.Time:
		if engine == "postgres" && strings.EqualFold(typeName, "TIMESTAMPTZ") {
			x = x.UTC() // pgx gives the time in the zone of the machine; the instant is the same and its text does not depend on the zone
		}
		return resultgrid.Time(x)
	case string:
		if isJSONType(typeName) {
			return resultgrid.JSON(x)
		}
		return resultgrid.Text(x)
	case []byte:
		return convertBytes(engine, strings.ToUpper(typeName), x)
	case driver.Valuer:
		if dv, err := x.Value(); err == nil {
			return convert(engine, typeName, dv)
		}
	}
	return resultgrid.Text(fmt.Sprint(v))
}

// convertBytes decides whether bytes are text, JSON, a GUID or data.
func convertBytes(engine, typeName string, b []byte) resultgrid.Value {
	switch {
	case isJSONType(typeName):
		return resultgrid.JSON(string(b))
	case typeName == "UNIQUEIDENTIFIER" && len(b) == 16:
		var id mssql.UniqueIdentifier
		if err := id.Scan(b); err == nil {
			return resultgrid.Text(id.String())
		}
	case engine == "sqlite", binaryTypes[typeName]:
		return resultgrid.Bytes(append([]byte(nil), b...))
	}
	if utf8.Valid(b) {
		return resultgrid.Text(string(b))
	}
	return resultgrid.Bytes(append([]byte(nil), b...))
}

// isJSONType tells whether the column type is JSON.
func isJSONType(name string) bool {
	switch strings.ToUpper(name) {
	case "JSON", "JSONB":
		return true
	}
	return false
}

// size estimates the memory a value takes, for the bound on the bytes of a result.
func size(v resultgrid.Value) int64 {
	return 40 + int64(len(v.S)) + int64(len(v.B))
}

// formatUnsigned writes an unsigned number too large for the grid's integers.
func formatUnsigned(x uint64) string { return strconv.FormatUint(x, 10) }
