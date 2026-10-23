package editor

import (
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/indent"
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
// "Change Tab Display Size" (displayOnly: the tab size only, keeping what Tab inserts). VS Code
// asks for the tab size with a list; until the quick input of area 2 can show it, the size is
// an argument.
//
// Ported from VS Code: src/vs/editor/contrib/indentation/browser/indentation.ts (ChangeIndentationSizeAction)
func indentUsing(spaces, displayOnly bool) action {
	return func(vm *ViewModel, args any) error {
		size, ok := tabSizeArgument(args)
		if !ok {
			return errors.New("TabSizeRequired", `give the tab size as an argument, {"tabSize": 4} (a list to pick it from comes with the quick input)`)
		}
		if displayOnly {
			_, spaces = vm.doc.Indentation()
		}
		vm.setIndentation(size, spaces)
		return nil
	}
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
