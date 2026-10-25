package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProblem(t *testing.T) {
	tests := []struct {
		name string
		line string
		want Problem
		ok   bool
	}{
		{"go vet", "./main.go:9:2: fmt.Printf format %d has arg x of wrong type string", Problem{"./main.go", 9, 2, "fmt.Printf format %d has arg x of wrong type string"}, true},
		{"go vet with the vet prefix", "vet: ./a.go:3:1: undefined: foo", Problem{"./a.go", 3, 1, "undefined: foo"}, true},
		{"a test failure of go test, indented, with no column", "    foo_test.go:12: got 1, want 2", Problem{"foo_test.go", 12, 1, "got 1, want 2"}, true},
		{"a path with directories", "internal/db/db.go:40:7: unused variable", Problem{"internal/db/db.go", 40, 7, "unused variable"}, true},
		{"an absolute path", "/tmp/x/a.c:1:1: error: boom", Problem{"/tmp/x/a.c", 1, 1, "error: boom"}, true},
		{"trailing blanks are dropped", "a.go:1:2: msg   ", Problem{"a.go", 1, 2, "msg"}, true},
		{"a line of prose", "FAIL: the tests failed", Problem{}, false},
		{"a time of day", "12:30:45", Problem{}, false},
		{"a time that has words after it parses, and names no file", "12:30:45 started", Problem{"12", 30, 1, "45 started"}, true},
		{"a path with spaces", "my file.go:1:1: x", Problem{}, false},
		{"line zero", "a.go:0:1: x", Problem{}, false},
		{"no message", "a.go:3:4:", Problem{}, false},
		{"empty", "", Problem{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseProblem(tt.line)
			if ok != tt.ok || got != tt.want {
				t.Errorf("parseProblem(%q) = %+v, %v; want %+v, %v", tt.line, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestIsWarning(t *testing.T) {
	for msg, want := range map[string]bool{"warning: unused": true, "Warning unused": true, "c.c: warning: x": true, "error: bad": false, "plain": false} {
		if got := isWarning(msg); got != want {
			t.Errorf("isWarning(%q) = %v", msg, got)
		}
	}
}

func TestResolveStaysInsideTheFolder(t *testing.T) {
	folder := t.TempDir()
	tests := []struct {
		path string
		want string
		ok   bool
	}{
		{"./a.go", filepath.Join(folder, "a.go"), true},
		{"sub/b.go", filepath.Join(folder, "sub", "b.go"), true},
		{filepath.Join(folder, "c.go"), filepath.Join(folder, "c.go"), true},
		{"../outside.go", "", false},
		{"/etc/passwd", "", false},
		{"sub/../../x.go", "", false},
	}
	for _, tt := range tests {
		got, ok := resolve(folder, tt.path)
		if ok != tt.ok || got != tt.want {
			t.Errorf("resolve(%q) = %q, %v; want %q, %v", tt.path, got, ok, tt.want, tt.ok)
		}
	}
	if isFile(folder) || isFile(filepath.Join(folder, "missing")) {
		t.Error("isFile accepts a folder or a missing file")
	}
	if err := os.WriteFile(filepath.Join(folder, "f"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !isFile(filepath.Join(folder, "f")) {
		t.Error("isFile refuses a file")
	}
}
