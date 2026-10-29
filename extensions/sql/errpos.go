package sql

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	mssql "github.com/microsoft/go-mssqldb"
	"github.com/rm4n0s/errors"
)

// QueryError is a statement that failed, as the user is told of it: the server's words, its code and, when the server says where, the
// place in the statement.
type QueryError struct {
	// Message is what the server said, without the code; Code is its code ("42601", "1064", "208"), or empty.
	Message, Code string
	// Offset is where the error is in the text of the statement, in bytes, and Length how much of the text to mark; Offset is -1
	// when the server does not say where.
	Offset, Length int
}

// Text is the error as one line: "42601: syntax error at or near "FROM"".
func (q QueryError) Text() string {
	if q.Code == "" {
		return q.Message
	}
	return q.Code + ": " + q.Message
}

// describeError reads a driver's error for a statement of an engine. The password is not in it: a driver's error for a statement
// names the statement and the server's words, and the connection string is only in the errors of connecting.
func describeError(engine string, err error, stmt string) QueryError {
	q := QueryError{Message: err.Error(), Offset: -1}
	switch engine {
	case "postgres":
		var pe *pgconn.PgError
		if errors.As(err, &pe) {
			q.Message, q.Code = pe.Message, pe.Code
			if pe.Detail != "" {
				q.Message += ". " + pe.Detail
			}
			if pe.Hint != "" {
				q.Message += " (" + pe.Hint + ")"
			}
			if pe.Position > 0 {
				q.Offset = byteOffsetOfRune(stmt, int(pe.Position)-1)
			}
		}
	case "mysql":
		var me *mysql.MySQLError
		if errors.As(err, &me) {
			q.Message, q.Code = me.Message, strconv.Itoa(int(me.Number))
			q.Offset = mysqlOffset(me.Message, stmt)
		}
	case "sqlserver":
		var se mssql.Error
		if errors.As(err, &se) {
			q.Message, q.Code = se.Message, strconv.Itoa(int(se.Number))
			if se.LineNo > 0 {
				q.Offset = lineOffset(stmt, int(se.LineNo))
			}
		}
	case "sqlite":
		q.Message, q.Code = sqliteMessage(err)
		q.Offset = sqliteOffset(q.Message, stmt)
	}
	if q.Offset >= 0 {
		q.Length = tokenLength(stmt, q.Offset)
	}
	return q
}

// byteOffsetOfRune converts the index of a character to the offset of its first byte; text shorter than that gives its end.
func byteOffsetOfRune(s string, index int) int {
	for i := range s {
		if index == 0 {
			return i
		}
		index--
	}
	return len(s)
}

// lineOffset is the offset of the start of a line (counted from 1) of the text; -1 for a line the text does not have.
func lineOffset(s string, line int) int {
	off := 0
	for ; line > 1; line-- {
		i := strings.IndexByte(s[off:], '\n')
		if i < 0 {
			return -1
		}
		off += i + 1
	}
	// the mark starts at the first thing on the line, not at its white space
	for off < len(s) && (s[off] == ' ' || s[off] == '\t') {
		off++
	}
	return off
}

// nearRE finds the "near '...' at line N" of MySQL's syntax errors.
var nearRE = regexp.MustCompile(`(?s)near '(.*)' at line (\d+)`)

// mysqlOffset finds the place of a MySQL error: the text it quotes after "near" is the rest of the statement from the error on.
func mysqlOffset(message, stmt string) int {
	m := nearRE.FindStringSubmatch(message)
	if m == nil {
		return -1
	}
	if m[1] != "" {
		if i := strings.LastIndex(stmt, m[1]); i >= 0 {
			return i
		}
	}
	line, err := strconv.Atoi(m[2])
	if err != nil {
		return -1
	}
	return lineOffset(stmt, line)
}

// sqliteRE takes the code and the words out of modernc's "SQL logic error: near "x": syntax error (1)".
var sqliteRE = regexp.MustCompile(`^(.*?)(?: \((\d+)\))?$`)

// sqliteMessage cuts the code in brackets off the message of the SQLite driver.
func sqliteMessage(err error) (message, code string) {
	m := sqliteRE.FindStringSubmatch(strings.TrimSpace(err.Error()))
	if m == nil {
		return err.Error(), ""
	}
	code = m[2]
	if code == "1" {
		code = "" // SQLITE_ERROR is every error that has no code of its own: it says nothing
	}
	return strings.TrimPrefix(m[1], "SQL logic error: "), code
}

// sqliteNameRE finds the name SQLite complains about: near "x", no such table: x.
var sqliteNameRE = regexp.MustCompile(`near "([^"]+)"|(?:no such (?:table|column|function|index|view|module|collation sequence)|ambiguous column name|table [^ ]+ has no column named|unrecognized token): ?"?([^" ]+)"?`)

// sqliteOffset finds the name in the statement.
func sqliteOffset(message, stmt string) int {
	m := sqliteNameRE.FindStringSubmatch(message)
	if m == nil {
		return -1
	}
	name := m[1]
	if name == "" {
		name = m[2]
	}
	// the name of a column may be written with its table; the part after the dot is what the message has
	if i := strings.Index(stmt, name); i >= 0 {
		return i
	}
	if i := strings.Index(strings.ToLower(stmt), strings.ToLower(name)); i >= 0 {
		return i
	}
	return -1
}

// tokenLength is the length of the word, quoted name or run of characters at the offset, at least one character.
func tokenLength(stmt string, off int) int {
	if off >= len(stmt) {
		return 0
	}
	rest := stmt[off:]
	_, first := utf8.DecodeRuneInString(rest)
	if q := rest[0]; q == '"' || q == '\'' || q == '`' {
		if i := strings.IndexByte(rest[1:], q); i >= 0 {
			return i + 2
		}
		return len(rest)
	}
	n := 0
	for n < len(rest) && isNameByte(rest[n]) {
		n++
	}
	if n == 0 {
		return first
	}
	return n
}

// isNameByte tells whether a byte is part of an unquoted name.
func isNameByte(b byte) bool {
	return b == '_' || b == '$' || b == '.' || b >= 0x80 || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
