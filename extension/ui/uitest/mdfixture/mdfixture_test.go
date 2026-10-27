package mdfixture

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateMakesTheDocumentOfTheBudget(t *testing.T) {
	dir := t.TempDir()
	doc := Generate(t, dir)
	if n := strings.Count(doc.Text, "!["); n != ImageCount {
		t.Errorf("%d images, want %d", n, ImageCount)
	}
	if n := len(doc.Text); n < TargetBytes || n > TargetBytes+TargetBytes/5 {
		t.Errorf("the document has %d bytes, want about %d", n, TargetBytes)
	}
	files, err := filepath.Glob(filepath.Join(dir, ImageDir, "*.png"))
	if err != nil || len(files) != ImageCount {
		t.Errorf("%d image files (%v), want %d", len(files), err, ImageCount)
	}
	if again := Generate(t, t.TempDir()); again.Text != doc.Text {
		t.Error("the document is not deterministic")
	}
}
