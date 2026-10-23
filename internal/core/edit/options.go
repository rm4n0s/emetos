package edit

import (
	"regexp"

	"github.com/rm4n0s/emetos/internal/core/autoclose"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/comment"
	"github.com/rm4n0s/emetos/internal/core/indent"
)

// Options is everything the cursor and editing functions read besides the text
// (SPEC §7.2). Later steps add fields; none of the functions take the settings
// one by one.
type Options struct {
	// TabSize is editor.tabSize.
	TabSize int
	// InsertSpaces is editor.insertSpaces.
	InsertSpaces bool
	// WordSeparators is the classifier of editor.wordSeparators.
	WordSeparators *WordClassifier
	// MultiCursorMergeOverlapping is editor.multiCursorMergeOverlapping: cursors
	// that overlap or touch become one.
	MultiCursorMergeOverlapping bool
	// UseTabStops is editor.useTabStops: Backspace in the indentation of a line
	// deletes back to the previous tab stop.
	UseTabStops bool
	// WordAtPosition finds the word the language defines at a byte column of a
	// line, as the language's wordPattern does (VS Code's model.getWordAtPosition).
	// It returns the start and end columns of the word touching col, and false when
	// there is none. Nil means VS Code's default word pattern.
	WordAtPosition func(line string, col int) (start, end int, ok bool)
	// Overtype is the overtype input mode: a typed character replaces the one after
	// the cursor.
	Overtype bool
	// AutoIndent is editor.autoIndent: what a new line, typing and pasting do with the
	// indentation (SPEC §8.6.2).
	AutoIndent indent.AutoIndent
	// AutoIndentOnPaste is editor.autoIndentOnPaste.
	AutoIndentOnPaste bool
	// AutoIndentOnPasteWithinString is editor.autoIndentOnPasteWithinString.
	AutoIndentOnPasteWithinString bool
	// Indenter applies the indentation rules and the on-enter rules of the languages, with
	// Tokens. Without them a language has no rules and a new line keeps the indentation of the
	// line it starts on.
	Indenter *indent.Indenter
	// Tokens gives the Indenter the tokens of the document.
	Tokens TokenSource
	// AutoClosingBrackets is editor.autoClosingBrackets.
	AutoClosingBrackets autoclose.Strategy
	// AutoClosingQuotes is editor.autoClosingQuotes.
	AutoClosingQuotes autoclose.Strategy
	// AutoClosingComments is editor.autoClosingComments.
	AutoClosingComments autoclose.Strategy
	// AutoClosingDelete is editor.autoClosingDelete.
	AutoClosingDelete autoclose.Behavior
	// AutoClosingOvertype is editor.autoClosingOvertype.
	AutoClosingOvertype autoclose.Behavior
	// AutoSurround is editor.autoSurround.
	AutoSurround autoclose.Surround
	// AutoClosing is the auto-closing and surrounding configuration of the language of the
	// document. Without it nothing is closed and nothing is surrounded.
	AutoClosing *autoclose.Language
	// AutoClosed are the closing characters the editor typed and that are still there (the
	// Character of each AutoClosed that earlier results reported and that the text and the cursors
	// have not made stale).
	AutoClosed []buffer.Range
	// Comments gives the comment tokens of the languages (SPEC §8.6.5); without it nothing can be
	// commented.
	Comments comment.Languages
	// CommentInsertSpace is editor.comments.insertSpace: a space goes between a comment token and
	// the text.
	CommentInsertSpace bool
	// CommentIgnoreEmptyLines is editor.comments.ignoreEmptyLines: lines with nothing on them get
	// no line comment.
	CommentIgnoreEmptyLines bool
	// UnmatchedClosing tells whether a closing bracket that closes nothing and that would close the
	// opening bracket text starts at or after a position; without it there is none.
	UnmatchedClosing func(pos buffer.Position, opening string) bool
}

// DefaultOptions are VS Code's defaults.
func DefaultOptions() Options {
	return Options{
		TabSize:                       4,
		InsertSpaces:                  true,
		WordSeparators:                NewWordClassifier(DefaultWordSeparators),
		MultiCursorMergeOverlapping:   true,
		UseTabStops:                   true,
		AutoIndent:                    indent.AutoIndentFull,
		AutoIndentOnPasteWithinString: true,
		CommentInsertSpace:            true,
		CommentIgnoreEmptyLines:       true,
	}
}

// tabSize is the tab size to use: a size below 1 counts as 1.
func (o Options) tabSize() int { return tabSizeOf(o.TabSize) }

// indentSize is the size of one indentation level. VS Code's editor.indentSize
// is "tabSize" by default, and Emetos has no separate setting.
func (o Options) indentSize() int { return o.tabSize() }

// words is the word classifier, the default one when none is set.
func (o Options) words() *WordClassifier {
	if o.WordSeparators == nil {
		return NewWordClassifier(DefaultWordSeparators)
	}
	return o.WordSeparators
}

// defaultWordPattern is VS Code's DEFAULT_WORD_REGEXP: a number with a fraction
// or a run of characters that are not separators or white space.
var defaultWordPattern = regexp.MustCompile("(-?\\d*\\.\\d\\w*)|([^`~!@#$%^&*()\\-=+\\[{\\]}\\\\|;:'\",.<>/?\\s]+)")

// wordAtPosition is the word of the language at col, or VS Code's default word.
func (o Options) wordAtPosition(line string, col int) (start, end int, ok bool) {
	if o.WordAtPosition != nil {
		return o.WordAtPosition(line, col)
	}
	for _, m := range defaultWordPattern.FindAllStringIndex(line, -1) {
		switch {
		case m[0] <= col && col <= m[1]:
			return m[0], m[1], true
		case m[0] > col:
			return 0, 0, false
		}
	}
	return 0, 0, false
}
