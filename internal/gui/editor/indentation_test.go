package editor

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
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
