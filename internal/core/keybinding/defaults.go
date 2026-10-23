package keybinding

import "github.com/rm4n0s/emetos/internal/core/contextkey"

// Context keys the default keybindings test. Their meaning is VS Code's EditorContextKeys.
const (
	whenTextInputFocus = "textInputFocus"
	whenEditorFocus    = "editorFocus"
	whenEditorText     = "editorTextFocus"
	whenWritable       = "!editorReadonly"
	// whenNotWindowsA11y is the condition of the word moves: VS Code leaves Ctrl+Left and
	// Ctrl+Right to the screen reader's mode on Windows only.
	whenNotWindowsA11y = "(!accessibilityModeEnabled || !isWindows)"
)

// mustWhen reads the parts of a when-clause and joins them with &&. An empty part is
// skipped.
func mustWhen(parts ...string) contextkey.Expr {
	var exprs []contextkey.Expr
	for _, p := range parts {
		if p == "" {
			continue
		}
		e, err := contextkey.Parse(p)
		if err != nil {
			panic(err)
		}
		exprs = append(exprs, e)
	}
	if len(exprs) == 0 {
		return nil
	}
	return contextkey.And(exprs...)
}

// DefaultRegistry makes a registry with the default keymap of Emetos: VS Code's keymap for
// Linux, for the commands Emetos has. VS Code registers its defaults in code, in the
// keybindings of each command's registration; each step that adds a command adds its
// binding here, with the key, secondary keys, when-clause and weight it has there. A when
// that VS Code builds from the command's key expression and its precondition is written
// out in full.
//
// Ported from VS Code: src/vs/editor/browser/coreCommands.ts,
// src/vs/editor/browser/editorExtensions.ts and the contributions under
// src/vs/editor/contrib/ (linesOperations, multicursor, smartSelect, wordOperations,
// cursorUndo, lineSelection)
func DefaultRegistry() *Registry {
	r := NewRegistry()
	registerCursorKeys(r)
	registerEditKeys(r)
	registerLineKeys(r)
	registerMultiCursorKeys(r)
	return r
}

// registerCursorKeys registers the keys that move and select.
func registerCursorKeys(r *Registry) {
	input := func(command, primary string, secondary ...string) Rule {
		return Rule{Command: command, Weight: WeightEditorCore, When: mustWhen(whenTextInputFocus), Primary: primary, Secondary: secondary}
	}
	r.MustRegister(
		input("cursorLeft", "left"),
		input("cursorLeftSelect", "shift+left"),
		input("cursorRight", "right"),
		input("cursorRightSelect", "shift+right"),
		input("cursorUp", "up"),
		input("cursorUpSelect", "shift+up"),
		input("cursorPageUp", "pageup"),
		input("cursorPageUpSelect", "shift+pageup"),
		input("cursorDown", "down"),
		input("cursorDownSelect", "shift+down"),
		input("cursorPageDown", "pagedown"),
		input("cursorPageDownSelect", "shift+pagedown"),
		input("cursorHome", "home"),
		input("cursorHomeSelect", "shift+home"),
		input("cursorEnd", "end"),
		input("cursorEndSelect", "shift+end"),
		input("cursorTop", "ctrl+home"),
		input("cursorTopSelect", "ctrl+shift+home"),
		input("cursorBottom", "ctrl+end"),
		input("cursorBottomSelect", "ctrl+shift+end"),
		input("expandLineSelection", "ctrl+l"),
		Rule{Command: "cancelSelection", When: mustWhen(whenTextInputFocus, "editorHasSelection"), Primary: "escape", Secondary: []string{"shift+escape"}},
		// The weight one above the other core commands lets it win over cancelSelection.
		Rule{Command: "removeSecondaryCursors", Weight: WeightEditorCore + 1, When: mustWhen(whenTextInputFocus, "editorHasMultipleSelections"), Primary: "escape", Secondary: []string{"shift+escape"}},
	)
	// With editor.columnSelection on, a plain Shift+arrow selects a column.
	column := func(command, primary string) Rule {
		return Rule{Command: command, Weight: WeightEditorCore + 1, When: mustWhen(whenTextInputFocus, "editorColumnSelection"), Primary: primary}
	}
	r.MustRegister(
		column("cursorColumnSelectLeft", "shift+left"),
		column("cursorColumnSelectRight", "shift+right"),
		column("cursorColumnSelectUp", "shift+up"),
		column("cursorColumnSelectPageUp", "shift+pageup"),
		column("cursorColumnSelectDown", "shift+down"),
		column("cursorColumnSelectPageDown", "shift+pagedown"),
	)
	// Word moves are contributions, so they weigh more than the core moves.
	word := func(command, primary string) Rule {
		return Rule{Command: command, Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus, whenNotWindowsA11y), Primary: primary}
	}
	r.MustRegister(
		word("cursorWordLeft", "ctrl+left"),
		word("cursorWordLeftSelect", "ctrl+shift+left"),
		word("cursorWordEndRight", "ctrl+right"),
		word("cursorWordEndRightSelect", "ctrl+shift+right"),
		Rule{Command: "cursorUndo", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus), Primary: "ctrl+u"},
	)
}

// registerEditKeys registers the keys that type, delete and undo.
func registerEditKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "undo", Weight: WeightEditorCore, Primary: "ctrl+z"},
		Rule{Command: "redo", Weight: WeightEditorCore, Primary: "ctrl+y", Secondary: []string{"ctrl+shift+z"}},
		Rule{Command: "editor.action.selectAll", Weight: WeightEditorCore, Primary: "ctrl+a"},
		Rule{Command: "outdent", Weight: WeightEditorCore, When: mustWhen(whenEditorText, "!editorTabMovesFocus", whenWritable), Primary: "shift+tab"},
		Rule{Command: "tab", Weight: WeightEditorCore, When: mustWhen(whenEditorText, "!editorTabMovesFocus", whenWritable), Primary: "tab"},
		Rule{Command: "deleteLeft", Weight: WeightEditorCore, When: mustWhen(whenTextInputFocus, whenWritable), Primary: "backspace", Secondary: []string{"shift+backspace"}},
		Rule{Command: "deleteRight", Weight: WeightEditorCore, When: mustWhen(whenTextInputFocus, whenWritable), Primary: "delete"},
		Rule{Command: "deleteWordLeft", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus, whenWritable), Primary: "ctrl+backspace"},
		Rule{Command: "deleteWordRight", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus, whenWritable), Primary: "ctrl+delete"},
	)
}

// registerLineKeys registers the keys of the commands on lines.
func registerLineKeys(r *Registry) {
	text := func(command, primary string) Rule {
		return Rule{Command: command, Weight: WeightEditorContrib, When: mustWhen(whenEditorText, whenWritable), Primary: primary}
	}
	r.MustRegister(
		text("editor.action.copyLinesUpAction", "ctrl+alt+shift+up"),
		text("editor.action.copyLinesDownAction", "ctrl+alt+shift+down"),
		text("editor.action.moveLinesUpAction", "alt+up"),
		text("editor.action.moveLinesDownAction", "alt+down"),
		text("editor.action.trimTrailingWhitespace", "ctrl+k ctrl+x"),
		text("editor.action.indentLines", "ctrl+]"),
		text("editor.action.outdentLines", "ctrl+["),
		text("editor.action.insertLineBefore", "ctrl+shift+enter"),
		text("editor.action.insertLineAfter", "ctrl+enter"),
		Rule{Command: "editor.action.deleteLines", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus, whenWritable), Primary: "ctrl+shift+k"},
	)
}

// registerMultiCursorKeys registers the keys that add cursors and expand the selection.
func registerMultiCursorKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "editor.action.insertCursorAbove", Weight: WeightEditorContrib, When: mustWhen(whenEditorText), Primary: "shift+alt+up", Secondary: []string{"ctrl+shift+up"}},
		Rule{Command: "editor.action.insertCursorBelow", Weight: WeightEditorContrib, When: mustWhen(whenEditorText), Primary: "shift+alt+down", Secondary: []string{"ctrl+shift+down"}},
		Rule{Command: "editor.action.insertCursorAtEndOfEachLineSelected", Weight: WeightEditorContrib, When: mustWhen(whenEditorText), Primary: "shift+alt+i"},
		Rule{Command: "editor.action.addSelectionToNextFindMatch", Weight: WeightEditorContrib, When: mustWhen(whenEditorFocus), Primary: "ctrl+d"},
		Rule{Command: "editor.action.moveSelectionToNextFindMatch", Weight: WeightEditorContrib, When: mustWhen(whenEditorFocus), Primary: "ctrl+k ctrl+d"},
		Rule{Command: "editor.action.selectHighlights", Weight: WeightEditorContrib, When: mustWhen(whenEditorFocus), Primary: "ctrl+shift+l"},
		Rule{Command: "editor.action.smartSelect.expand", Weight: WeightEditorContrib, When: mustWhen(whenEditorText), Primary: "shift+alt+right"},
		Rule{Command: "editor.action.smartSelect.shrink", Weight: WeightEditorContrib, When: mustWhen(whenEditorText), Primary: "shift+alt+left"},
	)
}
