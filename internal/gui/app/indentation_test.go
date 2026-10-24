package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// openWith opens a file with a window whose settings are the given ones.
func openWith(t *testing.T, extra map[string]any, name, text string) *document.Document {
	t.Helper()
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, extra)
	win, err := NewWindow(context.Background(), newTestProcess(ProcessOptions{Paths: paths}), "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := win.Documents.OpenFile(context.Background(), path, document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestAFileOpensWithTheIndentationItUses(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		file     string
		text     string
		tabSize  int
		spaces   bool
	}{
		{"two spaces found in the text", nil, "a.py", "def f():\n  if x:\n    y\n  z\n", 2, true},
		{"tabs found in the text", nil, "a.py", "def f():\n\tif x:\n\t\ty\n\tz\n", 4, false},
		{"the text says nothing: the settings", map[string]any{"editor.tabSize": 3}, "a.py", "x\ny\n", 3, true},
		{"detection off: the settings win", map[string]any{"editor.detectIndentation": false, "editor.tabSize": 8, "editor.insertSpaces": false}, "a.py", "def f():\n  y\n  z\n", 8, false},
		{"detection on: the text wins over the settings", map[string]any{"editor.tabSize": 8}, "a.py", "def f():\n  y\n  z\n", 2, true},
		{"a Go file starts with tabs", nil, "a.go", "package a\n", 4, false},
		{"and detection finds spaces in one", nil, "a.go", "package a\n\nfunc f() {\n    x()\n    y()\n}\n", 4, true},
		{"a YAML file starts with two spaces, as its extension says", nil, "a.yaml", "a: 1\nb: 2\n", 2, true},
		{"a Makefile starts with tabs", nil, "Makefile", "all:\n", 4, false},
		{"an empty file", nil, "a.txt", "", 4, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := openWith(t, tc.settings, tc.file, tc.text)
			if size, spaces := doc.Indentation(); size != tc.tabSize || spaces != tc.spaces {
				t.Errorf("indentation = %d, %v; want %d, %v", size, spaces, tc.tabSize, tc.spaces)
			}
		})
	}
}
