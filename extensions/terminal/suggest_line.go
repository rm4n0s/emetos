package terminal

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// The command line being typed (SPEC §10.25 "Terminal IntelliSense"): what the cursor is in, split the way a shell splits it, so
// that the providers know whether a command, an argument or a file is being completed.

// sugWord is a word of the command line: its value with the quotes and escapes taken out, and where its text is in the line.
type sugWord struct {
	text       string
	start, end int
}

// lineContext is the command line up to the cursor.
type lineContext struct {
	// line is the whole line and cursor the byte offset of the cursor in it.
	line   string
	cursor int
	// cmdStart is where the current command starts: after the last |, &&, ;, ( or line break before the cursor, and the
	// blanks after it.
	cmdStart int
	// words are the words of the current command before the word being typed, not counting the variable assignments that
	// start it and the targets of redirections; the first one is the command.
	words []sugWord
	// cur is the word being typed, from its first character to the cursor ("" at the cursor when it follows a blank). Its
	// text is without quotes; quote is the quote that is still open in it (0 for none).
	cur   sugWord
	quote byte
	// command says that cur is the first word of a command, redirect that it is the target of a redirection.
	command, redirect bool
}

// redirectWord matches the operators that redirect: > >> < << <<< 2> &> >& 2>&1 and the like.
var redirectWord = regexp.MustCompile(`^(?:[0-9]*[<>]+&?[0-9-]*|&>>?)$`)

// assignWord matches a variable assignment at the start of a command: NAME=value.
var assignWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// isSeparatorAt returns the length of the operator at s[i] after which a new command starts (0 when there is none).
func isSeparatorAt(s string, i int) (n int) {
	switch s[i] {
	case ';', '\n', '(':
		return 1
	case '|':
		if i+1 < len(s) && (s[i+1] == '|' || s[i+1] == '&') {
			return 2
		}
		return 1
	case '&':
		if i+1 < len(s) && s[i+1] == '&' {
			return 2
		}
		// &> and >& redirect, a lone & ends a command
		if i+1 < len(s) && s[i+1] == '>' || i > 0 && s[i-1] == '>' {
			return 0
		}
		return 1
	}
	return 0
}

// newLineContext reads the command line up to the cursor (a byte offset).
func newLineContext(line string, cursor int) lineContext {
	cursor = min(max(cursor, 0), len(line))
	for cursor > 0 && cursor < len(line) && !utf8.RuneStart(line[cursor]) {
		cursor--
	}
	lc := lineContext{line: line, cursor: cursor}
	before := line[:cursor]

	// the start of the current command: after the last separator that is not quoted or escaped
	start := 0
	var quote byte
	for i := 0; i < len(before); i++ {
		c := before[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
		case c == '\\' && i+1 < len(before):
			i++
		case quote == '"':
			if c == '"' {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '`':
			start = i + 1
		case c == '$' && i+1 < len(before) && before[i+1] == '(':
			start = i + 2
			i++
		default:
			if n := isSeparatorAt(before, i); n > 0 {
				start = i + n
				i += n - 1
			}
		}
	}
	for start < len(before) && (before[start] == ' ' || before[start] == '\t') {
		start++
	}
	lc.cmdStart = start

	// the words of the current command
	var words []sugWord
	i := start
	for i < len(before) {
		for i < len(before) && (before[i] == ' ' || before[i] == '\t') {
			i++
		}
		if i >= len(before) {
			break
		}
		w, q, next := readWord(before, i)
		words = append(words, w)
		lc.quote = q
		i = next
	}
	// the last word is the one being typed unless the line ends with a blank (outside a quote)
	endsInBlank := len(before) > start && (before[len(before)-1] == ' ' || before[len(before)-1] == '\t') && lc.quote == 0 &&
		!endsEscaped(before)
	if len(words) > 0 && !endsInBlank {
		lc.cur = words[len(words)-1]
		words = words[:len(words)-1]
	} else {
		lc.cur = sugWord{start: cursor, end: cursor}
		lc.quote = 0
	}

	// leave out the assignments that start the command and the targets of redirections
	redirectNext := false
	for _, w := range words {
		switch {
		case redirectNext:
			redirectNext = false
		case redirectWord.MatchString(w.text):
			redirectNext = true
		case len(lc.words) == 0 && assignWord.MatchString(w.text):
		default:
			lc.words = append(lc.words, w)
		}
	}
	switch {
	case redirectNext:
		lc.redirect = true
	case redirectWord.MatchString(lc.cur.text) && lc.quote == 0 && lc.cur.end == lc.cursor && lc.cur.start != lc.cursor:
		// an operator being typed is not a word to complete
		lc.redirect = false
	}
	if len(lc.words) == 0 && !lc.redirect {
		if loc := assignWord.FindStringIndex(lc.cur.text); loc != nil && lc.cur.text != "" {
			// VAR=value: the value is completed, like a file
			cut := loc[1]
			lc.cur.text = lc.cur.text[cut:]
			lc.cur.start += cut
			lc.command = false
		} else {
			lc.command = true
		}
	}
	return lc
}

// endsEscaped tells whether the text ends with a backslash that escapes the blank before it.
func endsEscaped(s string) bool {
	n := 0
	for i := len(s) - 2; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// readWord reads the word that starts at s[i] and returns it, the quote that is still open at its end and where it ends.
func readWord(s string, i int) (w sugWord, open byte, next int) {
	w.start = i
	var b strings.Builder
	var quote byte
	for ; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				b.WriteByte(c)
			}
		case quote == '"':
			switch {
			case c == '"':
				quote = 0
			case c == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0:
				i++
				b.WriteByte(s[i])
			default:
				b.WriteByte(c)
			}
		case c == '\\':
			if i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			}
		case c == '\'' || c == '"':
			quote = c
		case c == ' ' || c == '\t':
			w.text, w.end = b.String(), i
			return w, 0, i
		default:
			b.WriteByte(c)
		}
	}
	w.text, w.end = b.String(), i
	return w, quote, i
}

// escapeLabel puts a backslash before the characters a shell would take as syntax in a file name (VS Code's
// escapeTerminalCompletionLabel, with the blank too).
func escapeLabel(s string) string {
	if !strings.ContainsAny(s, "[]()'\"\\`*?;|&<> \t$#!{}~") {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if strings.ContainsRune("[]()'\"\\`*?;|&<> \t$#!{}", r) || r == '~' && i == 0 {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
