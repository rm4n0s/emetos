package editor

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func runCommand(t *testing.T, vm *ViewModel, id string) {
	t.Helper()
	if err := vm.RunCommand(id, nil); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
}

func TestToggleLineComment(t *testing.T) {
	vm := ruled(t, "go", "func main() {\n\tfoo()\n}")
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 0}, Active: buffer.Position{Line: 2, Col: 1}}})
	runCommand(t, vm, "editor.action.commentLine")
	if got, want := vm.Document().Text(), "// func main() {\n// \tfoo()\n// }"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	runCommand(t, vm, "editor.action.commentLine")
	if got, want := vm.Document().Text(), "func main() {\n\tfoo()\n}"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	// One undo step for each.
	runCommand(t, vm, "undo")
	if got, want := vm.Document().Text(), "// func main() {\n// \tfoo()\n// }"; got != want {
		t.Errorf("text after undo = %q, want %q", got, want)
	}
}

func TestAddAndRemoveLineComment(t *testing.T) {
	vm := ruled(t, "typescript", "a();\nb();")
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{}, Active: buffer.Position{Line: 1, Col: 4}}})
	runCommand(t, vm, "editor.action.addCommentLine")
	runCommand(t, vm, "editor.action.addCommentLine")
	if got, want := vm.Document().Text(), "// // a();\n// // b();"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	runCommand(t, vm, "editor.action.removeCommentLine")
	if got, want := vm.Document().Text(), "// a();\n// b();"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestToggleBlockComment(t *testing.T) {
	vm := ruled(t, "go", "abc def")
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{}, Active: buffer.Position{Col: 3}}})
	runCommand(t, vm, "editor.action.blockComment")
	if got, want := vm.Document().Text(), "/* abc */ def"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	runCommand(t, vm, "editor.action.blockComment")
	if got, want := vm.Document().Text(), "abc def"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestCommentSettings(t *testing.T) {
	vm := ruled(t, "go", "foo()\n\nbar()")
	opts := vm.Options()
	opts.CommentsInsertSpace, opts.CommentsIgnoreEmptyLines = false, false
	vm.SetOptions(opts)
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{}, Active: buffer.Position{Line: 2, Col: 5}}})
	runCommand(t, vm, "editor.action.commentLine")
	if got, want := vm.Document().Text(), "//foo()\n//\n//bar()"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestLineCommentUsesTheLanguageOfTheCode(t *testing.T) {
	// Code in a Markdown fence is commented in the language of the fence; the Markdown around it
	// has no line comment, so it gets a block comment.
	text := "# Title\n\n```go\nfoo()\n```\n"
	vm := ruled(t, "markdown", text)
	caretAt(vm, 3, 2)
	runCommand(t, vm, "editor.action.commentLine")
	if got, want := vm.Document().Text(), "# Title\n\n```go\n// foo()\n```\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	runCommand(t, vm, "editor.action.commentLine")
	if got := vm.Document().Text(); got != text {
		t.Errorf("text = %q, want %q", got, text)
	}
	caretAt(vm, 0, 3)
	runCommand(t, vm, "editor.action.commentLine")
	if got, want := vm.Document().Text(), "<!-- # Title -->\n\n```go\nfoo()\n```\n"; got != want {
		t.Errorf("Markdown text = %q, want %q", got, want)
	}
}
