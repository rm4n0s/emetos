package dialect

import (
	"slices"
	"sort"
	"testing"
)

func TestTransactionAndUseStatements(t *testing.T) {
	tests := []struct {
		d               Dialect
		begin, use, esc string
	}{
		{Postgres{}, "BEGIN", "", ""},
		{MySQL{}, "START TRANSACTION", "USE `shop`", "USE `a``b`"},
		{SQLite{}, "BEGIN", "", ""},
		{SQLServer{}, "BEGIN TRANSACTION", "USE [shop]", "USE [a]]b]"},
	}
	for _, tc := range tests {
		t.Run(tc.d.ID(), func(t *testing.T) {
			tx := tc.d.Transaction()
			if tx.Begin != tc.begin || tx.Commit == "" || tx.Rollback == "" {
				t.Errorf("Transaction() = %+v", tx)
			}
			// what the statements say is a transaction control to Classify
			for _, sql := range []string{tx.Begin, tx.Commit, tx.Rollback} {
				if got := Classify(tc.d, sql).Class; got != ClassTransaction {
					t.Errorf("Classify(%q) = %v", sql, got)
				}
			}
			if got := tc.d.UseDatabase("shop"); got != tc.use {
				t.Errorf("UseDatabase = %q, want %q", got, tc.use)
			}
			if tc.esc != "" {
				a, b := "a`b", "a]b"
				name := a
				if tc.d.ID() == "sqlserver" {
					name = b
				}
				if got := tc.d.UseDatabase(name); got != tc.esc {
					t.Errorf("UseDatabase(%q) = %q, want %q", name, got, tc.esc)
				}
			}
			if tc.d.UseDatabase("") != "" {
				t.Error("UseDatabase of no database is a statement")
			}
		})
	}
}

func TestKeywordsAreSortedCapitalsWithTheCommonOnes(t *testing.T) {
	for _, d := range All() {
		kw := d.Keywords()
		if !sort.StringsAreSorted(kw) || len(slices.Compact(slices.Clone(kw))) != len(kw) {
			t.Errorf("%s: keywords are not sorted and unique", d.ID())
		}
		for _, must := range []string{"SELECT", "FROM", "WHERE", "INSERT"} {
			if !slices.Contains(kw, must) {
				t.Errorf("%s: no keyword %s", d.ID(), must)
			}
		}
	}
	if !slices.Contains(Postgres{}.Keywords(), "RETURNING") || !slices.Contains(SQLServer{}.Keywords(), "TOP") || !slices.Contains(SQLite{}.Keywords(), "PRAGMA") {
		t.Error("an engine's own keyword is missing")
	}
}
