package editor

import (
	"strconv"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/autoclose"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/comment"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// maxTabSize is the largest tab size the commands accept (editor.tabSize goes to 100).
const maxTabSize = 100

// SetIndentationDefaults gives the view model what a new document of its language starts with,
// editor.tabSize and editor.insertSpaces, which Detect Indentation falls back on when the text
// says nothing. Without it the defaults are those of document.NewOptions.
func (vm *ViewModel) SetIndentationDefaults(defaults func() (tabSize int, insertSpaces bool)) {
	vm.indentDefaults = defaults
}

// setIndentation changes the indentation of the document: its tab size and whether Tab
// inserts spaces. The tab size is also the width of a tab in the text.
func (vm *ViewModel) setIndentation(tabSize int, insertSpaces bool) {
	vm.doc.SetIndentation(tabSize, insertSpaces)
	opts := vm.base
	opts.TabSize = max(tabSize, 1)
	vm.SetOptions(opts)
	vm.NotifyListeners()
	vm.sel.NotifyListeners()
}

// textLines reads the lines of the document for indent.Guess.
type textLines struct{ doc *document.Document }

func (t textLines) LineCount() int { return t.doc.Snapshot().LineCount() }

func (t textLines) LineContent(line int) string {
	s, _ := t.doc.Snapshot().LineContent(line)
	return s
}

// detectIndentation finds the indentation of the text and applies it: the defaults give way
// to what the first 10,000 lines say (SPEC §8.6).
func (vm *ViewModel) detectIndentation() {
	tabSize, insertSpaces := document.DefaultTabSize, true
	if vm.indentDefaults != nil {
		tabSize, insertSpaces = vm.indentDefaults()
	}
	g := indent.Guess(textLines{vm.doc}, tabSize, insertSpaces)
	vm.setIndentation(g.TabSize, g.InsertSpaces)
}

// tabSizeArgument reads the tab size that "Indent Using Spaces", "Indent Using Tabs" and "Change
// Tab Display Size" were given: a number, or {"tabSize": number}.
func tabSizeArgument(args any) (int, bool) {
	if m, ok := args.(map[string]any); ok {
		args = m["tabSize"]
	}
	var n float64
	switch v := args.(type) {
	case float64:
		n = v
	case int:
		n = float64(v)
	case int64:
		n = float64(v)
	default:
		return 0, false
	}
	if n != float64(int(n)) || n < 1 || n > maxTabSize {
		return 0, false
	}
	return int(n), true
}

// indentUsing makes the action of "Indent Using Spaces" (spaces true), "Indent Using Tabs" and
// "Change Tab Display Size" (displayOnly: the tab size only, keeping what Tab inserts). The tab size is
// the argument of the command, or, without one, what the user picks from a list of 1 to 8 (VS Code's
// "Select Tab Size for Current File"); without the quick input (SetQuickPick) there is no list and the
// argument is required.
//
// Ported from VS Code: src/vs/editor/contrib/indentation/browser/indentation.ts (ChangeIndentationSizeAction)
func indentUsing(spaces, displayOnly bool) action {
	return func(vm *ViewModel, args any) error {
		size, ok := tabSizeArgument(args)
		if !ok {
			if vm.snip.quick == nil {
				return errors.New("TabSizeRequired", `give the tab size as an argument, {"tabSize": 4}`)
			}
			vm.pickTabSize(spaces, displayOnly)
			return nil
		}
		vm.applyTabSize(size, spaces, displayOnly)
		return nil
	}
}

// applyTabSize makes the tab size the user chose, with the "Indent Using Spaces/Tabs" or the "Change Tab
// Display Size" rule.
func (vm *ViewModel) applyTabSize(size int, spaces, displayOnly bool) {
	if displayOnly {
		_, spaces = vm.doc.Indentation()
	}
	vm.setIndentation(size, spaces)
}

// tabSizePicks are the sizes the tab size picker offers.
const tabSizePicks = 8

// pickTabSize shows the picker of the tab size: the sizes 1 to 8, the one the settings give marked
// "Default Tab Size" (or "Configured Tab Size" when it is the document's too) and the document's marked
// "Current Tab Size", with the document's size active.
func (vm *ViewModel) pickTabSize(spaces, displayOnly bool) {
	current, _ := vm.doc.Indentation()
	configured := 0
	if vm.indentDefaults != nil {
		configured, _ = vm.indentDefaults()
	}
	items := make([]quickinput.Item, 0, tabSizePicks)
	for n := 1; n <= tabSizePicks; n++ {
		description := ""
		switch {
		case n == configured && n == current:
			description = "Configured Tab Size"
		case n == configured:
			description = "Default Tab Size"
		case n == current:
			description = "Current Tab Size"
		}
		items = append(items, quickinput.Item{
			ID: strconv.Itoa(n), Label: strconv.Itoa(n), Description: description,
			Accept: func(quickinput.KeyMods) {
				vm.runLater(func() error { vm.applyTabSize(n, spaces, displayOnly); return nil })
			},
		})
	}
	vm.snip.quick(quickinput.PickOptions{
		Placeholder: "Select Tab Size for Current File", Items: items,
		ActiveID: strconv.Itoa(min(max(current, 1), tabSizePicks)),
	})
}

// convertIndentation makes the action of Convert Indentation to Spaces and to Tabs.
func convertIndentation(toSpaces bool) action {
	return func(vm *ViewModel, _ any) error {
		if err := vm.apply(vm.commands().ConvertIndentation(vm.cursors.Primary().Selection, toSpaces)); err != nil {
			return err
		}
		tabSize, _ := vm.doc.Indentation()
		vm.setIndentation(tabSize, toSpaces)
		return nil
	}
}

// SetIndenter gives the view model the indentation rules of the languages (SPEC §8.6.2) and the
// code of the language of its document; with nil a new line keeps the indentation of the line it
// starts on and nothing is re-indented while typing.
func (vm *ViewModel) SetIndenter(ix *indent.Indenter, languageCode int) {
	vm.indenter, vm.languageCode = ix, languageCode
}

// SetAutoClosing gives the view model the auto-closing pairs and the surrounding pairs of the
// language of its document (SPEC §8.6.4); with nil nothing is closed and nothing is surrounded.
func (vm *ViewModel) SetAutoClosing(l *autoclose.Language) {
	vm.autoClosing = l
}

// SetComments gives the view model the comment tokens of the languages (SPEC §8.6.5); without
// them the comment commands do nothing.
func (vm *ViewModel) SetComments(l comment.Languages) {
	vm.comments = l
}

// autoClosedOwner is the slot of the decorations that track the auto-closed characters (see
// ViewModel.owner).
const autoClosedOwner = 1

// indentTokens gives the indentation rules the tokens of the document: a line is tokenized on the
// UI thread, within the budget of the text layer, before it is read.
type indentTokens struct{ vm *ViewModel }

func (s indentTokens) LineTokens(line int) []tokens.Token {
	tok := s.vm.tok.tokenizer
	if tok == nil || !tok.Active() {
		return nil
	}
	tok.Ensure(line, tokenBudget)
	return tok.Line(line)
}

func (s indentTokens) Language() int { return s.vm.languageCode }

// TypeIfInserting tells the type of token that ch would be if it were typed at the byte column col
// of the line (edit.InsertionTypes).
func (s indentTokens) TypeIfInserting(line, col int, ch string) (textmate.TokenType, bool) {
	tok := s.vm.tok.tokenizer
	if tok == nil || !tok.Active() || !tok.Ensure(line, tokenBudget) {
		return textmate.TokenOther, false
	}
	return tok.TypeIfInserting(line, col, ch)
}

func (s indentTokens) CheapToTokenize(line int) bool {
	tok := s.vm.tok.tokenizer
	return tok == nil || !tok.Active() || tok.Ensure(line, tokenBudget)
}

// autoIndent is editor.autoIndent; a value that is not a level is "full".
func (vm *ViewModel) autoIndent() indent.AutoIndent {
	level, _ := indent.ParseAutoIndent(vm.opts.AutoIndent)
	return level
}

// reindentLines is Reindent Lines: the lines of the document are indented as the rules of the
// language say.
func (vm *ViewModel) reindentLines() error {
	last := vm.doc.Snapshot().LineCount() - 1
	return vm.apply(vm.commands().Reindent(vm.cursors.Selections(), 0, last))
}

// autoIndentPasted fits the text just pasted over r to the indentation around it
// (editor.autoIndentOnPaste), when there is one cursor.
func (vm *ViewModel) autoIndentPasted(r buffer.Range) error {
	sels := vm.cursors.Selections()
	if !vm.opts.AutoIndentOnPaste || len(sels) != 1 {
		return nil
	}
	return vm.apply(vm.commands().AutoIndentPaste(sels[0], r))
}
