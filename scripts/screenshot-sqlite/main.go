// Command screenshot-sqlite makes the SQLite database of the SQL shot of README.md (scripts/screenshots.sh) from a file of SQL
// statements, so that no binary fixture is kept in the repository.
//
// Usage: go run ./scripts/screenshot-sqlite <out.db> <statements.sql>
package main

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/rm4n0s/errors"
	_ "modernc.org/sqlite"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "screenshot-sqlite:", err)
		os.Exit(1)
	}
}

// run reads the statements and executes them on a new database at the first argument.
func run(args []string) (err error) {
	if len(args) != 2 {
		return errors.New("ScreenshotSQLiteUsage", "usage: screenshot-sqlite <out.db> <statements.sql>")
	}
	script, err := os.ReadFile(args[1])
	if err != nil {
		return errors.NewErr("ScreenshotSQLiteReadFailed", err).SetMetadata("path", args[1])
	}
	if _, err := os.Stat(args[0]); err == nil {
		return errors.New("ScreenshotSQLiteExists", "the database exists already", "path", args[0])
	}
	db, err := sql.Open("sqlite", args[0])
	if err != nil {
		return errors.NewErr("ScreenshotSQLiteOpenFailed", err).SetMetadata("path", args[0])
	}
	defer func() {
		if cerr := db.Close(); err == nil && cerr != nil {
			err = errors.NewErr("ScreenshotSQLiteCloseFailed", cerr)
		}
	}()
	if _, err := db.Exec(string(script)); err != nil {
		return errors.NewErr("ScreenshotSQLiteExecFailed", err)
	}
	return nil
}
