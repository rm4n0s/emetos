package edit

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/comment"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// Ported from VS Code: reference/vscode/src/vs/editor/contrib/comment/test/browser/
// (lineCommentCommand.test.ts and blockCommentCommand.test.ts)

// commentRule is the comments of a test language (VS Code's CommentRule).
type commentRule struct {
	line     string
	noIndent bool
	bs, be   string
}

func (r commentRule) language() *comment.Language {
	return &comment.Language{Line: r.line, NoIndent: r.noIndent, BlockStart: r.bs, BlockEnd: r.be}
}

// commentLanguages gives the comment tokens of the languages of a test; code 0 is the language of
// the document.
type commentLanguages map[int]*comment.Language

func (l commentLanguages) Language(code int) *comment.Language {
	if lang, ok := l[code]; ok {
		return lang
	}
	return &comment.Language{}
}

// commentSource gives each line of a text the language that of(line) says.
type commentSource struct {
	of func(line int) int
}

func (s commentSource) LineTokens(line int) []tokens.Token {
	return []tokens.Token{{End: 1 << 20, Language: uint16(s.of(line))}}
}
func (s commentSource) Language() int                 { return 0 }
func (s commentSource) CheapToTokenize(line int) bool { return true }

// commentEd is an editor of lines whose language has the comment rule.
func commentEd(t *testing.T, rule commentRule, space, ignoreEmpty bool, lines ...string) *ed {
	t.Helper()
	e := newEd(t, DefaultOptions(), lines...)
	e.opts.Comments = commentLanguages{0: rule.language()}
	e.opts.Tokens = commentSource{of: func(int) int { return 0 }}
	e.opts.CommentInsertSpace, e.opts.CommentIgnoreEmptyLines = space, ignoreEmpty
	return e
}

type lineCommentCase struct {
	suite, name string
	kind        CommentKind
	cfg         commentRule
	space       bool
	ignoreEmpty bool
	lines       []string
	sel         sel4
	want        []string
	wantSel     sel4
}

var lineCommentCases = []lineCommentCase{
	{suite: "Editor Contrib - Line Comment Command", name: "comment single line", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"some text", "\tsome more text"}, sel: sel4{1, 1, 1, 1},
		want: []string{"!@# some text", "\tsome more text"}, wantSel: sel4{1, 5, 1, 5}},
	{suite: "Editor Contrib - Line Comment Command", name: "case insensitive", kind: CommentToggle, cfg: commentRule{line: "rem", noIndent: false, bs: "", be: ""}, space: true, ignoreEmpty: true,
		lines: []string{"REM some text"}, sel: sel4{1, 1, 1, 1},
		want: []string{"some text"}, wantSel: sel4{1, 1, 1, 1}},
	{suite: "Editor Contrib - Line Comment Command", name: "comment with token column fixed", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: true, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"some text", "\tsome more text"}, sel: sel4{2, 1, 2, 1},
		want: []string{"some text", "!@# \tsome more text"}, wantSel: sel4{2, 5, 2, 5}},
	{suite: "Editor Contrib - Line Comment Command", name: "detects indentation", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\tsome text", "\tsome more text"}, sel: sel4{2, 2, 1, 1},
		want: []string{"\t!@# some text", "\t!@# some more text"}, wantSel: sel4{2, 2, 1, 1}},
	{suite: "Editor Contrib - Line Comment Command", name: "detects mixed indentation", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\tsome text", "    some more text"}, sel: sel4{2, 2, 1, 1},
		want: []string{"\t!@# some text", "    !@# some more text"}, wantSel: sel4{2, 2, 1, 1}},
	{suite: "Editor Contrib - Line Comment Command", name: "ignores whitespace lines", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\tsome text", "\t   ", "", "\tsome more text"}, sel: sel4{4, 2, 1, 1},
		want: []string{"\t!@# some text", "\t   ", "", "\t!@# some more text"}, wantSel: sel4{4, 2, 1, 1}},
	{suite: "Editor Contrib - Line Comment Command", name: "removes its own", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\t!@# some text", "\t   ", "\t\t!@# some more text"}, sel: sel4{3, 2, 1, 1},
		want: []string{"\tsome text", "\t   ", "\t\tsome more text"}, wantSel: sel4{3, 2, 1, 1}},
	{suite: "Editor Contrib - Line Comment Command", name: "works in only whitespace", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\t    ", "\t", "\t\tsome more text"}, sel: sel4{3, 1, 1, 1},
		want: []string{"\t!@#     ", "\t!@# ", "\t\tsome more text"}, wantSel: sel4{3, 1, 1, 1}},
	{suite: "Editor Contrib - Line Comment Command", name: "bug 9697 - whitespace before comment token", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\t !@#first", "\tsecond line"}, sel: sel4{1, 1, 1, 1},
		want: []string{"\t first", "\tsecond line"}, wantSel: sel4{1, 1, 1, 1}},
	{suite: "Editor Contrib - Line Comment Command", name: "bug 10162 - line comment before caret", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"first!@#", "\tsecond line"}, sel: sel4{1, 1, 1, 1},
		want: []string{"!@# first!@#", "\tsecond line"}, wantSel: sel4{1, 5, 1, 5}},
	{suite: "Editor Contrib - Line Comment Command", name: "comment single line - leading whitespace", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"first!@#", "\tsecond line"}, sel: sel4{2, 3, 2, 1},
		want: []string{"first!@#", "\t!@# second line"}, wantSel: sel4{2, 7, 2, 1}},
	{suite: "Editor Contrib - Line Comment Command", name: "ignores invisible selection", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 1, 1, 1},
		want: []string{"!@# first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{2, 1, 1, 5}},
	{suite: "Editor Contrib - Line Comment Command", name: "multiple lines", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 4, 1, 1},
		want: []string{"!@# first", "!@# \tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{2, 8, 1, 5}},
	{suite: "Editor Contrib - Line Comment Command", name: "multiple modes on multiple lines", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{4, 4, 3, 1},
		want: []string{"first", "\tsecond line", "!@# third line", "!@# fourth line", "fifth"}, wantSel: sel4{4, 8, 3, 5}},
	{suite: "Editor Contrib - Line Comment Command", name: "toggle single line", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 1, 1, 1},
		want: []string{"!@# first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 5, 1, 5}},
	{suite: "Editor Contrib - Line Comment Command", name: "toggle single line", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"!@# first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 4, 1, 4},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 1, 1, 1}},
	{suite: "Editor Contrib - Line Comment Command", name: "toggle multiple lines", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 4, 1, 1},
		want: []string{"!@# first", "!@# \tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{2, 8, 1, 5}},
	{suite: "Editor Contrib - Line Comment Command", name: "toggle multiple lines", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"!@# first", "!@# \tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 7, 1, 4},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{2, 3, 1, 1}},
	{suite: "Editor Contrib - Line Comment Command", name: "issue #5964: Ctrl+/ to create comment when cursor is at the beginning of the line puts the cursor in a strange position", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 1, 1, 1},
		want: []string{"!@# first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 5, 1, 5}},
	{suite: "Editor Contrib - Line Comment Command", name: "issue #35673: Comment hotkeys throws the cursor before the comment", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"first", "", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 1, 2, 1},
		want: []string{"first", "!@# ", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{2, 5, 2, 5}},
	{suite: "Editor Contrib - Line Comment Command", name: "issue #35673: Comment hotkeys throws the cursor before the comment", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"first", "\t", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 2, 2, 2},
		want: []string{"first", "\t!@# ", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{2, 6, 2, 6}},
	{suite: "Editor Contrib - Line Comment Command", name: "issue #2837 \"Add Line Comment\" fault when blank lines involved", kind: CommentAdd, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"    if displayName == \"\":", "        displayName = groupName", "    description = getAttr(attributes, \"description\")", "    mailAddress = getAttr(attributes, \"mail\")", "", "    print \"||Group name|%s|\" % displayName", "    print \"||Description|%s|\" % description", "    print \"||Email address|[mailto:%s]|\" % mailAddress`"}, sel: sel4{1, 1, 8, 56},
		want: []string{"    !@# if displayName == \"\":", "    !@#     displayName = groupName", "    !@# description = getAttr(attributes, \"description\")", "    !@# mailAddress = getAttr(attributes, \"mail\")", "", "    !@# print \"||Group name|%s|\" % displayName", "    !@# print \"||Description|%s|\" % description", "    !@# print \"||Email address|[mailto:%s]|\" % mailAddress`"}, wantSel: sel4{1, 1, 8, 60}},
	{suite: "Editor Contrib - Line Comment Command", name: "issue #47004: Toggle comments shouldn't move cursor", kind: CommentAdd, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"    A line", "    Another line"}, sel: sel4{2, 7, 1, 1},
		want: []string{"    !@# A line", "    !@# Another line"}, wantSel: sel4{2, 11, 1, 1}},
	{suite: "Editor Contrib - Line Comment Command", name: "insertSpace false", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "", be: ""}, space: false, ignoreEmpty: true,
		lines: []string{"some text"}, sel: sel4{1, 1, 1, 1},
		want: []string{"!@#some text"}, wantSel: sel4{1, 4, 1, 4}},
	{suite: "Editor Contrib - Line Comment Command", name: "insertSpace false does not remove space", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "", be: ""}, space: false, ignoreEmpty: true,
		lines: []string{"!@#    some text"}, sel: sel4{1, 1, 1, 1},
		want: []string{"    some text"}, wantSel: sel4{1, 1, 1, 1}},
	{suite: "ignoreEmptyLines false", name: "does not ignore whitespace lines", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: false,
		lines: []string{"\tsome text", "\t   ", "", "\tsome more text"}, sel: sel4{4, 2, 1, 1},
		want: []string{"!@# \tsome text", "!@# \t   ", "!@# ", "!@# \tsome more text"}, wantSel: sel4{4, 6, 1, 5}},
	{suite: "ignoreEmptyLines false", name: "removes its own", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: false,
		lines: []string{"\t!@# some text", "\t   ", "\t\t!@# some more text"}, sel: sel4{3, 2, 1, 1},
		want: []string{"\tsome text", "\t   ", "\t\tsome more text"}, wantSel: sel4{3, 2, 1, 1}},
	{suite: "ignoreEmptyLines false", name: "works in only whitespace", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: false,
		lines: []string{"\t    ", "\t", "\t\tsome more text"}, sel: sel4{3, 1, 1, 1},
		want: []string{"\t!@#     ", "\t!@# ", "\t\tsome more text"}, wantSel: sel4{3, 1, 1, 1}},
	{suite: "ignoreEmptyLines false", name: "comments single line", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: false,
		lines: []string{"some text", "\tsome more text"}, sel: sel4{1, 1, 1, 1},
		want: []string{"!@# some text", "\tsome more text"}, wantSel: sel4{1, 5, 1, 5}},
	{suite: "ignoreEmptyLines false", name: "detects indentation", kind: CommentToggle, cfg: commentRule{line: "!@#", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: false,
		lines: []string{"\tsome text", "\tsome more text"}, sel: sel4{2, 2, 1, 1},
		want: []string{"\t!@# some text", "\t!@# some more text"}, wantSel: sel4{2, 2, 1, 1}},
	{suite: "Editor Contrib - Line Comment As Block Comment", name: "fall back to block comment command", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "(", be: ")"}, space: true, ignoreEmpty: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 1, 1, 1},
		want: []string{"( first )", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 3, 1, 3}},
	{suite: "Editor Contrib - Line Comment As Block Comment", name: "fall back to block comment command - toggle", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "(", be: ")"}, space: true, ignoreEmpty: true,
		lines: []string{"(first)", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 7, 1, 2},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 6, 1, 1}},
	{suite: "Editor Contrib - Line Comment As Block Comment", name: "bug 9513 - expand single line to uncomment auto block", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "(", be: ")"}, space: true, ignoreEmpty: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 1, 1, 1},
		want: []string{"( first )", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 3, 1, 3}},
	{suite: "Editor Contrib - Line Comment As Block Comment", name: "bug 9691 - always expand selection to line boundaries", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "(", be: ")"}, space: true, ignoreEmpty: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{3, 2, 1, 3},
		want: []string{"( first", "\tsecond line", "third line )", "fourth line", "fifth"}, wantSel: sel4{3, 2, 1, 5}},
	{suite: "Editor Contrib - Line Comment As Block Comment", name: "bug 9691 - always expand selection to line boundaries", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "(", be: ")"}, space: true, ignoreEmpty: true,
		lines: []string{"(first", "\tsecond line", "third line)", "fourth line", "fifth"}, sel: sel4{3, 11, 1, 2},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{3, 11, 1, 1}},
	{suite: "Editor Contrib - Line Comment As Block Comment 2", name: "no selection => uses indentation", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\t<!@#fifth#@!>\t\t"}, sel: sel4{1, 1, 1, 1},
		want: []string{"\t\t<!@# first\t     #@!>", "\t\tsecond line", "\tthird line", "fourth line", "\t\t<!@#fifth#@!>\t\t"}, wantSel: sel4{1, 1, 1, 1}},
	{suite: "Editor Contrib - Line Comment As Block Comment 2", name: "no selection => uses indentation", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\t\t<!@#first\t    #@!>", "\t\tsecond line", "\tthird line", "fourth line", "\t\t<!@#fifth#@!>\t\t"}, sel: sel4{1, 1, 1, 1},
		want: []string{"\t\tfirst\t   ", "\t\tsecond line", "\tthird line", "fourth line", "\t\t<!@#fifth#@!>\t\t"}, wantSel: sel4{1, 1, 1, 1}},
	{suite: "Editor Contrib - Line Comment As Block Comment 2", name: "can remove", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\t<!@#fifth#@!>\t\t"}, sel: sel4{5, 1, 5, 1},
		want: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\tfifth\t\t"}, wantSel: sel4{5, 1, 5, 1}},
	{suite: "Editor Contrib - Line Comment As Block Comment 2", name: "can remove", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\t<!@#fifth#@!>\t\t"}, sel: sel4{5, 3, 5, 3},
		want: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\tfifth\t\t"}, wantSel: sel4{5, 3, 5, 3}},
	{suite: "Editor Contrib - Line Comment As Block Comment 2", name: "can remove", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\t<!@#fifth#@!>\t\t"}, sel: sel4{5, 4, 5, 4},
		want: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\tfifth\t\t"}, wantSel: sel4{5, 3, 5, 3}},
	{suite: "Editor Contrib - Line Comment As Block Comment 2", name: "can remove", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\t<!@#fifth#@!>\t\t"}, sel: sel4{5, 16, 5, 3},
		want: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\tfifth\t\t"}, wantSel: sel4{5, 8, 5, 3}},
	{suite: "Editor Contrib - Line Comment As Block Comment 2", name: "can remove", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\t<!@#fifth#@!>\t\t"}, sel: sel4{5, 12, 5, 7},
		want: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\tfifth\t\t"}, wantSel: sel4{5, 8, 5, 3}},
	{suite: "Editor Contrib - Line Comment As Block Comment 2", name: "can remove", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\t<!@#fifth#@!>\t\t"}, sel: sel4{5, 18, 5, 18},
		want: []string{"\t\tfirst\t    ", "\t\tsecond line", "\tthird line", "fourth line", "\t\tfifth\t\t"}, wantSel: sel4{5, 10, 5, 10}},
	{suite: "Editor Contrib - Line Comment As Block Comment 2", name: "issue #993: Remove comment does not work consistently in HTML", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"     asd qwe", "     asd qwe", ""}, sel: sel4{1, 1, 3, 1},
		want: []string{"     <!@# asd qwe", "     asd qwe #@!>", ""}, wantSel: sel4{1, 1, 3, 1}},
	{suite: "Editor Contrib - Line Comment As Block Comment 2", name: "issue #993: Remove comment does not work consistently in HTML", kind: CommentToggle, cfg: commentRule{line: "", noIndent: false, bs: "<!@#", be: "#@!>"}, space: true, ignoreEmpty: true,
		lines: []string{"     <!@#asd qwe", "     asd qwe#@!>", ""}, sel: sel4{1, 1, 3, 1},
		want: []string{"     asd qwe", "     asd qwe", ""}, wantSel: sel4{1, 1, 3, 1}},
}

func runLineComment(e *ed, kind CommentKind) {
	switch kind {
	case CommentAdd:
		e.run(Commands.AddLineComment)
	case CommentRemove:
		e.run(Commands.RemoveLineComment)
	default:
		e.run(Commands.ToggleLineComment)
	}
}

func TestVSCodeLineComment(t *testing.T) {
	for _, tc := range lineCommentCases {
		t.Run(tc.suite+"/"+tc.name, func(t *testing.T) {
			e := commentEd(t, tc.cfg, tc.space, tc.ignoreEmpty, tc.lines...)
			e.at(tc.sel)
			runLineComment(e, tc.kind)
			e.assertText(strings.Join(tc.want, "\n"))
			e.assertSels(tc.wantSel)
		})
	}
}

type blockCommentCase struct {
	name    string
	space   bool
	lines   []string
	sel     sel4
	want    []string
	wantSel sel4
}

var blockCommentCases = []blockCommentCase{
	{name: "empty selection wraps itself", space: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 3, 1, 3},
		want: []string{"fi<0  0>rst", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 6, 1, 6}},
	{name: "invisible selection ignored", space: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 1, 1, 1},
		want: []string{"<0 first", " 0>\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 4, 2, 1}},
	{name: "bug9511", space: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 6, 1, 1},
		want: []string{"<0 first 0>", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 4, 1, 9}},
	{name: "bug9511", space: true,
		lines: []string{"<0first0>", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 8, 1, 3},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 1, 1, 6}},
	{name: "one line selection", space: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 6, 1, 3},
		want: []string{"fi<0 rst 0>", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 6, 1, 9}},
	{name: "one line selection toggle", space: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 6, 1, 3},
		want: []string{"fi<0 rst 0>", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 6, 1, 9}},
	{name: "one line selection toggle", space: true,
		lines: []string{"fi<0rst0>", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 8, 1, 5},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 3, 1, 6}},
	{name: "one line selection toggle", space: true,
		lines: []string{"<0 first 0>", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 10, 1, 1},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 1, 1, 6}},
	{name: "one line selection toggle", space: true,
		lines: []string{"<0 first0>", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 9, 1, 1},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 1, 1, 6}},
	{name: "one line selection toggle", space: true,
		lines: []string{"<0first 0>", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 9, 1, 1},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 1, 1, 6}},
	{name: "one line selection toggle", space: true,
		lines: []string{"fi<0rst0>", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{1, 8, 1, 5},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 3, 1, 6}},
	{name: "multi line selection", space: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 4, 1, 1},
		want: []string{"<0 first", "\tse 0>cond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 4, 2, 4}},
	{name: "multi line selection toggle", space: true,
		lines: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 4, 1, 1},
		want: []string{"<0 first", "\tse 0>cond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 4, 2, 4}},
	{name: "multi line selection toggle", space: true,
		lines: []string{"<0first", "\tse0>cond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 4, 1, 3},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 1, 2, 4}},
	{name: "multi line selection toggle", space: true,
		lines: []string{"<0 first", "\tse0>cond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 4, 1, 3},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 1, 2, 4}},
	{name: "multi line selection toggle", space: true,
		lines: []string{"<0first", "\tse 0>cond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 4, 1, 3},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 1, 2, 4}},
	{name: "multi line selection toggle", space: true,
		lines: []string{"<0 first", "\tse 0>cond line", "third line", "fourth line", "fifth"}, sel: sel4{2, 4, 1, 3},
		want: []string{"first", "\tsecond line", "third line", "fourth line", "fifth"}, wantSel: sel4{1, 1, 2, 4}},
	{name: "fuzzy removes", space: true,
		lines: []string{"asd <0 qwe", "asd 0> qwe"}, sel: sel4{2, 5, 1, 7},
		want: []string{"asd qwe", "asd qwe"}, wantSel: sel4{1, 5, 2, 4}},
	{name: "fuzzy removes", space: true,
		lines: []string{"asd <0 qwe", "asd 0> qwe"}, sel: sel4{2, 5, 1, 6},
		want: []string{"asd qwe", "asd qwe"}, wantSel: sel4{1, 5, 2, 4}},
	{name: "fuzzy removes", space: true,
		lines: []string{"asd <0 qwe", "asd 0> qwe"}, sel: sel4{2, 5, 1, 5},
		want: []string{"asd qwe", "asd qwe"}, wantSel: sel4{1, 5, 2, 4}},
	{name: "fuzzy removes", space: true,
		lines: []string{"asd <0 qwe", "asd 0> qwe"}, sel: sel4{2, 5, 1, 11},
		want: []string{"asd qwe", "asd qwe"}, wantSel: sel4{1, 5, 2, 4}},
	{name: "fuzzy removes", space: true,
		lines: []string{"asd <0 qwe", "asd 0> qwe"}, sel: sel4{2, 1, 1, 11},
		want: []string{"asd qwe", "asd qwe"}, wantSel: sel4{1, 5, 2, 4}},
	{name: "fuzzy removes", space: true,
		lines: []string{"asd <0 qwe", "asd 0> qwe"}, sel: sel4{2, 7, 1, 11},
		want: []string{"asd qwe", "asd qwe"}, wantSel: sel4{1, 5, 2, 4}},
	{name: "bug #30358", space: true,
		lines: []string{"<0 start 0> middle end"}, sel: sel4{1, 20, 1, 23},
		want: []string{"<0 start 0> middle <0 end 0>"}, wantSel: sel4{1, 23, 1, 26}},
	{name: "bug #30358", space: true,
		lines: []string{"<0 start 0> middle <0 end 0>"}, sel: sel4{1, 13, 1, 19},
		want: []string{"<0 start 0> <0 middle 0> <0 end 0>"}, wantSel: sel4{1, 16, 1, 22}},
	{name: "issue #34618", space: true,
		lines: []string{"<0  0> middle end"}, sel: sel4{1, 4, 1, 4},
		want: []string{" middle end"}, wantSel: sel4{1, 1, 1, 1}},
	{name: "insertSpace false", space: false,
		lines: []string{"some text"}, sel: sel4{1, 1, 1, 5},
		want: []string{"<0some0> text"}, wantSel: sel4{1, 3, 1, 7}},
	{name: "insertSpace false does not remove space", space: false,
		lines: []string{"<0 some 0> text"}, sel: sel4{1, 4, 1, 8},
		want: []string{" some  text"}, wantSel: sel4{1, 1, 1, 7}},
}

func TestVSCodeBlockComment(t *testing.T) {
	rule := commentRule{line: "!@#", bs: "<0", be: "0>"}
	for _, tc := range blockCommentCases {
		t.Run(tc.name, func(t *testing.T) {
			e := commentEd(t, rule, tc.space, true, tc.lines...)
			e.at(tc.sel)
			e.run(Commands.ToggleBlockComment)
			e.assertText(strings.Join(tc.want, "\n"))
			e.assertSels(tc.wantSel)
		})
	}
}
