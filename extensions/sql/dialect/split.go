package dialect

import "strings"

// Statement is one statement of a script, as the engine is to receive it.
type Statement struct {
	// Text is the statement without the semicolon, the delimiter or the GO that ends it. It is exactly src[Start:End]: the comments
	// before it and inside it are kept (the engine ignores them, and a position it reports in the statement is a position in the
	// source after Start is added).
	Text string
	// Start and End are the bounds of Text in the source, in bytes.
	Start, End int
	// Batch numbers the batches of the script (SQL Server's GO ends one; every other engine has the one batch 0). The statements of
	// a batch run one after the other.
	Batch int
	// Repeat is how many times the batch of the statement is to run: the count of the GO that ended it ("GO 5"), else 1.
	Repeat int
}

// Split cuts a script into the statements the engine would see by its own rules (the dialect's Syntax):
//   - a semicolon ends a statement, except inside a string, a quoted name, a comment, a dollar-quoted body, and a
//     BEGIN ... END body of a SQLite trigger, of a PostgreSQL BEGIN ATOMIC function and of a SQL Server block;
//   - MySQL: a DELIMITER line changes what ends a statement, and the line is not a statement;
//   - SQL Server: a line GO (with a count, perhaps) ends a batch, and a CREATE or ALTER of a procedure, function, trigger or view
//     goes on to the end of its batch, semicolons or not;
//   - a piece with nothing but comments and white space is not a statement.
//
// The statements come in the order of the script.
func Split(d Dialect, src string) []Statement {
	syn := d.Syntax()
	toks := lex(src, syn)
	var out []Statement
	s := splitter{src: src, syn: syn, toks: toks}
	batch, from := 0, 0
	for i, t := range toks {
		switch t.kind {
		case tokEnd:
			if s.depth == 0 && !s.swallow {
				out = s.finish(out, t.start, batch)
				continue
			}
		case tokGo:
			out = s.finish(out, t.start, batch)
			for k := from; k < len(out); k++ {
				out[k].Repeat = t.count
			}
			if len(out) > from {
				batch++
			}
			from = len(out)
			continue
		case tokDelimiter:
			// the line is a command of the client: it ends the statement before it and is nothing itself
			out = s.finish(out, t.start, batch)
			continue
		}
		s.see(i, t)
	}
	return s.finish(out, len(src), batch)
}

// splitter is the state of Split: the statement being read.
type splitter struct {
	src  string
	syn  Syntax
	toks []token
	// begin is the index of the first token of the statement being read (a comment before it counts), count the tokens read since,
	// and lastSig the index of the last token that is not a comment or white space.
	begin   int
	count   int
	lastSig int
	// words are the first words of the statement, upper case, up to four.
	words []string
	// depth is the nesting of BEGIN/CASE ... END blocks; swallow says that a semicolon is part of the statement.
	depth   int
	swallow bool
	// afterEnd says that only white space (and no line end) has come since a statement was ended: a comment now is the end of that
	// statement's line and does not belong to the next statement.
	afterEnd bool
}

// reset starts a new statement state.
func (s *splitter) reset() {
	s.begin, s.count, s.lastSig = 0, 0, 0
	s.words = s.words[:0]
	s.depth, s.swallow = 0, false
}

// see takes a token into the statement being read.
func (s *splitter) see(i int, t token) {
	if s.count == 0 {
		switch {
		case t.kind == tokSpace:
			if strings.Contains(s.src[t.start:t.end], "\n") {
				s.afterEnd = false
			}
			return
		case t.kind == tokComment && s.afterEnd:
			return
		}
		s.afterEnd = false
	}
	if s.count == 0 {
		s.begin = i
	}
	s.count++
	if !t.significant() {
		return
	}
	s.lastSig = i
	if t.kind != tokWord {
		return
	}
	w := t.upper(s.src)
	if len(s.words) < 4 {
		s.words = append(s.words, w)
	}
	s.track(i, w)
}

// track follows the keywords that open and close a block.
func (s *splitter) track(i int, w string) {
	next := func() string {
		for j := i + 1; j < len(s.toks); j++ {
			if s.toks[j].significant() {
				return s.toks[j].upper(s.src)
			}
		}
		return ""
	}
	first := func() string {
		if len(s.words) > 0 {
			return s.words[0]
		}
		return ""
	}
	create := first() == "CREATE" || first() == "ALTER"
	switch {
	case s.syn.Batches && len(s.words) <= 4 && create && isBatchObject(s.words):
		s.swallow = true
	case s.syn.TriggerBlocks && create && s.hasWord("TRIGGER"):
		s.block(w)
	case s.syn.AtomicBlocks && create && (s.hasWord("FUNCTION") || s.hasWord("PROCEDURE")):
		if w == "BEGIN" && next() == "ATOMIC" {
			s.depth++
		} else if s.depth > 0 {
			s.block(w)
		}
	case s.syn.Batches && !s.swallow:
		switch w {
		case "BEGIN":
			switch next() {
			case "TRAN", "TRANSACTION", "DISTRIBUTED", "DIALOG", "CONVERSATION":
			default:
				s.depth++
			}
		case "CASE":
			s.depth++
		case "END":
			if s.depth > 0 && next() != "CONVERSATION" {
				s.depth--
			}
		}
	}
}

// block follows BEGIN, CASE and END inside a body that the statement's kind says has blocks.
func (s *splitter) block(w string) {
	switch w {
	case "BEGIN", "CASE":
		s.depth++
	case "END":
		if s.depth > 0 {
			s.depth--
		}
	}
}

// hasWord tells whether one of the first words of the statement is w.
func (s *splitter) hasWord(w string) bool {
	for _, x := range s.words {
		if x == w {
			return true
		}
	}
	return false
}

// isBatchObject tells whether the first words are CREATE or ALTER (OR ALTER) of an object whose body runs to the end of the batch.
func isBatchObject(words []string) bool {
	for _, w := range words[1:] {
		switch w {
		case "PROC", "PROCEDURE", "FUNCTION", "TRIGGER", "VIEW":
			return true
		case "OR", "ALTER":
			continue
		}
		return false
	}
	return false
}

// finish ends the statement being read at the source position end (the terminator's start), appending it when it holds anything but
// comments, and starts the next. batch is the number of the batch it belongs to.
func (s *splitter) finish(out []Statement, end, batch int) []Statement {
	if s.count > 0 && s.lastSig >= s.begin && s.hasSignificant() {
		start := s.toks[s.begin].start
		stop := s.toks[s.lastSig].end
		if stop > end {
			stop = end
		}
		start, stop = trimSpaceBounds(s.src, start, stop)
		// the comment that follows the last token, on the same statement, stays out: the text ends with the last token
		if start < stop {
			out = append(out, Statement{Text: s.src[start:stop], Start: start, End: stop, Batch: batch, Repeat: 1})
		}
	}
	s.reset()
	s.afterEnd = true
	return out
}

// hasSignificant tells whether the statement being read has a token that is not a comment.
func (s *splitter) hasSignificant() bool {
	for i := s.begin; i <= s.lastSig && i < len(s.toks); i++ {
		if s.toks[i].significant() {
			return true
		}
	}
	return false
}

// At returns the statement the cursor at the byte offset is in: the one that contains the offset, or the one that ends on the line
// of the cursor before it (the cursor is after the semicolon), else the next one, else the last. It reports false when the script has
// no statement. src is the script the statements were cut from.
func At(src string, stmts []Statement, offset int) (Statement, bool) {
	if len(stmts) == 0 {
		return Statement{}, false
	}
	offset = min(max(offset, 0), len(src))
	for i, st := range stmts {
		if offset >= st.Start && offset <= st.End {
			return st, true
		}
		if offset < st.Start {
			if i > 0 && !strings.Contains(src[stmts[i-1].End:offset], "\n") {
				return stmts[i-1], true
			}
			return st, true
		}
	}
	return stmts[len(stmts)-1], true
}
