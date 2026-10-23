package language

import "testing"

// detectRegistry has the languages the detection tests use.
func detectRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	for _, pkg := range []string{
		`{"contributes": {"languages": [
			{"id": "go", "extensions": [".go"]},
			{"id": "json", "extensions": [".json"]},
			{"id": "jsonc", "filenames": ["tsconfig.json"], "filenamePatterns": ["tsconfig.*.json"]},
			{"id": "typescript", "extensions": [".ts"], "firstLine": "^#!.*\\b(deno|bun)\\b"},
			{"id": "typescript-defs", "extensions": [".d.ts"]},
			{"id": "shellscript", "extensions": [".sh"], "firstLine": "^#!.*\\b(bash|sh)\\b"},
			{"id": "makefile", "filenames": ["Makefile", "makefile"]},
			{"id": "markdown", "extensions": [".md"], "filenamePatterns": ["**/.cursor/**/*.mdc"]},
			{"id": "dockerfile", "filenamePatterns": ["Dockerfile.*"]},
			{"id": "everything", "firstLine": ".*"},
			{"id": "broken", "firstLine": "(["},
			{"id": "emptyline", "firstLine": "^$"}
		]}}`,
		`{"contributes": {"languages": [{"id": "later", "extensions": [".ts"]}, {"id": "xml", "firstLine": "^<\\?xml"}]}}`,
	} {
		if err := r.AddExtension(ext(pkg, nil)); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestDetectByPathAndFirstLine(t *testing.T) {
	r := detectRegistry(t)
	tests := []struct {
		name, path, firstLine, want string
	}{
		{"extension", "/p/main.go", "", "go"},
		{"extension ignores case", "/P/MAIN.GO", "", "go"},
		{"file name before extension", "/p/tsconfig.json", "", "jsonc"},
		{"file name ignores case", "/p/MAKEFILE", "", "makefile"},
		{"glob before extension", "/p/tsconfig.build.json", "", "jsonc"},
		{"extension", "/p/data.json", "", "json"},
		{"the longest extension", "/p/types.d.ts", "", "typescript-defs"},
		{"the language registered last wins", "/p/main.ts", "", "later"},
		{"glob with a slash matches the path", "/home/u/.cursor/rules/x.mdc", "", "markdown"},
		{"glob with a slash needs the path", "/home/u/rules/x.mdc", "", PlainTextID},
		{"glob without a slash matches the name", "/p/Dockerfile.dev", "", "dockerfile"},
		{"plain text for what no language claims", "/p/notes.zzz", "", PlainTextID},
		{"the txt extension is plain text", "/p/a.txt", "", PlainTextID},
		{"first line", "/p/script", "#!/usr/bin/env bash", "shellscript"},
		{"first line for a file with no name", "", "#!/usr/bin/env bun", "typescript"},
		{"first line of the last language that matches", "", "<?xml version", "xml"},
		{"first line with a byte order mark", "/p/script", "\xef\xbb\xbf<?xml version=\"1.0\"?>", "xml"},
		{"first line is read up to its end", "/p/script", "<?xml\nmore", "xml"},
		{"a path that matches wins over the first line", "/p/main.go", "#!/bin/bash", "go"},
		{"no first line", "/p/script", "", PlainTextID},
		{"a first line that matches everything is left out", "/p/script", "hello", PlainTextID},
		{"an empty first line", "/p/script", "", PlainTextID},
		{"nothing", "", "", PlainTextID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.Detect(tc.path, tc.firstLine); got != tc.want {
				t.Errorf("Detect(%q, %q) = %q, want %q", tc.path, tc.firstLine, got, tc.want)
			}
		})
	}
	var found bool
	for _, p := range r.Problems() {
		if appErr, ok := errorsFrom(p); ok && appErr == "FirstLineInvalid" {
			found = true
		}
	}
	if !found {
		t.Errorf("no problem for the first line that does not compile: %v", r.Problems())
	}
}

func TestDetectByFileAssociations(t *testing.T) {
	r := detectRegistry(t)
	errs := r.SetFileAssociations(map[string]any{
		"*.json":          "jsonc",
		"data.json":       "json",
		"**/vendor/**":    "plaintext",
		"Makefile.*":      "makefile",
		"*.weird":         "unknown-language",
		"bad":             3.0,
		"**/special/*.go": "shellscript",
	})
	if len(errs) != 1 {
		t.Fatalf("errors = %v", errs)
	}
	wantTag(t, errs[0], "AssociationInvalid")
	tests := []struct {
		path, want string
	}{
		{"/p/other.json", "jsonc"},
		{"/p/data.json", "json"},
		{"/p/vendor/lib.go", PlainTextID},
		{"/p/Makefile.local", "makefile"},
		{"/p/a.weird", "unknown-language"},
		{"/p/special/x.go", "shellscript"},
		{"/p/x.go", "go"},
		{"/p/tsconfig.json", "jsonc"},
	}
	for _, tc := range tests {
		if got := r.Detect(tc.path, ""); got != tc.want {
			t.Errorf("Detect(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
	// The associations are replaced, not added to.
	if errs := r.SetFileAssociations(nil); len(errs) != 0 {
		t.Fatal(errs)
	}
	if got := r.Detect("/p/other.json", ""); got != "json" {
		t.Errorf("after clearing: %q", got)
	}
}
