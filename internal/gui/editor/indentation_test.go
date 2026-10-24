package editor

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/autoclose"
	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/comment"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/errors"
)

func runIndent(t *testing.T, text string, id string, args any, sels ...buffer.Selection) *ViewModel {
	t.Helper()
	vm := NewViewModel(newTestDoc(text), testOptions(), nil)
	if len(sels) > 0 {
		vm.Cursors().SetSelections(sels)
	}
	if err := vm.RunCommand(id, args); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return vm
}

func TestConvertIndentationCommands(t *testing.T) {
	vm := runIndent(t, "\tfoo\n\t\tbar\nbaz", "editor.action.indentationToSpaces", nil, cur(0, 3))
	if got := vm.Document().Text(); got != "    foo\n        bar\nbaz" {
		t.Errorf("text = %q", got)
	}
	if size, spaces := vm.Document().Indentation(); size != 4 || !spaces {
		t.Errorf("the document is now indented with spaces: %d %v", size, spaces)
	}
	if got := vm.Cursors().Primary().Selection; got != cur(0, 6) {
		t.Errorf("the cursor followed the text: %v", got)
	}
	// Back to tabs, with a document that uses 4 spaces.
	if err := vm.RunCommand("editor.action.indentationToTabs", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "\tfoo\n\t\tbar\nbaz" {
		t.Errorf("text = %q", got)
	}
	if _, spaces := vm.Document().Indentation(); spaces {
		t.Error("the document is now indented with tabs")
	}
	// One undo takes back the conversion of the text.
	if err := vm.RunCommand("undo", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "    foo\n        bar\nbaz" {
		t.Errorf("after undo: %q", got)
	}
}

func TestIndentUsingCommands(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		args        any
		wantTab     int
		wantSpaces  bool
		wantDisplay int
	}{
		{"spaces, a number", "editor.action.indentUsingSpaces", 2.0, 2, true, 2},
		{"spaces, an object", "editor.action.indentUsingSpaces", map[string]any{"tabSize": 8.0}, 8, true, 8},
		{"tabs", "editor.action.indentUsingTabs", 3.0, 3, false, 3},
		{"the display size keeps what Tab inserts (spaces)", "editor.action.changeTabDisplaySize", 6.0, 6, true, 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vm := runIndent(t, "\tx", tc.id, tc.args)
			if size, spaces := vm.Document().Indentation(); size != tc.wantTab || spaces != tc.wantSpaces {
				t.Errorf("document: %d %v", size, spaces)
			}
			if vm.Options().TabSize != tc.wantDisplay {
				t.Errorf("a tab is shown as %d columns", vm.Options().TabSize)
			}
		})
	}
	// With tabs in the document, the display size keeps tabs.
	vm := NewViewModel(newTestDoc("\tx"), testOptions(), nil)
	vm.Document().SetIndentation(4, false)
	if err := vm.RunCommand("editor.action.changeTabDisplaySize", 2.0); err != nil {
		t.Fatal(err)
	}
	if size, spaces := vm.Document().Indentation(); size != 2 || spaces {
		t.Errorf("document: %d %v", size, spaces)
	}
	for _, bad := range []any{nil, "x", 0.0, 101.0, 2.5, map[string]any{"tabSize": "4"}, map[string]any{}} {
		err := vm.RunCommand("editor.action.indentUsingSpaces", bad)
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("TabSizeRequired") {
			t.Errorf("argument %v: %v", bad, err)
		}
	}
}

func TestDetectIndentationCommand(t *testing.T) {
	vm := runIndent(t, "a\n  b\n    c\n  d\ne", "editor.action.detectIndentation", nil)
	if size, spaces := vm.Document().Indentation(); size != 2 || !spaces {
		t.Errorf("two spaces: %d %v", size, spaces)
	}
	if vm.Options().TabSize != 2 {
		t.Errorf("the tab size is shown: %d", vm.Options().TabSize)
	}
	// Tabs.
	vm = runIndent(t, "a\n\tb\n\t\tc\n\td", "editor.action.detectIndentation", nil)
	if _, spaces := vm.Document().Indentation(); spaces {
		t.Error("tabs")
	}
	// A text that says nothing gives the defaults of the settings.
	vm = NewViewModel(newTestDoc("a\nb"), testOptions(), nil)
	vm.SetIndentationDefaults(func() (int, bool) { return 3, false })
	if err := vm.RunCommand("editor.action.detectIndentation", nil); err != nil {
		t.Fatal(err)
	}
	if size, spaces := vm.Document().Indentation(); size != 3 || spaces {
		t.Errorf("the defaults: %d %v", size, spaces)
	}
	// And without defaults, those of a new document.
	vm = runIndent(t, "a", "editor.action.detectIndentation", nil)
	if size, spaces := vm.Document().Indentation(); size != 4 || !spaces {
		t.Errorf("document defaults: %d %v", size, spaces)
	}
}

func TestTabSizeArgument(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want int
		ok   bool
	}{
		{4.0, 4, true}, {4, 4, true}, {int64(8), 8, true}, {map[string]any{"tabSize": 2.0}, 2, true},
		{0.0, 0, false}, {1.0, 1, true}, {100.0, 100, true}, {101.0, 0, false}, {1.5, 0, false},
		{"4", 0, false}, {nil, 0, false}, {map[string]any{}, 0, false},
	} {
		if got, ok := tabSizeArgument(tc.in); got != tc.want || ok != tc.ok {
			t.Errorf("tabSizeArgument(%v) = %d, %v", tc.in, got, ok)
		}
	}
	_ = strings.TrimSpace
}

// ruled makes the view model of a document in a bundled language, with its tokens and the
// indentation rules of the languages, the cursor at the end of the text.
func ruled(t testing.TB, languageID, text string) *ViewModel {
	t.Helper()
	reg, err := language.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	var g *textmate.Grammar // plain text has none
	if languageID != "plaintext" {
		if g, err = reg.NewGrammar(languageID, regex.NewCache(1<<14)); err != nil {
			t.Fatal(err)
		}
	}
	opts := document.NewOptions()
	opts.LanguageID = languageID
	doc := document.New(document.FileURI("/sample."+languageID), text, opts)
	tok := tokens.New(doc, g, tokens.Options{})
	t.Cleanup(tok.Close)
	tok.Settle()
	vm := NewViewModel(doc, testOptions(), nil)
	vm.SetTokenizer(tok)
	vm.SetIndenter(indent.New(indent.NewRegistryLanguages(reg, brackets.NewRegistryLanguages(reg))), reg.Code(languageID))
	vm.SetAutoClosing(autoclose.NewRegistryLanguages(reg).Language(reg.Code(languageID)))
	vm.SetComments(comment.NewRegistryLanguages(reg))
	vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(doc.Snapshot().PositionAt(doc.Snapshot().Len()))})
	return vm
}

func TestEnterUsesTheRulesOfTheLanguage(t *testing.T) {
	tests := []struct {
		name, language, text, want string
	}{
		{"Go indents after a brace", "go", "func main() {", "func main() {\n    "},
		{"Go splits a pair of braces", "go", "func main() {}", "func main() {\n    \n}"},
		{"TypeScript indents after an arrow", "typescript", "const f = (n) =>", "const f = (n) =>\n    "},
		{"TypeScript continues a doc comment", "typescript", "/**", "/**\n * "},
		{"plain text keeps the indentation", "plaintext", "  hello", "  hello\n  "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vm := ruled(t, tc.language, tc.text)
			if tc.name == "Go splits a pair of braces" {
				vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Col: len("func main() {")})})
			}
			if err := vm.RunCommand("type", "\n"); err != nil {
				t.Fatal(err)
			}
			if got := vm.Document().Text(); got != tc.want {
				t.Errorf("text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAutoIndentSettingChangesWhatEnterDoes(t *testing.T) {
	for level, want := range map[string]string{
		"none": "func main() {\n", "keep": "func main() {\n", "brackets": "func main() {\n    ", "advanced": "func main() {\n    ", "full": "func main() {\n    ",
	} {
		vm := ruled(t, "go", "func main() {")
		opts := vm.Options()
		opts.AutoIndent = level
		vm.SetOptions(opts)
		if err := vm.RunCommand("type", "\n"); err != nil {
			t.Fatal(err)
		}
		if got := vm.Document().Text(); got != want {
			t.Errorf("editor.autoIndent %q: text = %q, want %q", level, got, want)
		}
	}
}

func TestTypingACloserReindentsTheLine(t *testing.T) {
	vm := ruled(t, "go", "func main() {\n    foo()\n    ")
	if err := vm.RunCommand("type", "}"); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "func main() {\n    foo()\n}" {
		t.Errorf("text = %q", got)
	}
}

func TestReindentLinesCommand(t *testing.T) {
	vm := ruled(t, "go", "func main() {\nif a {\nfoo()\n}\n}\n")
	if err := vm.RunCommand("editor.action.reindentlines", nil); err != nil {
		t.Fatal(err)
	}
	if got, want := vm.Document().Text(), "func main() {\n    if a {\n        foo()\n    }\n}\n"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if err := vm.RunCommand("undo", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "func main() {\nif a {\nfoo()\n}\n}\n" {
		t.Errorf("one undo takes the reindentation back: %q", got)
	}

	vm = ruled(t, "go", "func main() {\n    if a {\nfoo()\nbar()\n    }\n}\n")
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 2}, Active: buffer.Position{Line: 4}}})
	if err := vm.RunCommand("editor.action.reindentselectedlines", nil); err != nil {
		t.Fatal(err)
	}
	if got, want := vm.Document().Text(), "func main() {\n    if a {\n        foo()\n        bar()\n    }\n}\n"; got != want {
		t.Errorf("selected lines: text = %q, want %q", got, want)
	}
}

func TestAutoIndentOnPaste(t *testing.T) {
	const text = "func main() {\n    if a {\n\n    }\n}\n"
	pasted := "foo()\nbar()"
	for _, tc := range []struct {
		name string
		on   bool
		want string
	}{
		{"off by default", false, "func main() {\n    if a {\nfoo()\nbar()\n    }\n}\n"},
		{"on", true, "func main() {\n    if a {\n        foo()\n        bar()\n    }\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := ruled(t, "go", text)
			vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 2})})
			opts := vm.Options()
			opts.AutoIndentOnPaste = tc.on
			vm.SetOptions(opts)
			if err := vm.PasteText(pasted); err != nil {
				t.Fatal(err)
			}
			if got := vm.Document().Text(); got != tc.want {
				t.Errorf("text = %q, want %q", got, tc.want)
			}
			if tc.on {
				if err := vm.RunCommand("undo", nil); err != nil {
					t.Fatal(err)
				}
				if got := vm.Document().Text(); got != "func main() {\n    if a {\nfoo()\nbar()\n    }\n}\n" {
					t.Errorf("undo takes the indentation back, not the paste: %q", got)
				}
			}
		})
	}
}

func TestLineBreakInsertKeepsTheCursor(t *testing.T) {
	vm := ruled(t, "go", "func main() {}")
	vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Col: len("func main() {")})})
	if err := vm.RunCommand("lineBreakInsert", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "func main() {\n    \n}" {
		t.Errorf("text = %q", got)
	}
	if got := vm.Cursors().Primary().Selection; got != buffer.NewCursor(buffer.Position{Col: len("func main() {")}) {
		t.Errorf("cursor = %v", got)
	}
}

func TestIndentUsingPicksTheTabSizeWithTheQuickInput(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		documentTab int
		documentSp  bool
		configured  int
		pick        string
		wantTab     int
		wantSpaces  bool
	}{
		{"spaces", "editor.action.indentUsingSpaces", 4, false, 4, "2", 2, true},
		{"tabs", "editor.action.indentUsingTabs", 4, true, 4, "8", 8, false},
		{"the display size keeps the spaces", "editor.action.changeTabDisplaySize", 4, true, 4, "3", 3, true},
		{"the display size keeps the tabs", "editor.action.changeTabDisplaySize", 4, false, 4, "6", 6, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := NewViewModel(newTestDoc("\tx"), testOptions(), nil)
			vm.Document().SetIndentation(tt.documentTab, tt.documentSp)
			vm.SetIndentationDefaults(func() (int, bool) { return tt.configured, true })
			var opts quickinput.PickOptions
			vm.SetQuickPick(func(o quickinput.PickOptions) { opts = o })
			if err := vm.RunCommand(tt.id, nil); err != nil {
				t.Fatal(err)
			}
			if opts.Placeholder != "Select Tab Size for Current File" || len(opts.Items) != 8 {
				t.Fatalf("pick: %q, %d items", opts.Placeholder, len(opts.Items))
			}
			if opts.ActiveID != "4" {
				t.Errorf("the current size is active: %q", opts.ActiveID)
			}
			if got := opts.Items[3].Description; got != "Configured Tab Size" {
				t.Errorf("4 is both the setting's and the document's: %q", got)
			}
			// nothing changed until a size is picked
			if size, _ := vm.Document().Indentation(); size != tt.documentTab {
				t.Fatalf("changed before the pick: %d", size)
			}
			for _, it := range opts.Items {
				if it.Label == tt.pick {
					it.Accept(quickinput.KeyMods{})
				}
			}
			if size, spaces := vm.Document().Indentation(); size != tt.wantTab || spaces != tt.wantSpaces {
				t.Errorf("document: %d %v; want %d %v", size, spaces, tt.wantTab, tt.wantSpaces)
			}
			if vm.Options().TabSize != tt.wantTab {
				t.Errorf("a tab is shown as %d columns", vm.Options().TabSize)
			}
		})
	}
}

func TestTabSizePickDescriptions(t *testing.T) {
	vm := NewViewModel(newTestDoc("x"), testOptions(), nil)
	vm.Document().SetIndentation(2, true)
	vm.SetIndentationDefaults(func() (int, bool) { return 4, true })
	var opts quickinput.PickOptions
	vm.SetQuickPick(func(o quickinput.PickOptions) { opts = o })
	if err := vm.RunCommand("editor.action.indentUsingSpaces", nil); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"1": "", "2": "Current Tab Size", "3": "", "4": "Default Tab Size", "8": ""}
	for _, it := range opts.Items {
		if w, ok := want[it.Label]; ok && it.Description != w {
			t.Errorf("size %s: %q, want %q", it.Label, it.Description, w)
		}
	}
	if opts.ActiveID != "2" {
		t.Errorf("active %q", opts.ActiveID)
	}
	// a document with a tab size past the list activates the last size
	vm.Document().SetIndentation(12, true)
	if err := vm.RunCommand("editor.action.indentUsingSpaces", nil); err != nil {
		t.Fatal(err)
	}
	if opts.ActiveID != "8" {
		t.Errorf("active %q for a tab size of 12", opts.ActiveID)
	}
	// an argument still wins over the list
	opts = quickinput.PickOptions{}
	if err := vm.RunCommand("editor.action.indentUsingSpaces", 3.0); err != nil {
		t.Fatal(err)
	}
	if len(opts.Items) != 0 {
		t.Error("an argument shows no list")
	}
	if size, _ := vm.Document().Indentation(); size != 3 {
		t.Errorf("size %d", size)
	}
}
