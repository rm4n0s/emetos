package dialect

import (
	"slices"
	"strings"
)

// Syntax implements Dialect: dollar quotes, E” strings, nested comments; BEGIN ATOMIC bodies.
func (Postgres) Syntax() Syntax {
	return Syntax{DollarQuotes: true, EscapeStrings: true, NestedComments: true, AtomicBlocks: true}
}

// Transaction implements Dialect.
func (Postgres) Transaction() TxStatements {
	return TxStatements{Begin: "BEGIN", Commit: "COMMIT", Rollback: "ROLLBACK"}
}

// UseDatabase implements Dialect: a PostgreSQL connection serves one database, so there is nothing to switch.
func (Postgres) UseDatabase(string) string { return "" }

// Keywords implements Dialect.
func (Postgres) Keywords() []string {
	return keywords(postgresKeywords)
}

// Syntax implements Dialect: backslash escapes, double-quoted strings, backticks, # comments, DELIMITER.
func (MySQL) Syntax() Syntax {
	return Syntax{BackslashEscapes: true, DoubleQuotedStrings: true, HashComments: true, DashesNeedSpace: true, Backticks: true, DelimiterCommand: true}
}

// Transaction implements Dialect.
func (MySQL) Transaction() TxStatements {
	return TxStatements{Begin: "START TRANSACTION", Commit: "COMMIT", Rollback: "ROLLBACK"}
}

// UseDatabase implements Dialect: USE `database`.
func (m MySQL) UseDatabase(database string) string {
	if database == "" {
		return ""
	}
	return "USE " + m.Quote(database)
}

// Keywords implements Dialect.
func (MySQL) Keywords() []string { return keywords(mysqlKeywords) }

// Syntax implements Dialect: [brackets], backticks, and the BEGIN ... END body of a trigger.
func (SQLite) Syntax() Syntax { return Syntax{Brackets: true, Backticks: true, TriggerBlocks: true} }

// Transaction implements Dialect.
func (SQLite) Transaction() TxStatements {
	return TxStatements{Begin: "BEGIN", Commit: "COMMIT", Rollback: "ROLLBACK"}
}

// UseDatabase implements Dialect: SQLite has the one file and its attached schemas, which are named in the statements.
func (SQLite) UseDatabase(string) string { return "" }

// Keywords implements Dialect.
func (SQLite) Keywords() []string { return keywords(sqliteKeywords) }

// Syntax implements Dialect: [brackets], nested comments, GO, blocks and the procedures that run to the end of the batch.
func (SQLServer) Syntax() Syntax {
	return Syntax{Brackets: true, NestedComments: true, GoSeparator: true, Batches: true}
}

// Transaction implements Dialect.
func (SQLServer) Transaction() TxStatements {
	return TxStatements{Begin: "BEGIN TRANSACTION", Commit: "COMMIT TRANSACTION", Rollback: "ROLLBACK TRANSACTION"}
}

// UseDatabase implements Dialect: USE [database].
func (s SQLServer) UseDatabase(database string) string {
	if database == "" {
		return ""
	}
	return "USE " + s.Quote(database)
}

// Keywords implements Dialect.
func (SQLServer) Keywords() []string { return keywords(sqlserverKeywords) }

// keywords joins the keywords every engine has with the engine's own, sorted and without repeats.
func keywords(own string) []string {
	all := strings.Fields(commonKeywords + " " + own)
	slices.Sort(all)
	return slices.Compact(all)
}

// commonKeywords are the keywords of SQL that the four engines share.
const commonKeywords = `ADD ALL ALTER AND ANY AS ASC BEGIN BETWEEN BY CASE CAST CHECK COLUMN COMMIT CONSTRAINT COUNT CREATE CROSS DEFAULT DELETE DESC
DISTINCT DROP ELSE END EXCEPT EXISTS FALSE FOREIGN FROM FULL GROUP HAVING IN INDEX INNER INSERT INTERSECT INTO IS JOIN KEY LEFT LIKE
LIMIT NOT NULL OFFSET ON OR ORDER OUTER PRIMARY REFERENCES RIGHT ROLLBACK SELECT SET TABLE THEN TRUE UNION UNIQUE UPDATE VALUES VIEW
WHEN WHERE WITH`

const postgresKeywords = `ANALYZE ARRAY BIGINT BIGSERIAL BOOLEAN CASCADE COMMENT CONCURRENTLY COPY CURRENT_DATE CURRENT_TIMESTAMP DO ENUM EXPLAIN
EXTENSION FETCH FUNCTION GRANT ILIKE INHERITS INTEGER INTERVAL JSONB LATERAL MATERIALIZED NUMERIC OVER PARTITION PROCEDURE RETURNING
REVOKE SCHEMA SEQUENCE SERIAL SHOW TEXT TIMESTAMP TIMESTAMPTZ TRIGGER TRUNCATE UUID VACUUM VARCHAR WINDOW`

const mysqlKeywords = `ANALYZE AUTO_INCREMENT BIGINT BLOB CALL CHAR DATABASE DATABASES DATETIME DECIMAL DESCRIBE ENGINE ENUM EXPLAIN FUNCTION GRANT
IGNORE INT JSON LOCK LONGTEXT MEDIUMTEXT PROCEDURE RENAME REPLACE REVOKE SCHEMA SHOW STRAIGHT_JOIN TEXT TINYINT TRIGGER TRUNCATE UNSIGNED
USE VARCHAR`

const sqliteKeywords = `ANALYZE ATTACH AUTOINCREMENT BLOB DATABASE DETACH EXPLAIN GLOB INDEXED INTEGER NUMERIC PRAGMA QUERY PLAN REAL REINDEX
REPLACE ROWID STRICT TEXT TRIGGER TEMP TEMPORARY VACUUM VIRTUAL WITHOUT`

const sqlserverKeywords = `BIGINT BIT CATCH CLUSTERED DATABASE DATETIME2 DECLARE DENY EXEC EXECUTE FUNCTION GO GRANT IDENTITY IF NVARCHAR NONCLUSTERED
OUTPUT PRINT PROC PROCEDURE RAISERROR REVOKE SCHEMA TOP TRAN TRANSACTION TRIGGER TRUNCATE TRY UNIQUEIDENTIFIER USE VARCHAR WHILE`
