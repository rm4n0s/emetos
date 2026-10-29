//go:build emetos_display

package main

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The SQL query editors and the results panel as a user meets them (step 19.3, SPEC §10.21): a SQLite file as the database, the
// explorer of Databases, a query editor and its results, through the compositor. No server is started.

// makeSQLiteFixture makes the library database in a folder of the test and returns its path.
func makeSQLiteFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "library.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening the fixture: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing the fixture: %v", err)
		}
	}()
	for _, stmt := range []string{
		`CREATE TABLE authors (id INTEGER PRIMARY KEY, name TEXT NOT NULL, born INTEGER)`,
		`CREATE TABLE books (id INTEGER PRIMARY KEY, author_id INTEGER REFERENCES authors(id), title TEXT NOT NULL, year INTEGER)`,
		`INSERT INTO authors VALUES (1, 'Ursula K. Le Guin', 1929), (2, 'Stanislaw Lem', 1921), (3, 'Octavia E. Butler', 1947)`,
		`INSERT INTO books VALUES (1, 1, 'The Dispossessed', 1974), (2, 1, 'A Wizard of Earthsea', 1968), (3, 2, 'Solaris', 1961),
			(4, 3, 'Kindred', 1979), (5, 2, 'The Cyberiad', 1965)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("filling the fixture with %q: %v", stmt, err)
		}
	}
	return path
}

func TestRealInputSQLQuery(t *testing.T) {
	file := makeSQLiteFixture(t)
	conns, err := json.Marshal(map[string]any{"version": 1, "connections": []map[string]any{
		{"id": "lib1", "name": "Library", "engine": "sqlite", "scope": "user", "file": file, "color": "#4a90d9"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	a := display.Start(t, display.Options{
		Args:  []string{".", "notes.txt"},
		State: map[string]string{"db-connections.json": string(conns)},
		Project: map[string]string{
			"notes.txt": "queries live in the explorer of Databases\n",
		},
	})
	a.Settle()

	// the view of the connections, through the command that shows it
	a.Press("ctrl+shift+p")
	a.Type("Show Databases")
	a.Eventually("the palette to list the command", func() (bool, string) {
		for _, tx := range a.Texts() {
			if tx.Text == "Show Databases" || tx.Text == "View: Show Databases" {
				return true, tx.Text
			}
		}
		return false, a.Dump()
	})
	a.Press("enter")
	a.WaitText("Library")

	// the menu of the connection opens it
	a.RightClickText("Library")
	a.WaitText("Connect")
	a.ClickText("Connect")

	// the connection opens in the background and is not expanded by itself: expand it until its children show. SQLite has one
	// database, "main", which may or may not be a level of the tree.
	a.Eventually("the tree to show Tables or main", func() (bool, string) {
		_, tables := a.Text("Tables")
		_, main := a.Text("main")
		if tables || main {
			return true, ""
		}
		if tx, ok := a.Text("Library"); ok {
			a.Click(tx.X+tx.Width/2, tx.Y+tx.Height/2)
			a.Press("right")
		}
		return false, a.Dump()
	})
	for _, name := range []string{"main", "Tables"} {
		if tx, ok := a.Text(name); ok {
			a.Click(tx.X+tx.Width/2, tx.Y+tx.Height/2)
			a.Press("right")
			a.Settle()
		}
	}
	a.WaitText("books")

	// the menu of the table: Select Top 1000 opens an editor and runs it
	books := a.WaitText("books")
	a.RightClick(books.X+books.Width/2, books.Y+books.Height/2)
	a.WaitText("Select Top 1000")
	a.ClickText("Select Top 1000")
	a.WaitText("The Dispossessed")
	a.WaitText("Solaris")
	a.Settle()
	a.Screenshot(filepath.Join(proofDir(t), "19.3-sql-query.png"))
}
