package jsonc

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/rm4n0s/errors"
)

// FormattingOptions say how to indent and break lines.
type FormattingOptions struct {
	// TabSize is the number of spaces of an indent when InsertSpaces is set; 0 means 4.
	// It also tells how wide a tab is when the indentation of existing text is measured.
	TabSize int
	// InsertSpaces indents with spaces; otherwise with tabs.
	InsertSpaces bool
	// EOL is the line break to use when the text has none to copy; "" means "\n".
	EOL string
}

func (o FormattingOptions) tabSize() int {
	if o.TabSize <= 0 {
		return 4
	}
	return o.TabSize
}

// Edit replaces Length bytes at Offset with Content: an insert when Length is 0, a
// removal when Content is empty.
type Edit struct {
	Offset, Length int
	Content        string
}

// Range is a part of a text, in bytes.
type Range struct {
	Offset, Length int
}

func isEOL(text string, offset int) bool {
	return text[offset] == '\r' || text[offset] == '\n'
}

// lineEnding is the first line break of text, or the one of the options.
func lineEnding(opts FormattingOptions, text string) string {
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				return "\r\n"
			}
			return "\r"
		case '\n':
			return "\n"
		}
	}
	if opts.EOL != "" {
		return opts.EOL
	}
	return "\n"
}

func computeIndentLevel(content string, opts FormattingOptions) int {
	chars := 0
	for i := 0; i < len(content); i++ {
		switch content[i] {
		case ' ':
			chars++
		case '\t':
			chars += opts.tabSize()
		default:
			return chars / opts.tabSize()
		}
	}
	return chars / opts.tabSize()
}

// Format returns the edits that indent and space the text, or the lines of the range
// when there is one. Comments are kept; text that is not valid JSON is left as it is
// where the formatter cannot tell what it means. The edits are in order and do not overlap.
//
// Ported from VS Code: src/vs/base/common/jsonFormatter.ts (format).
func Format(text string, rng *Range, opts FormattingOptions) []Edit {
	var (
		initialIndentLevel int
		formatText         string
		formatTextStart    int
		rangeStart         int
		rangeEnd           int
	)
	if rng != nil {
		rangeStart = max(0, min(rng.Offset, len(text)))
		rangeEnd = max(rangeStart, min(rng.Offset+rng.Length, len(text)))
		formatTextStart = rangeStart
		for formatTextStart > 0 && !isEOL(text, formatTextStart-1) {
			formatTextStart--
		}
		endOffset := rangeEnd
		for endOffset < len(text) && !isEOL(text, endOffset) {
			endOffset++
		}
		formatText = text[formatTextStart:endOffset]
		initialIndentLevel = computeIndentLevel(formatText, opts)
	} else {
		formatText = text
		rangeEnd = len(text)
	}
	eol := lineEnding(opts, text)

	lineBreak := false
	indentLevel := 0
	indentValue := "\t"
	if opts.InsertSpaces {
		indentValue = strings.Repeat(" ", opts.tabSize())
	}

	sc := NewScanner(formatText, false)
	hasError := false

	newLineAndIndent := func() string {
		return eol + strings.Repeat(indentValue, max(0, initialIndentLevel+indentLevel))
	}
	scanNext := func() SyntaxKind {
		token := sc.Scan()
		lineBreak = false
		for token == Trivia || token == LineBreakTrivia {
			lineBreak = lineBreak || token == LineBreakTrivia
			token = sc.Scan()
		}
		hasError = token == Unknown || sc.Error() != ScanNone
		return token
	}
	var edits []Edit
	addEdit := func(content string, start, end int) {
		if !hasError && start < rangeEnd && end > rangeStart && text[start:end] != content {
			edits = append(edits, Edit{Offset: start, Length: end - start, Content: content})
		}
	}

	first := scanNext()
	if first != EOF {
		addEdit(strings.Repeat(indentValue, initialIndentLevel), formatTextStart, sc.Offset()+formatTextStart)
	}
	for first != EOF {
		firstEnd := sc.Offset() + sc.Length() + formatTextStart
		second := scanNext()

		replace := ""
		for !lineBreak && (second == LineCommentTrivia || second == BlockCommentTrivia) {
			// Comments on the same line stay there, with one space before.
			addEdit(" ", firstEnd, sc.Offset()+formatTextStart)
			firstEnd = sc.Offset() + sc.Length() + formatTextStart
			if second == LineCommentTrivia {
				replace = newLineAndIndent()
			} else {
				replace = ""
			}
			second = scanNext()
		}

		switch second {
		case CloseBraceToken:
			if first != OpenBraceToken {
				indentLevel--
				replace = newLineAndIndent()
			}
		case CloseBracketToken:
			if first != OpenBracketToken {
				indentLevel--
				replace = newLineAndIndent()
			}
		default:
			switch first {
			case OpenBracketToken, OpenBraceToken:
				indentLevel++
				replace = newLineAndIndent()
			case CommaToken, LineCommentTrivia:
				replace = newLineAndIndent()
			case BlockCommentTrivia:
				if lineBreak {
					replace = newLineAndIndent()
				} else {
					// A symbol after a comment on the same line stays there, a space apart.
					replace = " "
				}
			case ColonToken:
				replace = " "
			case StringLiteral, NullKeyword, TrueKeyword, FalseKeyword, NumericLiteral, CloseBraceToken, CloseBracketToken:
				switch {
				case first == StringLiteral && second == ColonToken:
					replace = ""
				case second == LineCommentTrivia || second == BlockCommentTrivia:
					replace = " "
				case second != CommaToken && second != EOF:
					hasError = true
				}
			case Unknown:
				hasError = true
			}
			if lineBreak && (second == LineCommentTrivia || second == BlockCommentTrivia) {
				replace = newLineAndIndent()
			}
		}
		addEdit(replace, firstEnd, sc.Offset()+formatTextStart)
		first = second
	}
	return edits
}

// ToFormattedString writes a value as JSON, indented as the options say.
//
// Ported from VS Code: src/vs/base/common/jsonFormatter.ts (toFormattedString).
func ToFormattedString(value any, opts FormattingOptions) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	indent := "\t"
	if opts.InsertSpaces {
		indent = strings.Repeat(" ", opts.tabSize())
	}
	enc.SetIndent("", indent)
	if err := enc.Encode(value); err != nil {
		return "", errors.NewErr("JSONCEncodeFailed", err)
	}
	content := strings.TrimSuffix(buf.String(), "\n")
	if opts.EOL != "" {
		content = strings.NewReplacer("\r\n", opts.EOL, "\r", opts.EOL, "\n", opts.EOL).Replace(content)
	}
	return content, nil
}
