package edit

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// participate runs the save participants over a text with the cursors given and applies the result to a document,
// the way the editor does, returning the text and the selections after.
func participate(t *testing.T, text string, sels []buffer.Selection, p SaveParticipants, autoSave bool) (string, []buffer.Selection) {
	t.Helper()
	doc := document.New(document.FileURI("/t/f.txt"), text, document.NewOptions())
	res := NewCommands(NewModel(doc.Snapshot()), DefaultOptions()).SaveParticipants(sels, p, autoSave)
	if len(res.Edits) == 0 {
		return doc.Text(), sels
	}
	if _, err := doc.Edit(res.Edits, document.EditOptions{Kind: res.Kind}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	return doc.Text(), res.Selections
}

func atPos(line, col int) buffer.Selection {
	return buffer.Selection{Anchor: buffer.Position{Line: line, Col: col}, Active: buffer.Position{Line: line, Col: col}}
}

var (
	trim     = SaveParticipants{TrimTrailingWhitespace: true}
	finalNL  = SaveParticipants{InsertFinalNewline: true}
	trimNLs  = SaveParticipants{TrimFinalNewlines: true}
	allThree = SaveParticipants{TrimTrailingWhitespace: true, InsertFinalNewline: true, TrimFinalNewlines: true}
)

// Ported from VS Code: src/vs/workbench/contrib/codeEditor/test/browser/saveParticipant.test.ts ('insert final new line',
// 'trim final new lines', 'trim final new lines bug#46075', 'trim whitespace'), with the cases added for the other
// combinations; the buffer holds "\n", so the line ending of the file does not matter here.
func TestSaveParticipantsTexts(t *testing.T) {
	tests := []struct {
		name string
		p    SaveParticipants
		in   string
		want string
	}{
		{"final newline: nothing for an empty document", finalNL, "", ""},
		{"final newline: not when the last line is empty already", finalNL, "Hello New Line\n", "Hello New Line\n"},
		{"final newline: added to one line", finalNL, "Hello New Line", "Hello New Line\n"},
		{"final newline: added to several lines", finalNL, "Hello\nNew\nLine", "Hello\nNew\nLine\n"},
		{"final newline: not after a last line of white space", finalNL, "a\n  \t", "a\n  \t"},
		{"final newline: kept white space is not trimmed by it", finalNL, "a  ", "a  \n"},
		{"final newline: a document of one line break", finalNL, "\n", "\n"},

		{"trim newlines: a last line with text", trimNLs, "Trim New Line", "Trim New Line"},
		{"trim newlines: one final newline stays", trimNLs, "Trim New Line\n", "Trim New Line\n"},
		{"trim newlines: two become one", trimNLs, "Trim New Line\n\n", "Trim New Line\n"},
		{"trim newlines: many become one", trimNLs, "Trim New Line\nTrim New Line\n\n\n", "Trim New Line\nTrim New Line\n"},
		{"trim newlines: blank lines inside stay", trimNLs, "a\n\n\nb\n\n", "a\n\n\nb\n"},
		{"trim newlines: a line of spaces is not empty without the trimming of white space", trimNLs, "a\n  \n\n", "a\n  \n"},
		{"trim newlines: only newlines leave the empty text", trimNLs, "\n\n\n", ""},
		{"trim newlines: one line", trimNLs, "", ""},
		{"trim newlines: two empty lines leave an empty line", trimNLs, "\n", ""},
		{"trim newlines: idempotent", trimNLs, "Test\n", "Test\n"},

		{"trim whitespace: spaces and a tab", trim, "Test \t", "Test"},
		{"trim whitespace: every line", trim, "a  \nb\t\n  \nc \t \n", "a\nb\n\nc\n"},
		{"trim whitespace: leading white space stays", trim, "  a  ", "  a"},
		{"trim whitespace: other characters stay", trim, "a ", "a "},

		{"all: nothing to do", allThree, "a\nb\n", "a\nb\n"},
		{"all: trimming then the final newline", allThree, "a  \nb \t", "a\nb\n"},
		{"all: trailing blank lines and spaces", allThree, "a  \n  \n\t\n\n", "a\n"},
		{"all: a document of white space", allThree, "  \n \n", ""},
		{"all: empty", allThree, "", ""},
		{"trim whitespace and final newline", SaveParticipants{TrimTrailingWhitespace: true, InsertFinalNewline: true}, "a \n b ", "a\n b\n"},
		{"trim whitespace and trim newlines keep the blank lines of white space away", SaveParticipants{TrimTrailingWhitespace: true, TrimFinalNewlines: true}, "a\n   \n\n", "a\n"},
		{"final newline and trim newlines", SaveParticipants{InsertFinalNewline: true, TrimFinalNewlines: true}, "a\n\n\n", "a\n"},
		{"none", SaveParticipants{}, "a  \n\n\n", "a  \n\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := participate(t, tt.in, nil, tt.p, false); got != tt.want {
				t.Errorf("%q -> %q, want %q", tt.in, got, tt.want)
			}
			// saving again changes nothing
			if again, _ := participate(t, tt.want, nil, tt.p, false); again != tt.want {
				t.Errorf("a second save of %q gives %q", tt.want, again)
			}
		})
	}
}

func TestSaveParticipantsAutoSaveKeepsTheCursorLines(t *testing.T) {
	tests := []struct {
		name string
		p    SaveParticipants
		in   string
		sels []buffer.Selection
		want string
	}{
		{"white space in front of the cursor stays", trim, "ab  \ncd  ", []buffer.Selection{atPos(0, 3)}, "ab \ncd"},
		{"white space after the cursor goes", trim, "ab  ", []buffer.Selection{atPos(0, 1)}, "ab"},
		{"a cursor at the end of the white space keeps it all", trim, "ab  ", []buffer.Selection{atPos(0, 4)}, "ab  "},
		{"the rightmost cursor of a line decides", trim, "ab   ", []buffer.Selection{atPos(0, 3), atPos(0, 4)}, "ab  "},
		{"every cursor line", trim, "a \nb \nc ", []buffer.Selection{atPos(0, 2), atPos(2, 2)}, "a \nb\nc "},
		{"a selection counts by its active end", trim, "ab  ", []buffer.Selection{{Anchor: buffer.Position{Line: 0, Col: 0}, Active: buffer.Position{Line: 0, Col: 3}}}, "ab "},
		{"lines up to the cursor keep their final newlines", trimNLs, "a\n\n\n\n", []buffer.Selection{atPos(2, 0)}, "a\n\n\n"},
		{"a cursor above the blank lines changes nothing", trimNLs, "a\n\n\n", []buffer.Selection{atPos(0, 1)}, "a\n"},
		{"the cursor on the last line", trimNLs, "a\n\n\n", []buffer.Selection{atPos(3, 0)}, "a\n\n\n"},
		{"the final newline is added with the cursor at the end", finalNL, "a", []buffer.Selection{atPos(0, 1)}, "a\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := participate(t, tt.in, tt.sels, tt.p, true); got != tt.want {
				t.Errorf("%q -> %q, want %q", tt.in, got, tt.want)
			}
			// without autoSave the cursors do not matter
			explicit, _ := participate(t, tt.in, tt.sels, tt.p, false)
			plain, _ := participate(t, tt.in, nil, tt.p, false)
			if explicit != plain {
				t.Errorf("an explicit save depends on the cursors: %q and %q", explicit, plain)
			}
		})
	}
}

func TestSaveParticipantsKeepTheSelections(t *testing.T) {
	sel := func(l1, c1, l2, c2 int) buffer.Selection {
		return buffer.Selection{Anchor: buffer.Position{Line: l1, Col: c1}, Active: buffer.Position{Line: l2, Col: c2}}
	}
	tests := []struct {
		name string
		p    SaveParticipants
		in   string
		sels []buffer.Selection
		want []buffer.Selection
	}{
		{"cursor inside the text stays", trim, "ab  \ncd", []buffer.Selection{atPos(1, 1)}, []buffer.Selection{atPos(1, 1)}},
		{"cursor in trimmed white space goes to the end of the line", trim, "ab   ", []buffer.Selection{atPos(0, 4)}, []buffer.Selection{atPos(0, 2)}},
		{"a selection over trimmed white space shrinks", trim, "ab  \ncd  ", []buffer.Selection{sel(0, 1, 1, 4)}, []buffer.Selection{sel(0, 1, 1, 2)}},
		{"the cursor at the end of the last line stays in front of the new line break", finalNL, "ab", []buffer.Selection{atPos(0, 2)}, []buffer.Selection{atPos(0, 2)}},
		{"a cursor in deleted lines goes to the last line", trimNLs, "ab\n\n\n", []buffer.Selection{atPos(3, 0)}, []buffer.Selection{atPos(1, 0)}},
		{"a selection of the whole text", allThree, "ab \n\n", []buffer.Selection{sel(0, 0, 2, 0)}, []buffer.Selection{sel(0, 0, 1, 0)}},
		{"several cursors", trim, "a  \nb  \nc", []buffer.Selection{atPos(0, 0), atPos(1, 3), atPos(2, 1)}, []buffer.Selection{atPos(0, 0), atPos(1, 1), atPos(2, 1)}},
		{"no edit, no change", trim, "ab", []buffer.Selection{atPos(0, 1)}, []buffer.Selection{atPos(0, 1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := participate(t, tt.in, tt.sels, tt.p, false)
			if len(got) != len(tt.want) {
				t.Fatalf("selections = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("selection %d = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// The three participants are one step of the history: one undo restores the text from before the save.
func TestSaveParticipantsAreOneUndoStep(t *testing.T) {
	doc := document.New(document.FileURI("/t/f.txt"), "a  \nb \t\n\n\n", document.NewOptions())
	res := NewCommands(NewModel(doc.Snapshot()), DefaultOptions()).SaveParticipants(nil, allThree, false)
	if _, err := doc.Edit(res.Edits, document.EditOptions{Kind: res.Kind}); err != nil {
		t.Fatal(err)
	}
	if doc.Text() != "a\nb\n" {
		t.Fatalf("text = %q", doc.Text())
	}
	if _, err := doc.Undo(); err != nil {
		t.Fatal(err)
	}
	if doc.Text() != "a  \nb \t\n\n\n" || doc.CanUndo() {
		t.Errorf("after one undo %q (canUndo %v)", doc.Text(), doc.CanUndo())
	}
}

func TestSaveParticipantsAny(t *testing.T) {
	if (SaveParticipants{}).Any() || !trim.Any() || !finalNL.Any() || !trimNLs.Any() {
		t.Error("Any is wrong")
	}
}
