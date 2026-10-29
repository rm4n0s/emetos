package dialect

import "strings"

// Class says what a statement does to the database, as far as its words tell (SPEC §10.21 "Safety").
type Class int

// The classes.
const (
	// ClassEmpty is a statement with nothing in it.
	ClassEmpty Class = iota
	// ClassRead only reads: SELECT, WITH over reads, VALUES, SHOW, DESCRIBE, EXPLAIN, a reading PRAGMA.
	ClassRead
	// ClassWrite changes data, the schema, the privileges or the settings that make a session read-only; and anything the words do
	// not show to be harmless (CALL, EXEC, a function in a statement cannot be seen at all).
	ClassWrite
	// ClassTransaction begins, ends or marks a transaction (BEGIN, COMMIT, ROLLBACK, SAVEPOINT).
	ClassTransaction
	// ClassSession changes the session only: USE, SET.
	ClassSession
)

// String names the class.
func (c Class) String() string {
	return [...]string{"empty", "read", "write", "transaction", "session"}[c]
}

// Info is what Classify finds out about a statement.
type Info struct {
	// Class is what the statement does.
	Class Class
	// Verb is the first word, in capitals ("SELECT", "DELETE"); empty for an empty statement.
	Verb string
	// Destructive says that the statement can lose data that is not easily brought back: DROP, TRUNCATE, ALTER, and DELETE or UPDATE
	// without a WHERE. Why says which, for the question the user is asked.
	Destructive bool
	Why         string
	// Rows says that the statement probably returns rows (a read, a statement with RETURNING or OUTPUT, CALL, EXEC), so it is run as
	// a query and not as a command that counts the rows it changed.
	Rows bool
	// Schema says that the statement changes the schema (CREATE, ALTER, DROP, RENAME, TRUNCATE...), so what the editor knows of the
	// catalog is out of date.
	Schema bool
	// Begins and Ends say that a transaction starts or ends with the statement (BEGIN, START TRANSACTION; COMMIT,
	// ROLLBACK, END), as the session must know whether one is open.
	Begins, Ends bool
}

// readingPragmas are the SQLite pragmas that read when they have an argument (PRAGMA table_info(books)).
var readingPragmas = map[string]bool{
	"table_info": true, "table_xinfo": true, "table_list": true, "index_list": true, "index_info": true, "index_xinfo": true,
	"foreign_key_list": true, "foreign_key_check": true, "integrity_check": true, "quick_check": true, "database_list": true,
	"collation_list": true, "function_list": true, "module_list": true, "pragma_list": true, "compile_options": true,
}

// actingPragmas are the SQLite pragmas that do something when written without an argument.
var actingPragmas = map[string]bool{
	"wal_checkpoint": true, "optimize": true, "shrink_memory": true, "incremental_vacuum": true,
}

// Classify reads the first words of a statement and says what it does. It is a check of the words, not of the meaning: a SELECT that
// calls a function that writes is a read to it, which is why a read-only connection is also read-only in the engine where the engine
// can do that (Dialect.ReadOnlySession).
func Classify(d Dialect, text string) Info {
	syn := d.Syntax()
	src := text
	all := lex(src, syn)
	var toks []token
	for _, t := range all {
		if t.significant() && t.kind != tokEnd {
			toks = append(toks, t)
		}
	}
	// the opening parentheses of (SELECT ...) UNION ... are not the verb
	for len(toks) > 0 && toks[0].kind == tokPunct && src[toks[0].start] == '(' {
		toks = toks[1:]
	}
	if len(toks) == 0 {
		return Info{}
	}
	c := classifier{src: src, d: d, toks: toks}
	return c.classify()
}

// classifier reads the tokens of one statement.
type classifier struct {
	src  string
	d    Dialect
	toks []token
}

// word is the capitalised word at index i; "" when it is not a word.
func (c *classifier) word(i int) string {
	if i < 0 || i >= len(c.toks) {
		return ""
	}
	return c.toks[i].upper(c.src)
}

// has tells whether the word occurs anywhere in the statement outside strings, names and comments.
func (c *classifier) has(w string) bool {
	for i := range c.toks {
		if c.word(i) == w {
			return true
		}
	}
	return false
}

// classify decides the class from the verb.
func (c *classifier) classify() Info {
	verb := c.word(0)
	info := Info{Verb: verb, Class: ClassWrite}
	switch verb {
	case "SELECT":
		info.Class, info.Rows = ClassRead, true
		if c.selectInto() {
			info.Class, info.Schema = ClassWrite, true
		}
	case "WITH":
		c.with(&info)
	case "VALUES", "SHOW", "DESCRIBE", "DESC", "TABLE", "LIST":
		info.Class, info.Rows = ClassRead, true
	case "EXPLAIN":
		info.Rows = true
		info.Class = ClassRead
		if c.has("ANALYZE") || c.has("ANALYSE") {
			// EXPLAIN ANALYZE runs the statement: it is what that statement is
			rest := c.afterExplain()
			if rest != nil {
				inner := (&classifier{src: c.src, d: c.d, toks: rest}).classify()
				if inner.Class != ClassRead {
					info.Class, info.Destructive, info.Why = ClassWrite, inner.Destructive, inner.Why
				}
			}
		}
	case "PRAGMA":
		c.pragma(&info)
	case "USE":
		info.Class = ClassSession
	case "SET", "RESET":
		info.Class = ClassSession
		if c.touchesReadOnly() {
			info.Class = ClassWrite
		}
	case "BEGIN", "START":
		info.Class, info.Begins = ClassTransaction, c.beginsTransaction()
		if c.has("WRITE") {
			info.Class = ClassWrite
		}
	case "COMMIT", "ROLLBACK", "END", "ABORT":
		info.Class = ClassTransaction
		info.Ends = verb != "ROLLBACK" || (!c.has("TO") && !c.has("SAVEPOINT"))
		if verb == "END" && c.word(1) != "" && c.word(1) != "TRANSACTION" && c.word(1) != "TRAN" && c.word(1) != "WORK" {
			info.Class, info.Ends = ClassWrite, false
		}
	case "SAVEPOINT", "RELEASE":
		info.Class = ClassTransaction
	case "CALL", "EXEC", "EXECUTE":
		info.Rows = true
	case "INSERT", "UPDATE", "DELETE", "MERGE", "REPLACE":
		info.Rows = c.has("RETURNING") || c.has("OUTPUT")
		c.destructiveDML(&info, 0)
	case "DROP", "TRUNCATE", "ALTER":
		info.Destructive, info.Schema = true, true
		info.Why = verb
		if o := c.objectWord(1); o != "" {
			info.Why = verb + " " + o
		}
	case "CREATE", "RENAME", "COMMENT":
		info.Schema = true
	}
	return info
}

// afterExplain returns the tokens after EXPLAIN and its options: ANALYZE, VERBOSE, a parenthesised list.
func (c *classifier) afterExplain() []token {
	i := 1
	for i < len(c.toks) {
		switch w := c.word(i); {
		case w == "ANALYZE" || w == "ANALYSE" || w == "VERBOSE" || w == "QUERY" || w == "PLAN":
			i++
		case c.toks[i].kind == tokPunct && c.src[c.toks[i].start] == '(':
			depth := 0
			for ; i < len(c.toks); i++ {
				if c.toks[i].kind != tokPunct {
					continue
				}
				switch c.src[c.toks[i].start] {
				case '(':
					depth++
				case ')':
					depth--
				}
				if depth == 0 {
					i++
					break
				}
			}
		default:
			return c.toks[i:]
		}
	}
	return nil
}

// selectInto tells whether a SELECT creates something: PostgreSQL and SQL Server SELECT ... INTO table, MySQL's INTO OUTFILE and
// DUMPFILE. MySQL's INTO @variable and SELECT ... INTO of a parenthesised sub-select are not writes.
func (c *classifier) selectInto() bool {
	depth := 0
	for i, t := range c.toks {
		if t.kind == tokPunct {
			switch c.src[t.start] {
			case '(':
				depth++
			case ')':
				depth--
			}
			continue
		}
		if depth != 0 || c.word(i) != "INTO" {
			continue
		}
		switch c.word(i + 1) {
		case "OUTFILE", "DUMPFILE":
			return true
		}
		if i+1 < len(c.toks) && c.toks[i+1].kind == tokPunct && c.src[c.toks[i+1].start] == '@' {
			return false
		}
		return c.d.ID() != "mysql"
	}
	return false
}

// with classifies WITH ... : a read unless a common table expression or the statement after them writes.
func (c *classifier) with(info *Info) {
	info.Class, info.Rows = ClassRead, true
	for i := range c.toks {
		switch c.word(i) {
		case "INSERT", "UPDATE", "DELETE", "MERGE", "REPLACE":
			if c.word(i) == "UPDATE" && (c.word(i-1) == "FOR" || c.word(i-1) == "NO" || c.word(i-1) == "KEY") {
				continue // FOR UPDATE locks rows of a read
			}
			if c.word(i) == "REPLACE" && c.toks[min(i+1, len(c.toks)-1)].kind == tokPunct {
				continue // the function replace(...)
			}
			info.Class = ClassWrite
			c.destructiveDML(info, i)
		}
	}
	if info.Class == ClassWrite {
		info.Rows = c.has("RETURNING") || c.has("OUTPUT")
	}
	if info.Class == ClassRead && c.selectInto() {
		info.Class = ClassWrite
	}
}

// destructiveDML marks a DELETE or UPDATE whose statement (from token index at, to the end of its parenthesis) has no WHERE.
func (c *classifier) destructiveDML(info *Info, at int) {
	verb := c.word(at)
	if verb != "DELETE" && verb != "UPDATE" {
		return
	}
	depth := 0
	for i := at + 1; i < len(c.toks); i++ {
		t := c.toks[i]
		if t.kind == tokPunct {
			switch c.src[t.start] {
			case '(':
				depth++
			case ')':
				depth--
				if depth < 0 {
					info.Destructive, info.Why = true, verb+" without WHERE"
					return
				}
			}
			continue
		}
		if depth == 0 && c.word(i) == "WHERE" {
			return
		}
	}
	info.Destructive, info.Why = true, verb+" without WHERE"
}

// pragma classifies SQLite's PRAGMA.
func (c *classifier) pragma(info *Info) {
	name := strings.ToLower(c.pragmaName())
	hasValue := false
	for _, t := range c.toks {
		if t.kind == tokPunct && (c.src[t.start] == '=' || c.src[t.start] == '(') {
			hasValue = true
			break
		}
	}
	switch {
	case actingPragmas[name]:
		info.Class = ClassWrite
	case !hasValue:
		info.Class, info.Rows = ClassRead, true
	case readingPragmas[name] && !c.hasEquals():
		info.Class, info.Rows = ClassRead, true
	default:
		info.Class, info.Rows = ClassWrite, true
	}
}

// pragmaName returns the name of the pragma, without a schema before it.
func (c *classifier) pragmaName() string {
	for i := 1; i < len(c.toks); i++ {
		t := c.toks[i]
		if t.kind == tokWord || t.kind == tokIdent {
			if i+2 < len(c.toks) && c.toks[i+1].kind == tokPunct && c.src[c.toks[i+1].start] == '.' {
				continue
			}
			return strings.Trim(t.text(c.src), "\"`[]")
		}
	}
	return ""
}

// hasEquals tells whether the statement has an equals sign outside strings.
func (c *classifier) hasEquals() bool {
	for _, t := range c.toks {
		if t.kind == tokPunct && c.src[t.start] == '=' {
			return true
		}
	}
	return false
}

// touchesReadOnly tells whether a SET changes whether the session can write.
func (c *classifier) touchesReadOnly() bool {
	for i := range c.toks {
		w := c.word(i)
		switch {
		case w == "WRITE" || w == "READ":
			return true
		case strings.Contains(w, "READ_ONLY") || strings.Contains(w, "READONLY") || strings.Contains(w, "QUERY_ONLY") || strings.Contains(w, "READ_WRITE"):
			return true
		case strings.HasPrefix(w, "DEFAULT_TRANSACTION") || strings.HasPrefix(w, "TRANSACTION_READ") || w == "TX_READ_ONLY":
			return true
		}
	}
	return false
}

// beginsTransaction tells whether BEGIN or START opens a transaction (and is not the beginning of a block).
func (c *classifier) beginsTransaction() bool {
	if c.word(0) == "START" {
		return c.word(1) == "TRANSACTION"
	}
	switch c.word(1) {
	case "", "TRANSACTION", "TRAN", "WORK", "DEFERRED", "IMMEDIATE", "EXCLUSIVE", "ISOLATION", "READ", "NOT", "DEFERRABLE", "DISTRIBUTED":
		return true
	}
	return false
}

// objectWord returns the word that names the kind of object after the verb (TABLE in DROP TABLE), skipping the words that qualify it.
func (c *classifier) objectWord(i int) string {
	for ; i < len(c.toks); i++ {
		switch w := c.word(i); w {
		case "TEMPORARY", "TEMP", "UNLOGGED", "GLOBAL", "LOCAL", "MATERIALIZED", "UNIQUE", "CLUSTERED", "NONCLUSTERED", "OR", "REPLACE", "CONCURRENTLY":
			continue
		case "TABLE", "VIEW", "INDEX", "SCHEMA", "DATABASE", "FUNCTION", "PROCEDURE", "TRIGGER", "SEQUENCE", "TYPE", "EXTENSION", "ROLE", "USER", "DOMAIN", "EVENT", "PROC":
			return w
		default:
			return ""
		}
	}
	return ""
}

// Allowed tells whether a statement of this class may run on a connection that is read-only.
func (i Info) Allowed() bool {
	switch i.Class {
	case ClassEmpty, ClassRead, ClassTransaction, ClassSession:
		return true
	}
	return false
}
