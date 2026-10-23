package editor

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// typeKeys types text key by key, as the keyboard does.
func typeKeys(t *testing.T, vm *ViewModel, text string) {
	t.Helper()
	for _, r := range text {
		if err := vm.RunCommand("type", string(r)); err != nil {
			t.Fatal(err)
		}
	}
}

func caretAt(vm *ViewModel, line, col int) {
	vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: line, Col: col})})
}

func TestTypingAnOpeningBracketClosesIt(t *testing.T) {
	vm := ruled(t, "go", "x := ")
	typeKeys(t, vm, "(")
	if got := vm.Document().Text(); got != "x := ()" {
		t.Fatalf("text = %q", got)
	}
	typeKeys(t, vm, "1)")
	if got := vm.Document().Text(); got != "x := (1)" {
		t.Errorf("text = %q, the closing bracket must be typed over", got)
	}
	if p := vm.Cursors().Primary().Position(); p.Col != len("x := (1)") {
		t.Errorf("cursor at %v, want after the closing bracket", p)
	}
}

func TestTypeOverStopsWhenTheCursorLeaves(t *testing.T) {
	vm := ruled(t, "go", "x := ")
	typeKeys(t, vm, "(")
	caretAt(vm, 0, len("x := "))
	caretAt(vm, 0, len("x := ("))
	typeKeys(t, vm, ")")
	if got := vm.Document().Text(); got != "x := ())" {
		t.Errorf("text = %q, a closing bracket the cursor left is not typed over", got)
	}
}

func TestBackspaceDeletesTheClosingBracketItTyped(t *testing.T) {
	vm := ruled(t, "go", "x := ")
	typeKeys(t, vm, "(")
	if err := vm.RunCommand("deleteLeft", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "x := " {
		t.Errorf("text = %q", got)
	}
	// A bracket the user typed in the text is not a pair of the editor's.
	vm = ruled(t, "go", "f()")
	caretAt(vm, 0, 2)
	if err := vm.RunCommand("deleteLeft", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "f)" {
		t.Errorf("text = %q", got)
	}
}

func TestQuotesAreClosedByTheTokensOfTheLine(t *testing.T) {
	vm := ruled(t, "typescript", "const s = ")
	typeKeys(t, vm, "'")
	if got := vm.Document().Text(); got != "const s = ''" {
		t.Errorf("text = %q, a quote that begins a string is closed", got)
	}
	vm = ruled(t, "typescript", `const s = "it`)
	typeKeys(t, vm, "'")
	if got := vm.Document().Text(); got != `const s = "it'` {
		t.Errorf("text = %q, a quote inside a string is not closed", got)
	}
	// The quote that ends a string is not closed either: the tokenizer is asked what a character
	// typed there would be in.
	vm = ruled(t, "typescript", "const s = `a ${b}")
	typeKeys(t, vm, "`")
	if got := vm.Document().Text(); got != "const s = `a ${b}`" {
		t.Errorf("text = %q, a quote that ends a string is not closed", got)
	}
}

func TestOpeningWithSeveralCharactersClosesAfterAnUndoStop(t *testing.T) {
	vm := ruled(t, "typescript", "/*")
	typeKeys(t, vm, "*")
	if got := vm.Document().Text(); got != "/** */" {
		t.Fatalf("text = %q", got)
	}
	if err := vm.RunCommand("undo", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "/**" {
		t.Errorf("text after undo = %q, want only the closing text gone", got)
	}
}

func TestTypingAnOpeningCharacterOverASelectionSurroundsIt(t *testing.T) {
	vm := ruled(t, "go", "abc def")
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Col: 0}, Active: buffer.Position{Col: 3}}})
	typeKeys(t, vm, "(")
	if got := vm.Document().Text(); got != "(abc) def" {
		t.Fatalf("text = %q", got)
	}
	if s := vm.Cursors().Primary().Selection; s.Start().Col != 1 || s.End().Col != 4 {
		t.Errorf("selection = %v, want abc selected", s)
	}
}

func TestAutoClosingSettings(t *testing.T) {
	for _, tc := range []struct {
		name, key, value, typed, text, want string
	}{
		{"brackets never", "brackets", "never", "(", "x := ", "x := ("},
		{"brackets before whitespace", "brackets", "beforeWhitespace", "(", "x := ", "x := ()"},
		{"quotes never", "quotes", "never", `"`, "x := ", "x := \""},
		{"surround never", "surround", "never", "(", "abc", "("},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := ruled(t, "go", tc.text)
			opts := vm.Options()
			switch tc.key {
			case "brackets":
				opts.AutoClosingBrackets = tc.value
			case "quotes":
				opts.AutoClosingQuotes = tc.value
			case "surround":
				opts.AutoSurround = tc.value
				vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{}, Active: buffer.Position{Col: 3}}})
			}
			vm.SetOptions(opts)
			typeKeys(t, vm, tc.typed)
			if got := vm.Document().Text(); got != tc.want {
				t.Errorf("text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOvertypeAndDeleteSettings(t *testing.T) {
	vm := ruled(t, "go", "f()")
	opts := vm.Options()
	opts.AutoClosingOvertype = "always"
	opts.AutoClosingDelete = "always"
	vm.SetOptions(opts)
	caretAt(vm, 0, 2)
	typeKeys(t, vm, ")")
	if got := vm.Document().Text(); got != "f()" {
		t.Errorf("text = %q, always types over any closing bracket", got)
	}
	caretAt(vm, 0, 2)
	if err := vm.RunCommand("deleteLeft", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "f" {
		t.Errorf("text = %q, always deletes the pair", got)
	}
}
