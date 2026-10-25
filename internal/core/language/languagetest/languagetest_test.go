package languagetest

import "testing"

func TestBundledHasTheLanguages(t *testing.T) {
	r, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range r.Problems() {
		t.Errorf("problem: %v", p)
	}
	for file, want := range map[string]string{"main.go": "go", "README.md": "markdown", "a.ts": "typescript", "go.mod": "go.mod", "x.txt": "plaintext"} {
		if got := r.Detect(file, ""); got != want {
			t.Errorf("Detect(%q) = %q, want %q", file, got, want)
		}
	}
	if !r.HasGrammar("markdown") {
		t.Error("no grammar for markdown")
	}
}
