package tsconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, rel, text string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOutDir(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"plain", `{"compilerOptions": {"outDir": "out"}}`, "out"},
		{"comments and trailing commas", "{\n// the output\n\"compilerOptions\": {\"outDir\": \"./dist\", /* x */\n},\n}", "./dist"},
		{"a comment marker inside a string is text", `{"compilerOptions": {"outDir": "a//b", "other": "/* not a comment */"}}`, "a//b"},
		{"an escaped quote in a string", `{"compilerOptions": {"x": "say \"hi\" // not a comment", "outDir": "o"}}`, "o"},
		{"no outDir", `{"compilerOptions": {"strict": true}}`, ""},
		{"no options", `{}`, ""},
		{"not JSON", `{`, ""},
		{"an outDir that is not text", `{"compilerOptions": {"outDir": 5}}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := write(t, t.TempDir(), "tsconfig.json", tt.text)
			if got := OutDir(p); got != tt.want {
				t.Errorf("OutDir = %q, want %q", got, tt.want)
			}
		})
	}
	if got := OutDir(filepath.Join(t.TempDir(), "missing.json")); got != "" {
		t.Errorf("a file that is not there gave %q", got)
	}
}

func TestNearest(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "work")
	write(t, root, "tsconfig.json", "{}")
	write(t, root, "pkg/tsconfig.json", "{}")
	write(t, root, "pkg/src/deep/a.ts", "")
	write(t, root, "other/b.ts", "")
	write(t, base, "outside/c.ts", "")
	write(t, base, "tsconfig.json", "{}")
	tests := []struct {
		name, file, stop, want string
	}{
		{"the nearest one wins", filepath.Join(root, "pkg", "src", "deep", "a.ts"), root, filepath.Join(root, "pkg", "tsconfig.json")},
		{"the folder's own", filepath.Join(root, "other", "b.ts"), root, filepath.Join(root, "tsconfig.json")},
		{"never above the folder", filepath.Join(root, "other", "b.ts"), filepath.Join(root, "other"), ""},
		{"a file outside the folder looks a few levels up", filepath.Join(base, "outside", "c.ts"), root, filepath.Join(base, "tsconfig.json")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Nearest(tt.file, tt.stop)
			if got != tt.want || ok != (tt.want != "") {
				t.Errorf("Nearest = %q, %v, want %q", got, ok, tt.want)
			}
		})
	}
}
