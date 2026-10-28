package terminal

import (
	"slices"
	"testing"
)

func wordTexts(ws []sugWord) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.text
	}
	return out
}

func TestLineContext(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		cursor       int // -1 is the end
		wantWords    []string
		wantCur      string
		wantCurStart int
		wantCmdStart int
		wantCommand  bool
		wantRedirect bool
		wantQuote    byte
	}{
		{"an empty line", "", -1, nil, "", 0, 0, true, false, 0},
		{"the first word", "gi", -1, nil, "gi", 0, 0, true, false, 0},
		{"after a blank", "git ", -1, []string{"git"}, "", 4, 0, false, false, 0},
		{"an argument", "git che", -1, []string{"git"}, "che", 4, 0, false, false, 0},
		{"two words and the third", "git commit -m", -1, []string{"git", "commit"}, "-m", 11, 0, false, false, 0},
		{"after a pipe", "ls | gr", -1, nil, "gr", 5, 5, true, false, 0},
		{"after and", "make && gi", -1, nil, "gi", 8, 8, true, false, 0},
		{"after a semicolon without a blank", "cd /tmp;ls", -1, nil, "ls", 8, 8, true, false, 0},
		{"inside a command substitution", "echo $(gi", -1, nil, "gi", 7, 7, true, false, 0},
		{"a pipe in quotes is not an operator", `echo "a | b" c`, -1, []string{"echo", "a | b"}, "c", 13, 0, false, false, 0},
		{"a variable before the command", "FOO=1 BAR=2 git st", -1, []string{"git"}, "st", 16, 0, false, false, 0},
		{"a variable assignment is completed as a path", "FOO=/us", -1, nil, "/us", 4, 0, false, false, 0},
		{"after a redirection", "echo hi > ou", -1, []string{"echo", "hi"}, "ou", 10, 0, false, true, 0},
		{"a redirection with a number", "cmd 2> er", -1, []string{"cmd"}, "er", 7, 0, false, true, 0},
		{"an escaped blank is part of the word", `cat my\ fi`, -1, []string{"cat"}, "my fi", 4, 0, false, false, 0},
		{"an open double quote", `cat "my fi`, -1, []string{"cat"}, "my fi", 4, 0, false, false, '"'},
		{"an open single quote", `cat 'a b`, -1, []string{"cat"}, "a b", 4, 0, false, false, '\''},
		{"the cursor in the middle", "git che out", 7, []string{"git"}, "che", 4, 0, false, false, 0},
		{"a trailing blank after an escaped blank", `cat a\ `, -1, []string{"cat"}, "a ", 4, 0, false, false, 0},
		{"a line break starts a command", "ls\ngi", -1, nil, "gi", 3, 3, true, false, 0},
		{"a background job", "sleep 1 & gi", -1, nil, "gi", 10, 10, true, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cursor := tt.cursor
			if cursor < 0 {
				cursor = len(tt.line)
			}
			lc := newLineContext(tt.line, cursor)
			if got := wordTexts(lc.words); !slices.Equal(got, tt.wantWords) && (len(got) != 0 || len(tt.wantWords) != 0) {
				t.Errorf("words = %q, want %q", got, tt.wantWords)
			}
			if lc.cur.text != tt.wantCur || lc.cur.start != tt.wantCurStart {
				t.Errorf("cur = %q at %d, want %q at %d", lc.cur.text, lc.cur.start, tt.wantCur, tt.wantCurStart)
			}
			if lc.cmdStart != tt.wantCmdStart {
				t.Errorf("cmdStart = %d, want %d", lc.cmdStart, tt.wantCmdStart)
			}
			if lc.command != tt.wantCommand || lc.redirect != tt.wantRedirect || lc.quote != tt.wantQuote {
				t.Errorf("command, redirect, quote = %v, %v, %q; want %v, %v, %q", lc.command, lc.redirect, lc.quote, tt.wantCommand, tt.wantRedirect, tt.wantQuote)
			}
		})
	}
}

func TestEscapeLabel(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain.txt", "plain.txt"},
		{"my file", `my\ file`},
		{"a(b)[c]", `a\(b\)\[c\]`},
		{"it's", `it\'s`},
		{"a&b;c|d", `a\&b\;c\|d`},
		{"~tilde", `\~tilde`},
		{"back\\slash", `back\\slash`},
	}
	for _, tt := range tests {
		if got := escapeLabel(tt.in); got != tt.want {
			t.Errorf("escapeLabel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
