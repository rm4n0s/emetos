package workbench

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/fileio"
)

func sel(al, ac, bl, bc int) buffer.Selection {
	return buffer.Selection{Anchor: buffer.Position{Line: al, Col: ac}, Active: buffer.Position{Line: bl, Col: bc}}
}

func TestSelectionLabel(t *testing.T) {
	tests := []struct {
		selections, line, col, chars int
		want                         string
	}{
		{0, 1, 1, 0, ""},
		{1, 1, 1, 0, "Ln 1, Col 1"},
		{1, 12, 5, 0, "Ln 12, Col 5"},
		{1, 3, 9, 6, "Ln 3, Col 9 (6 selected)"},
		{2, 1, 1, 0, "2 selections"},
		{3, 4, 4, 0, "3 selections"},
		{2, 1, 1, 10, "2 selections (10 characters selected)"},
	}
	for _, tt := range tests {
		if got := SelectionLabel(tt.selections, tt.line, tt.col, tt.chars); got != tt.want {
			t.Errorf("SelectionLabel(%d, %d, %d, %d) = %q, want %q", tt.selections, tt.line, tt.col, tt.chars, got, tt.want)
		}
	}
}

func TestSelectionStatus(t *testing.T) {
	text := buffer.New("hello world\n\tindented\nüñí é\n😀 x\nlast").Snapshot()
	tests := []struct {
		name string
		sels []buffer.Selection
		tab  int
		eol  fileio.LineEnding
		want string
	}{
		{"no selection", nil, 4, fileio.LF, ""},
		{"a cursor", []buffer.Selection{sel(0, 5, 0, 5)}, 4, fileio.LF, "Ln 1, Col 6"},
		{"a cursor after a tab counts the tab stop", []buffer.Selection{sel(1, 1, 1, 1)}, 4, fileio.LF, "Ln 2, Col 5"},
		{"a tab of 8", []buffer.Selection{sel(1, 1, 1, 1)}, 8, fileio.LF, "Ln 2, Col 9"},
		{"after the tab and two letters", []buffer.Selection{sel(1, 3, 1, 3)}, 4, fileio.LF, "Ln 2, Col 7"},
		{"accents are one column each, two bytes long", []buffer.Selection{sel(2, 6, 2, 6)}, 4, fileio.LF, "Ln 3, Col 4"},
		{"an astral character is one column", []buffer.Selection{sel(3, 4, 3, 4)}, 4, fileio.LF, "Ln 4, Col 2"},
		{"a selection on one line counts code points", []buffer.Selection{sel(2, 0, 2, 6)}, 4, fileio.LF, "Ln 3, Col 4 (3 selected)"},
		{"a backwards selection", []buffer.Selection{sel(0, 5, 0, 0)}, 4, fileio.LF, "Ln 1, Col 1 (5 selected)"},
		{"an astral character is one character", []buffer.Selection{sel(3, 0, 3, 4)}, 4, fileio.LF, "Ln 4, Col 2 (1 selected)"},
		{"a selection over two lines counts the line break", []buffer.Selection{sel(0, 6, 1, 0)}, 4, fileio.LF, "Ln 2, Col 1 (6 selected)"},
		{"and two for CRLF", []buffer.Selection{sel(0, 6, 1, 0)}, 4, fileio.CRLF, "Ln 2, Col 1 (7 selected)"},
		{"three lines of CRLF", []buffer.Selection{sel(0, 6, 2, 0)}, 4, fileio.CRLF, "Ln 3, Col 1 (18 selected)"},
		{"two cursors", []buffer.Selection{sel(0, 1, 0, 1), sel(1, 1, 1, 1)}, 4, fileio.LF, "2 selections"},
		{"two selections", []buffer.Selection{sel(0, 0, 0, 2), sel(4, 0, 4, 4)}, 4, fileio.LF, "2 selections (6 characters selected)"},
		{"the whole text", []buffer.Selection{sel(0, 0, 4, 4)}, 4, fileio.LF, "Ln 5, Col 5 (36 selected)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var primary buffer.Selection
			if len(tt.sels) > 0 {
				primary = tt.sels[0]
			}
			if got := SelectionStatus(text, tt.sels, primary, tt.tab, tt.eol); got != tt.want {
				t.Errorf("SelectionStatus = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCharacterCount(t *testing.T) {
	text := buffer.New("ab\ncd\n\nef").Snapshot()
	tests := []struct {
		s    buffer.Selection
		eol  fileio.LineEnding
		want int
	}{
		{sel(0, 0, 0, 0), fileio.LF, 0},
		{sel(0, 0, 0, 2), fileio.LF, 2},
		{sel(0, 1, 1, 1), fileio.LF, 3},
		{sel(0, 1, 1, 1), fileio.CRLF, 4},
		{sel(0, 0, 3, 2), fileio.LF, 9},
		{sel(0, 0, 3, 2), fileio.CRLF, 12},
		{sel(3, 2, 0, 0), fileio.CRLF, 12},
		{sel(0, 0, 0, 99), fileio.LF, 2},
		{sel(0, 0, 0, 1), "", 1},
	}
	for _, tt := range tests {
		if got := CharacterCount(text, tt.s, tt.eol); got != tt.want {
			t.Errorf("CharacterCount(%v, %q) = %d, want %d", tt.s, tt.eol, got, tt.want)
		}
	}
}

func TestIndentationAndLineEndingLabels(t *testing.T) {
	if got := IndentationLabel(4, true); got != "Spaces: 4" {
		t.Errorf("spaces: %q", got)
	}
	if got := IndentationLabel(8, false); got != "Tab Size: 8" {
		t.Errorf("tabs: %q", got)
	}
	for id, want := range map[string]string{
		"utf8": "UTF-8", "utf8bom": "UTF-8 with BOM", "utf16le": "UTF-16 LE", "utf16be": "UTF-16 BE",
		"windows1252": "Windows 1252", "iso88591": "ISO 8859-1", "shiftjis": "Shift JIS", "unknown-thing": "unknown-thing",
	} {
		if got := EncodingLabel(id); got != want {
			t.Errorf("EncodingLabel(%q) = %q, want %q", id, got, want)
		}
	}
	if EOLLabel(fileio.LF) != "LF" || EOLLabel(fileio.CRLF) != "CRLF" || EOLLabel("") != "LF" {
		t.Error("line ending labels")
	}
}

func TestProblemsLabels(t *testing.T) {
	tests := []struct {
		c       ProblemCounts
		text    string
		tooltip string
	}{
		{ProblemCounts{}, "$(error) 0 $(warning) 0", "No Problems"},
		{ProblemCounts{Errors: 1, Warnings: 2}, "$(error) 1 $(warning) 2", "Errors: 1, Warnings: 2"},
		{ProblemCounts{Warnings: 5}, "$(error) 0 $(warning) 5", "Warnings: 5"},
		{ProblemCounts{Errors: 1, Infos: 3}, "$(error) 1 $(warning) 0 $(info) 3", "Errors: 1, Infos: 3"},
		{ProblemCounts{Errors: 999, Warnings: 1000}, "$(error) 999 $(warning) 1K", "Errors: 999, Warnings: 1000"},
		{ProblemCounts{Errors: 4321, Warnings: 9999}, "$(error) 4K $(warning) 9K", "Errors: 4321, Warnings: 9999"},
		{ProblemCounts{Errors: 10000, Warnings: 12345}, "$(error) 10K+ $(warning) 10K+", "Errors: 10000, Warnings: 12345"},
		{ProblemCounts{Errors: 1, Warnings: 1, Infos: 1}, "$(error) 1 $(warning) 1 $(info) 1", "Errors: 1, Warnings: 1, Infos: 1"},
	}
	for _, tt := range tests {
		if got := ProblemsText(tt.c); got != tt.text {
			t.Errorf("ProblemsText(%+v) = %q, want %q", tt.c, got, tt.text)
		}
		if got := ProblemsTooltip(tt.c); got != tt.tooltip {
			t.Errorf("ProblemsTooltip(%+v) = %q, want %q", tt.c, got, tt.tooltip)
		}
	}
}
