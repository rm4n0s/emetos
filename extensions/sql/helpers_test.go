package sql

import "github.com/rm4n0s/emetos/extensions/sql/dialect"

// dialectQuery is a catalog-style query with no arguments, for tests that read a table through the service.
func dialectQuery(sql string) dialect.Query { return dialect.Query{SQL: sql} }
