package dialect

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Syntax says how an engine's SQL is written, as far as splitting a script into statements and reading the first words of a
// statement need it: which quotes, comments and block structures there are. It is data: the lexer reads it.
type Syntax struct {
	// DollarQuotes says that $tag$ ... $tag$ is a string (PostgreSQL).
	DollarQuotes bool
	// EscapeStrings says that E'...' is a string in which a backslash escapes the next character (PostgreSQL).
	EscapeStrings bool
	// BackslashEscapes says that a backslash escapes the next character in every string (MySQL, whose default mode is this one).
	BackslashEscapes bool
	// DoubleQuotedStrings says that "..." is a string, not an identifier (MySQL's default mode).
	DoubleQuotedStrings bool
	// HashComments says that # starts a comment that ends with the line (MySQL).
	HashComments bool
	// DashesNeedSpace says that -- starts a comment only when a space or a control character follows (MySQL).
	DashesNeedSpace bool
	// NestedComments says that /* */ comments nest (PostgreSQL, SQL Server).
	NestedComments bool
	// Brackets says that [...] quotes an identifier (SQL Server, SQLite).
	Brackets bool
	// Backticks says that `...` quotes an identifier (MySQL, SQLite).
	Backticks bool
	// DelimiterCommand says that a line DELIMITER x changes the end of a statement (MySQL's client).
	DelimiterCommand bool
	// GoSeparator says that a line with GO alone, and perhaps a count, ends a batch (SQL Server's tools).
	GoSeparator bool
	// TriggerBlocks says that CREATE TRIGGER has a BEGIN ... END body in which semicolons do not end the statement (SQLite).
	TriggerBlocks bool
	// AtomicBlocks says that BEGIN ATOMIC ... END is the body of a function or procedure (PostgreSQL 14).
	AtomicBlocks bool
	// Batches says that BEGIN ... END blocks are statements of their own and that CREATE PROCEDURE and its kin run to the end of
	// the batch (SQL Server).
	Batches bool
}

// tokenKind is the kind of a lexical token.
type tokenKind uint8

const (
	// tokSpace is white space.
	tokSpace tokenKind = iota
	// tokComment is a comment, - - , # or / * * /.
	tokComment
	// tokWord is a keyword or an unquoted identifier.
	tokWord
	// tokIdent is a quoted identifier.
	tokIdent
	// tokString is a string literal (also a dollar-quoted one).
	tokString
	// tokNumber is a number.
	tokNumber
	// tokPunct is any other character, one at a time.
	tokPunct
	// tokEnd ends a statement: the semicolon, or the delimiter that DELIMITER set.
	tokEnd
	// tokGo is a line GO or GO n.
	tokGo
	// tokDelimiter is a DELIMITER line; the token's count is not used and its text holds the new delimiter in value.
	tokDelimiter
)

// token is a piece of the source.
type token struct {
	kind       tokenKind
	start, end int
	// value is the new delimiter of a tokDelimiter; count is the repeat count of a tokGo (1 when none).
	value string
	count int
	// open says that a string, quoted identifier or comment is not closed.
	open bool
}

// text returns the source of the token.
func (t token) text(src string) string { return src[t.start:t.end] }

// isIdentByte tells whether a byte can be part of an unquoted identifier (a letter, a digit, an underscore, a dollar sign or part
// of a multi-byte character).
func isIdentByte(b byte) bool {
	return b == '_' || b == '$' || b >= 0x80 || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// isDigit tells whether the byte is 0 to 9.
func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// lex reads the source into tokens by the rules of the syntax. A token that is not closed runs to the end of the source and is
// marked open. The function never fails: whatever the text is, it is cut into tokens that together are the text.
func lex(src string, syn Syntax) []token {
	l := lexer{src: src, syn: syn, delim: ";"}
	return l.run()
}

// lexer is the state of lex.
type lexer struct {
	src    string
	syn    Syntax
	toks   []token
	i      int
	delim  string
	lineAt bool
}

// run reads every token.
func (l *lexer) run() []token {
	l.lineAt = true
	for l.i < len(l.src) {
		start := l.i
		c := l.src[l.i]
		switch c {
		case '\n':
			l.i++
			l.add(tokSpace, start)
			l.lineAt = true
			continue
		case ' ', '\t', '\r', '\f', '\v':
			for l.i < len(l.src) && (l.src[l.i] == ' ' || l.src[l.i] == '\t' || l.src[l.i] == '\r' || l.src[l.i] == '\f' || l.src[l.i] == '\v') {
				l.i++
			}
			l.add(tokSpace, start)
			continue
		}
		wasLineStart := l.lineAt
		l.lineAt = false
		switch {
		case l.delim != ";" && strings.HasPrefix(l.src[l.i:], l.delim):
			l.i += len(l.delim)
			l.add(tokEnd, start)
		case c == ';' && l.delim == ";":
			l.i++
			l.add(tokEnd, start)
		case c == '-' && l.hasPrefix("--") && (!l.syn.DashesNeedSpace || l.dashSpace()):
			l.lineComment(start)
		case c == '#' && l.syn.HashComments:
			l.lineComment(start)
		case c == '/' && l.hasPrefix("/*"):
			l.blockComment(start)
		case c == '\'':
			l.quoted(start, '\'', l.syn.BackslashEscapes, tokString)
		case c == '"':
			if l.syn.DoubleQuotedStrings {
				l.quoted(start, '"', l.syn.BackslashEscapes, tokString)
			} else {
				l.quoted(start, '"', false, tokIdent)
			}
		case c == '`' && l.syn.Backticks:
			l.quoted(start, '`', false, tokIdent)
		case c == '[' && l.syn.Brackets:
			l.bracket(start)
		case c == '$' && l.syn.DollarQuotes && l.dollarQuote(start):
		case isDigit(c):
			l.number(start)
		case isIdentByte(c):
			l.word(start, wasLineStart)
		default:
			_, size := utf8.DecodeRuneInString(l.src[l.i:])
			l.i += size
			l.add(tokPunct, start)
		}
	}
	return l.toks
}

// add appends the token that runs from start to the current position.
func (l *lexer) add(kind tokenKind, start int) {
	l.toks = append(l.toks, token{kind: kind, start: start, end: l.i})
}

// hasPrefix tells whether the source continues with s at the current position.
func (l *lexer) hasPrefix(s string) bool { return strings.HasPrefix(l.src[l.i:], s) }

// dashSpace tells whether the two dashes at the current position are followed by a space, a control character or the end.
func (l *lexer) dashSpace() bool {
	if l.i+2 >= len(l.src) {
		return true
	}
	c := l.src[l.i+2]
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c < 0x20
}

// lineComment reads to the end of the line, which it leaves.
func (l *lexer) lineComment(start int) {
	for l.i < len(l.src) && l.src[l.i] != '\n' {
		l.i++
	}
	l.add(tokComment, start)
}

// blockComment reads a /* */ comment.
func (l *lexer) blockComment(start int) {
	depth := 0
	for l.i < len(l.src) {
		switch {
		case l.hasPrefix("/*") && (depth == 0 || l.syn.NestedComments):
			depth++
			l.i += 2
		case l.hasPrefix("*/"):
			depth--
			l.i += 2
			if depth == 0 {
				l.add(tokComment, start)
				return
			}
		default:
			l.i++
		}
	}
	l.toks = append(l.toks, token{kind: tokComment, start: start, end: l.i, open: true})
}

// quoted reads a string or a quoted identifier closed by quote, in which the quote doubled is the quote itself and, when
// backslash is set, a backslash escapes the next character.
func (l *lexer) quoted(start int, quote byte, backslash bool, kind tokenKind) {
	l.i++
	for l.i < len(l.src) {
		c := l.src[l.i]
		switch {
		case backslash && c == '\\':
			l.i += 2
		case c == quote && l.i+1 < len(l.src) && l.src[l.i+1] == quote:
			l.i += 2
		case c == quote:
			l.i++
			l.add(kind, start)
			return
		default:
			l.i++
		}
	}
	l.i = len(l.src)
	l.toks = append(l.toks, token{kind: kind, start: start, end: l.i, open: true})
}

// bracket reads [identifier], in which ]] is a closing bracket in the name.
func (l *lexer) bracket(start int) {
	l.i++
	for l.i < len(l.src) {
		if l.src[l.i] == ']' {
			if l.i+1 < len(l.src) && l.src[l.i+1] == ']' {
				l.i += 2
				continue
			}
			l.i++
			l.add(tokIdent, start)
			return
		}
		l.i++
	}
	l.toks = append(l.toks, token{kind: tokIdent, start: start, end: l.i, open: true})
}

// dollarQuote reads $tag$ ... $tag$ when the text at the current position starts one, and reports whether it did. A $ that follows
// a letter or a digit is part of a name and never gets here (the word reader takes it).
func (l *lexer) dollarQuote(start int) bool {
	j := l.i + 1
	for j < len(l.src) && (l.src[j] == '_' || l.src[j] >= 0x80 || (l.src[j] >= 'a' && l.src[j] <= 'z') || (l.src[j] >= 'A' && l.src[j] <= 'Z') || (j > l.i+1 && isDigit(l.src[j]))) {
		j++
	}
	if j >= len(l.src) || l.src[j] != '$' {
		return false
	}
	tag := l.src[l.i : j+1]
	end := strings.Index(l.src[j+1:], tag)
	if end < 0 {
		l.i = len(l.src)
		l.toks = append(l.toks, token{kind: tokString, start: start, end: l.i, open: true})
		return true
	}
	l.i = j + 1 + end + len(tag)
	l.add(tokString, start)
	return true
}

// number reads a number: digits, a fraction, an exponent, and 0x digits.
func (l *lexer) number(start int) {
	for l.i < len(l.src) && (isIdentByte(l.src[l.i]) && l.src[l.i] != '$' || l.src[l.i] == '.') {
		l.i++
	}
	l.add(tokNumber, start)
}

// word reads a keyword or an identifier, and the lines the syntax gives a meaning: DELIMITER and GO. A string with a prefix (E'x',
// N'x', X'ab') is the word and then the string, except E'...' of PostgreSQL, which has escapes.
func (l *lexer) word(start int, lineStart bool) {
	for l.i < len(l.src) && isIdentByte(l.src[l.i]) {
		if l.delim != ";" && strings.HasPrefix(l.src[l.i:], l.delim) && l.i > start {
			break
		}
		l.i++
	}
	w := l.src[start:l.i]
	if lineStart {
		switch {
		case l.syn.DelimiterCommand && strings.EqualFold(w, "delimiter"):
			l.delimiterLine(start)
			return
		case l.syn.GoSeparator && strings.EqualFold(w, "go"):
			if n, ok := l.goLine(); ok {
				l.toks = append(l.toks, token{kind: tokGo, start: start, end: l.i, count: n})
				return
			}
		}
	}
	if l.syn.EscapeStrings && len(w) == 1 && (w[0] == 'e' || w[0] == 'E') && l.i < len(l.src) && l.src[l.i] == '\'' {
		l.quoted(start, '\'', true, tokString)
		return
	}
	l.add(tokWord, start)
}

// delimiterLine reads the rest of a DELIMITER line, which sets the new delimiter.
func (l *lexer) delimiterLine(start int) {
	for l.i < len(l.src) && l.src[l.i] != '\n' {
		l.i++
	}
	value := strings.TrimSpace(l.src[start+len("delimiter") : l.i])
	if value != "" && !strings.ContainsAny(value, " \t") {
		l.delim = value
	}
	l.toks = append(l.toks, token{kind: tokDelimiter, start: start, end: l.i, value: l.delim})
}

// goLine reads what follows GO on its line: nothing, a count, and a comment. It leaves the position after them, and reports false
// (leaving the position where it was) when the line has anything else, as GO could then be a name.
func (l *lexer) goLine() (count int, ok bool) {
	j := l.i
	for j < len(l.src) && (l.src[j] == ' ' || l.src[j] == '\t' || l.src[j] == '\r') {
		j++
	}
	count = 1
	k := j
	for k < len(l.src) && isDigit(l.src[k]) {
		k++
	}
	if k > j {
		n, err := strconv.Atoi(l.src[j:k])
		if err != nil || n < 1 {
			return 0, false
		}
		count = n
		j = k
		for j < len(l.src) && (l.src[j] == ' ' || l.src[j] == '\t' || l.src[j] == '\r') {
			j++
		}
	}
	if j+1 < len(l.src) && l.src[j] == '-' && l.src[j+1] == '-' {
		for j < len(l.src) && l.src[j] != '\n' {
			j++
		}
	}
	if j < len(l.src) && l.src[j] != '\n' {
		return 0, false
	}
	l.i = j
	return count, true
}

// significant tells whether the token is part of a statement's meaning (not white space and not a comment).
func (t token) significant() bool { return t.kind != tokSpace && t.kind != tokComment }

// upper returns the word of a token in capitals; "" for a token that is not a word.
func (t token) upper(src string) string {
	if t.kind != tokWord {
		return ""
	}
	return strings.ToUpper(src[t.start:t.end])
}

// trimSpaceBounds returns the bounds of the text between start and end without the white space at either end.
func trimSpaceBounds(src string, start, end int) (int, int) {
	for start < end {
		r, size := utf8.DecodeRuneInString(src[start:end])
		if !unicode.IsSpace(r) {
			break
		}
		start += size
	}
	for end > start {
		r, size := utf8.DecodeLastRuneInString(src[start:end])
		if !unicode.IsSpace(r) {
			break
		}
		end -= size
	}
	return start, end
}
