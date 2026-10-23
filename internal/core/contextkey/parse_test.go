package contextkey

import (
	"fmt"
	"strings"
	"testing"
)

func tokenName(t Token) string {
	switch t.Type {
	case TokRegexStr:
		return "RegexStr"
	case TokStr:
		return "Str"
	case TokQuotedStr:
		return "QuotedStr"
	case TokError:
		return "ErrorToken"
	}
	return t.String()
}

func hasLexeme(t Token) bool {
	return t.Type == TokRegexStr || t.Type == TokStr || t.Type == TokQuotedStr || t.Type == TokError
}

// Ported from VS Code: scanner.test.ts
func TestVSCodeScanner(t *testing.T) {
	for _, c := range scannerCases {
		t.Run(c.name, func(t *testing.T) {
			got := (&Scanner{}).Reset(c.input).Scan()
			if len(got) != len(c.want) {
				t.Fatalf("%d tokens, want %d: %+v", len(got), len(c.want), got)
			}
			for i, w := range c.want {
				g := got[i]
				if tokenName(g) != w.typ || g.Offset != w.offset || (hasLexeme(g) && g.Lexeme != w.lexeme) {
					t.Errorf("token %d = %s@%d %q, want %s@%d %q", i, tokenName(g), g.Offset, g.Lexeme, w.typ, w.offset, w.lexeme)
				}
			}
		})
	}
}

func parseToStr(input string) string {
	p := NewParser(ParserConfig{RegexErrorRecovery: true})
	var b strings.Builder
	expr := p.Parse(input)
	if expr != nil {
		return expr.Serialize()
	}
	lex := p.LexingErrors()
	if len(lex) > 0 {
		b.WriteString("Lexing errors:\n\n")
		for _, e := range lex {
			fmt.Fprintf(&b, "Unexpected token '%s' at offset %d. %s\n", e.Lexeme, e.Offset, e.AdditionalInfo)
		}
	}
	if len(p.ParsingErrors()) > 0 {
		if len(lex) > 0 {
			b.WriteString("\n --- \n")
		}
		b.WriteString("Parsing errors:\n\n")
		for _, e := range p.ParsingErrors() {
			fmt.Fprintf(&b, "Unexpected '%s' at offset %d.\n", e.Lexeme, e.Offset)
		}
	}
	return b.String()
}

// Ported from VS Code: parser.test.ts
func TestVSCodeParser(t *testing.T) {
	for _, c := range parserCases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseToStr(c.input); got != c.want {
				t.Errorf("parse(%q):\n got %q\nwant %q", c.input, got, c.want)
			}
		})
	}
}
